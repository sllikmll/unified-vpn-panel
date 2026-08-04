package protocolconnections

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ProtocolConnection{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestParseProtocolFixtures(t *testing.T) {
	fixtures := []struct {
		protocol string
		name     string
		raw      string
		want     []string
	}{
		{
			protocol: "wireguard",
			name:     "wg-main",
			raw:      "[Interface]\nPrivateKey = priv\nAddress = 10.0.0.2/32, fd00::2/128\nDNS = 1.1.1.1\nMTU = 1280\n[Peer]\nPublicKey = pub\nPresharedKey = psk\nEndpoint = vpn.example.com:51820\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\n",
			want:     []string{"type: wireguard", "private-key: priv", "pre-shared-key: psk", "allowed-ips:"},
		},
		{
			protocol: "amnezia",
			name:     "awg",
			raw:      "[Interface]\nPrivateKey = priv\nAddress = 10.0.0.2/32\nJc = 4\nJmin = 40\nS1 = abc\n[Peer]\nPublicKey = pub\nEndpoint = awg.example.com:51820\n",
			want:     []string{"type: wireguard", "amnezia-wg-option:", "jc: 4", "s1: abc"},
		},
		{
			protocol: "hysteria2",
			name:     "hy",
			raw:      "hy2://secret@hy.example.com:443?obfs=salamander&obfs-password=obfs&sni=sni.example.com#ignored",
			want:     []string{"type: hysteria2", "password: secret", "obfs: salamander", "obfs-password: obfs"},
		},
		{
			protocol: "vless",
			name:     "vl",
			raw:      "vless://11111111-1111-1111-1111-111111111111@v.example.com:443?security=reality&type=xhttp&pbk=pub&sid=28000000&fp=chrome&path=%2Fx&host=h.example.com&flow=xtls-rprx-vision-udp443#old",
			want:     []string{"type: vless", "flow: xtls-rprx-vision", "reality-opts:", "short-id: '28000000'", "xhttp-opts:"},
		},
		{
			protocol: "trojan",
			name:     "tr",
			raw:      "trojan://pass@tr.example.com:443?security=tls&type=ws&path=%2Fws&host=cdn.example.com#old",
			want:     []string{"type: trojan", "password: pass", "ws-opts:", "Host: cdn.example.com"},
		},
		{
			protocol: "mieru",
			name:     "mi",
			raw:      "mieru://user:pass@mi.example.com:2999#old",
			want:     []string{"Mieru connection mi is stored", "injection is disabled"},
		},
		{
			protocol: "naiveproxy",
			name:     "naive",
			raw:      "naive+https://user:pass@naive.example.com:443#old",
			want:     []string{"type: http", "username: user", "password: pass", "tls: true"},
		},
		{
			protocol: "vmess",
			name:     "vm",
			raw:      "vmess://eyJwcyI6Im9sZCIsImFkIjoidm0uZXhhbXBsZS5jb20iLCJwb3J0Ijo0NDMsImlkIjoiMjIyMjIyMjItMjIyMi0yMjIyLTIyMjItMjIyMjIyMjIyMjIyIiwiYWlkIjowLCJuZXQiOiJ3cyIsInRscyI6InRscyIsInBhdGgiOiIvd3MiLCJob3N0IjoiY2RuLmV4YW1wbGUuY29tIn0",
			want:     []string{"type: vmess", "uuid: 22222222-2222-2222-2222-222222222222", "ws-opts:"},
		},
		{
			protocol: "shadowsocks",
			name:     "ss",
			raw:      "ss://YWVzLTI1Ni1nY206cGFzcw@ss.example.com:8388#old",
			want:     []string{"type: ss", "cipher: aes-256-gcm", "password: pass"},
		},
	}

	for _, tt := range fixtures {
		t.Run(tt.protocol, func(t *testing.T) {
			conn, err := ParseConnection(tt.protocol, tt.raw, tt.name)
			if err != nil {
				t.Fatalf("ParseConnection: %v", err)
			}
			if conn.Protocol != tt.protocol || conn.Name != tt.name {
				t.Fatalf("identity = %s/%s", conn.Protocol, conn.Name)
			}
			for _, want := range tt.want {
				if !strings.Contains(conn.MihomoYAML, want) {
					t.Fatalf("yaml missing %q:\n%s", want, conn.MihomoYAML)
				}
			}
			if conn.MihomoJSON != "" {
				var decoded map[string]any
				if err := json.Unmarshal([]byte(conn.MihomoJSON), &decoded); err != nil {
					t.Fatalf("mihomo json invalid: %v", err)
				}
			}
		})
	}
}

func TestServiceRedactsListAndRevealsExplicitly(t *testing.T) {
	svc := NewService(testDB(t))
	conn, replaced, err := svc.Import(ImportRequest{
		Protocol: "trojan",
		Name:     "secret-node",
		Content:  "trojan://very-secret@tr.example.com:443#n",
	})
	if err != nil || replaced {
		t.Fatalf("Import err=%v replaced=%v", err, replaced)
	}
	list, err := svc.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Connections) != 1 {
		t.Fatalf("connections=%d", len(list.Connections))
	}
	payload, _ := json.Marshal(list.Connections[0])
	if strings.Contains(string(payload), "very-secret") || strings.Contains(string(payload), "rawSource") {
		t.Fatalf("list leaked secret/raw: %s", payload)
	}
	revealed, err := svc.Get(conn.Id, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(revealed.RawSource, "very-secret") {
		t.Fatalf("explicit reveal did not include raw source")
	}
}

func TestServiceDuplicateNameRejected(t *testing.T) {
	svc := NewService(testDB(t))
	_, _, err := svc.Import(ImportRequest{Protocol: "trojan", Name: "dup", Content: "trojan://a@one.example.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Import(ImportRequest{Protocol: "trojan", Name: "dup", Content: "trojan://b@two.example.com:443"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestMalformedInputRejected(t *testing.T) {
	cases := []struct {
		protocol string
		raw      string
	}{
		{protocol: "wireguard", raw: "[Interface]\nPrivateKey = only-private\n"},
		{protocol: "vless", raw: "vless://missing-host"},
		{protocol: "naiveproxy", raw: "https://example.com/no-credentials"},
		{protocol: "shadowsocks", raw: "ss://not-base64"},
		{protocol: "mieru", raw: "mieru://"},
		{protocol: "mieru", raw: "mieru://%zz"},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			if _, err := ParseConnection(tc.protocol, tc.raw, "bad"); err == nil {
				t.Fatalf("expected malformed input to be rejected")
			}
		})
	}
}

func TestParseConnectionRejectsExplicitUnsupportedProtocol(t *testing.T) {
	_, err := ParseConnection("not-a-protocol", "trojan://password@example.com:443", "bad")
	if err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("explicit unsupported protocol must be rejected, got %v", err)
	}
}

func TestParseAmneziaDataURLSchemes(t *testing.T) {
	config := "[Interface]\nPrivateKey = private-key\nAddress = 10.0.0.2/32\nJc = 4\nJmin = 40\nJmax = 70\nS1 = 0\nS2 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n[Peer]\nPublicKey = public-key\nEndpoint = vpn.example.com:51820\nAllowedIPs = 0.0.0.0/0\n"
	payload := base64.RawURLEncoding.EncodeToString([]byte(config))
	for _, scheme := range []string{"awg://", "amneziawg://"} {
		t.Run(scheme, func(t *testing.T) {
			conn, err := ParseConnection("amnezia", scheme+payload, "awg")
			if err != nil {
				t.Fatalf("parse %s: %v", scheme, err)
			}
			if !strings.Contains(conn.MihomoYAML, "amnezia-wg-option:") {
				t.Fatalf("AWG options missing from YAML:\n%s", conn.MihomoYAML)
			}
		})
	}
}

func TestParseNaiveRequiresPassword(t *testing.T) {
	_, err := ParseConnection("naiveproxy", "naive+https://user@naive.example.com:443", "naive")
	if err == nil {
		t.Fatal("NaiveProxy URI without password must be rejected")
	}
}

func TestRedactHysteriaObfsPassword(t *testing.T) {
	redacted := Redact("- name: hy\n  password: auth-secret\n  obfs-password: obfs-secret\n")
	if strings.Contains(redacted, "auth-secret") || strings.Contains(redacted, "obfs-secret") {
		t.Fatalf("redaction leaked Hysteria2 credentials: %s", redacted)
	}
}

func TestMieruProtocolLabelIsSpelledCorrectly(t *testing.T) {
	for _, spec := range Protocols {
		if spec.Id == "mieru" {
			if spec.Label != "Mieru" {
				t.Fatalf("Mieru label = %q", spec.Label)
			}
			return
		}
	}
	t.Fatal("Mieru protocol missing")
}

func TestImportRotatedCredentialsReplacesExistingConnection(t *testing.T) {
	svc := NewService(testDB(t))
	first, replaced, err := svc.Import(ImportRequest{Protocol: "trojan", Name: "edge", Content: "trojan://first-secret@example.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Fatal("first import reported replacement")
	}
	rotated, replaced, err := svc.Import(ImportRequest{Protocol: "trojan", Name: "edge", Content: "trojan://rotated-secret@example.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("credential rotation did not replace existing connection")
	}
	if first.Id != rotated.Id {
		t.Fatalf("credential rotation changed connection id: %q != %q", first.Id, rotated.Id)
	}
	var count int64
	if err := svc.db.Model(&model.ProtocolConnection{}).Where("name = ?", "edge").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("connection count after credential rotation = %d", count)
	}
}

func TestImportRotatedCredentialsPreservesOperationalState(t *testing.T) {
	svc := NewService(testDB(t))
	created, _, err := svc.Import(ImportRequest{
		Protocol:  "trojan",
		Name:      "edge-state",
		Content:   "trojan://first-secret@example.com:443",
		Selectors: []string{"GLOBAL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := svc.Update(created.Id, UpdateRequest{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	rotated, replaced, err := svc.Import(ImportRequest{
		Protocol: "trojan",
		Name:     "edge-state",
		Content:  "trojan://rotated-secret@example.com:443",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("credential rotation did not replace existing connection")
	}
	if rotated.Enabled {
		t.Fatal("credential rotation re-enabled a disabled connection")
	}
	if len(rotated.Selectors) != 1 || rotated.Selectors[0] != "GLOBAL" {
		t.Fatalf("credential rotation changed selectors: %#v", rotated.Selectors)
	}
}

func TestManagedBlockAndRoundTripExport(t *testing.T) {
	svc := NewService(testDB(t))
	if _, _, err := svc.Import(ImportRequest{Protocol: "trojan", Name: "tr", Content: "trojan://pass@tr.example.com:443", Selectors: []string{"GLOBAL"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Import(ImportRequest{Protocol: "mieru", Name: "mi", Content: "mieru://u:p@mi.example.com:2999", Selectors: []string{"GLOBAL"}}); err != nil {
		t.Fatal(err)
	}
	block, err := svc.ManagedBlock()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "# unified-managed-proxies:start") || !strings.Contains(block, "type: trojan") {
		t.Fatalf("bad block:\n%s", block)
	}
	if strings.Contains(block, "Mieru connection") {
		t.Fatalf("mieru placeholder should not be injected:\n%s", block)
	}
	exported, err := svc.ExportYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported, "proxies:") || !strings.Contains(exported, "type: trojan") {
		t.Fatalf("bad export:\n%s", exported)
	}
}

func TestProtocolSecretBoundariesAcrossGeneratedCredentialKeys(t *testing.T) {
	svc := NewService(testDB(t))
	fixtures := []struct {
		protocol string
		name     string
		content  string
		secrets  []string
	}{
		{
			protocol: "naiveproxy",
			name:     "naive-boundary",
			content:  "naive+https://naive-user-secret:naive-password-secret@naive.example.com:443",
			secrets:  []string{"naive-user-secret", "naive-password-secret"},
		},
		{
			protocol: "vless",
			name:     "vless-boundary",
			content:  "vless://aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee@vless.example.com:443?security=tls&type=tcp",
			secrets:  []string{"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},
		},
		{
			protocol: "hysteria2",
			name:     "hysteria-boundary",
			content:  "hy2://hysteria-password-secret@hy.example.com:443?obfs=salamander&obfs-password=hysteria-obfs-secret",
			secrets:  []string{"hysteria-password-secret", "hysteria-obfs-secret"},
		},
		{
			protocol: "wireguard",
			name:     "wireguard-boundary",
			content:  "[Interface]\nPrivateKey = wireguard-private-secret\nAddress = 10.0.0.2/32\n[Peer]\nPublicKey = public-key\nPresharedKey = wireguard-psk-secret\nEndpoint = wg.example.com:51820\nAllowedIPs = 0.0.0.0/0\n",
			secrets:  []string{"wireguard-private-secret", "wireguard-psk-secret"},
		},
	}

	ids := make(map[string]string, len(fixtures))
	allSecrets := make([]string, 0, 7)
	for _, fixture := range fixtures {
		view, _, err := svc.Import(ImportRequest{
			Protocol:  fixture.protocol,
			Name:      fixture.name,
			Content:   fixture.content,
			Selectors: []string{"GLOBAL"},
		})
		if err != nil {
			t.Fatalf("import %s: %v", fixture.protocol, err)
		}
		ids[fixture.name] = view.Id
		allSecrets = append(allSecrets, fixture.secrets...)
		assertNoSecrets(t, "import response", view, fixture.secrets)
	}

	list, err := svc.List("")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, "list", list, allSecrets)

	preview, err := svc.ManagedBlockRedacted()
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, "preview", preview, allSecrets)
	if !strings.Contains(preview, "<redacted>") {
		t.Fatalf("redacted managed block does not contain redaction marker:\n%s", preview)
	}

	exported, err := svc.ExportYAML()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range allSecrets {
		if !strings.Contains(exported, secret) {
			t.Fatalf("explicit export unexpectedly hid credential %q", secret)
		}
	}

	for _, fixture := range fixtures {
		revealed, err := svc.Get(ids[fixture.name], true)
		if err != nil {
			t.Fatalf("reveal %s: %v", fixture.protocol, err)
		}
		for _, secret := range fixture.secrets {
			if !strings.Contains(revealed.RawSource+revealed.MihomoYAML, secret) {
				t.Fatalf("explicit reveal unexpectedly hid %s credential %q", fixture.protocol, secret)
			}
		}
	}
}

func assertNoSecrets(t *testing.T, boundary string, value any, secrets []string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", boundary, err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("%s leaked credential %q", boundary, secret)
		}
	}
}
