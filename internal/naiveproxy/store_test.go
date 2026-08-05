package naiveproxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOSConfigStorePersistsTypedStateAndMode0600(t *testing.T) {
	dir := t.TempDir()
	store := newOSConfigStoreForTest(filepath.Join(dir, "Caddyfile"), filepath.Join(dir, "server.json"))
	server := validServer()
	rendered, err := GenerateCaddyfile(server)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.AtomicWrite(context.Background(), server, []byte(rendered))
	if err != nil {
		t.Fatal(err)
	}
	if backup.HadPrevious {
		t.Fatal("first write should not report previous config")
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Users) != len(server.Users) || loaded.Users[0].Password == "" {
		t.Fatalf("typed state did not round-trip credentials: %#v", loaded.Users)
	}
	for _, path := range []string{store.configPath, store.statePath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 0600", path, got)
		}
	}
}

func TestOSConfigStoreRollbackRestoresOldFileAndTypedState(t *testing.T) {
	dir := t.TempDir()
	store := newOSConfigStoreForTest(filepath.Join(dir, "Caddyfile"), filepath.Join(dir, "server.json"))
	oldServer := validServer()
	oldRendered, _ := GenerateCaddyfile(oldServer)
	if _, err := store.AtomicWrite(context.Background(), oldServer, []byte(oldRendered)); err != nil {
		t.Fatal(err)
	}
	oldRendered = oldRendered + "# preserved operator comment\n"
	if err := os.WriteFile(store.configPath, []byte(oldRendered), 0o600); err != nil {
		t.Fatal(err)
	}
	next := validServer()
	next.Users[0].Username = "changed"
	next.Users[0].Enabled = true
	backup, err := store.AtomicWrite(context.Background(), next, []byte("attempted config"))
	if err != nil {
		t.Fatal(err)
	}
	if !backup.HadPrevious {
		t.Fatal("expected backup to include previous typed state")
	}
	if err := store.Rollback(backup); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(store.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != oldRendered {
		t.Fatalf("old config was not restored\n%s", got)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Users[0].Username != oldServer.Users[0].Username {
		t.Fatalf("old typed state was not restored: %#v", loaded.Users)
	}
}

func TestOSConfigStoreRollbackCleansInitialAttempt(t *testing.T) {
	dir := t.TempDir()
	store := newOSConfigStoreForTest(filepath.Join(dir, "Caddyfile"), filepath.Join(dir, "server.json"))
	backup, err := store.AtomicWrite(context.Background(), validServer(), []byte("attempted config"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Rollback(backup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.configPath); !os.IsNotExist(err) {
		t.Fatalf("config should be removed after initial rollback, err=%v", err)
	}
	if _, err := store.Load(context.Background()); err == nil || !strings.Contains(err.Error(), ErrConfigNotFound.Error()) {
		t.Fatalf("state should be removed after initial rollback, err=%v", err)
	}
}

func TestOSConfigStoreCleansConfigWhenStateWriteFails(t *testing.T) {
	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newOSConfigStoreForTest(filepath.Join(dir, "Caddyfile"), filepath.Join(blockingFile, "server.json"))
	if _, err := store.AtomicWrite(context.Background(), validServer(), []byte("attempted config")); err == nil {
		t.Fatal("expected state write failure")
	}
	if _, err := os.Stat(store.configPath); !os.IsNotExist(err) {
		t.Fatalf("config should be cleaned after partial write failure, err=%v", err)
	}
}
