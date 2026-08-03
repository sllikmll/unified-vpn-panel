package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func initConfigProfileTestDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestConfigProfileMigrationCreatesTable(t *testing.T) {
	initConfigProfileTestDB(t)

	migrator := database.GetDB().Migrator()
	if !migrator.HasTable(&model.ConfigProfile{}) {
		t.Fatal("config_profiles table was not migrated")
	}
	for _, col := range []string{"id", "name", "description", "enabled", "version", "profile", "created_at", "updated_at"} {
		if !migrator.HasColumn(&model.ConfigProfile{}, col) {
			t.Fatalf("config_profiles missing column %s", col)
		}
	}
	if !migrator.HasTable(&model.ConfigProfileNodeAssignment{}) {
		t.Fatal("config_profile_node_assignments table was not migrated")
	}
}

func TestConfigProfileServiceCanonicalizesJSON(t *testing.T) {
	initConfigProfileTestDB(t)
	s := ConfigProfileService{}

	created, err := s.Create(&model.ConfigProfile{
		Name:    "VLESS Reality",
		Enabled: true,
		Version: 1,
		Profile: `{
			"inbounds": [
				{ "streamSettings": {"security": "reality", "network": "tcp"}, "port": 2443, "protocol": "vless", "listen": "127.0.0.1" }
			],
			"labels": {"tier": "edge", "region": "eu"}
		}`,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	want := `{"inbounds":[{"listen":"127.0.0.1","port":2443,"protocol":"vless","streamSettings":{"network":"tcp","security":"reality"}}],"labels":{"region":"eu","tier":"edge"}}`
	if created.Profile != want {
		t.Fatalf("canonical profile = %s, want %s", created.Profile, want)
	}
}

func TestConfigProfileServiceRejectsSecretTemplates(t *testing.T) {
	initConfigProfileTestDB(t)
	s := ConfigProfileService{}

	_, err := s.Create(&model.ConfigProfile{
		Name:    "bad",
		Version: 1,
		Profile: `{"inbounds":[{"protocol":"vless","port":2444,"settings":{"clients":[{"password":"do-not-store"}]}}]}`,
	})
	if err == nil {
		t.Fatal("Create with secret-like template key succeeded")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("Create error = %v, want secret rejection", err)
	}
}

func TestConfigProfileServiceRejectsPortConflict(t *testing.T) {
	initConfigProfileTestDB(t)
	if err := database.GetDB().Create(&model.Inbound{
		Remark:   "existing",
		Tag:      "existing-443",
		Port:     2443,
		Listen:   "",
		Protocol: model.VLESS,
		Settings: `{"clients":[]}`,
	}).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}

	s := ConfigProfileService{}
	_, err := s.Create(&model.ConfigProfile{
		Name:    "conflict",
		Version: 1,
		Profile: `{"inbounds":[{"protocol":"vless","port":2443,"listen":"0.0.0.0","streamSettings":{"network":"tcp"}}]}`,
	})
	if err == nil {
		t.Fatal("Create with conflicting profile port succeeded")
	}
	if !strings.Contains(err.Error(), "already used") {
		t.Fatalf("Create error = %v, want port conflict", err)
	}
}

func TestConfigProfileServiceRejectsProfileInternalPortConflict(t *testing.T) {
	initConfigProfileTestDB(t)
	s := ConfigProfileService{}

	_, err := s.Create(&model.ConfigProfile{
		Name:    "internal-conflict",
		Version: 1,
		Profile: `{"inbounds":[{"protocol":"vless","port":2443},{"protocol":"trojan","port":2443}]}`,
	})
	if err == nil {
		t.Fatal("Create with internally conflicting profile ports succeeded")
	}
	if !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("Create error = %v, want internal conflict", err)
	}
}
