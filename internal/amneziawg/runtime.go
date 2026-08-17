package amneziawg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrBackendUnavailable        = errors.New("amneziawg backend unavailable")
	ErrDockerMountMismatch       = errors.New("amneziawg docker mount mismatch")
	ErrUnsupportedInterface      = errors.New("amneziawg interface unsupported by backend profile")
	ErrPeerOperationsUnsupported = errors.New("amneziawg peer mutations require a complete desired endpoint config")
)

type Backend interface {
	Detect(ctx context.Context) (SafeStatus, error)
	Up(ctx context.Context, iface string) error
	Apply(ctx context.Context, iface, config string) error
	Verify(ctx context.Context, iface string) error
	Down(ctx context.Context, iface string) error
	Delete(ctx context.Context, iface string) error
	Status(ctx context.Context, iface string) (SafeStatus, error)
	Rollback(ctx context.Context, iface string, backup string) error
}

type CommandBackend struct {
	LookPath      func(string) (string, error)
	Run           func(context.Context, string, ...string) error
	Output        func(context.Context, string, ...string) ([]byte, error)
	DockerProfile BackendProfile
	NativeProfile BackendProfile
}

func NewCommandBackend() *CommandBackend {
	return &CommandBackend{
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, name string, args ...string) error {
			return exec.CommandContext(ctx, name, args...).Run()
		},
		Output: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		DockerProfile: DockerBackendProfile(),
		NativeProfile: NativeBackendProfile(),
	}
}

func (b *CommandBackend) Detect(ctx context.Context) (SafeStatus, error) {
	profile := b.dockerProfile()
	if b.run(ctx, "docker", "container", "inspect", profile.ContainerName) == nil {
		if err := b.verifyDockerMount(ctx); err != nil {
			return SafeStatus{Backend: BackendDocker, State: StateStopped}, err
		}
		state := StateStopped
		running, err := b.output(ctx, "docker", "inspect", "--format", "{{.State.Running}}", profile.ContainerName)
		if err != nil {
			return SafeStatus{Backend: BackendDocker, State: StateStopped}, err
		}
		if strings.TrimSpace(string(running)) == "true" {
			state = StateRunning
		}
		return SafeStatus{Backend: BackendDocker, Available: true, State: state}, nil
	}
	return SafeStatus{Backend: BackendNone, State: StateStopped}, nil
}

func (b *CommandBackend) Up(ctx context.Context, iface string) error {
	st, err := b.Detect(ctx)
	if err != nil {
		return err
	}
	switch st.Backend {
	case BackendDocker:
		container := b.dockerProfile().ContainerName
		if err := b.run(ctx, "docker", "start", container); err != nil {
			return err
		}
		var lastErr error
		for attempt := 0; attempt < 50; attempt++ {
			if err := b.run(ctx, "docker", "exec", container, "awg2-reconcile", "verify"); err == nil {
				return nil
			} else {
				lastErr = err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		return fmt.Errorf("amneziawg runtime did not become ready: %w", lastErr)
	default:
		return ErrBackendUnavailable
	}
}

func (b *CommandBackend) Apply(ctx context.Context, iface, config string) error {
	st, err := b.Detect(ctx)
	if err != nil {
		return err
	}
	switch st.Backend {
	case BackendDocker:
		if iface != "awg0" {
			return fmt.Errorf("%w: docker profile reads awg0.conf", ErrUnsupportedInterface)
		}
		if config == "" {
			return fmt.Errorf("empty amneziawg config")
		}
		return b.run(ctx, "docker", "exec", b.dockerProfile().ContainerName, "awg2-reconcile", "apply")
	default:
		return ErrBackendUnavailable
	}
}

func (b *CommandBackend) Verify(ctx context.Context, iface string) error {
	st, err := b.Detect(ctx)
	if err != nil {
		return err
	}
	switch st.Backend {
	case BackendDocker:
		return b.run(ctx, "docker", "exec", b.dockerProfile().ContainerName, "awg2-reconcile", "verify")
	default:
		return ErrBackendUnavailable
	}
}

func (b *CommandBackend) Down(ctx context.Context, iface string) error {
	st, err := b.Detect(ctx)
	if err != nil {
		return err
	}
	switch st.Backend {
	case BackendDocker:
		return b.run(ctx, "docker", "stop", b.dockerProfile().ContainerName)
	default:
		return ErrBackendUnavailable
	}
}

func (b *CommandBackend) Delete(ctx context.Context, iface string) error {
	return b.Down(ctx, iface)
}

func (b *CommandBackend) Status(ctx context.Context, iface string) (SafeStatus, error) {
	st, err := b.Detect(ctx)
	if err != nil || st.Backend == BackendNone {
		return st, err
	}
	if err := b.Verify(ctx, iface); err == nil {
		st.State = StateRunning
		raw, dumpErr := b.output(ctx, "docker", "exec", b.dockerProfile().ContainerName, "awg", "show", iface, "dump")
		if dumpErr != nil {
			return st, dumpErr
		}
		st.Peers = parsePeerDump(raw)
	} else {
		st.State = StateStopped
	}
	return st, nil
}

func (b *CommandBackend) Rollback(ctx context.Context, iface string, _ string) error {
	st, err := b.Detect(ctx)
	if err != nil {
		return err
	}
	if st.Backend == BackendDocker && st.State != StateRunning {
		return b.Up(ctx, iface)
	}
	return b.Apply(ctx, iface, "rollback")
}

func parsePeerDump(raw []byte) []PeerStatus {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 2 {
		return nil
	}
	out := make([]PeerStatus, 0, len(lines)-1)
	for _, line := range lines[1:] {
		fields := strings.Split(line, "	")
		if len(fields) < 8 {
			continue
		}
		handshake, err1 := strconv.ParseInt(fields[4], 10, 64)
		rx, err2 := strconv.ParseInt(fields[5], 10, 64)
		tx, err3 := strconv.ParseInt(fields[6], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, PeerStatus{PublicKey: fields[0], Enabled: true, LastHandshakeUnix: handshake, RxBytes: rx, TxBytes: tx})
	}
	return out
}

func (b *CommandBackend) run(ctx context.Context, name string, args ...string) error {
	if b.Run == nil {
		return fmt.Errorf("runner is nil")
	}
	return b.Run(ctx, name, args...)
}

func (b *CommandBackend) output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if b.Output == nil {
		return nil, nil
	}
	return b.Output(ctx, name, args...)
}

func (b *CommandBackend) dockerProfile() BackendProfile {
	if b.DockerProfile.Kind == BackendDocker {
		return b.DockerProfile
	}
	return DockerBackendProfile()
}

func (b *CommandBackend) nativeProfile() BackendProfile {
	if b.NativeProfile.Kind == BackendNative {
		return b.NativeProfile
	}
	return NativeBackendProfile()
}

func (b *CommandBackend) verifyDockerMount(ctx context.Context) error {
	profile := b.dockerProfile()
	raw, err := b.output(ctx, "docker", "container", "inspect", profile.ContainerName, "--format", "{{json .Mounts}}")
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	var mounts []struct {
		Source      string
		Destination string
	}
	if err := json.Unmarshal(raw, &mounts); err != nil {
		return fmt.Errorf("%w: inspect mounts", ErrDockerMountMismatch)
	}
	for _, mount := range mounts {
		if filepath.Clean(mount.Destination) != filepath.Clean(profile.ContainerConfigDir) {
			continue
		}
		if filepath.Clean(mount.Source) != filepath.Clean(profile.HostConfigDir) {
			return fmt.Errorf("%w: %s -> %s", ErrDockerMountMismatch, mount.Source, mount.Destination)
		}
		return nil
	}
	return fmt.Errorf("%w: missing %s", ErrDockerMountMismatch, profile.ContainerConfigDir)
}

type FileStore struct {
	Dir string
}

func DefaultFileStore() FileStore {
	return FileStore{Dir: NativeConfigDir}
}

func (s FileStore) Load(iface string) (string, error) {
	raw, err := os.ReadFile(s.path(iface))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(raw), err
}

func (s FileStore) SaveAtomic(iface, config string) (string, error) {
	if err := validateIface(iface); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	backup, err := s.Load(iface)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(s.Dir, iface+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(config); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return backup, os.Rename(tmpName, s.path(iface))
}

func (s FileStore) Delete(iface string) (string, error) {
	backup, err := s.Load(iface)
	if err != nil {
		return "", err
	}
	if err := os.Remove(s.path(iface)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return backup, nil
}

func (s FileStore) path(iface string) string {
	return filepath.Join(s.Dir, iface+".conf")
}

func validateIface(iface string) error {
	if iface == "" || len(iface) > 32 {
		return fmt.Errorf("invalid interface")
	}
	for _, r := range iface {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return fmt.Errorf("invalid interface")
	}
	return nil
}

type Store interface {
	Load(iface string) (string, error)
	SaveAtomic(iface, config string) (backup string, err error)
	Delete(iface string) (backup string, err error)
}

type Runtime struct {
	backend Backend
	store   Store
	mu      sync.Mutex
	current DesiredConfig
}

func NewRuntime(backend Backend, store Store) *Runtime {
	return &Runtime{backend: backend, store: store}
}

func (r *Runtime) Detect(ctx context.Context) (SafeStatus, error) {
	return r.backend.Detect(ctx)
}

func (r *Runtime) Apply(ctx context.Context, desired DesiredConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ValidateServer(desired.Server); err != nil {
		return err
	}
	st, err := r.backend.Detect(ctx)
	if err != nil {
		return err
	}
	if st.Backend == BackendNone {
		return ErrBackendUnavailable
	}
	store := r.storeFor(st.Backend)
	cfg, err := RenderServerConfig(desired.Server, desired.Clients)
	if err != nil {
		return err
	}
	backup, err := store.SaveAtomic(desired.Server.InterfaceName, cfg)
	if err != nil {
		return err
	}
	restoreStopped := st.State != StateRunning
	if st.State != StateRunning {
		if err := r.backend.Up(ctx, desired.Server.InterfaceName); err != nil {
			return errors.Join(err, r.rollback(ctx, store, desired.Server.InterfaceName, backup, restoreStopped))
		}
	}
	if err := r.backend.Apply(ctx, desired.Server.InterfaceName, cfg); err != nil {
		return errors.Join(err, r.rollback(ctx, store, desired.Server.InterfaceName, backup, restoreStopped))
	}
	if err := r.backend.Verify(ctx, desired.Server.InterfaceName); err != nil {
		return errors.Join(err, r.rollback(ctx, store, desired.Server.InterfaceName, backup, restoreStopped))
	}
	r.current = desired
	return nil
}

func (r *Runtime) Start(ctx context.Context, iface string) error {
	return r.backend.Up(ctx, iface)
}

func (r *Runtime) Stop(ctx context.Context, iface string) error {
	return r.backend.Down(ctx, iface)
}

func (r *Runtime) Observe(ctx context.Context, iface string) (SafeStatus, error) {
	st, err := r.backend.Status(ctx, iface)
	if err != nil {
		return st, err
	}
	ids := map[string]string{}
	for _, client := range r.current.Clients {
		ids[client.PublicKey] = client.ID
	}
	if len(ids) == 0 {
		store, detectErr := r.backend.Detect(ctx)
		if detectErr == nil {
			raw, _ := r.storeFor(store.Backend).Load(iface)
			ids = peerIDsFromConfig(raw)
		}
	}
	for i := range st.Peers {
		st.Peers[i].ClientID = ids[st.Peers[i].PublicKey]
	}
	return st, nil
}

func peerIDsFromConfig(raw string) map[string]string {
	out := map[string]string{}
	clientID := ""
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			clientID = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "PublicKey") && clientID != "" {
			out[strings.TrimSpace(value)] = clientID
			clientID = ""
		}
	}
	return out
}

func (r *Runtime) Delete(ctx context.Context, iface string) error {
	st, err := r.backend.Detect(ctx)
	if err != nil {
		return err
	}
	store := r.storeFor(st.Backend)
	backup, err := store.Delete(iface)
	if err != nil {
		return err
	}
	if err := r.backend.Delete(ctx, iface); err != nil {
		return errors.Join(err, r.rollback(ctx, store, iface, backup, false))
	}
	return nil
}

func (r *Runtime) UpsertPeer(ctx context.Context, client Client) error {
	return ErrPeerOperationsUnsupported
}

func (r *Runtime) DeletePeer(ctx context.Context, clientID string) error {
	return ErrPeerOperationsUnsupported
}

func (r *Runtime) SetPeerEnabled(ctx context.Context, clientID string, enabled bool) error {
	return ErrPeerOperationsUnsupported
}

func (r *Runtime) PeerStatus(_ context.Context, clientID string) (PeerStatus, error) {
	return PeerStatus{ClientID: clientID}, ErrPeerOperationsUnsupported
}

func (r *Runtime) ExportPeer(_ context.Context, clientID string) (string, error) {
	return "", ErrPeerOperationsUnsupported
}

func (r *Runtime) rollback(ctx context.Context, store Store, iface, backup string, restoreStopped bool) error {
	if strings.TrimSpace(backup) == "" {
		_, deleteErr := store.Delete(iface)
		stopErr := r.backend.Down(ctx, iface)
		return errors.Join(deleteErr, stopErr)
	}
	if _, err := store.SaveAtomic(iface, backup); err != nil {
		if restoreStopped {
			return errors.Join(err, r.backend.Down(ctx, iface))
		}
		return err
	}
	restoreErr := r.backend.Rollback(ctx, iface, backup)
	if restoreStopped {
		return errors.Join(restoreErr, r.backend.Down(ctx, iface))
	}
	return restoreErr
}

func (r *Runtime) storeFor(kind BackendKind) Store {
	if r.store != nil {
		return r.store
	}
	switch kind {
	case BackendDocker:
		if b, ok := r.backend.(*CommandBackend); ok {
			return FileStore{Dir: b.dockerProfile().HostConfigDir}
		}
		return FileStore{Dir: DockerBackendProfile().HostConfigDir}
	case BackendNative:
		if b, ok := r.backend.(*CommandBackend); ok {
			return FileStore{Dir: b.nativeProfile().HostConfigDir}
		}
		return FileStore{Dir: NativeBackendProfile().HostConfigDir}
	default:
		return FileStore{Dir: NativeConfigDir}
	}
}

type MemoryStore map[string]string

func (s MemoryStore) Load(iface string) (string, error) {
	return s[iface], nil
}

func (s MemoryStore) SaveAtomic(iface, config string) (string, error) {
	backup := s[iface]
	s[iface] = config
	return backup, nil
}

func (s MemoryStore) Delete(iface string) (string, error) {
	backup := s[iface]
	delete(s, iface)
	return backup, nil
}

type FakeBackend struct {
	DockerAvailable bool
	NativeAvailable bool
	VerifyErr       error
	RolledBack      bool
	Stopped         bool
	LastConfig      string
	Peers           []PeerStatus
}

func (b *FakeBackend) Detect(context.Context) (SafeStatus, error) {
	switch {
	case b.DockerAvailable:
		state := StateRunning
		if b.Stopped {
			state = StateStopped
		}
		return SafeStatus{Backend: BackendDocker, Available: true, State: state}, nil
	case b.NativeAvailable:
		return SafeStatus{Backend: BackendNative, Available: true, State: StateRunning}, nil
	default:
		return SafeStatus{Backend: BackendNone, State: StateStopped}, nil
	}
}

func (b *FakeBackend) Up(context.Context, string) error {
	b.Stopped = false
	return nil
}

func (b *FakeBackend) Apply(_ context.Context, _ string, config string) error {
	b.LastConfig = config
	return nil
}
func (b *FakeBackend) Verify(context.Context, string) error { return b.VerifyErr }
func (b *FakeBackend) Down(context.Context, string) error {
	b.Stopped = true
	return nil
}
func (b *FakeBackend) Delete(context.Context, string) error { return nil }
func (b *FakeBackend) Status(context.Context, string) (SafeStatus, error) {
	status, err := b.Detect(context.Background())
	status.Peers = append([]PeerStatus(nil), b.Peers...)
	return status, err
}

func (b *FakeBackend) Rollback(context.Context, string, string) error {
	b.RolledBack = true
	return nil
}
