package service

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestNodeSSHHostKeyPolicy(t *testing.T) {
	pub := testPublicKey(t)
	knownHosts := "node.example.com " + pub.Type() + " " + base64.StdEncoding.EncodeToString(pub.Marshal())
	cases := []struct {
		name    string
		cfg     NodeSSHConfig
		wantErr string
	}{
		{
			name: "known hosts",
			cfg: NodeSSHConfig{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: "secret",
				HostKeyMode: "known_hosts", KnownHosts: knownHosts,
			},
		},
		{
			name: "pin",
			cfg: NodeSSHConfig{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: "secret",
				HostKeyMode: "pin", HostKeyFingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			},
		},
		{
			name: "insecure",
			cfg: NodeSSHConfig{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: "secret",
				HostKeyMode: "insecure",
			},
		},
		{
			name: "pin requires fingerprint",
			cfg: NodeSSHConfig{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: "secret",
				HostKeyMode: "pin",
			},
			wantErr: "hostKeyFingerprint is required",
		},
		{
			name: "known hosts requires data",
			cfg: NodeSSHConfig{
				Address: "node.example.com", Port: 22, Username: "root",
				AuthMethod: "password", Password: "secret",
				HostKeyMode: "known_hosts",
			},
			wantErr: "knownHosts is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			callback, err := BuildSSHHostKeyCallback(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("BuildSSHHostKeyCallback() error = %v", err)
				}
				if callback == nil {
					t.Fatal("BuildSSHHostKeyCallback() callback = nil")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("BuildSSHHostKeyCallback() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseSSHFingerprint(t *testing.T) {
	if _, err := ParseSSHFingerprint("MD5:aa:bb"); err == nil {
		t.Fatal("ParseSSHFingerprint accepted non-SHA256 fingerprint")
	}
	if _, err := ParseSSHFingerprint(ssh.FingerprintSHA256(testPublicKey(t))); err != nil {
		t.Fatalf("ParseSSHFingerprint rejected valid SHA256 fingerprint: %v", err)
	}
}

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	key, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	return key
}
