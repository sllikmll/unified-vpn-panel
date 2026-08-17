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

func TestRuntimeFirstApplyFailureRemovesStateAndStops(t *testing.T) {
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
	if !be.Stopped {
		t.Fatal("backend did not stop after first verify failure")
	}
	if _, ok := store["awg0"]; ok {
		t.Fatalf("store retained failed first config = %q", store["awg0"])
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
		Run: func(_ context.Context, name string, args ...string) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		},
		Output: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), ".State.Running") {
				return []byte("true\n"), nil
			}
			return []byte(`[{"Source":"/opt/amnezia/state/amnezia-awg2","Destination":"/opt/amnezia/awg"}]`), nil
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
		"docker exec amnezia-awg2 awg2-reconcile apply",
		"docker container inspect amnezia-awg2",
		"docker exec amnezia-awg2 awg2-reconcile verify",
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

func TestRuntimeDockerApplyPersistsNewConfigBeforeReconcile(t *testing.T) {
	dir := t.TempDir()
	profile := DockerBackendProfile()
	profile.HostConfigDir = dir
	var sawNewConfigAtReconcile bool
	be := &CommandBackend{
		DockerProfile: profile,
		Run: func(_ context.Context, name string, args ...string) error {
			call := name + " " + strings.Join(args, " ")
			switch call {
			case "docker container inspect amnezia-awg2":
				return nil
			case "docker exec amnezia-awg2 awg2-reconcile apply":
				raw, err := os.ReadFile(filepath.Join(dir, "awg0.conf"))
				if err != nil {
					return err
				}
				sawNewConfigAtReconcile = strings.Contains(string(raw), "PrivateKey = SERVER_PRIVATE_NEW")
				return nil
			case "docker exec amnezia-awg2 awg2-reconcile verify":
				return nil
			default:
				return fmt.Errorf("unexpected command %q", call)
			}
		},
		Output: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), ".State.Running") {
				return []byte("true\n"), nil
			}
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
	if !sawNewConfigAtReconcile {
		t.Fatal("reconcile happened before awg0.conf contained the supplied config")
	}
	info, err := os.Stat(filepath.Join(dir, "awg0.conf"))
	if err != nil {
		t.Fatalf("stat awg0.conf: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 0600", got)
	}
}

func TestParsePeerDumpKeepsHandshakeAndCounters(t *testing.T) {
	raw := []byte("priv	pub	51820	0\nCLIENT_PUBLIC	PSK	198.51.100.1:1234	10.66.66.2/32	1720000000	123	456	25\n")
	peers := parsePeerDump(raw)
	if len(peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(peers))
	}
	got := peers[0]
	if got.PublicKey != "CLIENT_PUBLIC" || got.LastHandshakeUnix != 1720000000 || got.RxBytes != 123 || got.TxBytes != 456 {
		t.Fatalf("peer = %+v", got)
	}
}

func TestPeerIDsFromPersistedConfig(t *testing.T) {
	raw := "[Peer]\n# client-1\nPublicKey = CLIENT_PUBLIC\nAllowedIPs = 10.66.66.2/32\n"
	ids := peerIDsFromConfig(raw)
	if ids["CLIENT_PUBLIC"] != "client-1" {
		t.Fatalf("ids = %#v", ids)
	}
}
