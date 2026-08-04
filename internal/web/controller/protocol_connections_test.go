package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/protocolconnections"
)

func setupProtocolConnectionControllerDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestProtocolConnectionPreviewRouteRedactsWhileExportReveals(t *testing.T) {
	setupProtocolConnectionControllerDB(t)
	gin.SetMode(gin.TestMode)

	const secret = "controller-preview-secret"
	svc := protocolconnections.NewService(nil)
	if _, _, err := svc.Import(protocolconnections.ImportRequest{
		Protocol:  "trojan",
		Name:      "controller-preview",
		Content:   "trojan://" + secret + "@tr.example.com:443",
		Selectors: []string{"GLOBAL"},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	router := gin.New()
	api := router.Group("/panel/api/proxy-connections")
	NewProtocolConnectionController(api)

	preview := httptest.NewRecorder()
	previewReq := httptest.NewRequest(http.MethodPost, "/panel/api/proxy-connections/preview", nil)
	router.ServeHTTP(preview, previewReq)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", preview.Code, preview.Body.String())
	}
	if strings.Contains(preview.Body.String(), secret) {
		t.Fatal("preview route leaked a connection secret")
	}
	if !strings.Contains(preview.Body.String(), "redacted") {
		t.Fatal("preview route did not return a redaction marker")
	}

	exported := httptest.NewRecorder()
	exportReq := httptest.NewRequest(http.MethodGet, "/panel/api/proxy-connections/export.yaml", nil)
	router.ServeHTTP(exported, exportReq)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", exported.Code, exported.Body.String())
	}
	if !strings.Contains(exported.Body.String(), secret) {
		t.Fatal("explicit export route unexpectedly hid the connection secret")
	}
}
