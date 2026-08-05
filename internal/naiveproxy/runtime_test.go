package naiveproxy

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeRunner struct {
	failAt string
	cmds   []Command
}

func (f *fakeRunner) Run(_ context.Context, cmd Command) error {
	f.cmds = append(f.cmds, cmd)
	for _, arg := range cmd.Argv {
		if arg == f.failAt || cmd.Name == f.failAt {
			return errors.New("forced failure")
		}
	}
	return nil
}

func (f *fakeRunner) Observe(context.Context, string) (Observation, error) {
	return Observation{State: BackendActive}, nil
}

type fakeStore struct {
	current     Server
	hasCurrent  bool
	writes      int
	loads       int
	commits     int
	rollbacks   int
	deletes     int
	lastServer  Server
	lastBytes   []byte
	noOldConfig bool
}

func (f *fakeStore) Load(context.Context) (Server, error) {
	f.loads++
	if !f.hasCurrent {
		return Server{}, ErrConfigNotFound
	}
	return f.current, nil
}

func (f *fakeStore) AtomicWrite(_ context.Context, server Server, contents []byte) (Backup, error) {
	f.writes++
	f.lastServer = server
	f.lastBytes = append([]byte(nil), contents...)
	backup := Backup{ConfigPath: FixedConfigPath, StatePath: FixedStatePath, OldServer: f.current, HadPrevious: f.hasCurrent && !f.noOldConfig}
	f.current = server
	f.hasCurrent = true
	return backup, nil
}

func (f *fakeStore) Commit(Backup) error {
	f.commits++
	return nil
}

func (f *fakeStore) Rollback(Backup) error {
	f.rollbacks++
	return nil
}

func (f *fakeStore) Delete(context.Context) error {
	f.deletes++
	return nil
}

type fakeHealth struct {
	err error
}

func (f fakeHealth) Verify(context.Context, Endpoint) error {
	return f.err
}

func TestRuntimeUsesFixedArgvOnly(t *testing.T) {
	runner := &fakeRunner{}
	store := &fakeStore{}
	rt, err := NewRuntime(runner, store, fakeHealth{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Apply(context.Background(), validServer()); err != nil {
		t.Fatal(err)
	}
	want := []Command{
		validateCommand(FixedConfigPath),
		reloadCommand(FixedConfigPath),
	}
	if !reflect.DeepEqual(runner.cmds, want) {
		t.Fatalf("commands mismatch\nwant %#v\ngot  %#v", want, runner.cmds)
	}
	if store.commits != 1 || store.rollbacks != 0 {
		t.Fatalf("commit/rollback mismatch: commits=%d rollbacks=%d", store.commits, store.rollbacks)
	}
}

func TestRuntimeRollsBackOnValidationFailure(t *testing.T) {
	runner := &fakeRunner{failAt: "validate"}
	store := &fakeStore{current: validServer(), hasCurrent: true}
	rt, _ := NewRuntime(runner, store, fakeHealth{})
	if err := rt.Apply(context.Background(), validServer()); err == nil {
		t.Fatal("expected validation failure")
	}
	if store.rollbacks != 1 || store.commits != 0 {
		t.Fatalf("expected rollback only, commits=%d rollbacks=%d", store.commits, store.rollbacks)
	}
	want := []Command{
		validateCommand(FixedConfigPath),
		reloadCommand(FixedConfigPath),
	}
	if !reflect.DeepEqual(runner.cmds, want) {
		t.Fatalf("rollback should reload old runtime\nwant %#v\ngot  %#v", want, runner.cmds)
	}
}

func TestRuntimeRollsBackOnReloadFailure(t *testing.T) {
	runner := &fakeRunner{failAt: "reload"}
	store := &fakeStore{current: validServer(), hasCurrent: true}
	rt, _ := NewRuntime(runner, store, fakeHealth{})
	if err := rt.Apply(context.Background(), validServer()); err == nil {
		t.Fatal("expected reload failure")
	}
	if store.rollbacks != 1 || store.commits != 0 {
		t.Fatalf("expected rollback only, commits=%d rollbacks=%d", store.commits, store.rollbacks)
	}
	if len(runner.cmds) != 3 || runner.cmds[2].Name != FixedExecutableName || runner.cmds[2].Argv[0] != "reload" {
		t.Fatalf("expected attempted reload plus rollback reload, got %#v", runner.cmds)
	}
}

func TestRuntimeRollsBackOnHealthFailure(t *testing.T) {
	runner := &fakeRunner{}
	store := &fakeStore{current: validServer(), hasCurrent: true}
	rt, _ := NewRuntime(runner, store, fakeHealth{err: errors.New("tls failed")})
	if err := rt.Apply(context.Background(), validServer()); err == nil {
		t.Fatal("expected health failure")
	}
	if store.rollbacks != 1 || store.commits != 0 {
		t.Fatalf("expected rollback only, commits=%d rollbacks=%d", store.commits, store.rollbacks)
	}
	if len(runner.cmds) != 3 || runner.cmds[2].Argv[0] != "reload" {
		t.Fatalf("expected rollback reload of old runtime, got %#v", runner.cmds)
	}
}

func TestRuntimeStopsAndCleansWhenInitialApplyFails(t *testing.T) {
	runner := &fakeRunner{failAt: "reload"}
	store := &fakeStore{noOldConfig: true}
	rt, _ := NewRuntime(runner, store, fakeHealth{})
	if err := rt.Apply(context.Background(), validServer()); err == nil {
		t.Fatal("expected reload failure")
	}
	wantLast := serviceCommand("stop")
	if !reflect.DeepEqual(runner.cmds[len(runner.cmds)-1], wantLast) {
		t.Fatalf("expected stop after failed first apply, got %#v", runner.cmds)
	}
}

func TestRuntimeRejectsArbitraryConfigPath(t *testing.T) {
	if _, err := newRuntimeWithConfigPath(&fakeRunner{}, &fakeStore{}, fakeHealth{}, "/tmp/Caddyfile"); err == nil {
		t.Fatal("expected fixed config path validation")
	}
	if _, err := newRuntimeWithConfigPath(&fakeRunner{}, &fakeStore{}, fakeHealth{}, "/tmp/caddy-naive/Caddyfile"); err == nil {
		t.Fatal("expected exact package-owned path validation")
	}
}

func TestRuntimeApplyUserHydratesAndUpsertsDurably(t *testing.T) {
	runner := &fakeRunner{}
	store := &fakeStore{current: validServer(), hasCurrent: true}
	rt, _ := NewRuntime(runner, store, fakeHealth{})
	err := rt.ApplyUser(context.Background(), User{ID: "2", Username: "beta2", Password: "beta-password-updated", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if store.loads == 0 {
		t.Fatal("expected typed state hydration before user apply")
	}
	if len(store.lastServer.Users) != 3 {
		t.Fatalf("expected stable-ID update, got %#v", store.lastServer.Users)
	}
	for _, u := range store.lastServer.Users {
		if u.ID == "2" && (u.Username != "beta2" || !u.Enabled) {
			t.Fatalf("user was not updated by stable ID: %#v", u)
		}
	}
}

func TestRuntimeDeleteUserHydratesAndRemovesDurably(t *testing.T) {
	runner := &fakeRunner{}
	store := &fakeStore{current: validServer(), hasCurrent: true}
	rt, _ := NewRuntime(runner, store, fakeHealth{})
	if err := rt.DeleteUser(context.Background(), "3"); err != nil {
		t.Fatal(err)
	}
	if store.loads == 0 {
		t.Fatal("expected typed state hydration before user delete")
	}
	for _, u := range store.lastServer.Users {
		if u.ID == "3" {
			t.Fatalf("deleted user persisted: %#v", store.lastServer.Users)
		}
	}
}

func TestRuntimeRestartHydratesAndVerifiesCurrentEndpoint(t *testing.T) {
	runner := &fakeRunner{}
	store := &fakeStore{current: validServer(), hasCurrent: true}
	health := &recordingHealth{}
	rt, _ := NewRuntime(runner, store, health)
	if err := rt.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.loads == 0 {
		t.Fatal("expected restart to hydrate typed state")
	}
	if health.endpoint.Domain != validServer().Endpoint.Domain {
		t.Fatalf("health endpoint mismatch: %#v", health.endpoint)
	}
}

type recordingHealth struct {
	endpoint Endpoint
}

func (r *recordingHealth) Verify(_ context.Context, endpoint Endpoint) error {
	r.endpoint = endpoint
	return nil
}
