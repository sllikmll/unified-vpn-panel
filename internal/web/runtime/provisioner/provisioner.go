package provisioner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
)

type Provisioner interface {
	Plan(kind model.RuntimeKind) Plan
	Install(ctx context.Context, kind model.RuntimeKind) (Result, error)
	Update(ctx context.Context, kind model.RuntimeKind) (Result, error)
	Uninstall(ctx context.Context, kind model.RuntimeKind) (Result, error)
}

type TransactionalProvisioner interface {
	BeginInstall(ctx context.Context, kind model.RuntimeKind) (Transaction, error)
	BeginUpdate(ctx context.Context, kind model.RuntimeKind) (Transaction, error)
}

type Transaction interface {
	Result() Result
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) (Result, error)
}

type Plan struct {
	RuntimeKind         model.RuntimeKind `json:"runtimeKind"`
	Supported           bool              `json:"supported"`
	Blocked             bool              `json:"blocked"`
	Reason              string            `json:"reason,omitempty"`
	ArtifactRef         string            `json:"artifactRef,omitempty"`
	Version             string            `json:"version,omitempty"`
	RequiresPinnedImage bool              `json:"requiresPinnedImage,omitempty"`
	Backend             string            `json:"backend,omitempty"`
	Capabilities        []string          `json:"capabilities,omitempty"`
}

type Result struct {
	RuntimeKind model.RuntimeKind `json:"runtimeKind"`
	ArtifactRef string            `json:"artifactRef,omitempty"`
	Version     string            `json:"version,omitempty"`
	State       string            `json:"state"`
	RolledBack  bool              `json:"rolledBack,omitempty"`
	SummaryCode string            `json:"summaryCode,omitempty"`
}

const (
	AWG2ImageRef         = "ghcr.io/sllikmll/unified-vpn-panel-protocol-awg2@sha256:538dfb87a642932430e6c0e1ab83b53ea53bc61104ff60ba6d0310bb279e24d8"
	NaiveProxyImageRef   = "ghcr.io/sllikmll/unified-vpn-panel-protocol-naive-caddy@sha256:eb3dc466b930f15186dad947b19ac52f4f60eac8db683ea8e8d03f2a6862ed8"
	MieruMitaVersion     = "v3.35.0"
	MieruManifestPath    = "runtime-images/mieru/mita-v3.35.0.manifest.json"
	AWG2ContainerName    = "unified-vpn-awg2-runtime"
	NaiveContainerName   = "unified-vpn-naive-runtime"
	AWG2HostConfigPath   = awg.DockerHostStateDir + "/awg0.conf"
	AWG2GuestConfigPath  = awg.DockerContainerConfigDir + "/awg0.conf"
	NaiveHostConfigPath  = naiveproxy.FixedConfigPath
	NaiveGuestConfig     = naiveproxy.FixedConfigPath
	DefaultMitaPath      = "/usr/local/bin/mita"
	MaxMieruArchiveBytes = 64 << 20
)

var (
	ErrArtifactBlocked = errors.New("runtime artifact precondition blocked")
	ErrUnsafeCommand   = errors.New("runtime provisioner command is not allowlisted")
	ErrDockerNotFound  = errors.New("docker object not found")
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type ContainerInspector interface {
	ContainerExists(ctx context.Context, name string) (bool, error)
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, name string, args ...string) error {
	if err := ValidateDockerCommand(name, args); err != nil {
		return err
	}
	return exec.CommandContext(ctx, name, args...).Run()
}

func (OSRunner) ContainerExists(ctx context.Context, name string) (bool, error) {
	args := []string{"container", "inspect", name}
	if err := ValidateDockerCommand("docker", args); err != nil {
		return false, err
	}
	err := exec.CommandContext(ctx, "docker", args...).Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

type FileSystem interface {
	Stat(path string) (os.FileInfo, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Rename(oldPath, newPath string) error
	Remove(path string) error
	MkdirAll(path string, perm os.FileMode) error
	OpenFile(path string, flag int, perm os.FileMode) (io.WriteCloser, error)
}

type OSFileSystem struct{}

func (OSFileSystem) Stat(path string) (os.FileInfo, error) { return os.Stat(path) }
func (OSFileSystem) ReadFile(path string) ([]byte, error)  { return os.ReadFile(path) }
func (OSFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (OSFileSystem) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (OSFileSystem) Remove(path string) error             { return os.Remove(path) }
func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (OSFileSystem) OpenFile(path string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(path, flag, perm)
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Config struct {
	AWG2ImageRef       string
	NaiveProxyImageRef string
	MieruManifestPath  string
	MitaPath           string
	Runner             Runner
	FS                 FileSystem
	HTTPClient         HTTPDoer
	GOOS               string
	GOARCH             string
	Health             func(context.Context, model.RuntimeKind) error
}

type LocalProvisioner struct {
	cfg Config
}

func NewLocal(cfg Config) *LocalProvisioner {
	if cfg.AWG2ImageRef == "" {
		cfg.AWG2ImageRef = AWG2ImageRef
	}
	if cfg.NaiveProxyImageRef == "" {
		cfg.NaiveProxyImageRef = NaiveProxyImageRef
	}
	if cfg.MieruManifestPath == "" {
		cfg.MieruManifestPath = MieruManifestPath
	}
	if cfg.MitaPath == "" {
		cfg.MitaPath = DefaultMitaPath
	}
	if cfg.Runner == nil {
		cfg.Runner = OSRunner{}
	}
	if cfg.FS == nil {
		cfg.FS = OSFileSystem{}
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	return &LocalProvisioner{cfg: cfg}
}

func (p *LocalProvisioner) Plan(kind model.RuntimeKind) Plan {
	switch kind {
	case model.RuntimeAmneziaWG:
		return dockerPlan(kind, p.cfg.AWG2ImageRef, "docker-awg2")
	case model.RuntimeNaiveProxy:
		return dockerPlan(kind, p.cfg.NaiveProxyImageRef, "docker-naiveproxy")
	case model.RuntimeMieru:
		a, err := p.mieruArtifact()
		if err != nil {
			return Plan{RuntimeKind: kind, Supported: false, Blocked: true, Reason: err.Error(), Version: MieruMitaVersion, Backend: "native-mita"}
		}
		return Plan{RuntimeKind: kind, Supported: true, Blocked: false, ArtifactRef: mieruArtifactRef(a), Version: MieruMitaVersion, Backend: "native-mita", Capabilities: []string{"checksum", "atomic-install", "rollback"}}
	default:
		return Plan{RuntimeKind: kind, Supported: false, Blocked: true, Reason: "unsupported managed runtime"}
	}
}

func dockerPlan(kind model.RuntimeKind, ref, backend string) Plan {
	out := Plan{RuntimeKind: kind, RequiresPinnedImage: true, ArtifactRef: ref, Backend: backend, Capabilities: []string{"docker", "rollback", "host-network"}}
	if !ValidGHCRDigestRef(ref) {
		out.Supported = false
		out.Blocked = true
		out.Reason = "missing immutable GHCR image digest"
		return out
	}
	out.Supported = true
	return out
}

func (p *LocalProvisioner) Install(ctx context.Context, kind model.RuntimeKind) (Result, error) {
	tx, err := p.BeginInstall(ctx, kind)
	if err != nil {
		if tx != nil {
			return tx.Result(), err
		}
		return Result{}, err
	}
	if p.cfg.Health != nil {
		if err := p.cfg.Health(ctx, kind); err != nil {
			res, _ := tx.Rollback(ctx)
			return res, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return tx.Result(), err
	}
	return tx.Result(), nil
}

func (p *LocalProvisioner) Update(ctx context.Context, kind model.RuntimeKind) (Result, error) {
	tx, err := p.BeginUpdate(ctx, kind)
	if err != nil {
		if tx != nil {
			return tx.Result(), err
		}
		return Result{}, err
	}
	if p.cfg.Health != nil {
		if err := p.cfg.Health(ctx, kind); err != nil {
			res, _ := tx.Rollback(ctx)
			return res, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return tx.Result(), err
	}
	return tx.Result(), nil
}

func (p *LocalProvisioner) Uninstall(ctx context.Context, kind model.RuntimeKind) (Result, error) {
	switch kind {
	case model.RuntimeAmneziaWG:
		return p.dockerUninstall(ctx, kind, AWG2ContainerName)
	case model.RuntimeNaiveProxy:
		return p.dockerUninstall(ctx, kind, NaiveContainerName)
	case model.RuntimeMieru:
		if err := p.cfg.FS.Remove(p.cfg.MitaPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Result{}, err
		}
		return Result{RuntimeKind: kind, Version: MieruMitaVersion, State: "removed", SummaryCode: "uninstalled"}, nil
	default:
		return Result{}, fmt.Errorf("%w: unsupported managed runtime", ErrArtifactBlocked)
	}
}

func (p *LocalProvisioner) BeginInstall(ctx context.Context, kind model.RuntimeKind) (Transaction, error) {
	return p.beginInstallOrUpdate(ctx, kind)
}

func (p *LocalProvisioner) BeginUpdate(ctx context.Context, kind model.RuntimeKind) (Transaction, error) {
	return p.beginInstallOrUpdate(ctx, kind)
}

func (p *LocalProvisioner) beginInstallOrUpdate(ctx context.Context, kind model.RuntimeKind) (Transaction, error) {
	plan := p.Plan(kind)
	if plan.Blocked || !plan.Supported {
		tx := staticTx{res: Result{RuntimeKind: kind, ArtifactRef: plan.ArtifactRef, Version: plan.Version, State: "blocked", SummaryCode: "blocked"}}
		return tx, fmt.Errorf("%w: %s", ErrArtifactBlocked, plan.Reason)
	}
	switch kind {
	case model.RuntimeAmneziaWG:
		return p.beginDocker(ctx, kind, plan.ArtifactRef, AWG2ContainerName, awg2DockerArgs(plan.ArtifactRef))
	case model.RuntimeNaiveProxy:
		return p.beginDocker(ctx, kind, plan.ArtifactRef, NaiveContainerName, naiveDockerArgs(plan.ArtifactRef))
	case model.RuntimeMieru:
		return p.beginMieru(ctx, plan)
	default:
		return nil, fmt.Errorf("%w: unsupported managed runtime", ErrArtifactBlocked)
	}
}

type staticTx struct{ res Result }

func (t staticTx) Result() Result                           { return t.res }
func (t staticTx) Commit(context.Context) error             { return nil }
func (t staticTx) Rollback(context.Context) (Result, error) { return t.res, nil }

type dockerTx struct {
	p       *LocalProvisioner
	res     Result
	name    string
	prev    string
	hadPrev bool
}

func (t *dockerTx) Result() Result { return t.res }

func (t *dockerTx) Commit(ctx context.Context) error {
	if t.hadPrev {
		if err := t.p.cfg.Runner.Run(ctx, "docker", "rm", "-f", t.prev); err != nil {
			return err
		}
	}
	t.res.State = "running"
	t.res.SummaryCode = "installed"
	return nil
}

func (t *dockerTx) Rollback(ctx context.Context) (Result, error) {
	err := t.p.rollbackDocker(ctx, t.name, t.prev, t.hadPrev)
	t.res.State = "rolled_back"
	t.res.RolledBack = true
	t.res.SummaryCode = "rollback"
	return t.res, err
}

func (p *LocalProvisioner) beginDocker(ctx context.Context, kind model.RuntimeKind, image, name string, createArgs []string) (Transaction, error) {
	prev := name + "-previous"
	next := name + "-next"
	if err := p.removeIfExists(ctx, next); err != nil {
		return nil, err
	}
	if exists, err := p.containerExists(ctx, prev); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("stale owned docker transaction exists: %s", prev)
	}
	if err := p.cfg.Runner.Run(ctx, "docker", "pull", image); err != nil {
		return nil, err
	}
	if err := p.cfg.Runner.Run(ctx, "docker", "image", "inspect", image); err != nil {
		return nil, err
	}
	if err := p.cfg.Runner.Run(ctx, "docker", append([]string{"create", "--name", next}, createArgs...)...); err != nil {
		return nil, err
	}
	hadPrev, err := p.containerExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if hadPrev {
		if err := p.cfg.Runner.Run(ctx, "docker", "stop", name); err != nil {
			_ = p.removeIfExists(ctx, next)
			return nil, err
		}
		if err := p.cfg.Runner.Run(ctx, "docker", "rename", name, prev); err != nil {
			_ = p.removeIfExists(ctx, next)
			return nil, err
		}
	}
	if err := p.cfg.Runner.Run(ctx, "docker", "rename", next, name); err != nil {
		return nil, err
	}
	tx := &dockerTx{p: p, res: Result{RuntimeKind: kind, ArtifactRef: image, State: "running", SummaryCode: "installed"}, name: name, prev: prev, hadPrev: hadPrev}
	if err := p.cfg.Runner.Run(ctx, "docker", "start", name); err != nil {
		res, _ := tx.Rollback(ctx)
		tx.res = res
		return tx, err
	}
	return tx, nil
}

func (p *LocalProvisioner) rollbackDocker(ctx context.Context, name, prev string, hadPrev bool) error {
	var errs []error
	errs = append(errs, p.cfg.Runner.Run(ctx, "docker", "stop", name))
	errs = append(errs, p.cfg.Runner.Run(ctx, "docker", "rm", "-f", name))
	if hadPrev {
		errs = append(errs, p.cfg.Runner.Run(ctx, "docker", "rename", prev, name))
		errs = append(errs, p.cfg.Runner.Run(ctx, "docker", "start", name))
	}
	return errors.Join(errs...)
}

func (p *LocalProvisioner) dockerUninstall(ctx context.Context, kind model.RuntimeKind, name string) (Result, error) {
	if exists, err := p.containerExists(ctx, name); err != nil {
		return Result{}, err
	} else if !exists {
		return Result{RuntimeKind: kind, State: "removed", SummaryCode: "uninstalled"}, nil
	}
	if err := p.cfg.Runner.Run(ctx, "docker", "stop", name); err != nil {
		return Result{}, err
	}
	if err := p.cfg.Runner.Run(ctx, "docker", "rm", "-f", name); err != nil {
		return Result{}, err
	}
	return Result{RuntimeKind: kind, State: "removed", SummaryCode: "uninstalled"}, nil
}

func (p *LocalProvisioner) containerExists(ctx context.Context, name string) (bool, error) {
	inspector, ok := p.cfg.Runner.(ContainerInspector)
	if !ok {
		return false, fmt.Errorf("docker runner cannot inspect containers")
	}
	return inspector.ContainerExists(ctx, name)
}

func (p *LocalProvisioner) removeIfExists(ctx context.Context, name string) error {
	exists, err := p.containerExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return p.cfg.Runner.Run(ctx, "docker", "rm", "-f", name)
}

func awg2DockerArgs(image string) []string {
	return []string{"--network", "host", "--cap-add", "NET_ADMIN", "--device", "/dev/net/tun", "-v", AWG2HostConfigPath + ":" + AWG2GuestConfigPath + ":ro", image}
}

func naiveDockerArgs(image string) []string {
	return []string{"--network", "host", "-v", NaiveHostConfigPath + ":" + NaiveGuestConfig + ":ro", image}
}

func ValidGHCRDigestRef(ref string) bool {
	if ref == NaiveProxyImageRef {
		return true
	}
	const prefix = "ghcr.io/sllikmll/unified-vpn-panel-protocol-"
	if !strings.HasPrefix(ref, prefix) {
		return false
	}
	at := strings.LastIndex(ref, "@sha256:")
	if at <= len(prefix) {
		return false
	}
	digest := ref[at+len("@sha256:"):]
	if len(digest) != 64 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func ValidateDockerCommand(name string, args []string) error {
	if name != "docker" || len(args) == 0 {
		return ErrUnsafeCommand
	}
	for _, arg := range args {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return ErrUnsafeCommand
		}
	}
	switch args[0] {
	case "image":
		if len(args) == 3 && args[1] == "inspect" && ValidGHCRDigestRef(args[2]) {
			return nil
		}
	case "container":
		if len(args) == 3 && args[1] == "inspect" && ownedContainer(args[2]) {
			return nil
		}
	case "pull":
		if len(args) == 2 && ValidGHCRDigestRef(args[1]) {
			return nil
		}
	case "create":
		if validateCreateArgs(args[1:]) {
			return nil
		}
	case "start", "stop":
		if len(args) == 2 && ownedContainer(args[1]) {
			return nil
		}
	case "rm":
		if len(args) == 3 && args[1] == "-f" && ownedContainer(args[2]) {
			return nil
		}
	case "rename":
		if len(args) == 3 && ownedContainer(args[1]) && ownedContainer(args[2]) {
			return nil
		}
	}
	return ErrUnsafeCommand
}

func validateCreateArgs(args []string) bool {
	if len(args) < 4 || args[0] != "--name" || !ownedContainer(args[1]) || !ValidGHCRDigestRef(args[len(args)-1]) {
		return false
	}
	joined := strings.Join(args[2:len(args)-1], "\x00")
	allowedAWG := strings.Join(awg2DockerArgs(args[len(args)-1])[:len(awg2DockerArgs(args[len(args)-1]))-1], "\x00")
	allowedNaive := strings.Join(naiveDockerArgs(args[len(args)-1])[:len(naiveDockerArgs(args[len(args)-1]))-1], "\x00")
	return joined == allowedAWG || joined == allowedNaive
}

func ownedContainer(name string) bool {
	switch name {
	case AWG2ContainerName, AWG2ContainerName + "-previous", AWG2ContainerName + "-next", NaiveContainerName, NaiveContainerName + "-previous", NaiveContainerName + "-next":
		return true
	default:
		return false
	}
}

type mieruManifest struct {
	Artifacts []mieruArtifact `json:"artifacts"`
}

type mieruArtifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func (p *LocalProvisioner) mieruArtifact() (mieruArtifact, error) {
	if p.cfg.GOOS != "linux" || (p.cfg.GOARCH != "amd64" && p.cfg.GOARCH != "arm64") {
		return mieruArtifact{}, fmt.Errorf("mieru native install supports linux amd64/arm64 only")
	}
	for _, a := range embeddedMieruArtifacts {
		if a.OS == p.cfg.GOOS && a.Arch == p.cfg.GOARCH && a.Kind == "tar.gz" {
			if !validMieruArtifact(a) {
				return mieruArtifact{}, fmt.Errorf("invalid mieru artifact manifest entry")
			}
			return a, nil
		}
	}
	return mieruArtifact{}, fmt.Errorf("mieru artifact missing for platform")
}

func validMieruArtifact(a mieruArtifact) bool {
	return strings.HasPrefix(a.URL, "https://github.com/enfein/mieru/releases/download/v3.35.0/") && len(a.SHA256) == 64
}

func mieruArtifactRef(a mieruArtifact) string {
	return "mieru:mita:" + MieruMitaVersion + ":" + a.OS + "/" + a.Arch + ":sha256:" + strings.ToLower(a.SHA256)
}

var embeddedMieruArtifacts = []mieruArtifact{
	{OS: "linux", Arch: "amd64", Kind: "tar.gz", URL: "https://github.com/enfein/mieru/releases/download/v3.35.0/mita_3.35.0_linux_amd64.tar.gz", SHA256: "a07d5afc5e1353ab346bb3ddbe95c7f960828204be529f4a88d688dfe83e252d"},
	{OS: "linux", Arch: "arm64", Kind: "tar.gz", URL: "https://github.com/enfein/mieru/releases/download/v3.35.0/mita_3.35.0_linux_arm64.tar.gz", SHA256: "808849223d34ccd9ad86afc0eedef4d6c827133258e96dc3f3794bd17e7d54de"},
}

type mieruTx struct {
	p       *LocalProvisioner
	res     Result
	target  string
	backup  string
	hadPrev bool
}

func (t *mieruTx) Result() Result { return t.res }

func (t *mieruTx) Commit(context.Context) error {
	if t.hadPrev {
		if err := t.p.cfg.FS.Remove(t.backup); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	t.res.State = "installed"
	t.res.SummaryCode = "installed"
	return nil
}

func (t *mieruTx) Rollback(context.Context) (Result, error) {
	err := rollbackMita(t.p.cfg.FS, t.target, t.backup, t.hadPrev)
	t.res.State = "rolled_back"
	t.res.RolledBack = true
	t.res.SummaryCode = "rollback"
	return t.res, err
}

func (p *LocalProvisioner) beginMieru(ctx context.Context, plan Plan) (Transaction, error) {
	a, err := p.mieruArtifact()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxMieruArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > MaxMieruArchiveBytes {
		return nil, fmt.Errorf("download exceeds size limit")
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), a.SHA256) {
		return nil, fmt.Errorf("checksum mismatch")
	}
	bin, err := extractSingleMita(raw)
	if err != nil {
		return nil, err
	}
	target := p.cfg.MitaPath
	dir := filepath.Dir(target)
	if err := p.cfg.FS.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	backup := target + ".previous"
	hadPrev := false
	if _, err := p.cfg.FS.Stat(target); err == nil {
		hadPrev = true
		if _, err := p.cfg.FS.Stat(backup); err == nil {
			return nil, fmt.Errorf("stale mieru install transaction exists: %s", backup)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := p.cfg.FS.Rename(target, backup); err != nil {
			return nil, err
		}
	}
	tmp := target + ".tmp"
	w, err := p.cfg.FS.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		_ = rollbackMita(p.cfg.FS, target, backup, hadPrev)
		return nil, err
	}
	if _, err := w.Write(bin); err != nil {
		_ = w.Close()
		_ = rollbackMita(p.cfg.FS, target, backup, hadPrev)
		return nil, err
	}
	if err := w.Close(); err != nil {
		_ = rollbackMita(p.cfg.FS, target, backup, hadPrev)
		return nil, err
	}
	if err := p.cfg.FS.Rename(tmp, target); err != nil {
		_ = rollbackMita(p.cfg.FS, target, backup, hadPrev)
		return nil, err
	}
	return &mieruTx{p: p, res: Result{RuntimeKind: model.RuntimeMieru, ArtifactRef: plan.ArtifactRef, Version: MieruMitaVersion, State: "installed", SummaryCode: "installed"}, target: target, backup: backup, hadPrev: hadPrev}, nil
}

func rollbackMita(fs FileSystem, target, backup string, hadPrev bool) error {
	var errs []error
	if err := fs.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if hadPrev {
		errs = append(errs, fs.Rename(backup, target))
	}
	return errors.Join(errs...)
}

func extractSingleMita(raw []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var found []byte
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := filepath.Clean(h.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) || strings.Contains(name, string(filepath.Separator)+".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe archive path")
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(name) != "mita" {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("archive contains multiple mita binaries")
		}
		found, err = io.ReadAll(io.LimitReader(tr, MaxMieruArchiveBytes+1))
		if err != nil {
			return nil, err
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("archive missing mita binary")
	}
	return found, nil
}

func Kinds() []model.RuntimeKind {
	out := []model.RuntimeKind{model.RuntimeAmneziaWG, model.RuntimeMieru, model.RuntimeNaiveProxy}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
