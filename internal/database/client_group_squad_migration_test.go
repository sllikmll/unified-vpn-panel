package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestClientGroupSquadColumnsMigratedWithDefaults(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	if err := db.Migrator().DropColumn(&model.ClientGroup{}, "assigned_inbound_ids"); err != nil {
		t.Fatalf("drop assigned_inbound_ids: %v", err)
	}
	if err := db.Migrator().DropColumn(&model.ClientGroup{}, "description"); err != nil {
		t.Fatalf("drop description: %v", err)
	}
	if err := db.Migrator().DropColumn(&model.ClientGroup{}, "enable"); err != nil {
		t.Fatalf("drop enable: %v", err)
	}
	if err := initModels(); err != nil {
		t.Fatalf("initModels: %v", err)
	}
	for _, col := range []string{"description", "enable", "assigned_inbound_ids", "default_total_gb", "default_expiry_time"} {
		if !db.Migrator().HasColumn(&model.ClientGroup{}, col) {
			t.Fatalf("client_groups.%s was not migrated", col)
		}
	}

	if err := db.Exec("INSERT INTO client_groups (name) VALUES (?)", "legacy").Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := migrateClientGroupSquadColumns(); err != nil {
		t.Fatalf("migrateClientGroupSquadColumns: %v", err)
	}

	var got model.ClientGroup
	if err := db.Where("name = ?", "legacy").First(&got).Error; err != nil {
		t.Fatalf("load group: %v", err)
	}
	if got.AssignedInboundIds != "[]" || !got.Enable {
		t.Fatalf("migrated defaults = assigned %q enable %v, want [] and true", got.AssignedInboundIds, got.Enable)
	}
}
