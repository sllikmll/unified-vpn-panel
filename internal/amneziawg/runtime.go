package amneziawg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var ErrBackendUnavailable = errors.New("amneziawg backend unavailable")
var ErrDockerMountMismatch = errors.New("amneziawg docker mount mismatch")
var ErrUnsupportedInterface = errors.New("amneziawg interface unsupported by backend profile")
var ErrPeerOperationsUnsupported = errors.New("amneziawg peer mutations require a complete desired endpoint config")

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
	if err := b.run(ctx, "docker", "container", "inspect", b.dockerProfile().ContainerName); err == nil {
		if err := b.verifyDockerMount(ctx); err != nil {
			return SafeStatus{Backend: BackendDocker, Available: false, State: StateStopped}, err
		}
		return SafeStatus{Backend: BackendDocker, Available: true, State: StateRunning}, nil
	}
	if b.has("awg") && b.has("awg-quick") {
		return SafeStatus{Backend: BackendNative, Available: true, State: StateStopped}, nil
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
		return b.run(ctx, "docker", "start", b.dockerProfile().ContainerName)
	case BackendNative:
		return b.run(ctx, "awg-quick", "up", iface)
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
		return b.run(ctx, "docker", "restart", b.dockerProfile().ContainerName)
	case BackendNative:
		_ = b.run(ctx, "awg-quick", "down", iface)
		return b.run(ctx, "awg-quick", "up", iface)
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
		return b.run(ctx, "docker", "exec", b.dockerProfile().ContainerName, "awg", "show", iface)
	case BackendNative:
		return b.run(ctx, "awg", "show", iface)
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
	case BackendNative:
		return b.run(ctx, "awg-quick", "down", iface)
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
	} else {
		st.State = StateStopped
	}
	return st, nil
}

func (b *CommandBackend) Rollback(ctx context.Context, iface string, _ string) error {
	return b.Apply(ctx, iface, "")
}

func (b *CommandBackend) has(name string) bool {
	_, err := b.LookPath(name)
	return err == nil
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
	if err := r.backend.Apply(ctx, desired.Server.InterfaceName, cfg); err != nil {
		_ = r.rollback(ctx, store, desired.Server.InterfaceName, backup)
		return err
	}
	if err := r.backend.Verify(ctx, desired.Server.InterfaceName); err != nil {
		_ = r.rollback(ctx, store, desired.Server.InterfaceName, backup)
		return err
	}
	r.current = DesiredConfig{Server: desired.Server}
	return nil
}

func (r *Runtime) Start(ctx context.Context, iface string) error {
	return r.backend.Up(ctx, iface)
}

func (r *Runtime) Observe(ctx context.Context, iface string) (SafeStatus, error) {
	return r.backend.Status(ctx, iface)
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
		_ = r.rollback(ctx, store, iface, backup)
		return err
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

func (r *Runtime) rollback(ctx context.Context, store Store, iface, backup string) error {
	if _, err := store.SaveAtomic(iface, backup); err != nil {
		return err
	}
	return r.backend.Rollback(ctx, iface, backup)
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
	LastConfig      string
}

func (b *FakeBackend) Detect(context.Context) (SafeStatus, error) {
	switch {
	case b.DockerAvailable:
		return SafeStatus{Backend: BackendDocker, Available: true, State: StateRunning}, nil
	case b.NativeAvailable:
		return SafeStatus{Backend: BackendNative, Available: true, State: StateRunning}, nil
	default:
		return SafeStatus{Backend: BackendNone, State: StateStopped}, nil
	}
}

func (b *FakeBackend) Up(context.Context, string) error { return nil }
func (b *FakeBackend) Apply(_ context.Context, _ string, config string) error {
	b.LastConfig = config
	return nil
}
func (b *FakeBackend) Verify(context.Context, string) error { return b.VerifyErr }
func (b *FakeBackend) Down(context.Context, string) error   { return nil }
func (b *FakeBackend) Delete(context.Context, string) error { return nil }
func (b *FakeBackend) Status(context.Context, string) (SafeStatus, error) {
	return b.Detect(context.Background())
}
func (b *FakeBackend) Rollback(context.Context, string, string) error {
	b.RolledBack = true
	return nil
}
