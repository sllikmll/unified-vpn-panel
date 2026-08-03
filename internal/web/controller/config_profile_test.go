package controller

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	xuilogger "github.com/mhsanaei/3x-ui/v3/internal/logger"
)

func newConfigProfileTestDB(t *testing.T) {
	t.Helper()
	xuilogger.InitLogger(logging.ERROR)
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestConfigProfileControllerCRUD(t *testing.T) {
	newConfigProfileTestDB(t)
	engine := gin.New()
	NewConfigProfileController(engine.Group("/panel/api/profiles"))

	add := doHostReq(t, engine, http.MethodPost, "/panel/api/profiles/add", map[string]any{
		"name":        "edge-vless",
		"description": "edge profile",
		"enabled":     true,
		"version":     1,
		"profile":     `{"inbounds":[{"protocol":"vless","port":2443}]}`,
	})
	if !add.Success {
		t.Fatalf("add not successful: %s", add.Msg)
	}
	var created model.ConfigProfile
	if err := json.Unmarshal(add.Obj, &created); err != nil {
		t.Fatalf("decode created profile: %v", err)
	}
	if created.Id == 0 || created.Profile != `{"inbounds":[{"port":2443,"protocol":"vless"}]}` {
		t.Fatalf("created profile = %+v", created)
	}

	update := doHostReq(t, engine, http.MethodPost, "/panel/api/profiles/update/1", map[string]any{
		"name":        "edge-vless-updated",
		"description": "updated",
		"enabled":     false,
		"version":     2,
		"profile":     `{"inbounds":[{"protocol":"trojan","port":2444}]}`,
	})
	if !update.Success {
		t.Fatalf("update not successful: %s", update.Msg)
	}

	clone := doHostReq(t, engine, http.MethodPost, "/panel/api/profiles/clone/1", map[string]any{"name": "edge-vless-copy"})
	if !clone.Success {
		t.Fatalf("clone not successful: %s", clone.Msg)
	}
	var cloned model.ConfigProfile
	if err := json.Unmarshal(clone.Obj, &cloned); err != nil {
		t.Fatalf("decode cloned profile: %v", err)
	}
	if cloned.Id == created.Id || cloned.Name != "edge-vless-copy" || cloned.Version != 2 {
		t.Fatalf("cloned profile = %+v", cloned)
	}

	list := doHostReq(t, engine, http.MethodGet, "/panel/api/profiles/list", nil)
	var profiles []model.ConfigProfile
	if err := json.Unmarshal(list.Obj, &profiles); err != nil {
		t.Fatalf("decode profile list: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("list length = %d, want 2", len(profiles))
	}

	del := doHostReq(t, engine, http.MethodPost, "/panel/api/profiles/del/1", nil)
	if !del.Success {
		t.Fatalf("delete not successful: %s", del.Msg)
	}
}
