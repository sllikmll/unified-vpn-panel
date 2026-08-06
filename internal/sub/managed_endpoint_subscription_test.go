package sub

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/protocolconnections"
)

func initManagedSubDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XUI_MANAGED_SECRET_KEY_FILE", filepath.Join(dir, "managed-secret.master"))
	if err := database.InitDB(filepath.Join(dir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestGetSubsIncludesBoundManagedClientsAndPreservesLegacyOnlyOutput(t *testing.T) {
	initManagedSubDB(t)
	db := database.GetDB()

	rec := model.ClientRecord{Email: "alice@example.test", SubID: "sub-all", UUID: "11111111-1111-1111-1111-111111111111", Enable: true}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	legacy := model.Inbound{UserId: 1, Remark: "legacy", Tag: "legacy", Listen: "legacy.example.test", Port: 443, Protocol: model.VLESS, Enable: true, Settings: `{"clients":[{"email":"alice@example.test","id":"11111111-1111-1111-1111-111111111111","subId":"sub-all","enable":true}],"decryption":"none"}`, StreamSettings: `{"network":"tcp","security":"none"}`}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: legacy.Id}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewSubService("")
	before, _, _, _, err := svc.GetSubs("sub-all", "sub.example.test")
	if err != nil {
		t.Fatalf("legacy GetSubs: %v", err)
	}
	beforeBody := strings.Join(before, "\n")

	createManagedEndpointForSub(t, model.RuntimeAmneziaWG, rec.SubID, "awg-sub")
	createManagedEndpointForSub(t, model.RuntimeAmneziaWG, rec.SubID, "awg-sub-remote")
	createManagedEndpointForSub(t, model.RuntimeMieru, rec.SubID, "mieru-sub")
	createManagedEndpointForSub(t, model.RuntimeNaiveProxy, rec.SubID, "naive-sub")

	after, _, _, _, err := svc.GetSubs("sub-all", "sub.example.test")
	if err != nil {
		t.Fatalf("managed GetSubs: %v", err)
	}
	body := strings.Join(after, "\n")
	if !strings.Contains(body, beforeBody) {
		t.Fatalf("legacy output changed or disappeared\nbefore:\n%s\nafter:\n%s", beforeBody, body)
	}
	for _, want := range []string{"awg://", "mierus://", "naive+https://"} {
		if !strings.Contains(body, want) {
			t.Fatalf("managed subscription missing %s in:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "@mieru-node.example.test?") {
		t.Fatalf("managed Mieru subscription did not use endpoint public host: %s", body)
	}
	var awgLink string
	for _, link := range after {
		if strings.HasPrefix(link, "awg://") {
			awgLink = link
			break
		}
	}
	if awgLink == "" {
		t.Fatal("missing AWG link")
	}
	payload := strings.TrimPrefix(strings.Split(strings.Split(awgLink, "#")[0], "?")[0], "awg://")
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("AWG payload is not raw-url base64 config: %v", err)
	}
	cfg := string(decoded)
	for _, want := range []string{"[Interface]", "PrivateKey = CLIENT_PRIVATE", "Address = 10.66.66.2/32", "DNS = 1.1.1.1", "MTU = 1420", "Jc = 3", "Jmin = 40", "Jmax = 120", "S1 = 20", "S2 = 30", "S3 = 10", "S4 = 8", "H1 = 10-1000", "H2 = 1001-2000", "H3 = 2001-3000", "H4 = 3001-4000", "[Peer]", "PublicKey = SERVER_PUBLIC", "PresharedKey = CLIENT_PSK", "Endpoint = sub.example.test:51820", "AllowedIPs = 0.0.0.0/0", "PersistentKeepalive = 25"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("canonical AWG config missing %q:\n%s", want, cfg)
		}
	}
	conn, err := protocolconnections.ParseConnection("amnezia", awgLink, "awg-roundtrip")
	if err != nil {
		t.Fatalf("ParseConnection(amnezia): %v", err)
	}
	for _, want := range []string{"type: wireguard", "amnezia-wg-option:", "jc: 3", "jmin: 40", "s1: 20", "h1: 10-1000"} {
		if !strings.Contains(conn.MihomoYAML, want) {
			t.Fatalf("round-trip Mihomo YAML missing %q:\n%s", want, conn.MihomoYAML)
		}
	}

	clash, _, err := NewSubClashService(false, "", svc).GetClash("sub-all", "sub.example.test")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	for _, want := range []string{"type: vless", "type: wireguard", "amnezia-wg-option:", "name: awg-sub", "name: awg-sub-remote", "type: mieru", "name: mieru-sub", "username: user-mieru-sub", "type: http", "name: naive-sub", "username: user-naive-sub", "password: password-naive-sub"} {
		if !strings.Contains(clash, want) {
			t.Fatalf("Clash output missing %q:\n%s", want, clash)
		}
	}
	for _, bad := range []string{"name: alice@example.test", "name: alice@example.test-2"} {
		if strings.Contains(clash, bad) {
			t.Fatalf("Clash output used client email instead of managed endpoint name %q:\n%s", bad, clash)
		}
	}

	other, _, _, _, err := svc.GetSubs("other-sub", "sub.example.test")
	if err != nil {
		t.Fatalf("other GetSubs: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("managed clients leaked to unrelated subscriber: %#v", other)
	}
}

func createManagedEndpointForSub(t *testing.T, kind model.RuntimeKind, subID, tag string) {
	t.Helper()
	db := database.GetDB()
	endpoint := model.ManagedEndpoint{
		UserId: 1, RuntimeKind: kind, Protocol: model.ManagedProtocol(kind), Tag: tag,
		Remark: tag, Listen: "", Port: managedTestPort(kind), Enable: true, Status: model.EndpointActive,
	}
	if kind == model.RuntimeMieru {
		endpoint.Listen = "mieru-node.example.test"
	}
	switch kind {
	case model.RuntimeAmneziaWG:
		endpoint.DesiredConfig = `{"server":{"enable":true,"interfaceName":"awg0","listenPort":51820,"mtu":1420,"privateKey":"managed-secret://managed_endpoint/0/server.privateKey","publicKey":"SERVER_PUBLIC","ipv4Address":"10.66.66.1/24","ipv4Pool":"10.66.66.0/24","dns":"1.1.1.1","endpoint":"vpn.example.test","jc":3,"jmin":40,"jmax":120,"s1":20,"s2":30,"s3":10,"s4":8,"h1":"10-1000","h2":"1001-2000","h3":"2001-3000","h4":"3001-4000"},"clients":[]}`
	case model.RuntimeMieru:
		endpoint.DesiredConfig = `{"portBindings":[{"port":2999,"protocol":"TCP"}],"mtu":1280}`
	case model.RuntimeNaiveProxy:
		endpoint.DesiredConfig = `{"endpoint":{"domain":"naive.example.test","listenIp":"127.0.0.1","port":443},"users":[]}`
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	seal := service.NewManagedSecretEnvelopeService(nil)
	if kind == model.RuntimeAmneziaWG {
		secret, err := seal.Encrypt(service.ManagedSecretAAD{OwnerType: "managed_endpoint", OwnerId: endpoint.Id, SecretKind: "server.privateKey"}, []byte("SERVER_PRIVATE"))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&secret).Error; err != nil {
			t.Fatal(err)
		}
	}
	var rec model.ClientRecord
	if err := db.First(&rec, "sub_id = ?", subID).Error; err != nil {
		t.Fatal(err)
	}
	client := model.ManagedEndpointClient{EndpointId: endpoint.Id, ClientId: rec.Id, SubID: rec.SubID, Email: rec.Email, Enable: true, State: model.EndpointClientApplied, PublicIdentity: "user-" + tag, Address: "10.66.66.2/32"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	var secrets []model.ManagedSecret
	switch kind {
	case model.RuntimeAmneziaWG:
		for secretKind, value := range map[string]string{"privateKey": "CLIENT_PRIVATE", "publicKey": "CLIENT_PUBLIC", "presharedKey": "CLIENT_PSK"} {
			secret, err := seal.Encrypt(service.ManagedSecretAAD{OwnerType: "managed_endpoint_client", OwnerId: client.Id, SecretKind: secretKind}, []byte(value))
			if err != nil {
				t.Fatal(err)
			}
			secrets = append(secrets, secret)
		}
	case model.RuntimeMieru, model.RuntimeNaiveProxy:
		secret, err := seal.Encrypt(service.ManagedSecretAAD{OwnerType: "managed_endpoint_client", OwnerId: client.Id, SecretKind: "password"}, []byte("password-"+tag))
		if err != nil {
			t.Fatal(err)
		}
		secrets = append(secrets, secret)
	}
	if len(secrets) > 0 {
		if err := db.Create(&secrets).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func managedTestPort(kind model.RuntimeKind) int {
	switch kind {
	case model.RuntimeAmneziaWG:
		return 51820
	case model.RuntimeMieru:
		return 2999
	default:
		return 443
	}
}
