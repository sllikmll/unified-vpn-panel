package controller

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/nodecommand"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

var nodeCommandReplayGuard = nodecommand.NewMemoryReplayGuard(2048, 15*time.Minute, time.Now)

type nodeCommandDriverProvider struct{}

func (nodeCommandDriverProvider) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	mgr := runtime.GetManager()
	if mgr == nil {
		return nil, errors.New("runtime manager unavailable")
	}
	rt, err := mgr.RuntimeFor(nil)
	if err != nil {
		return nil, err
	}
	managed, ok := rt.(runtime.ManagedRuntime)
	if !ok {
		return nil, errors.New("managed runtime unavailable")
	}
	return managed.Driver(kind)
}

func handleNodeCommand(c *gin.Context) {
	token := c.GetString("api_bearer_token")
	if strings.TrimSpace(token) == "" {
		pureJsonMsg(c, http.StatusUnauthorized, false, "unauthorized")
		return
	}
	req, err := nodecommand.DecodeRequest(c.Request.Body, nodecommand.DecodeOptions{SealKey: []byte(token)})
	if err != nil {
		pureJsonMsg(c, http.StatusBadRequest, false, "invalid node command")
		return
	}
	panelGUID, err := (&service.SettingService{}).GetPanelGuid()
	if err != nil || panelGUID == "" {
		pureJsonMsg(c, http.StatusServiceUnavailable, false, "node command unavailable")
		return
	}
	session := nodecommand.NewAuthenticatedSession(req.NodeID, panelGUID, "api", "node-command-v1", time.Now().Add(-time.Second), time.Now().Add(15*time.Minute))
	if session.TargetGUID() != req.TargetGUID {
		resp := nodecommand.Response{Version: nodecommand.ProtocolV1, CommandID: req.CommandID, IdempotencyKey: req.IdempotencyKey, NodeID: req.NodeID, TargetGUID: req.TargetGUID, Status: nodecommand.StatusFailed, Operation: req.Operation, DesiredGeneration: req.DesiredGeneration, ErrorCode: nodecommand.ErrorCodeUnauthorized, SummaryCode: nodecommand.SummaryUnauthorized}
		jsonObj(c, resp, nil)
		return
	}
	if replayed, ok, err := nodeCommandReplayGuard.Begin(c.Request.Context(), req); err != nil {
		resp := nodecommand.Response{Version: nodecommand.ProtocolV1, CommandID: req.CommandID, IdempotencyKey: req.IdempotencyKey, NodeID: req.NodeID, TargetGUID: req.TargetGUID, Status: nodecommand.StatusFailed, Operation: req.Operation, DesiredGeneration: req.DesiredGeneration, ErrorCode: nodecommand.ErrorCodeReplayConflict, SummaryCode: nodecommand.SummaryReplayConflict}
		jsonObj(c, resp, nil)
		return
	} else if ok {
		jsonObj(c, replayed, nil)
		return
	}
	exec := nodecommand.AWGExecutor{Provider: nodeCommandDriverProvider{}, ResponseSealKey: []byte(token)}
	resp, err := exec.Execute(c.Request.Context(), session, req)
	if err != nil {
		_ = nodeCommandReplayGuard.Abort(c.Request.Context(), req)
		pureJsonMsg(c, http.StatusBadRequest, false, "node command failed")
		return
	}
	if err := nodeCommandReplayGuard.Commit(c.Request.Context(), req, resp); err != nil {
		pureJsonMsg(c, http.StatusBadRequest, false, "node command failed")
		return
	}
	jsonObj(c, resp, nil)
}

func bearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return token
	}
	return ""
}
