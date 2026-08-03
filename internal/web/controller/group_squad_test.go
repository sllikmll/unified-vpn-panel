package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func setupControllerGroupDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestGroupListRedactsClientSecrets(t *testing.T) {
	setupControllerGroupDB(t)
	gin.SetMode(gin.TestMode)

	rec := &model.ClientRecord{
		Email:  "secret-member@x",
		SubID:  "sub-secret",
		UUID:   uuid.NewString(),
		Secret: "ee1234567890abcdef1234567890abcd7777772e636c6f7564666c6172652e636f6d",
		Group:  "redacted",
		Enable: true,
	}
	if err := database.GetDB().Create(rec).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := database.GetDB().Create(&model.ClientGroup{Name: "redacted", Enable: true, AssignedInboundIds: "[]"}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	r := gin.New()
	api := r.Group("/panel/api/clients")
	NewGroupController(api)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panel/api/clients/groups", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), rec.Secret) {
		t.Fatalf("group list leaked client secret: %s", w.Body.String())
	}
	var resp struct {
		Success bool              `json:"success"`
		Obj     []json.RawMessage `json:"obj"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Success || len(resp.Obj) != 1 {
		t.Fatalf("response = %s, want one successful group", w.Body.String())
	}
}
