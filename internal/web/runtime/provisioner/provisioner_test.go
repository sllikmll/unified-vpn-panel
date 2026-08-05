package provisioner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
)

const validDigest = "ghcr.io/sllikmll/unified-vpn-panel-protocol-awg2@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDefaultDockerPlansUseImmutableRuntimeDigests(t *testing.T) {
	p := NewLocal(Config{})
	cases := []struct {
		kind model.RuntimeKind
		ref  string
	}{
		{model.RuntimeAmneziaWG, "ghcr.io/sllikmll/unified-vpn-panel-protocol-awg2@sha256:538dfb80a24f4f18e84aadbadd98472ace726452e96b36441d422fba7c5e24d8"},
		{model.RuntimeNaiveProxy, "ghcr.io/sllikmll/unified-vpn-panel-protocol-naive-caddy@sha256:1bedc66132c2e22782c9d8c58d28e5232d7757a1adfcce69fd475842796e36ff"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			plan := p.Plan(tc.kind)
			if !plan.Supported || plan.Blocked {
				t.Fatalf("plan = %+v, want supported unblocked", plan)
			}
			if plan.ArtifactRef != tc.ref {
				t.Fatalf("artifact ref = %q, want %q", plan.ArtifactRef, tc.ref)
			}
			if strings.Contains(plan.ArtifactRef, ":latest") {
				t.Fatalf("plan uses mutable latest ref: %+v", plan)
			}
		})
	}
}

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	calls       []call
	fail        map[string]bool
	inspectFail map[string]error
	exists      map[string]bool
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	if err := ValidateDockerCommand(name, args); err != nil {
		return err
	}
	copied := append([]string(nil), args...)
	r.calls = append(r.calls, call{name: name, args: copied})
	if r.fail[strings.Join(append([]string{name}, args...), " ")] {
		return fmt.Errorf("failed")
	}
	if name == "docker" {
		switch args[0] {
		case "create":
			r.ensureExists()
			r.exists[args[2]] = true
		case "rename":
			r.ensureExists()
			r.exists[args[2]] = r.exists[args[1]]
			delete(r.exists, args[1])
		case "rm":
			r.ensureExists()
			delete(r.exists, args[2])
		}
	}
	return nil
}

func (r *fakeRunner) ContainerExists(_ context.Context, name string) (bool, error) {
	if err := r.inspectFail[name]; err != nil {
		return false, err
	}
	r.ensureExists()
	return r.exists[name], nil
}

func (r *fakeRunner) ensureExists() {
	if r.exists == nil {
		r.exists = map[string]bool{AWG2ContainerName: true, NaiveContainerName: true}
	}
}

func TestDockerImageRefMissingBlocksInstall(t *testing.T) {
	p := NewLocal(Config{AWG2ImageRef: "invalid"})
	plan := p.Plan(model.RuntimeAmneziaWG)
	if !plan.Blocked || plan.Supported {
		t.Fatalf("plan = %+v, want blocked unsupported", plan)
	}
	if _, err := p.Install(context.Background(), model.RuntimeAmneziaWG); err == nil {
		t.Fatal("Install succeeded without immutable digest")
	}
}

func TestDockerRejectsMutableAndShellCommands(t *testing.T) {
	rejected := [][]string{
		{"pull", "ghcr.io/sllikmll/unified-vpn-panel-protocol-awg2:latest"},
		{"run", "--rm", validDigest},
		{"system", "prune", "-a"},
		{"volume", "rm", "x-ui"},
	}
	for _, args := range rejected {
		if err := ValidateDockerCommand("docker", args); err == nil {
			t.Fatalf("accepted unsafe docker args %#v", args)
		}
	}
	if err := ValidateDockerCommand("sh", []string{"-c", "docker pull " + validDigest}); err == nil {
		t.Fatal("accepted shell command")
	}
}

func TestDockerInstallRollbackAfterStartFailure(t *testing.T) {
	r := &fakeRunner{fail: map[string]bool{"docker start unified-vpn-awg2-runtime": true}}
	p := NewLocal(Config{AWG2ImageRef: validDigest, Runner: r, FS: &memFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}})
	res, err := p.Install(context.Background(), model.RuntimeAmneziaWG)
	if err == nil {
		t.Fatal("Install succeeded despite failed start")
	}
	if !res.RolledBack {
		t.Fatalf("result = %+v, want rollback", res)
	}
	joined := joinCalls(r.calls)
	for _, forbidden := range []string{"docker volume", "docker image rm", "docker system", " sh "} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("executed destructive/unsafe command: %s", joined)
		}
	}
	for _, want := range []string{
		"docker stop unified-vpn-awg2-runtime",
		"docker rename unified-vpn-awg2-runtime unified-vpn-awg2-runtime-previous",
		"docker rm -f unified-vpn-awg2-runtime",
		"docker rename unified-vpn-awg2-runtime-previous unified-vpn-awg2-runtime",
		"docker start unified-vpn-awg2-runtime",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing rollback command %q in %s", want, joined)
		}
	}
}

func TestDockerInstallFirstInstallRemovesFailedCurrentWithoutInventingPrevious(t *testing.T) {
	r := &fakeRunner{exists: map[string]bool{}}
	p := NewLocal(Config{AWG2ImageRef: validDigest, Runner: r, FS: &memFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}, Health: func(context.Context, model.RuntimeKind) error {
		return fmt.Errorf("apply failed")
	}})
	res, err := p.Install(context.Background(), model.RuntimeAmneziaWG)
	if err == nil || !res.RolledBack {
		t.Fatalf("Install result=%+v err=%v, want rollback", res, err)
	}
	joined := joinCalls(r.calls)
	if strings.Contains(joined, "rename unified-vpn-awg2-runtime-previous unified-vpn-awg2-runtime") {
		t.Fatalf("first install rollback restored invented previous container: %s", joined)
	}
	if strings.Contains(joined, "rm -f unified-vpn-awg2-runtime-previous") {
		t.Fatalf("first install touched previous container: %s", joined)
	}
}

func TestDockerInspectErrorsAreNotTreatedAsAbsence(t *testing.T) {
	r := &fakeRunner{exists: map[string]bool{}, inspectFail: map[string]error{AWG2ContainerName: fmt.Errorf("permission denied")}}
	p := NewLocal(Config{AWG2ImageRef: validDigest, Runner: r, FS: &memFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}})
	if _, err := p.Install(context.Background(), model.RuntimeAmneziaWG); err == nil {
		t.Fatal("Install succeeded despite docker inspect failure")
	}
	if strings.Contains(joinCalls(r.calls), "rename unified-vpn-awg2-runtime") {
		t.Fatalf("continued after inspect failure: %s", joinCalls(r.calls))
	}
}

func TestDockerStalePreviousFailsClosed(t *testing.T) {
	r := &fakeRunner{exists: map[string]bool{AWG2ContainerName + "-previous": true}}
	p := NewLocal(Config{AWG2ImageRef: validDigest, Runner: r, FS: &memFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}})
	if _, err := p.Install(context.Background(), model.RuntimeAmneziaWG); err == nil {
		t.Fatal("Install succeeded with stale previous container")
	}
	if strings.Contains(joinCalls(r.calls), "rm -f "+AWG2ContainerName+"-previous") {
		t.Fatalf("deleted previous before owning transaction: %s", joinCalls(r.calls))
	}
}

func TestDockerUninstallIdempotentOwnedContainerOnly(t *testing.T) {
	r := &fakeRunner{exists: map[string]bool{NaiveContainerName: true}}
	p := NewLocal(Config{NaiveProxyImageRef: strings.Replace(validDigest, "awg2", "naiveproxy", 1), Runner: r})
	if _, err := p.Uninstall(context.Background(), model.RuntimeNaiveProxy); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	joined := joinCalls(r.calls)
	if joined != "docker stop unified-vpn-naive-runtime\ndocker rm -f unified-vpn-naive-runtime" {
		t.Fatalf("calls = %q", joined)
	}
}

func TestDockerUninstallMissingContainerIsNoop(t *testing.T) {
	r := &fakeRunner{exists: map[string]bool{}}
	p := NewLocal(Config{NaiveProxyImageRef: strings.Replace(validDigest, "awg2", "naiveproxy", 1), Runner: r})
	if _, err := p.Uninstall(context.Background(), model.RuntimeNaiveProxy); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := joinCalls(r.calls); got != "" {
		t.Fatalf("missing uninstall ran docker commands: %q", got)
	}
}

func TestPrepareAtomicConfigMountRemovesOnlyEmptyMalformedDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "runtime")
	configPath := filepath.Join(dir, "awg0.conf")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareAtomicConfigMount(OSFileSystem{}, dir, configPath, 0o700); err != nil {
		t.Fatalf("prepareAtomicConfigMount empty malformed dir: %v", err)
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed config directory still exists: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("runtime directory not prepared: info=%v err=%v", info, err)
	}
	if err := os.Mkdir(configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "unexpected"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareAtomicConfigMount(OSFileSystem{}, dir, configPath, 0o700); err == nil {
		t.Fatal("non-empty malformed config directory was removed")
	}
	if _, err := os.Stat(filepath.Join(configPath, "unexpected")); err != nil {
		t.Fatalf("non-empty malformed directory contents were not preserved: %v", err)
	}
}

func TestNaiveDockerMountUsesOwnedDirectoryForAtomicConfigReplacement(t *testing.T) {
	args := naiveDockerArgs(strings.Replace(validDigest, "awg2", "naiveproxy", 1))
	mount := findMountArg(t, args)
	if want := naiveproxy.FixedConfigDir + ":" + naiveproxy.FixedConfigDir + ":ro"; mount != want {
		t.Fatalf("naive mount = %q, want %q", mount, want)
	}
	if strings.Contains(mount, filepath.Base(naiveproxy.FixedConfigPath)) {
		t.Fatalf("naive bind source must be a directory, got %q", mount)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{naiveproxy.DockerDataVolume + ":/data", naiveproxy.DockerConfigVolume + ":/config"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("naive create args missing persistent volume %q: %q", want, joined)
		}
	}
}

func TestAWG2DockerMountUsesOwnedDirectoryForAtomicConfigReplacement(t *testing.T) {
	profile := awg.DockerBackendProfile()
	args := awg2DockerArgs(validDigest)
	mount := findMountArg(t, args)
	if want := profile.HostConfigDir + ":" + profile.ContainerConfigDir + ":ro"; mount != want {
		t.Fatalf("awg2 mount = %q, want %q", mount, want)
	}
	if strings.Contains(mount, "awg0.conf") {
		t.Fatalf("awg2 bind source must be a directory, got %q", mount)
	}
}

func findMountArg(t *testing.T, args []string) string {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-v" {
			return args[i+1]
		}
	}
	t.Fatalf("missing docker mount in args %#v", args)
	return ""
}

type memFS struct {
	files map[string][]byte
	modes map[string]os.FileMode
}

func (fs *memFS) Stat(path string) (os.FileInfo, error) {
	if _, ok := fs.files[path]; !ok {
		return nil, os.ErrNotExist
	}
	return fakeInfo{}, nil
}

func (fs *memFS) ReadFile(path string) ([]byte, error) {
	b, ok := fs.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func (fs *memFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	fs.files[path] = append([]byte(nil), data...)
	fs.modes[path] = perm
	return nil
}

func (fs *memFS) Rename(oldPath, newPath string) error {
	b, ok := fs.files[oldPath]
	if !ok {
		return os.ErrNotExist
	}
	fs.files[newPath] = b
	fs.modes[newPath] = fs.modes[oldPath]
	delete(fs.files, oldPath)
	delete(fs.modes, oldPath)
	return nil
}

func (fs *memFS) Remove(path string) error {
	delete(fs.files, path)
	delete(fs.modes, path)
	return nil
}
func (fs *memFS) MkdirAll(string, os.FileMode) error { return nil }
func (fs *memFS) OpenFile(path string, _ int, perm os.FileMode) (io.WriteCloser, error) {
	return &memWriter{fs: fs, path: path, perm: perm}, nil
}

type memWriter struct {
	fs   *memFS
	path string
	perm os.FileMode
	buf  bytes.Buffer
}

func (w *memWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *memWriter) Close() error {
	w.fs.files[w.path] = append([]byte(nil), w.buf.Bytes()...)
	w.fs.modes[w.path] = w.perm
	return nil
}

type fakeInfo struct{}

func (fakeInfo) Name() string       { return "file" }
func (fakeInfo) Size() int64        { return 1 }
func (fakeInfo) Mode() os.FileMode  { return 0o755 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (fakeInfo) Sys() any           { return nil }

type fakeHTTP struct{ body []byte }

func (h fakeHTTP) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(h.body))}, nil
}

func TestMieruChecksumMismatchDoesNotInstall(t *testing.T) {
	archive := mitaArchive(t, []byte("mita-binary"))
	fs := &memFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}
	p := NewLocal(Config{MieruManifestPath: "manifest.json", MitaPath: "/usr/local/bin/mita", FS: fs, HTTPClient: fakeHTTP{body: archive}, GOOS: "linux", GOARCH: "amd64"})
	if _, err := p.Install(context.Background(), model.RuntimeMieru); err == nil {
		t.Fatal("Install succeeded with checksum mismatch")
	}
	if _, ok := fs.files["/usr/local/bin/mita"]; ok {
		t.Fatal("mita was installed despite checksum mismatch")
	}
}

func TestMieruHappyPathInstallsAtomically0755(t *testing.T) {
	archive := mitaArchive(t, []byte("mita-binary"))
	fs := &memFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}
	embeddedMieruArtifacts = []mieruArtifact{{OS: "linux", Arch: "amd64", Kind: "tar.gz", URL: "https://github.com/enfein/mieru/releases/download/v3.35.0/mita_3.35.0_linux_amd64.tar.gz", SHA256: sha256Hex(archive)}}
	t.Cleanup(func() {
		embeddedMieruArtifacts = []mieruArtifact{
			{OS: "linux", Arch: "amd64", Kind: "tar.gz", URL: "https://github.com/enfein/mieru/releases/download/v3.35.0/mita_3.35.0_linux_amd64.tar.gz", SHA256: "a07d5afc5e1353ab346bb3ddbe95c7f960828204be529f4a88d688dfe83e252d"},
			{OS: "linux", Arch: "arm64", Kind: "tar.gz", URL: "https://github.com/enfein/mieru/releases/download/v3.35.0/mita_3.35.0_linux_arm64.tar.gz", SHA256: "808849223d34ccd9ad86afc0eedef4d6c827133258e96dc3f3794bd17e7d54de"},
		}
	})
	p := NewLocal(Config{MitaPath: "/usr/local/bin/mita", FS: fs, HTTPClient: fakeHTTP{body: archive}, GOOS: "linux", GOARCH: "amd64"})
	if _, err := p.Install(context.Background(), model.RuntimeMieru); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := string(fs.files["/usr/local/bin/mita"]); got != "mita-binary" {
		t.Fatalf("mita = %q", got)
	}
	if fs.modes["/usr/local/bin/mita.tmp"] != 0o755 && fs.modes["/usr/local/bin/mita"] != 0o755 {
		t.Fatalf("mode = %#o, want 0755", fs.modes["/usr/local/bin/mita"])
	}
}

func TestMieruTransactionKeepsPreviousUntilCommitAndRollbackRestores(t *testing.T) {
	archive := mitaArchive(t, []byte("new-mita"))
	origArtifacts := append([]mieruArtifact(nil), embeddedMieruArtifacts...)
	embeddedMieruArtifacts = []mieruArtifact{{OS: "linux", Arch: "amd64", Kind: "tar.gz", URL: "https://github.com/enfein/mieru/releases/download/v3.35.0/mita_3.35.0_linux_amd64.tar.gz", SHA256: sha256Hex(archive)}}
	t.Cleanup(func() { embeddedMieruArtifacts = origArtifacts })

	fs := &memFS{files: map[string][]byte{"/usr/local/bin/mita": []byte("old-mita")}, modes: map[string]os.FileMode{}}
	p := NewLocal(Config{MitaPath: "/usr/local/bin/mita", FS: fs, HTTPClient: fakeHTTP{body: archive}, GOOS: "linux", GOARCH: "amd64"})
	tx, err := p.BeginUpdate(context.Background(), model.RuntimeMieru)
	if err != nil {
		t.Fatalf("BeginUpdate: %v", err)
	}
	if got := string(fs.files["/usr/local/bin/mita"]); got != "new-mita" {
		t.Fatalf("current mita = %q, want new", got)
	}
	if got := string(fs.files["/usr/local/bin/mita.previous"]); got != "old-mita" {
		t.Fatalf("previous mita = %q, want old retained", got)
	}
	if _, err := tx.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := string(fs.files["/usr/local/bin/mita"]); got != "old-mita" {
		t.Fatalf("rollback current mita = %q, want old", got)
	}

	fs.files["/usr/local/bin/mita"] = []byte("old-mita")
	tx, err = p.BeginUpdate(context.Background(), model.RuntimeMieru)
	if err != nil {
		t.Fatalf("BeginUpdate second: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, ok := fs.files["/usr/local/bin/mita.previous"]; ok {
		t.Fatal("commit left .previous backup behind")
	}
}

func TestMieruStalePreviousFailsClosed(t *testing.T) {
	archive := mitaArchive(t, []byte("new-mita"))
	origArtifacts := append([]mieruArtifact(nil), embeddedMieruArtifacts...)
	embeddedMieruArtifacts = []mieruArtifact{{OS: "linux", Arch: "amd64", Kind: "tar.gz", URL: "https://github.com/enfein/mieru/releases/download/v3.35.0/mita_3.35.0_linux_amd64.tar.gz", SHA256: sha256Hex(archive)}}
	t.Cleanup(func() { embeddedMieruArtifacts = origArtifacts })

	fs := &memFS{files: map[string][]byte{"/usr/local/bin/mita": []byte("current"), "/usr/local/bin/mita.previous": []byte("previous")}, modes: map[string]os.FileMode{}}
	p := NewLocal(Config{MitaPath: "/usr/local/bin/mita", FS: fs, HTTPClient: fakeHTTP{body: archive}, GOOS: "linux", GOARCH: "amd64"})
	if _, err := p.BeginUpdate(context.Background(), model.RuntimeMieru); err == nil {
		t.Fatal("BeginUpdate succeeded with stale .previous backup")
	}
	if got := string(fs.files["/usr/local/bin/mita.previous"]); got != "previous" {
		t.Fatalf("stale previous was modified: %q", got)
	}
}

func TestMieruPlanUsesEmbeddedManifestWhenCWDHasNoRepoFiles(t *testing.T) {
	p := NewLocal(Config{MieruManifestPath: filepath.Join(t.TempDir(), "missing.json"), GOOS: "linux", GOARCH: "arm64"})
	plan := p.Plan(model.RuntimeMieru)
	if !plan.Supported || plan.ArtifactRef != "mieru:mita:v3.35.0:linux/arm64:sha256:808849223d34ccd9ad86afc0eedef4d6c827133258e96dc3f3794bd17e7d54de" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestEmbeddedMieruManifestMatchesSourceManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "runtime-images", "mieru", "mita-v3.35.0.manifest.json"))
	if err != nil {
		t.Fatalf("Read source manifest: %v", err)
	}
	var manifest mieruManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("Unmarshal source manifest: %v", err)
	}
	want := map[string]mieruArtifact{}
	for _, a := range manifest.Artifacts {
		if a.Kind == "tar.gz" && a.OS == "linux" && (a.Arch == "amd64" || a.Arch == "arm64") {
			want[a.Arch] = a
		}
	}
	for _, got := range embeddedMieruArtifacts {
		if source, ok := want[got.Arch]; !ok || got.URL != source.URL || got.SHA256 != source.SHA256 {
			t.Fatalf("embedded artifact %+v does not match source %+v", got, source)
		}
		delete(want, got.Arch)
	}
	if len(want) != 0 {
		t.Fatalf("missing embedded artifacts: %+v", want)
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func mitaArchive(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "mita", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func joinCalls(calls []call) string {
	var lines []string
	for _, call := range calls {
		lines = append(lines, call.name+" "+strings.Join(call.args, " "))
	}
	return strings.Join(lines, "\n")
}
