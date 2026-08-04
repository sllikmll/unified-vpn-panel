package database

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestManagedEndpointTablesAutoMigrateAndHideSecrets(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	migrator := GetDB().Migrator()
	for _, table := range []any{
		&model.ManagedEndpoint{},
		&model.ManagedEndpointClient{},
		&model.ManagedEndpointClientTraffic{},
		&model.ManagedSecret{},
		&model.ManagedEndpointApplyLog{},
	} {
		if !migrator.HasTable(table) {
			t.Fatalf("expected table for %T to be auto-migrated", table)
		}
	}

	baseEndpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeWireGuard, Protocol: "wireguard", Tag: "shared-tag"}
	if err := GetDB().Create(&baseEndpoint).Error; err != nil {
		t.Fatalf("create managed endpoint: %v", err)
	}
	otherUserEndpoint := model.ManagedEndpoint{UserId: 2, RuntimeKind: model.RuntimeWireGuard, Protocol: "wireguard", Tag: "shared-tag"}
	if err := GetDB().Create(&otherUserEndpoint).Error; err != nil {
		t.Fatalf("same managed endpoint tag for another user must be allowed: %v", err)
	}
	duplicateEndpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeWireGuard, Protocol: "wireguard", Tag: "shared-tag"}
	if err := GetDB().Create(&duplicateEndpoint).Error; err == nil {
		t.Fatal("duplicate managed endpoint tag for the same user must fail")
	}

	secret := model.ManagedSecret{Ciphertext: []byte("top-secret")}
	raw, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal ManagedSecret: %v", err)
	}
	if strings.Contains(string(raw), "Ciphertext") || strings.Contains(string(raw), "top-secret") {
		t.Fatalf("ManagedSecret JSON leaked ciphertext: %s", raw)
	}

	applyLog := model.ManagedEndpointApplyLog{Error: "raw-error", RollbackToken: "raw-token"}
	raw, err = json.Marshal(applyLog)
	if err != nil {
		t.Fatalf("marshal ManagedEndpointApplyLog: %v", err)
	}
	if strings.Contains(string(raw), "raw-error") || strings.Contains(string(raw), "raw-token") || strings.Contains(string(raw), "error") || strings.Contains(string(raw), "rollbackToken") {
		t.Fatalf("ManagedEndpointApplyLog JSON leaked internals: %s", raw)
	}

	endpoint := model.ManagedEndpoint{LastError: "raw-error"}
	raw, err = json.Marshal(endpoint)
	if err != nil {
		t.Fatalf("marshal ManagedEndpoint: %v", err)
	}
	if strings.Contains(string(raw), "raw-error") || strings.Contains(string(raw), "lastError") {
		t.Fatalf("ManagedEndpoint JSON leaked LastError: %s", raw)
	}

	client := model.ManagedEndpointClient{LastError: "raw-error"}
	raw, err = json.Marshal(client)
	if err != nil {
		t.Fatalf("marshal ManagedEndpointClient: %v", err)
	}
	if strings.Contains(string(raw), "raw-error") || strings.Contains(string(raw), "lastError") {
		t.Fatalf("ManagedEndpointClient JSON leaked LastError: %s", raw)
	}
}
