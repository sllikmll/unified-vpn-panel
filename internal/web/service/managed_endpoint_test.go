package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/mieru"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/provisioner"
)

type managedTestProvider struct {
	driver driver.Driver
	prov   provisioner.Provisioner
	err    error
}

func (p managedTestProvider) DriverForEndpoint(model.ManagedEndpoint) (driver.Driver, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.driver, nil
}

func (p managedTestProvider) ProvisionerForEndpoint(model.ManagedEndpoint) (provisioner.Provisioner, error) {
	if p.prov != nil {
		return p.prov, p.err
	}
	return managedTestProvisioner{}, p.err
}

type managedTestProvisioner struct{}

func (managedTestProvisioner) Plan(kind model.RuntimeKind) provisioner.Plan {
	return provisioner.Plan{RuntimeKind: kind, Supported: true, Version: "test", ArtifactRef: "test-ref"}
}

func (managedTestProvisioner) Install(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "running"}, nil
}

func (managedTestProvisioner) Update(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "running"}, nil
}

func (managedTestProvisioner) Uninstall(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "removed"}, nil
}

type managedBlockingProvisioner struct{}

func (managedBlockingProvisioner) Plan(kind model.RuntimeKind) provisioner.Plan {
	return provisioner.Plan{RuntimeKind: kind, Blocked: true, Reason: "missing immutable GHCR image digest"}
}

func (managedBlockingProvisioner) Install(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "blocked"}, provisioner.ErrArtifactBlocked
}

func (managedBlockingProvisioner) Update(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "blocked"}, provisioner.ErrArtifactBlocked
}

func (managedBlockingProvisioner) Uninstall(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "removed"}, nil
}

type managedTestDriver struct {
	kind        model.RuntimeKind
	fail        error
	stopCalls   *int
	deleteCalls *int
}

func (d managedTestDriver) Kind() model.RuntimeKind { return d.kind }
func (d managedTestDriver) Capabilities() driver.Capabilities {
	return driver.Capabilities{EndpointLifecycle: true, ClientCRUD: true, Detect: true, Status: true, Health: true}
}

func (d managedTestDriver) Create(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointActive}, d.fail
}

func (d managedTestDriver) Update(context.Context, *model.Inbound, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointActive}, d.fail
}

func (d managedTestDriver) Delete(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	if d.deleteCalls != nil {
		(*d.deleteCalls)++
	}
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointDeleted}, d.fail
}

func (d managedTestDriver) Enable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointActive}, d.fail
}

func (d managedTestDriver) Disable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointDisabled}, d.fail
}

func (d managedTestDriver) Stop(context.Context, *model.Inbound) error {
	if d.stopCalls != nil {
		(*d.stopCalls)++
	}
	return d.fail
}
func (d managedTestDriver) Restart(context.Context) error { return d.fail }
func (d managedTestDriver) Status(context.Context, *model.Inbound) (driver.StatusResult, error) {
	return driver.StatusResult{RuntimeKind: d.kind, Status: model.EndpointActive}, d.fail
}

func (d managedTestDriver) Detect(context.Context) (driver.DetectResult, error) {
	return driver.DetectResult{RuntimeKind: d.kind, Available: d.fail == nil}, d.fail
}

func (d managedTestDriver) Health(context.Context, *model.Inbound) (driver.HealthResult, error) {
	return driver.HealthResult{RuntimeKind: d.kind, Status: model.EndpointActive}, d.fail
}
func (d managedTestDriver) Clients() driver.ClientDriver { return managedTestClientDriver{} }

type managedTestClientDriver struct{}

func (managedTestClientDriver) Create(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, nil
}

func (managedTestClientDriver) Update(context.Context, *model.Inbound, string, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, nil
}

func (managedTestClientDriver) Delete(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, nil
}

func (managedTestClientDriver) Enable(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, nil
}

func (managedTestClientDriver) Disable(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, nil
}

func (managedTestClientDriver) Status(context.Context, *model.Inbound, string) (driver.ClientStatusResult, error) {
	return driver.ClientStatusResult{}, nil
}

func initManagedEndpointServiceDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestManagedEndpointCreateClientPersistsEncryptedSecretsOnly(t *testing.T) {
	initManagedEndpointServiceDB(t)
	key := strings.Repeat("k", 32)
	mutations := ManagedEndpointMutationService{
		Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeAmneziaWG}},
		Secrets: NewManagedSecretEnvelopeService(ManagedSecretStaticKeySource{Key: []byte(key), KeyID: "test-key"}),
	}
	enable := true
	view, err := mutations.Create(context.Background(), 1, ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeAmneziaWG,
		Protocol:    "amneziawg",
		Tag:         "awg-managed",
		Port:        51820,
		Enable:      &enable,
		AWG:         &ManagedAWGConfig{ServerPrivateKey: "SERVER_PRIVATE_SECRET", ServerPublicKey: "SERVER_PUBLIC"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if view == nil || view.Source != ManagedEndpointSourceManaged {
		t.Fatalf("view = %#v", view)
	}
	rec := model.ClientRecord{Email: "client@example.test", SubID: "sub-client", Enable: true}
	if err := database.GetDB().Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	client, err := mutations.CreateClient(context.Background(), 1, view.Id, ManagedEndpointClientCreateRequest{
		SubID:        "sub-client",
		PrivateKey:   "CLIENT_PRIVATE_SECRET",
		PublicKey:    "CLIENT_PUBLIC",
		PreSharedKey: "CLIENT_PSK_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if client.ClientId != rec.Id || client.Email != rec.Email {
		t.Fatalf("client binding = id %d email %q, want id %d email %q", client.ClientId, client.Email, rec.Id, rec.Email)
	}
	if client.Address != "10.66.66.2/32" {
		t.Fatalf("client address = %q", client.Address)
	}
	var endpoints []model.ManagedEndpoint
	if err := database.GetDB().Find(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(endpoints)
	for _, forbidden := range []string{"SERVER_PRIVATE_SECRET", "CLIENT_PRIVATE_SECRET", "CLIENT_PSK_SECRET"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("endpoint desired state leaked %q: %s", forbidden, raw)
		}
	}
	sqlDB, err := database.GetDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	var dump string
	if err := sqlDB.QueryRow("SELECT group_concat(CAST(ciphertext AS TEXT), '') FROM managed_secrets").Scan(&dump); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"SERVER_PRIVATE_SECRET", "CLIENT_PRIVATE_SECRET", "CLIENT_PSK_SECRET"} {
		if strings.Contains(dump, forbidden) {
			t.Fatalf("secret ciphertext leaked %q", forbidden)
		}
	}
}

func TestManagedEndpointSecretGenerationUsesNewestForRestartReconstruction(t *testing.T) {
	initManagedEndpointServiceDB(t)
	key := strings.Repeat("k", 32)
	mutations := ManagedEndpointMutationService{
		Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeAmneziaWG}},
		Secrets: NewManagedSecretEnvelopeService(ManagedSecretStaticKeySource{Key: []byte(key), KeyID: "test-key"}),
	}
	endpoint := model.ManagedEndpoint{
		UserId: 1, RuntimeKind: model.RuntimeAmneziaWG, Protocol: "amneziawg", Tag: "awg", Port: 51820, Enable: true, Status: model.EndpointActive,
		DesiredConfig: `{"server":{"enable":true,"interfaceName":"awg0","listenPort":51820,"mtu":1420,"privateKey":"managed-secret://managed_endpoint/1/server.privateKey","publicKey":"SERVER_PUBLIC","ipv4Address":"10.66.66.1/24","ipv4Pool":"10.66.66.0/24","dns":"1.1.1.1","endpoint":"vpn.example.test"},"clients":[]}`,
	}
	if err := database.GetDB().Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	serverSecret, err := mutations.encryptSecret("managed_endpoint", endpoint.Id, "server.privateKey", []byte("SERVER_PRIVATE"))
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertManagedSecrets(database.GetDB(), []model.ManagedSecret{serverSecret}); err != nil {
		t.Fatal(err)
	}
	client := model.ManagedEndpointClient{EndpointId: endpoint.Id, Email: "client@example.test", Enable: true, State: model.EndpointClientApplied, PublicIdentity: "client-1", Address: "10.66.66.2/32"}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	for _, gen := range []struct {
		kind  string
		value string
	}{
		{"privateKey", "OLD_PRIVATE"},
		{"publicKey", "OLD_PUBLIC"},
		{"presharedKey", "OLD_PSK"},
		{"privateKey", "NEW_PRIVATE"},
		{"publicKey", "NEW_PUBLIC"},
		{"presharedKey", "NEW_PSK"},
	} {
		secret, err := mutations.encryptSecret("managed_endpoint_client", client.Id, gen.kind, []byte(gen.value))
		if err != nil {
			t.Fatal(err)
		}
		if err := upsertManagedSecrets(database.GetDB(), []model.ManagedSecret{secret}); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := database.GetDB().Model(&model.ManagedSecret{}).Where("owner_type = ? AND owner_id = ? AND secret_kind = ?", "managed_endpoint_client", client.Id, "privateKey").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("historical privateKey generations retained = %d, want 2", count)
	}
	clients, err := mutations.awgClientsFromDB(database.GetDB(), endpoint.Id, awg.ClientDefaults{})
	if err != nil {
		t.Fatalf("awgClientsFromDB: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("clients = %d, want 1", len(clients))
	}
	if clients[0].PrivateKey != "NEW_PRIVATE" || clients[0].PublicKey != "NEW_PUBLIC" || clients[0].PresharedKey != "NEW_PSK" {
		t.Fatalf("client secrets = %#v, want newest generation", clients[0])
	}
	inbound, err := mutations.inboundFromDurable(endpoint)
	if err != nil {
		t.Fatalf("inboundFromDurable: %v", err)
	}
	if strings.Contains(inbound.Settings, "OLD_PRIVATE") || !strings.Contains(inbound.Settings, "NEW_PRIVATE") {
		t.Fatalf("runtime settings did not use newest generation: %s", inbound.Settings)
	}
}

func TestManagedEndpointSecretGenerationFallsBackOnLegacyUniqueIndex(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	if err := db.Exec("CREATE UNIQUE INDEX legacy_secret_owner ON managed_secrets(owner_type, owner_id, secret_kind)").Error; err != nil {
		t.Fatal(err)
	}
	mutations := ManagedEndpointMutationService{Secrets: NewManagedSecretEnvelopeService(ManagedSecretStaticKeySource{Key: []byte(strings.Repeat("k", 32)), KeyID: "test-key"})}
	for _, value := range []string{"old", "new"} {
		secret, err := mutations.encryptSecret("managed_endpoint_client", 9, "password", []byte(value))
		if err != nil {
			t.Fatal(err)
		}
		if err := upsertManagedSecrets(db, []model.ManagedSecret{secret}); err != nil {
			t.Fatalf("upsert %q: %v", value, err)
		}
	}
	rows, err := newestManagedSecrets(db, "managed_endpoint_client", 9, "password")
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := mutations.decryptSecret(rows["password"])
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "new" {
		t.Fatalf("legacy unique fallback plaintext = %q, want new", plaintext)
	}
}

func TestManagedEndpointClientCreateRejectsMissingUnknownAndDisabledSubBinding(t *testing.T) {
	initManagedEndpointServiceDB(t)
	mutations := ManagedEndpointMutationService{
		Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru}},
		Secrets: NewManagedSecretEnvelopeService(ManagedSecretStaticKeySource{Key: []byte(strings.Repeat("k", 32)), KeyID: "test-key"}),
	}
	view, err := mutations.Create(context.Background(), 1, ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeMieru,
		Protocol:    "mieru",
		Tag:         "mieru-managed",
		Port:        2999,
		Mieru:       &ManagedMieruConfig{Transport: "TCP"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, req := range []ManagedEndpointClientCreateRequest{
		{Password: "pw"},
		{SubID: "missing", Password: "pw"},
	} {
		if _, err := mutations.CreateClient(context.Background(), 1, view.Id, req); err == nil {
			t.Fatalf("CreateClient(%#v) succeeded without valid enabled subscription binding", req)
		}
	}
	disabled := model.ClientRecord{Email: "off@example.test", SubID: "disabled-sub", Enable: false}
	if err := database.GetDB().Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("id = ?", disabled.Id).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := mutations.CreateClient(context.Background(), 1, view.Id, ManagedEndpointClientCreateRequest{SubID: disabled.SubID, Password: "pw"}); err == nil {
		t.Fatal("CreateClient accepted disabled subscription client binding")
	}
}

func TestManagedEndpointClientResponseRedactsRuntimeMaterialAndShowsBinding(t *testing.T) {
	client := model.ManagedEndpointClient{
		Id: 1, EndpointId: 2, ClientId: 3, Email: "alice@example.test", SubID: "sub-alice",
		Enable: true, State: model.EndpointClientApplied, PublicIdentity: "runtime-user",
		Address: "10.0.0.2/32", CredentialRef: "managed-secret://x", ClientConfig: "raw", ObservedConfig: "observed",
	}
	raw, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{`"subId":"sub-alice"`, `"email":"alice@example.test"`, `"state":"applied"`, `"publicIdentity":"runtime-user"`, `"address":"10.0.0.2/32"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %s in %s", want, body)
		}
	}
	for _, forbidden := range []string{"CredentialRef", "credentialRef", "ClientConfig", "clientConfig", "ObservedConfig", "observedConfig", "raw", "managed-secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("managed client JSON leaked %q in %s", forbidden, body)
		}
	}
}

func TestManagedEndpointListClientsHydratesSubIDFromClientRecord(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	rec := model.ClientRecord{Email: "alice@example.test", SubID: "authoritative-sub", Enable: true}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru", Status: model.EndpointActive}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	client := model.ManagedEndpointClient{EndpointId: endpoint.Id, ClientId: rec.Id, Email: "stale@example.test", Enable: true, State: model.EndpointClientApplied, PublicIdentity: "user-1", Address: "10.0.0.2/32"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := (ManagedEndpointMutationService{}).ListClients(1, endpoint.Id)
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].SubID != rec.SubID || rows[0].Status != model.EndpointClientApplied || rows[0].Address != client.Address || rows[0].PublicIdentity != client.PublicIdentity {
		t.Fatalf("hydrated client = %#v", rows[0])
	}
}

func TestManagedEndpointSubscriptionURLsRespectEnableFlags(t *testing.T) {
	setupSettingTestDB(t)
	s := SettingService{}
	for key, value := range map[string]string{
		"subEnable":      "true",
		"subJsonEnable":  "false",
		"subClashEnable": "true",
		"subURI":         "https://sub.example/raw/",
		"subJsonURI":     "https://sub.example/json/",
		"subClashURI":    "https://sub.example/clash/",
	} {
		if err := s.saveSetting(key, value); err != nil {
			t.Fatalf("save %s: %v", key, err)
		}
	}
	urls := managedSubscriptionURLs("sub-1")
	if urls.Raw != "https://sub.example/raw/sub-1" || urls.Clash != "https://sub.example/clash/sub-1" {
		t.Fatalf("enabled subscription URLs = %#v", urls)
	}
	if urls.JSON != "" {
		t.Fatalf("disabled JSON subscription URL was returned: %#v", urls)
	}
	if err := s.saveSetting("subEnable", "false"); err != nil {
		t.Fatal(err)
	}
	if urls := managedSubscriptionURLs("sub-1"); urls.Raw != "" {
		t.Fatalf("disabled raw subscription URL was returned: %#v", urls)
	}
}

func TestManagedEndpointCreateFailsClosedWithoutRuntimeProvider(t *testing.T) {
	initManagedEndpointServiceDB(t)
	enable := true
	_, err := (ManagedEndpointMutationService{}).Create(context.Background(), 1, ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeMieru,
		Protocol:    "mieru",
		Tag:         "mieru-managed",
		Port:        2999,
		Enable:      &enable,
		Mieru:       &ManagedMieruConfig{},
	})
	if err == nil {
		t.Fatal("Create succeeded without a managed runtime provider")
	}
	var count int64
	if err := database.GetDB().Model(&model.ManagedEndpoint{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("endpoint row was created despite missing runtime provider: %d", count)
	}
}

func TestManagedEndpointUpdateFailureKeepsPreviousAppliedHash(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	oldDesired := `{"portBindings":[{"port":2999,"protocol":"TCP"}],"mtu":1200}`
	oldHash := fmt.Sprintf("%x", sha256.Sum256([]byte(oldDesired)))
	endpoint := model.ManagedEndpoint{
		UserId:           1,
		RuntimeKind:      model.RuntimeMieru,
		Protocol:         "mieru",
		Tag:              "mieru",
		Port:             2999,
		Enable:           true,
		Status:           model.EndpointActive,
		DesiredConfig:    oldDesired,
		ObservedConfig:   oldDesired,
		LastAppliedHash:  oldHash,
		LastObservedHash: oldHash,
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru, fail: errors.New("stderr secret token")}}}
	newMTU := 1300
	_, err := mutations.Update(context.Background(), 1, fmt.Sprintf("managed-%d", endpoint.Id), ManagedEndpointUpdateRequest{
		RuntimeKind: model.RuntimeMieru,
		Protocol:    "mieru",
		Mieru:       &ManagedMieruConfig{MTU: newMTU, Transport: "UDP"},
	})
	if err == nil {
		t.Fatal("Update succeeded despite runtime failure")
	}
	var got model.ManagedEndpoint
	if err := db.First(&got, endpoint.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != model.EndpointFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.LastAppliedHash != oldHash || got.LastObservedHash != oldHash || got.ObservedConfig != oldDesired {
		t.Fatalf("applied/observed changed on failure: hash=%q observedHash=%q observed=%s", got.LastAppliedHash, got.LastObservedHash, got.ObservedConfig)
	}
	if got.DesiredConfig == oldDesired {
		t.Fatal("attempted desired config was not persisted for retry")
	}
	if strings.Contains(got.LastError, "secret") || strings.Contains(got.LastError, "stderr") {
		t.Fatalf("unsafe runtime error leaked: %q", got.LastError)
	}
}

func TestManagedEndpointCreateIdempotencyHashConflict(t *testing.T) {
	initManagedEndpointServiceDB(t)
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru}}}
	req := ManagedEndpointCreateRequest{
		RuntimeKind:    model.RuntimeMieru,
		Protocol:       "mieru",
		Tag:            "idem-mieru",
		Port:           2999,
		IdempotencyKey: "idem-1",
		Mieru:          &ManagedMieruConfig{Transport: "TCP"},
	}
	first, err := mutations.Create(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second, err := mutations.Create(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("replayed Create: %v", err)
	}
	if first.Id != second.Id {
		t.Fatalf("replay returned %q, want %q", second.Id, first.Id)
	}
	req.Tag = "changed"
	_, err = mutations.Create(context.Background(), 1, req)
	if !errors.Is(err, ErrManagedIdempotencyConflict) {
		t.Fatalf("changed request error = %v, want idempotency conflict", err)
	}
	var count int64
	if err := database.GetDB().Model(&model.ManagedEndpoint{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("endpoint count = %d, want 1", count)
	}
}

func TestManagedEndpointInstallPlansReturnsManagedKinds(t *testing.T) {
	plans := ManagedEndpointService{}.InstallPlans()
	if len(plans) != 3 {
		t.Fatalf("plans len = %d, want 3", len(plans))
	}
	seen := map[model.RuntimeKind]bool{}
	for _, plan := range plans {
		seen[plan.RuntimeKind] = true
	}
	for _, kind := range []model.RuntimeKind{model.RuntimeAmneziaWG, model.RuntimeMieru, model.RuntimeNaiveProxy} {
		if !seen[kind] {
			t.Fatalf("missing install plan for %s in %+v", kind, plans)
		}
	}
}

func TestManagedEndpointInstallActionBlockedByImmutableArtifact(t *testing.T) {
	initManagedEndpointServiceDB(t)
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeAmneziaWG, Protocol: "amneziawg", Tag: "awg", Port: 51820, Enable: true, Status: model.EndpointActive, DesiredConfig: `{"server":{"interfaceName":"awg0","listenPort":51820,"enable":true}}`}
	if err := database.GetDB().Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeAmneziaWG}, prov: managedBlockingProvisioner{}}}
	_, _, err := mutations.EndpointAction(context.Background(), 1, fmt.Sprintf("managed-%d", endpoint.Id), "install", "install-blocked")
	if !errors.Is(err, ErrManagedRuntimeArtifactBlocked) {
		t.Fatalf("install err = %v, want artifact blocked", err)
	}
}

func TestManagedEndpointInstallActionAppliesDesiredAfterProvision(t *testing.T) {
	initManagedEndpointServiceDB(t)
	desired := `{"portBindings":[{"port":2999,"protocol":"TCP"}],"users":[{"name":"u","password":"p"}]}`
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru", Port: 2999, Enable: true, Status: model.EndpointActive, DesiredConfig: desired}
	if err := database.GetDB().Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru}, prov: managedTestProvisioner{}}}
	view, _, err := mutations.EndpointAction(context.Background(), 1, fmt.Sprintf("managed-%d", endpoint.Id), "install", "install-ok")
	if err != nil {
		t.Fatalf("install action: %v", err)
	}
	if view == nil || view.Status != model.EndpointActive {
		t.Fatalf("view = %+v, want active", view)
	}
}

type managedTxProvisioner struct {
	tx *managedTx
}

func (p managedTxProvisioner) Plan(kind model.RuntimeKind) provisioner.Plan {
	return provisioner.Plan{RuntimeKind: kind, Supported: true, Version: "test", ArtifactRef: "test-ref"}
}

func (p managedTxProvisioner) Install(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "running"}, nil
}

func (p managedTxProvisioner) Update(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "running"}, nil
}

func (p managedTxProvisioner) Uninstall(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{State: "removed"}, nil
}

func (p managedTxProvisioner) BeginInstall(context.Context, model.RuntimeKind) (provisioner.Transaction, error) {
	return p.tx, nil
}

func (p managedTxProvisioner) BeginUpdate(context.Context, model.RuntimeKind) (provisioner.Transaction, error) {
	return p.tx, nil
}

type managedTx struct {
	commits   int
	rollbacks int
}

func (t *managedTx) Result() provisioner.Result {
	return provisioner.Result{RuntimeKind: model.RuntimeMieru, ArtifactRef: "test-ref", Version: "test", State: "running"}
}

func (t *managedTx) Commit(context.Context) error {
	t.commits++
	return nil
}

func (t *managedTx) Rollback(context.Context) (provisioner.Result, error) {
	t.rollbacks++
	return provisioner.Result{RuntimeKind: model.RuntimeMieru, ArtifactRef: "test-ref", Version: "test", State: "rolled_back", RolledBack: true}, nil
}

func TestManagedEndpointCreateInstallsBeforeApplyAndCommits(t *testing.T) {
	initManagedEndpointServiceDB(t)
	tx := &managedTx{}
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{
		driver: managedTestDriver{kind: model.RuntimeMieru},
		prov:   managedTxProvisioner{tx: tx},
	}}
	enable := true
	view, err := mutations.Create(context.Background(), 1, ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeMieru,
		Protocol:    "mieru",
		Tag:         "mieru-create",
		Port:        2999,
		Enable:      &enable,
		Mieru:       &ManagedMieruConfig{MTU: 1400, PortBindings: []ManagedMieruPortBinding{{Port: 2999, Protocol: "TCP"}}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if view == nil || view.Status != model.EndpointActive {
		t.Fatalf("view = %+v, want active", view)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("tx commits=%d rollbacks=%d, want one commit", tx.commits, tx.rollbacks)
	}
}

func TestManagedEndpointInstallApplyFailureRollsBackProvisioner(t *testing.T) {
	initManagedEndpointServiceDB(t)
	desired := `{"portBindings":[{"port":2999,"protocol":"TCP"}],"users":[{"name":"u","password":"p"}]}`
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru", Port: 2999, Enable: true, Status: model.EndpointActive, DesiredConfig: desired}
	if err := database.GetDB().Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	tx := &managedTx{}
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru, fail: errors.New("apply failed")}, prov: managedTxProvisioner{tx: tx}}}
	_, _, err := mutations.EndpointAction(context.Background(), 1, fmt.Sprintf("managed-%d", endpoint.Id), "install", "install-rollback")
	if err == nil {
		t.Fatal("install action succeeded despite apply failure")
	}
	if tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("tx commits=%d rollbacks=%d, want rollback only", tx.commits, tx.rollbacks)
	}
	var stored model.ManagedEndpoint
	if err := database.GetDB().First(&stored, endpoint.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.EndpointRolledBack {
		t.Fatalf("status = %s, want rolled_back", stored.Status)
	}
}

func TestManagedEndpointUpdateApplySuccessCommitsProvisioner(t *testing.T) {
	initManagedEndpointServiceDB(t)
	desired := `{"portBindings":[{"port":2999,"protocol":"TCP"}],"users":[{"name":"u","password":"p"}]}`
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru", Port: 2999, Enable: true, Status: model.EndpointActive, DesiredConfig: desired}
	if err := database.GetDB().Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	tx := &managedTx{}
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru}, prov: managedTxProvisioner{tx: tx}}}
	if _, _, err := mutations.EndpointAction(context.Background(), 1, fmt.Sprintf("managed-%d", endpoint.Id), "update", "update-commit"); err != nil {
		t.Fatalf("update action: %v", err)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("tx commits=%d rollbacks=%d, want commit only", tx.commits, tx.rollbacks)
	}
}

func TestManagedEndpointUninstallStopsWithoutDriverDelete(t *testing.T) {
	initManagedEndpointServiceDB(t)
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru", Port: 2999, Enable: true, Status: model.EndpointActive, DesiredConfig: `{"portBindings":[{"port":2999,"protocol":"TCP"}],"users":[]}`}
	if err := database.GetDB().Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	stopCalls := 0
	deleteCalls := 0
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru, stopCalls: &stopCalls, deleteCalls: &deleteCalls}, prov: managedTestProvisioner{}}}
	if _, _, err := mutations.EndpointAction(context.Background(), 1, fmt.Sprintf("managed-%d", endpoint.Id), "uninstall", "uninstall-stop"); err != nil {
		t.Fatalf("uninstall action: %v", err)
	}
	if stopCalls != 1 || deleteCalls != 0 {
		t.Fatalf("stopCalls=%d deleteCalls=%d, want stop only before provisioner uninstall", stopCalls, deleteCalls)
	}
}

func TestManagedEndpointCanonicalConfigNormalizesAndRejectsPrivateFields(t *testing.T) {
	req := ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeMieru,
		Protocol:    "mieru",
		Tag:         "cfg",
		Port:        2999,
		Config:      json.RawMessage(`{"transport":"UDP","mtu":1280}`),
	}
	if err := req.normalizeConfig(); err != nil {
		t.Fatalf("normalizeConfig: %v", err)
	}
	if req.Mieru == nil || req.Mieru.Transport != "UDP" || req.Mieru.MTU != 1280 {
		t.Fatalf("normalized mieru config = %#v", req.Mieru)
	}

	bad := ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeAmneziaWG,
		Protocol:    "amneziawg",
		Tag:         "bad",
		Port:        51820,
		Config:      json.RawMessage(`{"endpoint":"x","serverPrivateKey":"secret"}`),
	}
	if err := bad.normalizeConfig(); err == nil {
		t.Fatal("canonical config accepted private key")
	}
}

func TestManagedRuntimeNodeCapabilityValidation(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	node := model.Node{Id: 7, Name: "edge", Address: "edge.example.test", Port: 2053, ApiToken: "token", Enable: true, Guid: "node-guid", RuntimeCapabilities: `["mieru"]`}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedRuntimeNode(7, model.RuntimeMieru); err != nil {
		t.Fatalf("mieru capability rejected: %v", err)
	}
	if _, err := validateManagedRuntimeNode(7, model.RuntimeNaiveProxy); err == nil {
		t.Fatal("unsupported runtime capability was accepted")
	}
	if err := db.Model(&node).Update("guid", "").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedRuntimeNode(7, model.RuntimeMieru); err == nil {
		t.Fatal("node with empty guid was accepted")
	}
}

func TestNodeAdvertisesManagedRuntimeCanonicalAndLegacyFormats(t *testing.T) {
	for _, raw := range []string{
		`{"managedProtocols":["mieru","naiveproxy"]}`,
		`["amneziawg","mieru"]`,
		`amneziawg,mieru`,
	} {
		if !nodeAdvertisesManagedRuntime(raw, model.RuntimeMieru) {
			t.Fatalf("capabilities %s did not advertise mieru", raw)
		}
	}
	for _, raw := range []string{"", `{}`, `{"managedProtocols":[]}`, `{"managedProtocols":"mieru"}`, `[`, `xray`} {
		if nodeAdvertisesManagedRuntime(raw, model.RuntimeMieru) {
			t.Fatalf("malformed/empty capabilities %s advertised mieru", raw)
		}
	}
}

func TestManagedEndpointCreateRejectsSecondActiveSingletonRuntime(t *testing.T) {
	initManagedEndpointServiceDB(t)
	mutations := ManagedEndpointMutationService{Drivers: managedTestProvider{driver: managedTestDriver{kind: model.RuntimeMieru}}}
	req := ManagedEndpointCreateRequest{RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru-a", Port: 2999, Mieru: &ManagedMieruConfig{}}
	if _, err := mutations.Create(context.Background(), 1, req); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	req.Tag = "mieru-b"
	req.Port = 3000
	if _, err := mutations.Create(context.Background(), 1, req); err == nil {
		t.Fatal("second active singleton runtime endpoint was accepted")
	}
	if err := database.GetDB().Model(&model.ManagedEndpoint{}).Where("tag = ?", "mieru-a").Updates(map[string]any{"status": model.EndpointDeleted, "enable": false}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := mutations.Create(context.Background(), 1, req); err != nil {
		t.Fatalf("Create after deleting prior singleton: %v", err)
	}
}

func TestManagedEndpointServiceProjectsLegacyAndNativeRowsRedacted(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	nativeInboundID := 42
	rows := []model.Inbound{
		{UserId: 1, Remark: "vless", Tag: "vless-tag", Port: 443, Protocol: model.VLESS, Enable: true, Up: 11, Down: 22, Settings: `{"clients":[{"email":"stale-a"},{"email":"stale-b"},{"email":"stale-c"}]}`},
		{UserId: 1, Remark: "mt", Tag: "mt-tag", Port: 8443, Protocol: model.MTProto, Enable: true, Settings: `{"secret":"must-not-leak","clients":[{"email":"stale-carol"}]}`},
		{UserId: 2, Remark: "other", Tag: "other-tag", Port: 9443, Protocol: model.Trojan, Enable: true},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("create inbound %d: %v", i, err)
		}
	}
	clients := []model.ClientRecord{
		{Email: "alice@example.test", Enable: true},
		{Email: "bob@example.test", Enable: true},
		{Email: "carol@example.test", Enable: true},
	}
	for i := range clients {
		if err := db.Create(&clients[i]).Error; err != nil {
			t.Fatalf("create client %d: %v", i, err)
		}
	}
	links := []model.ClientInbound{
		{ClientId: clients[0].Id, InboundId: rows[0].Id},
		{ClientId: clients[1].Id, InboundId: rows[0].Id},
		{ClientId: clients[2].Id, InboundId: rows[1].Id},
	}
	for i := range links {
		if err := db.Create(&links[i]).Error; err != nil {
			t.Fatalf("create client inbound %d: %v", i, err)
		}
	}
	native := model.ManagedEndpoint{
		UserId:         1,
		InboundId:      &nativeInboundID,
		RuntimeKind:    model.RuntimeWireGuard,
		Protocol:       model.ManagedProtocol("wireguard"),
		Tag:            "wg-tag",
		Remark:         "wg",
		Port:           51820,
		Enable:         true,
		Status:         model.EndpointActive,
		DesiredConfig:  `{"privateKey":"must-not-leak"}`,
		ObservedConfig: `{"runtime":"must-not-leak"}`,
		LastError:      "must-not-leak",
		LastHealthAt:   123,
	}
	if err := db.Create(&native).Error; err != nil {
		t.Fatalf("create managed endpoint: %v", err)
	}
	if err := db.Create(&model.ManagedEndpointClient{EndpointId: native.Id, Email: "dave", Enable: true, State: model.EndpointClientApplied}).Error; err != nil {
		t.Fatalf("create managed client: %v", err)
	}

	got, err := ManagedEndpointService{}.List(1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 projected endpoints, got %d: %#v", len(got), got)
	}
	byTag := map[string]ManagedEndpointView{}
	for _, row := range got {
		byTag[row.Tag] = row
	}
	if byTag["vless-tag"].RuntimeKind != model.RuntimeXray {
		t.Fatalf("vless runtimeKind = %q", byTag["vless-tag"].RuntimeKind)
	}
	if byTag["mt-tag"].RuntimeKind != model.RuntimeMTProto {
		t.Fatalf("mtproto runtimeKind = %q", byTag["mt-tag"].RuntimeKind)
	}
	if byTag["wg-tag"].ClientCount != 1 {
		t.Fatalf("native client count = %d", byTag["wg-tag"].ClientCount)
	}
	if byTag["vless-tag"].ClientCount != 2 {
		t.Fatalf("legacy vless client count = %d, want normalized client_inbounds count 2", byTag["vless-tag"].ClientCount)
	}
	if byTag["mt-tag"].ClientCount != 1 {
		t.Fatalf("legacy mt client count = %d, want normalized client_inbounds count 1", byTag["mt-tag"].ClientCount)
	}
	if byTag["wg-tag"].Health.Message != "" {
		t.Fatalf("native health message leaked LastError: %q", byTag["wg-tag"].Health.Message)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal list: %v", err)
	}
	body := string(raw)
	for _, forbidden := range []string{"DesiredConfig", "ObservedConfig", "desiredConfig", "observedConfig", "LastError", "lastError", "must-not-leak"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("managed endpoint list leaked %q in %s", forbidden, body)
		}
	}
}

func TestManagedEndpointCapabilitiesManagedBackendsMutable(t *testing.T) {
	caps := ManagedEndpointService{}.Capabilities()
	if len(caps.RuntimeKinds) == 0 {
		t.Fatal("expected static capability metadata")
	}
	byKind := map[model.RuntimeKind]ManagedEndpointCapability{}
	for _, cap := range caps.RuntimeKinds {
		byKind[cap.RuntimeKind] = cap
	}
	for _, kind := range []model.RuntimeKind{model.RuntimeXray, model.RuntimeMTProto, model.RuntimeWireGuard} {
		cap := byKind[kind]
		if cap.ServerLifecycle || cap.ClientCRUD || cap.Traffic || cap.Detect || cap.FirewallPolicy {
			t.Fatalf("%s legacy projection must remain read-only: %#v", kind, cap)
		}
	}
	for _, kind := range []model.RuntimeKind{model.RuntimeAmneziaWG, model.RuntimeMieru, model.RuntimeNaiveProxy} {
		cap := byKind[kind]
		if !cap.ServerLifecycle || !cap.ClientCRUD || !cap.Detect || cap.Traffic || cap.FirewallPolicy {
			t.Fatalf("%s managed capability mismatch: %#v", kind, cap)
		}
	}
}

func TestManagedEndpointInstallPlansUseExactPinnedDockerRefs(t *testing.T) {
	cases := []struct {
		kind model.RuntimeKind
		ref  string
	}{
		{model.RuntimeAmneziaWG, "ghcr.io/sllikmll/unified-vpn-panel-protocol-awg2@sha256:538dfb87a642932430e6c0e1ab83b53ea53bc61104ff60ba6d0310bb279e24d8"},
		{model.RuntimeNaiveProxy, "ghcr.io/sllikmll/unified-vpn-panel-protocol-naive-caddy@sha256:eb3dc466b930f15186dad947b19ac52f4f60eac8db683ea8e8d03f2a6862ed8"},
	}
	for _, tc := range cases {
		plan := ManagedEndpointService{}.InstallPlan(tc.kind)
		if !plan.Supported || plan.Blocked || !plan.RequiresPinnedImage {
			t.Fatalf("%s install plan = %#v, want supported unblocked pinned image", tc.kind, plan)
		}
		if plan.ImageRef != tc.ref || plan.ArtifactRef != tc.ref {
			t.Fatalf("%s install ref image=%q artifact=%q, want %q", tc.kind, plan.ImageRef, plan.ArtifactRef, tc.ref)
		}
		if strings.Contains(plan.ImageRef, ":latest") {
			t.Fatalf("install plan must not advertise latest image: %#v", plan)
		}
	}

	plan := ManagedEndpointService{}.InstallPlan(model.RuntimeAmneziaWG)
	foundDocker := false
	for _, profile := range plan.BackendProfiles {
		if profile.Kind == "docker-amnezia-awg2" {
			foundDocker = profile.ContainerName == "unified-vpn-awg2-runtime" &&
				profile.HostConfigDir == provisioner.AWG2HostConfigPath &&
				profile.ContainerConfigDir == provisioner.AWG2GuestConfigPath
		}
	}
	if !foundDocker {
		t.Fatalf("install plan missing fixed docker profile: %#v", plan.BackendProfiles)
	}
}

func TestManagedEndpointGetDoesNotExposeAnotherUsersRows(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	foreignInbound := model.Inbound{UserId: 2, Remark: "foreign", Tag: "foreign-inbound", Port: 9443, Protocol: model.VLESS, Enable: true}
	if err := db.Create(&foreignInbound).Error; err != nil {
		t.Fatalf("create foreign inbound: %v", err)
	}
	foreignManaged := model.ManagedEndpoint{UserId: 2, RuntimeKind: model.RuntimeWireGuard, Protocol: "wireguard", Tag: "foreign-managed"}
	if err := db.Create(&foreignManaged).Error; err != nil {
		t.Fatalf("create foreign managed endpoint: %v", err)
	}

	service := ManagedEndpointService{}
	for _, id := range []string{
		legacyManagedEndpointID(model.RuntimeXray, foreignInbound.Id),
		fmt.Sprintf("managed-%d", foreignManaged.Id),
	} {
		if _, err := service.Get(1, id); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("Get(%q) error = %v, want record not found", id, err)
		}
	}
}

func TestManagedEndpointListSuppressesLegacyRowWhenManagedProjectionReferencesIt(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	inbound := model.Inbound{UserId: 1, Remark: "legacy", Tag: "legacy-tag", Port: 443, Protocol: model.VLESS, Enable: true}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	managed := model.ManagedEndpoint{
		UserId: 1, InboundId: &inbound.Id, RuntimeKind: model.RuntimeXray,
		Protocol: "vless", Tag: "managed-tag", Enable: true, Status: model.EndpointActive,
	}
	if err := db.Create(&managed).Error; err != nil {
		t.Fatalf("create managed projection: %v", err)
	}

	rows, err := ManagedEndpointService{}.List(1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].Id != fmt.Sprintf("managed-%d", managed.Id) {
		t.Fatalf("expected only managed projection, got %#v", rows)
	}
}

func TestManagedFrontendConfigsDecodeAndReachDesiredState(t *testing.T) {
	mutations := ManagedEndpointMutationService{Secrets: testSecretService(23)}

	awgReq := ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeAmneziaWG, Protocol: "amneziawg", Tag: "awg", Port: 32001,
		Config: json.RawMessage(`{"interfaceName":"awg0","listenPort":32001,"mtu":1420,"ipv4Address":"10.77.0.1/24","ipv4Pool":"10.77.0.0/24","dns":"1.1.1.1","clientAllowedIPs":"0.0.0.0/0","persistentKeepalive":25,"jc":7,"jmin":40,"jmax":120,"s1":80,"s2":149,"s3":24,"s4":12,"h1":"100-200","h2":"300-400","h3":"500-600","h4":"700-800"}`),
	}
	if err := awgReq.normalizeConfig(); err != nil {
		t.Fatalf("AWG frontend config: %v", err)
	}
	awgRaw, _, err := mutations.buildAWGDesired(model.ManagedEndpoint{Id: 1, Port: 32001, Enable: true}, awgReq.AWG)
	if err != nil {
		t.Fatalf("build AWG desired: %v", err)
	}
	var awgCfg awg.DesiredConfig
	if err := json.Unmarshal([]byte(awgRaw), &awgCfg); err != nil {
		t.Fatal(err)
	}
	if awgCfg.Server.Jc != 7 || awgCfg.Server.IPv4Pool != "10.77.0.0/24" || awgCfg.ClientDefaults.PersistentKeepalive != 25 {
		t.Fatalf("AWG form values lost: %#v", awgCfg)
	}

	mieruReq := ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru", Port: 32002,
		Config: json.RawMessage(`{"portBindings":[{"protocol":"TCP","port":32002}],"mtu":1400}`),
	}
	if err := mieruReq.normalizeConfig(); err != nil {
		t.Fatalf("Mieru frontend config: %v", err)
	}
	mieruRaw, _, err := mutations.buildMieruDesired(model.ManagedEndpoint{Id: 2, Port: 32002, Enable: true}, mieruReq.Mieru)
	if err != nil {
		t.Fatalf("build Mieru desired: %v", err)
	}
	var mieruCfg mieru.ServerConfig
	if err := json.Unmarshal([]byte(mieruRaw), &mieruCfg); err != nil {
		t.Fatal(err)
	}
	if len(mieruCfg.PortBindings) != 1 || mieruCfg.PortBindings[0].Port != 32002 || mieruCfg.MTU != 1400 {
		t.Fatalf("Mieru form values lost: %#v", mieruCfg)
	}

	naiveReq := ManagedEndpointCreateRequest{
		RuntimeKind: model.RuntimeNaiveProxy, Protocol: "naiveproxy", Tag: "naive", Port: 32003,
		Config: json.RawMessage(`{"domain":"3xamstnew.dogonin.ru","sni":"3xamstnew.dogonin.ru","listenIp":"0.0.0.0","port":32003,"tlsMode":"acme","acmeEmail":""}`),
	}
	if err := naiveReq.normalizeConfig(); err != nil {
		t.Fatalf("Naive frontend config: %v", err)
	}
	naiveRaw, _, err := mutations.buildNaiveDesired(model.ManagedEndpoint{Id: 3, Port: 32003, Enable: true}, naiveReq.NaiveProxy)
	if err != nil {
		t.Fatalf("build Naive desired: %v", err)
	}
	var naiveCfg struct {
		Endpoint naiveproxy.Endpoint `json:"endpoint"`
	}
	if err := json.Unmarshal([]byte(naiveRaw), &naiveCfg); err != nil {
		t.Fatal(err)
	}
	if naiveCfg.Endpoint.Domain != "3xamstnew.dogonin.ru" || naiveCfg.Endpoint.Port != 32003 {
		t.Fatalf("Naive form values lost: %#v", naiveCfg)
	}
}
