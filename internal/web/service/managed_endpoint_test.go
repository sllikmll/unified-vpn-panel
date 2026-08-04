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

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

type managedTestProvider struct {
	driver driver.Driver
	err    error
}

func (p managedTestProvider) DriverForEndpoint(model.ManagedEndpoint) (driver.Driver, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.driver, nil
}

type managedTestDriver struct {
	kind model.RuntimeKind
	fail error
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
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointDeleted}, d.fail
}
func (d managedTestDriver) Enable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointActive}, d.fail
}
func (d managedTestDriver) Disable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{RuntimeKind: d.kind, Status: model.EndpointDisabled}, d.fail
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
	client, err := mutations.CreateClient(context.Background(), 1, view.Id, ManagedEndpointClientCreateRequest{
		Email:        "client@example.test",
		PrivateKey:   "CLIENT_PRIVATE_SECRET",
		PublicKey:    "CLIENT_PUBLIC",
		PreSharedKey: "CLIENT_PSK_SECRET",
	})
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
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

func TestManagedEndpointInstallPlanBlocksUnpinnedAWGImage(t *testing.T) {
	plan := ManagedEndpointService{}.InstallPlan(model.RuntimeAmneziaWG)
	if plan.Supported || !plan.Blocked || !plan.RequiresPinnedImage {
		t.Fatalf("AWG install plan must be blocked until a pinned image exists: %#v", plan)
	}
	if strings.Contains(plan.ImageRef, ":latest") {
		t.Fatalf("install plan must not advertise latest image: %#v", plan)
	}
	if !strings.Contains(plan.Reason, "pinned by digest") {
		t.Fatalf("install plan reason must name digest blocker: %#v", plan)
	}
	foundDocker := false
	for _, profile := range plan.BackendProfiles {
		if profile.Kind == "docker-amnezia-awg2" {
			foundDocker = profile.ContainerName == "amnezia-awg2" &&
				profile.HostConfigDir == "/opt/amnezia/state/amnezia-awg2" &&
				profile.ContainerConfigDir == "/opt/amnezia/awg"
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
