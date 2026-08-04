package mieru

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type command struct {
	name string
	args []string
}

type fakeRunner struct {
	calls   []command
	fail    map[string]error
	results map[string]RunnerResult
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (RunnerResult, error) {
	copied := append([]string(nil), args...)
	r.calls = append(r.calls, command{name: name, args: copied})
	key := name + " " + strings.Join(args, " ")
	if r.fail != nil {
		if err := r.fail[key]; err != nil {
			return RunnerResult{}, err
		}
	}
	if r.results != nil {
		return r.results[key], nil
	}
	return RunnerResult{}, nil
}

type fakeFS struct {
	files map[string][]byte
	next  int
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: map[string][]byte{}}
}

func (fs *fakeFS) ReadFile(path string) ([]byte, error) {
	b, ok := fs.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), b...), nil
}

func (fs *fakeFS) WriteFile(path string, data []byte, _ uint32) error {
	fs.files[path] = append([]byte(nil), data...)
	return nil
}

func (fs *fakeFS) Rename(oldPath, newPath string) error {
	b, ok := fs.files[oldPath]
	if !ok {
		return errors.New("not found")
	}
	fs.files[newPath] = b
	delete(fs.files, oldPath)
	return nil
}

func (fs *fakeFS) Remove(path string) error {
	delete(fs.files, path)
	return nil
}

func (fs *fakeFS) Exists(path string) bool {
	_, ok := fs.files[path]
	return ok
}

func (fs *fakeFS) TempFile(dir, pattern string) (string, error) {
	fs.next++
	return filepath.Join(dir, strings.Replace(pattern, "*", "tmp", 1)+string(rune('0'+fs.next))), nil
}

func TestCommandArgvSafety(t *testing.T) {
	runner := &fakeRunner{}
	rt := NewTrustedLocalRuntime(runner, newFakeFS(), TrustedConfigPath(DefaultConfigPath))
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if err := rt.Restart(context.Background()); err != nil {
		t.Fatalf("Restart() error: %v", err)
	}
	want := []command{
		{name: "mita", args: []string{"start"}},
		{name: "mita", args: []string{"stop"}},
		{name: "mita", args: []string{"stop"}},
		{name: "mita", args: []string{"start"}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("commands mismatch\nwant %+v\ngot  %+v", want, runner.calls)
	}
}

func TestAtomicRollbackOnVerifyFailure(t *testing.T) {
	configPath := DefaultConfigPath
	fs := newFakeFS()
	fs.files[configPath] = []byte(`{"portBindings":[{"port":443,"protocol":"TCP"}],"users":[{"name":"old@example.com","password":"old"}]}`)
	verifyErr := errors.New("verify failed")
	runner := &fakeRunner{fail: map[string]error{"mita describe config": verifyErr}}
	rt := NewTrustedLocalRuntime(runner, fs, TrustedConfigPath(configPath))
	err := rt.ApplyConfig(context.Background(), ServerConfig{
		PortBindings: []PortBinding{{Port: 8443, Protocol: TransportTCP}},
		Users:        []User{{Name: "new@example.com", Password: "new"}},
	})
	if err == nil {
		t.Fatal("ApplyConfig() got nil error")
	}
	if got := string(fs.files[configPath]); !strings.Contains(got, "old@example.com") || strings.Contains(got, "new@example.com") {
		t.Fatalf("config was not rolled back: %s", got)
	}
	wantCalls := []command{
		{name: "mita", args: []string{"apply", "config", configPath}},
		{name: "mita", args: []string{"stop"}},
		{name: "mita", args: []string{"start"}},
		{name: "mita", args: []string{"describe", "config"}},
		{name: "mita", args: []string{"apply", "config", configPath}},
		{name: "mita", args: []string{"stop"}},
		{name: "mita", args: []string{"start"}},
		{name: "mita", args: []string{"describe", "config"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("commands mismatch\nwant %+v\ngot  %+v", wantCalls, runner.calls)
	}
	if strings.Contains(err.Error(), "new") || strings.Contains(err.Error(), "old") {
		t.Fatalf("rollback error leaked config material: %v", err)
	}
}

func TestRollbackStopsAttemptedRuntimeWhenNoPriorConfig(t *testing.T) {
	configPath := DefaultConfigPath
	fs := newFakeFS()
	runner := &fakeRunner{fail: map[string]error{"mita describe config": errors.New("secret verify output")}}
	rt := NewTrustedLocalRuntime(runner, fs, TrustedConfigPath(configPath))
	err := rt.ApplyConfig(context.Background(), ServerConfig{
		PortBindings: []PortBinding{{Port: 8443, Protocol: TransportTCP}},
		Users:        []User{{Name: "new@example.com", Password: "new-secret"}},
	})
	if err == nil {
		t.Fatal("ApplyConfig() got nil error")
	}
	if fs.Exists(configPath) {
		t.Fatalf("new config was not removed after rollback: %s", string(fs.files[configPath]))
	}
	wantCalls := []command{
		{name: "mita", args: []string{"apply", "config", configPath}},
		{name: "mita", args: []string{"stop"}},
		{name: "mita", args: []string{"start"}},
		{name: "mita", args: []string{"describe", "config"}},
		{name: "mita", args: []string{"stop"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("commands mismatch\nwant %+v\ngot  %+v", wantCalls, runner.calls)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "new@example.com") {
		t.Fatalf("rollback error leaked command or config material: %v", err)
	}
}

func TestObserveDetectStates(t *testing.T) {
	ctx := context.Background()
	runner := &fakeRunner{results: map[string]RunnerResult{
		"mita version": {Output: "mita version 3.99.0"},
		"mita status":  {Output: `mita server status is "RUNNING"`},
	}}
	rt := NewTrustedLocalRuntime(runner, newFakeFS(), TrustedConfigPath(DefaultConfigPath))
	detected, err := rt.Detect(ctx)
	if err != nil || detected.State != StatusStopped || !detected.Installed || detected.MissingBinary {
		t.Fatalf("Detect() = %+v, %v", detected, err)
	}
	observed, err := rt.Observe(ctx)
	if err != nil || observed.State != StatusRunning || !observed.Running || observed.MissingBinary {
		t.Fatalf("Observe() = %+v, %v", observed, err)
	}

	missing := NewTrustedLocalRuntime(&fakeRunner{fail: map[string]error{"mita status": ErrMissingBinary}}, newFakeFS(), TrustedConfigPath(DefaultConfigPath))
	status, err := missing.Observe(ctx)
	if err != nil || status.State != StatusMissing || !status.MissingBinary || status.Installed {
		t.Fatalf("missing Observe() = %+v, %v", status, err)
	}

	stopped := NewTrustedLocalRuntime(&fakeRunner{results: map[string]RunnerResult{"mita status": {Output: `mita server status is "IDLE"`}}}, newFakeFS(), TrustedConfigPath(DefaultConfigPath))
	status, err = stopped.Observe(ctx)
	if err != nil || status.State != StatusStopped || status.Running {
		t.Fatalf("stopped Observe() = %+v, %v", status, err)
	}
}

func TestProductionRuntimeRejectsArbitraryConfigPath(t *testing.T) {
	if _, err := NewProductionRuntime(&fakeRunner{}, newFakeFS(), DefaultConfigPath); err != nil {
		t.Fatalf("NewProductionRuntime() rejected default path: %v", err)
	}
	if _, err := NewProductionRuntime(&fakeRunner{}, newFakeFS(), "/tmp/request-controlled.json"); err == nil {
		t.Fatal("NewProductionRuntime() accepted request-controlled config path")
	}
}

func TestDeleteStopsRuntimeAndRemovesState(t *testing.T) {
	configPath := DefaultConfigPath
	backupPath := configPath + ".bak"
	fs := newFakeFS()
	fs.files[configPath] = []byte(`{"portBindings":[{"port":443,"protocol":"TCP"}]}`)
	fs.files[backupPath] = []byte(`backup`)
	runner := &fakeRunner{}
	rt := NewTrustedLocalRuntime(runner, fs, TrustedConfigPath(configPath))
	if err := rt.Delete(context.Background()); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if fs.Exists(configPath) || fs.Exists(backupPath) {
		t.Fatalf("delete left state: config=%v backup=%v", fs.Exists(configPath), fs.Exists(backupPath))
	}
	wantCalls := []command{{name: "mita", args: []string{"stop"}}}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("commands mismatch\nwant %+v\ngot  %+v", wantCalls, runner.calls)
	}
}

func TestDeleteRestoresStateWhenStopFails(t *testing.T) {
	configPath := DefaultConfigPath
	backupPath := configPath + ".bak"
	fs := newFakeFS()
	fs.files[configPath] = []byte(`{"portBindings":[{"port":443,"protocol":"TCP"}]}`)
	fs.files[backupPath] = []byte(`backup`)
	runner := &fakeRunner{fail: map[string]error{"mita stop": errors.New("secret stop output")}}
	rt := NewTrustedLocalRuntime(runner, fs, TrustedConfigPath(configPath))
	err := rt.Delete(context.Background())
	if err == nil {
		t.Fatal("Delete() got nil error")
	}
	if !fs.Exists(configPath) || !fs.Exists(backupPath) {
		t.Fatalf("delete failure did not restore state: config=%v backup=%v", fs.Exists(configPath), fs.Exists(backupPath))
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("delete error leaked command output: %v", err)
	}
}

func TestOSRunnerRejectsUnexpectedCommandsBeforeExec(t *testing.T) {
	ctx := context.Background()
	runner := OSRunner{}
	if _, err := runner.Run(ctx, "sh", "-c", "mita status"); err == nil {
		t.Fatal("OSRunner accepted shell command")
	}
	if _, err := runner.Run(ctx, BinaryName, "apply", "config", "/tmp/request.json"); err == nil {
		t.Fatal("OSRunner accepted non-default config path")
	}
	if _, err := runner.Run(ctx, BinaryName, "get", "users"); err == nil {
		t.Fatal("OSRunner accepted unsupported lifecycle verb")
	}
}

func TestInstallPlanAllowlist(t *testing.T) {
	good := InstallArtifact{
		Version:     "v3.99.0",
		OS:          "linux",
		Arch:        "amd64",
		URL:         "https://github.com/enfein/mieru/releases/download/v3.99.0/mita-linux-amd64",
		SHA256:      strings.Repeat("a", 64),
		BinaryName:  "mita",
		Destination: "/usr/local/bin/mita",
	}
	if _, err := NewInstallPlan([]InstallArtifact{good}); err != nil {
		t.Fatalf("NewInstallPlan() rejected valid plan: %v", err)
	}
	bad := good
	bad.URL = "https://example.com/install.sh"
	if _, err := NewInstallPlan([]InstallArtifact{bad}); err == nil {
		t.Fatal("NewInstallPlan() accepted non-allowlisted URL")
	}
	bad = good
	bad.BinaryName = "sh"
	if _, err := NewInstallPlan([]InstallArtifact{bad}); err == nil {
		t.Fatal("NewInstallPlan() accepted non-mita binary")
	}
}

func TestUnsupportedCountersAndMissingBinary(t *testing.T) {
	rt := NewRuntime(nil, newFakeFS())
	status, err := rt.Detect(context.Background())
	if err != nil || !status.MissingBinary || status.State != StatusMissing {
		t.Fatalf("Detect() = %+v, %v", status, err)
	}
	counters, err := rt.Traffic(context.Background())
	if !errors.Is(err, ErrUnsupportedTraffic) || counters.Supported {
		t.Fatalf("Traffic() = %+v, %v", counters, err)
	}
}
