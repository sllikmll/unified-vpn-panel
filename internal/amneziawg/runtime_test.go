package amneziawg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDetectPrefersDockerThenNative(t *testing.T) {
	be := &FakeBackend{DockerAvailable: true, NativeAvailable: true}
	rt := NewRuntime(be, MemoryStore{})
	got, err := rt.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got.Backend != BackendDocker || !got.Available {
		t.Fatalf("Detect = %+v, want docker available", got)
	}
}

func TestRuntimeApplyRollsBackOnVerifyFailure(t *testing.T) {
	store := MemoryStore{}
	be := &FakeBackend{DockerAvailable: true, VerifyErr: errors.New("verify failed")}
	rt := NewRuntime(be, store)
	server := DefaultServer("awg0", 51820)
	server.PrivateKey = "SERVER_PRIVATE"
	server.PublicKey = "SERVER_PUBLIC"
	err := rt.Apply(context.Background(), DesiredConfig{Server: server})
	if err == nil {
		t.Fatal("Apply succeeded, want verify error")
	}
	if !be.RolledBack {
		t.Fatal("backend did not rollback after verify failure")
	}
	if got, _ := store.Load("awg0"); got != "" {
		t.Fatalf("store config after failed apply = %q, want empty", got)
	}
}

func TestRuntimePeerLifecycle(t *testing.T) {
	store := MemoryStore{}
	be := &FakeBackend{DockerAvailable: true}
	rt := NewRuntime(be, store)
	server := DefaultServer("awg0", 51820)
	server.PrivateKey = "SERVER_PRIVATE"
	server.PublicKey = "SERVER_PUBLIC"
	client := Client{ID: "client-1", Email: "u@example.test", PrivateKey: "CLIENT_PRIVATE", PublicKey: "CLIENT_PUBLIC", PresharedKey: "PSK", IPv4Address: "10.66.66.2/32", AllowedIPs: "10.66.66.2/32", Enable: true}
	if err := rt.Apply(context.Background(), DesiredConfig{Server: server, Clients: []Client{client}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := rt.PeerStatus(context.Background(), "client-1"); !errors.Is(err, ErrPeerOperationsUnsupported) {
		t.Fatalf("PeerStatus err = %v, want ErrPeerOperationsUnsupported", err)
	}
	if _, err := rt.ExportPeer(context.Background(), "client-1"); !errors.Is(err, ErrPeerOperationsUnsupported) {
		t.Fatalf("ExportPeer err = %v, want ErrPeerOperationsUnsupported", err)
	}
	if err := rt.UpsertPeer(context.Background(), client); !errors.Is(err, ErrPeerOperationsUnsupported) {
		t.Fatalf("UpsertPeer err = %v, want ErrPeerOperationsUnsupported", err)
	}
	if err := rt.DeletePeer(context.Background(), "client-1"); !errors.Is(err, ErrPeerOperationsUnsupported) {
		t.Fatalf("DeletePeer err = %v, want ErrPeerOperationsUnsupported", err)
	}
	if err := rt.SetPeerEnabled(context.Background(), "client-1", false); !errors.Is(err, ErrPeerOperationsUnsupported) {
		t.Fatalf("SetPeerEnabled err = %v, want ErrPeerOperationsUnsupported", err)
	}
	if got := store["awg0"]; strings.Contains(got, "CLIENT_PRIVATE") && strings.Count(got, "CLIENT_PRIVATE") != 1 {
		t.Fatalf("unexpected retained client material mutation: %s", got)
	}
}

func TestCommandBackendUsesFixedArgvAndDoesNotPassConfigAsArg(t *testing.T) {
	var calls []string
	be := &CommandBackend{
		LookPath: func(name string) (string, error) {
			if name == "awg" || name == "awg-quick" {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("missing")
		},
		Run: func(_ context.Context, name string, args ...string) error {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			if name == "docker" {
				return errors.New("no docker")
			}
			return nil
		},
	}

	const renderedConfig = "PrivateKey = SECRET\n"
	if err := be.Apply(context.Background(), "awg0", renderedConfig); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := be.Verify(context.Background(), "awg0"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	want := []string{
		"docker container inspect amnezia-awg2",
		"awg-quick down awg0",
		"awg-quick up awg0",
		"docker container inspect amnezia-awg2",
		"awg show awg0",
	}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	if strings.Contains(strings.Join(calls, "\n"), "SECRET") {
		t.Fatalf("argv leaked config material: %#v", calls)
	}
}

func TestCommandBackendRejectsDockerMountMismatch(t *testing.T) {
	profile := DockerBackendProfile()
	profile.HostConfigDir = t.TempDir()
	be := &CommandBackend{
		DockerProfile: profile,
		Run: func(_ context.Context, name string, args ...string) error {
			if name == "docker" && fmt.Sprint(args) == "[container inspect amnezia-awg2]" {
				return nil
			}
			return nil
		},
		Output: func(_ context.Context, name string, args ...string) ([]byte, error) {
			return []byte(`[{"Source":"/wrong/state","Destination":"/opt/amnezia/awg"}]`), nil
		},
	}
	if _, err := be.Detect(context.Background()); !errors.Is(err, ErrDockerMountMismatch) {
		t.Fatalf("Detect error = %v, want ErrDockerMountMismatch", err)
	}
}

func TestRuntimeDockerApplyPersistsNewConfigBeforeRestart(t *testing.T) {
	dir := t.TempDir()
	profile := DockerBackendProfile()
	profile.HostConfigDir = dir
	var sawNewConfigAtRestart bool
	be := &CommandBackend{
		DockerProfile: profile,
		Run: func(_ context.Context, name string, args ...string) error {
			call := name + " " + strings.Join(args, " ")
			switch call {
			case "docker container inspect amnezia-awg2":
				return nil
			case "docker restart amnezia-awg2":
				raw, err := os.ReadFile(filepath.Join(dir, "awg0.conf"))
				if err != nil {
					return err
				}
				sawNewConfigAtRestart = strings.Contains(string(raw), "PrivateKey = SERVER_PRIVATE_NEW")
				return nil
			case "docker exec amnezia-awg2 awg show awg0":
				return nil
			default:
				return fmt.Errorf("unexpected command %q", call)
			}
		},
		Output: func(_ context.Context, name string, args ...string) ([]byte, error) {
			return []byte(`[{"Source":"` + filepath.ToSlash(dir) + `","Destination":"/opt/amnezia/awg"}]`), nil
		},
	}
	rt := NewRuntime(be, nil)
	server := DefaultServer("awg0", 51820)
	server.PrivateKey = "SERVER_PRIVATE_NEW"
	server.PublicKey = "SERVER_PUBLIC_NEW"
	if err := rt.Apply(context.Background(), DesiredConfig{Server: server}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !sawNewConfigAtRestart {
		t.Fatal("docker restart happened before awg0.conf contained the supplied config")
	}
	info, err := os.Stat(filepath.Join(dir, "awg0.conf"))
	if err != nil {
		t.Fatalf("stat awg0.conf: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 0600", got)
	}
}
