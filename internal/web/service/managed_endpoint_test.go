package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func initManagedEndpointServiceDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
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

func TestManagedEndpointCapabilitiesPhase0ReadOnly(t *testing.T) {
	caps := ManagedEndpointService{}.Capabilities()
	if len(caps.RuntimeKinds) == 0 {
		t.Fatal("expected static capability metadata")
	}
	for _, cap := range caps.RuntimeKinds {
		if cap.ServerLifecycle || cap.ClientCRUD || cap.Traffic || cap.Detect || cap.FirewallPolicy {
			t.Fatalf("Phase 0 capability must keep mutations and runtime operations unavailable: %#v", cap)
		}
		if len(cap.NativeExport) != 0 || len(cap.Subscription) != 0 {
			t.Fatalf("Phase 0 capability must not advertise exports or subscriptions: %#v", cap)
		}
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
