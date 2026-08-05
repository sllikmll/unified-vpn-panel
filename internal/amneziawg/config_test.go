package amneziawg

import (
	"strings"
	"testing"
)

func TestValidateObfuscation20RejectsInvalidParams(t *testing.T) {
	valid := DefaultServer("awg0", 51820)
	cases := []struct {
		name   string
		mutate func(*Server)
	}{
		{"j order", func(s *Server) { s.Jmin, s.Jmax = 80, 40 }},
		{"s3 high", func(s *Server) { s.S3 = 65 }},
		{"s4 high", func(s *Server) { s.S4 = 33 }},
		{"h range reversed", func(s *Server) { s.H1 = "9-5" }},
		{"h too high", func(s *Server) { s.H2 = "4294967296" }},
		{"h negative", func(s *Server) { s.H3 = "-1" }},
		{"i1 shellish", func(s *Server) { s.I1 = "$(cat /etc/shadow)" }},
		{"ipv6 disabled", func(s *Server) { s.IPv6Enabled = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.mutate(&s)
			if err := ValidateServer(s); err == nil {
				t.Fatal("ValidateServer succeeded, want error")
			}
		})
	}
}

func TestValidateObfuscation20AcceptsMaxUint32HValue(t *testing.T) {
	s := DefaultServer("awg0", 51820)
	s.PrivateKey = "SERVER_PRIVATE"
	s.PublicKey = "SERVER_PUBLIC"
	s.H1 = "4294967295"
	s.H2 = "0-4294967295"
	if err := ValidateServer(s); err != nil {
		t.Fatalf("ValidateServer rejected uint32 max H value: %v", err)
	}
}

func TestGenerateObfuscation20RandomValidAndMobile(t *testing.T) {
	a, err := GenerateObfuscation20("mobile")
	if err != nil {
		t.Fatalf("GenerateObfuscation20 mobile: %v", err)
	}
	b, err := GenerateObfuscation20("mobile")
	if err != nil {
		t.Fatalf("GenerateObfuscation20 mobile second: %v", err)
	}
	if a == b {
		t.Fatal("two generated mobile presets are identical; want random values")
	}
	if a.Jc != 3 || a.Jmin < 30 || a.Jmin > 50 || a.Jmax < a.Jmin+20 || a.Jmax > a.Jmin+80 {
		t.Fatalf("mobile params out of expected range: %+v", a)
	}
	s := DefaultServer("awg0", 51820)
	s.Obfuscation20 = a
	s.PrivateKey = "SERVER_PRIVATE"
	s.PublicKey = "SERVER_PUBLIC"
	if err := ValidateServer(s); err != nil {
		t.Fatalf("generated params invalid: %v", err)
	}
}

func TestRenderConfigsIPv4OnlyAndRedaction(t *testing.T) {
	server := DefaultServer("awg0", 51820)
	server.PrivateKey = "SERVER_PRIVATE"
	server.PublicKey = "SERVER_PUBLIC"
	server.Endpoint = "vpn.example.test"
	client := Client{
		ID:                  "client-1",
		Email:               "u@example.test",
		PrivateKey:          "CLIENT_PRIVATE",
		PublicKey:           "CLIENT_PUBLIC",
		PresharedKey:        "PSK",
		IPv4Address:         "10.66.66.2/32",
		AllowedIPs:          "10.66.66.2/32",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
		Enable:              true,
	}
	cfg, err := RenderServerConfig(server, []Client{client})
	if err != nil {
		t.Fatalf("RenderServerConfig: %v", err)
	}
	clientCfg, err := RenderClientConfig(server, client)
	if err != nil {
		t.Fatalf("RenderClientConfig: %v", err)
	}
	if strings.Contains(cfg, "::") || strings.Contains(clientCfg, "::") {
		t.Fatalf("rendered IPv6 content despite IPv6 out of scope:\n%s\n%s", cfg, clientCfg)
	}
	for _, want := range []string{"PostUp = iptables -C FORWARD", "iptables -t nat -A POSTROUTING -s 10.66.66.0/24 -j MASQUERADE", "PostDown = iptables -D FORWARD", "|| true", "Jc = ", "S3 = ", "S4 = ", "H1 = ", "I1 = ", "Endpoint = vpn.example.test:51820"} {
		if !strings.Contains(cfg+"\n"+clientCfg, want) {
			t.Fatalf("missing %q in rendered config", want)
		}
	}
	status := SafeStatus{
		Backend: BackendDocker,
		State:   StateRunning,
		Peers: []PeerStatus{{
			ClientID: "client-1", Enabled: true, LastHandshakeUnix: 1,
		}},
	}
	redacted, err := MarshalSafeStatus(status)
	if err != nil {
		t.Fatalf("MarshalSafeStatus: %v", err)
	}
	for _, secret := range []string{"SERVER_PRIVATE", "CLIENT_PRIVATE", "PSK", "PrivateKey", "PresharedKey"} {
		if strings.Contains(string(redacted), secret) {
			t.Fatalf("safe status leaked %q: %s", secret, redacted)
		}
	}
}
