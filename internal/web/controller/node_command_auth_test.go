package controller

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/web/entity"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/nodecommand"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

func TestNodeCommandV1RequiresValidatedBearerBeforeDecode(t *testing.T) {
	engine, token := newNodeCommandAuthTestEngine(t)
	cases := []struct {
		name    string
		prepare func(*http.Request)
		want    int
	}{
		{
			name: "cookie only",
			prepare: func(req *http.Request) {
				addLoginCookie(t, engine, req)
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "empty bearer",
			prepare: func(req *http.Request) {
				addLoginCookie(t, engine, req)
				req.Header.Set("Authorization", "Bearer ")
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "invalid bearer",
			prepare: func(req *http.Request) {
				addLoginCookie(t, engine, req)
				req.Header.Set("Authorization", "Bearer wrong-token")
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "mtls without bearer",
			prepare: func(req *http.Request) {
				req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{&x509.Certificate{}}}}
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "valid bearer",
			prepare: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+token)
			},
			want: http.StatusOK,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte("{not json")
			if tt.want == http.StatusOK {
				body = validNodeCommandBody(t)
			}
			req := httptest.NewRequest(http.MethodPost, "/panel/api/node-command/v1", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			tt.prepare(req)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.want, w.Body.String())
			}
			if tt.want != http.StatusOK && bytes.Contains(w.Body.Bytes(), []byte("invalid node command")) {
				t.Fatalf("handler decoded body before rejecting bearer: %s", w.Body.String())
			}
			if tt.want == http.StatusOK {
				var msg entity.Msg
				if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if !msg.Success {
					t.Fatalf("success = false; body=%s", w.Body.String())
				}
			}
		})
	}
}

func newNodeCommandAuthTestEngine(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	if err := database.GetDB().Create(&model.User{Username: "node-command", Password: "x"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	const token = "node-command-valid-token"
	if err := database.GetDB().Create(&model.ApiToken{Name: "node-command", Token: crypto.HashTokenSHA256(token), Enabled: true}).Error; err != nil {
		t.Fatalf("seed api token: %v", err)
	}

	prevManager := runtime.GetManager()
	mgr := runtime.NewManager(runtime.LocalDeps{})
	mgr.SetLocalRuntimeOverride(fakeManagedRuntime{driver: fakeNodeCommandDriver{}})
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(prevManager) })
	nodeCommandReplayGuard = nodecommand.NewMemoryReplayGuard(16, time.Minute, time.Now)

	engine := gin.New()
	store := cookie.NewStore([]byte("node-command-auth-test-secret"))
	engine.Use(sessions.Sessions("3x-ui", store))
	a := &APIController{}
	engine.GET("/test-login", func(c *gin.Context) {
		u, err := a.userService.GetFirstUser()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if err := session.SetLoginUser(c, u); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})
	api := engine.Group("/panel/api")
	api.Use(a.checkAPIAuth)
	api.POST("/node-command/v1", handleNodeCommand)
	return engine, token
}

func addLoginCookie(t *testing.T, engine *gin.Engine, req *http.Request) {
	t.Helper()
	loginReq := httptest.NewRequest(http.MethodGet, "/test-login", nil)
	loginRec := httptest.NewRecorder()
	engine.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginRec.Code)
	}
	for _, cookie := range loginRec.Result().Cookies() {
		req.AddCookie(cookie)
	}
}

func validNodeCommandBody(t *testing.T) []byte {
	t.Helper()
	guid, err := (&service.SettingService{}).GetPanelGuid()
	if err != nil {
		t.Fatalf("panel guid: %v", err)
	}
	now := time.Now().UTC()
	req := nodecommand.Request{
		Version:           nodecommand.ProtocolV1,
		SupportedVersions: []nodecommand.ProtocolVersion{nodecommand.ProtocolV1},
		CommandID:         "cmd-detect",
		IdempotencyKey:    "idem-detect",
		NodeID:            7,
		TargetGUID:        guid,
		EndpointID:        9,
		RuntimeKind:       model.RuntimeAmneziaWG,
		Operation:         nodecommand.OperationEndpointDetect,
		DesiredGeneration: 1,
		IssuedAt:          now,
		ExpiresAt:         now.Add(time.Minute),
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return raw
}

type fakeManagedRuntime struct {
	driver driver.Driver
}

func (f fakeManagedRuntime) Name() string { return "fake-managed" }
func (f fakeManagedRuntime) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	if kind != model.RuntimeAmneziaWG {
		return nil, driver.ErrUnsupportedRuntime
	}
	return f.driver, nil
}
func (f fakeManagedRuntime) AddInbound(context.Context, *model.Inbound) error { return nil }
func (f fakeManagedRuntime) DelInbound(context.Context, *model.Inbound) error { return nil }
func (f fakeManagedRuntime) UpdateInbound(context.Context, *model.Inbound, *model.Inbound) error {
	return nil
}
func (f fakeManagedRuntime) AddUser(context.Context, *model.Inbound, map[string]any) error {
	return nil
}
func (f fakeManagedRuntime) RemoveUser(context.Context, *model.Inbound, string) error { return nil }
func (f fakeManagedRuntime) UpdateUser(context.Context, *model.Inbound, string, model.Client) error {
	return nil
}
func (f fakeManagedRuntime) DeleteUser(context.Context, *model.Inbound, string) error { return nil }
func (f fakeManagedRuntime) AddClient(context.Context, *model.Inbound, model.Client) error {
	return nil
}
func (f fakeManagedRuntime) DeleteClient(context.Context, string) error { return nil }
func (f fakeManagedRuntime) RestartXray(context.Context) error          { return nil }
func (f fakeManagedRuntime) ResetClientTraffic(context.Context, *model.Inbound, string) error {
	return nil
}
func (f fakeManagedRuntime) ResetInboundTraffic(context.Context, *model.Inbound) error {
	return nil
}
func (f fakeManagedRuntime) ResetAllTraffics(context.Context) error { return nil }

type fakeNodeCommandDriver struct{}

func (fakeNodeCommandDriver) Kind() model.RuntimeKind { return model.RuntimeAmneziaWG }
func (fakeNodeCommandDriver) Capabilities() driver.Capabilities {
	return driver.Capabilities{EndpointLifecycle: true, Detect: true, Status: true, Health: true}
}
func (fakeNodeCommandDriver) Create(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, driver.ErrUnsupportedOperation
}
func (fakeNodeCommandDriver) Update(context.Context, *model.Inbound, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, driver.ErrUnsupportedOperation
}
func (fakeNodeCommandDriver) Delete(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, driver.ErrUnsupportedOperation
}
func (fakeNodeCommandDriver) Enable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, driver.ErrUnsupportedOperation
}
func (fakeNodeCommandDriver) Disable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, driver.ErrUnsupportedOperation
}
func (fakeNodeCommandDriver) Restart(context.Context) error { return driver.ErrUnsupportedOperation }
func (fakeNodeCommandDriver) Status(context.Context, *model.Inbound) (driver.StatusResult, error) {
	return driver.StatusResult{}, driver.ErrUnsupportedOperation
}
func (fakeNodeCommandDriver) Detect(context.Context) (driver.DetectResult, error) {
	return driver.DetectResult{RuntimeKind: model.RuntimeAmneziaWG, Available: true}, nil
}
func (fakeNodeCommandDriver) Health(context.Context, *model.Inbound) (driver.HealthResult, error) {
	return driver.HealthResult{}, driver.ErrUnsupportedOperation
}
func (fakeNodeCommandDriver) Clients() driver.ClientDriver { return unsupportedClientDriver{} }

type unsupportedClientDriver struct{}

func (unsupportedClientDriver) Create(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}
func (unsupportedClientDriver) Update(context.Context, *model.Inbound, string, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}
func (unsupportedClientDriver) Delete(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}
func (unsupportedClientDriver) Enable(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}
func (unsupportedClientDriver) Disable(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}
func (unsupportedClientDriver) Status(context.Context, *model.Inbound, string) (driver.ClientStatusResult, error) {
	return driver.ClientStatusResult{}, driver.ErrUnsupportedOperation
}

var _ runtime.ManagedRuntime = fakeManagedRuntime{}
var _ driver.Driver = fakeNodeCommandDriver{}
var _ driver.ClientDriver = unsupportedClientDriver{}
