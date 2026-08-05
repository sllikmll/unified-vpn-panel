package mieru

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOSFileSystemWriteFileCreatesManagedConfigDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "server.conf.pb")
	if err := (OSFileSystem{}).WriteFile(path, []byte("config"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o, want 0600", info.Mode().Perm())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "config" {
		t.Fatalf("content=%q err=%v", data, err)
	}
	tempDir := filepath.Join(t.TempDir(), "missing-temp")
	tempPath, err := (OSFileSystem{}).TempFile(tempDir, "server-*.json")
	if err != nil {
		t.Fatalf("TempFile: %v", err)
	}
	if filepath.Dir(tempPath) != tempDir {
		t.Fatalf("TempFile path=%q, want dir %q", tempPath, tempDir)
	}
}
