package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

func initManagedEndpointController(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(locale.I18nType, string, ...string) string { return "" })
		session.SetAPIAuthUser(c, &model.User{Id: 1})
		c.Next()
	})
	NewManagedEndpointController(engine.Group("/panel/api/managed-endpoints"))
	return engine
}

func TestManagedEndpointReadOnlyRoutesRedactOutput(t *testing.T) {
	engine := initManagedEndpointController(t)
	db := database.GetDB()
	inbound := model.Inbound{UserId: 1, Remark: "MT", Tag: "mt-tag", Port: 8443, Protocol: model.MTProto, Enable: true, Settings: `{"secret":"must-not-leak","clients":[{"email":"a"}]}`}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	for _, path := range []string{"/panel/api/managed-endpoints/list", "/panel/api/managed-endpoints/" + "legacy-" + string(model.RuntimeMTProto) + "-" + "1"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "must-not-leak") || strings.Contains(rec.Body.String(), "desiredConfig") || strings.Contains(rec.Body.String(), "observedConfig") {
			t.Fatalf("%s leaked redacted output: %s", path, rec.Body.String())
		}
		var env struct {
			Success bool            `json:"success"`
			Obj     json.RawMessage `json:"obj"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if !env.Success || len(env.Obj) == 0 {
			t.Fatalf("unexpected envelope: %s", rec.Body.String())
		}
	}
}

func TestManagedEndpointInstallPlanRoute(t *testing.T) {
	engine := initManagedEndpointController(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panel/api/managed-endpoints/install-plan/amneziawg", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"imageRef"`) || !strings.Contains(rec.Body.String(), "pinned by digest") {
		t.Fatalf("install plan did not report blocked digest-pinned image state: %s", rec.Body.String())
	}
}
