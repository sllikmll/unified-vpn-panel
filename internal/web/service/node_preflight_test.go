package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNodePreflightRequestValidateAuth(t *testing.T) {
	cases := []struct {
		name    string
		req     NodePreflightRequest
		wantErr string
	}{
		{
			name: "password",
			req: NodePreflightRequest{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: strPtr("secret"),
				HostKeyMode: "insecure",
			},
		},
		{
			name: "private key",
			req: NodePreflightRequest{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "privateKey", PrivateKey: strPtr("-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----"),
				HostKeyMode: "insecure",
			},
		},
		{
			name: "missing password",
			req: NodePreflightRequest{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", HostKeyMode: "insecure",
			},
			wantErr: "password is required",
		},
		{
			name: "both secrets",
			req: NodePreflightRequest{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: strPtr("secret"), PrivateKey: strPtr("key"),
				HostKeyMode: "insecure",
			},
			wantErr: "password and privateKey are mutually exclusive",
		},
		{
			name: "known hosts default needs material",
			req: NodePreflightRequest{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: strPtr("secret"),
			},
			wantErr: "knownHosts is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestNodePreflightRequestWriteOnlyJSON(t *testing.T) {
	req := NodePreflightRequest{
		Address: "node.example.com", Port: 22, Username: "root",
		AuthMethod: "password", Password: strPtr("super-secret"), PrivateKey: strPtr("private-key"),
		HostKeyMode: "insecure",
	}
	buf, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(buf)
	for _, secret := range []string{"super-secret", "private-key", `"password":`, `"privateKey":`, `"privateKeyPassphrase":`} {
		if strings.Contains(got, secret) {
			t.Fatalf("marshaled request exposed %q in %s", secret, got)
		}
	}
}

func TestRedactNodePreflightSecrets(t *testing.T) {
	req := NodePreflightRequest{
		Password:             strPtr("super-secret"),
		PrivateKey:           strPtr("-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"),
		PrivateKeyPassphrase: strPtr("key-pass"),
	}
	err := RedactNodePreflightSecrets(&req, errors.New("ssh: super-secret key-pass abc failed"))
	got := err.Error()
	for _, secret := range []string{"super-secret", "key-pass", "abc"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted error still contains %q: %s", secret, got)
		}
	}
}

func TestParseNodePreflightOutput(t *testing.T) {
	out := `__XUI_PRE_OS=ubuntu
__XUI_PRE_ARCH=x86_64
__XUI_PRE_HOSTNAME=edge-1
__XUI_PRE_ROOT=1
__XUI_PRE_SUDO=1
__XUI_PRE_SYSTEMD=1
__XUI_PRE_DOCKER=0
__XUI_PRE_FREE_KB=10485760
__XUI_PRE_PORTS=22,80,443,2053
`
	got := ParseNodePreflightOutput(out)
	if got.OS != "ubuntu" || got.Arch != "amd64" || got.Hostname != "edge-1" {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if !got.Root || !got.Sudo || !got.Systemd || got.Docker {
		t.Fatalf("capability mismatch: %+v", got)
	}
	if got.FreeDiskBytes != 10485760*1024 {
		t.Fatalf("FreeDiskBytes = %d", got.FreeDiskBytes)
	}
	if len(got.OccupiedPorts) != 4 || got.OccupiedPorts[2] != 443 {
		t.Fatalf("OccupiedPorts = %+v", got.OccupiedPorts)
	}
}

func TestNodePreflightCommandTimeoutRedactsSecrets(t *testing.T) {
	exec := &fakePreflightExecutor{err: context.DeadlineExceeded}
	req := NodePreflightRequest{
		Address: "node.example.com", Port: 22, Username: "root",
		AuthMethod: "password", Password: strPtr("super-secret"),
		HostKeyMode: "insecure", TimeoutSeconds: 1,
	}
	_, err := (&NodeService{sshExecutor: exec}).Preflight(context.Background(), &req)
	if err == nil {
		t.Fatal("Preflight() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("Preflight() error = %q, want timeout", err.Error())
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("Preflight() exposed secret: %v", err)
	}
	if exec.timeout != time.Second {
		t.Fatalf("executor timeout = %v, want 1s", exec.timeout)
	}
}

type fakePreflightExecutor struct {
	out     string
	err     error
	timeout time.Duration
}

func (f *fakePreflightExecutor) Run(_ context.Context, _ NodeSSHConfig, _ string, timeout time.Duration) (string, error) {
	f.timeout = timeout
	return f.out, f.err
}

func strPtr(s string) *string { return &s }
