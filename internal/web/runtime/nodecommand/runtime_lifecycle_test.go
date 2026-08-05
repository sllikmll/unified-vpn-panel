package nodecommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/provisioner"
)

func TestRuntimeLifecycleRequestRejectsArbitraryCommandFields(t *testing.T) {
	raw := []byte(`{"version":"v1","supportedVersions":["v1"],"commandId":"cmd-1","idempotencyKey":"idem-1","nodeId":7,"targetGuid":"550e8400-e29b-41d4-a716-446655440000","endpointId":1,"runtimeKind":"amneziawg","operation":"runtime.install","desiredGeneration":1,"issuedAt":"2026-08-04T00:00:00Z","expiresAt":"2026-08-04T00:01:00Z","payload":{"runtimeKind":"amneziawg","artifactRef":"ghcr.io/sllikmll/unified-vpn-panel-protocol-awg2:latest","command":"sh"}}`)
	_, err := DecodeRequest(bytes.NewReader(raw), DecodeOptions{Now: func() time.Time { return time.Date(2026, 8, 4, 0, 0, 1, 0, time.UTC) }})
	if !errors.Is(err, ErrForbiddenField) {
		t.Fatalf("DecodeRequest err = %v, want forbidden command field", err)
	}
}

func TestRuntimeLifecycleExecutorUsesProvisionerRedactedResult(t *testing.T) {
	now := time.Now().UTC()
	req := Request{
		Version:           ProtocolV1,
		SupportedVersions: []ProtocolVersion{ProtocolV1},
		CommandID:         "cmd-runtime-install",
		IdempotencyKey:    "idem-runtime-install",
		NodeID:            7,
		TargetGUID:        "550e8400-e29b-41d4-a716-446655440000",
		EndpointID:        1,
		RuntimeKind:       model.RuntimeMieru,
		Operation:         OperationRuntimeInstall,
		DesiredGeneration: 1,
		IssuedAt:          now,
		ExpiresAt:         now.Add(time.Minute),
		Payload:           RuntimePayload{RuntimeKind: model.RuntimeMieru, ArtifactRef: fakeRuntimeArtifactRef},
	}
	exec := AWGExecutor{Provisioners: staticProvisionerProvider{p: fakeRuntimeProvisioner{}}}
	resp, err := exec.Execute(context.Background(), newAuthenticatedSession(7, req.TargetGUID, "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute)), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	raw, _ := json.Marshal(resp)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "stdout") || strings.Contains(string(raw), "stderr") {
		t.Fatalf("response leaked unsafe output: %s", raw)
	}
	if resp.Status != StatusSucceeded || resp.Result.RuntimeKind != model.RuntimeMieru || resp.Result.ArtifactVersion != "v3.35.0" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestRuntimeLifecycleExecutorRejectsArtifactRefMismatch(t *testing.T) {
	for _, op := range []Operation{OperationRuntimeInstall, OperationRuntimeUpdate} {
		now := time.Now().UTC()
		req := runtimeRequest(now, op, RuntimePayload{RuntimeKind: model.RuntimeMieru, ArtifactRef: "mieru:mita:v3.35.0:linux/amd64:sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"})
		exec := AWGExecutor{Provisioners: staticProvisionerProvider{p: fakeRuntimeProvisioner{}}}
		resp, err := exec.Execute(context.Background(), newAuthenticatedSession(7, req.TargetGUID, "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute)), req)
		if err != nil {
			t.Fatalf("%s Execute: %v", op, err)
		}
		if resp.Status != StatusFailed || resp.ErrorCode != ErrorCodeValidationFailed {
			t.Fatalf("%s response = %+v, want validation failure", op, resp)
		}
	}
}

func TestRuntimeLifecycleExecutorRejectsUninstallArtifactOverride(t *testing.T) {
	now := time.Now().UTC()
	req := runtimeRequest(now, OperationRuntimeUninstall, RuntimePayload{RuntimeKind: model.RuntimeMieru, ArtifactRef: fakeRuntimeArtifactRef})
	exec := AWGExecutor{Provisioners: staticProvisionerProvider{p: fakeRuntimeProvisioner{}}}
	resp, err := exec.Execute(context.Background(), newAuthenticatedSession(7, req.TargetGUID, "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute)), req)
	if err == nil {
		t.Fatalf("Execute response = %+v, want validation error", resp)
	}
}

func runtimeRequest(now time.Time, op Operation, payload RuntimePayload) Request {
	return Request{
		Version:           ProtocolV1,
		SupportedVersions: []ProtocolVersion{ProtocolV1},
		CommandID:         "cmd-runtime",
		IdempotencyKey:    "idem-runtime",
		NodeID:            7,
		TargetGUID:        "550e8400-e29b-41d4-a716-446655440000",
		EndpointID:        1,
		RuntimeKind:       payload.RuntimeKind,
		Operation:         op,
		DesiredGeneration: 1,
		IssuedAt:          now,
		ExpiresAt:         now.Add(time.Minute),
		Payload:           payload,
	}
}

type staticProvisionerProvider struct{ p provisioner.Provisioner }

func (p staticProvisionerProvider) Provisioner() provisioner.Provisioner { return p.p }

type fakeRuntimeProvisioner struct{}

const fakeRuntimeArtifactRef = "mieru:mita:v3.35.0:linux/amd64:sha256:a07d5afc5e1353ab346bb3ddbe95c7f960828204be529f4a88d688dfe83e252d"

func (fakeRuntimeProvisioner) Plan(kind model.RuntimeKind) provisioner.Plan {
	return provisioner.Plan{RuntimeKind: kind, Supported: true, ArtifactRef: fakeRuntimeArtifactRef, Version: "v3.35.0"}
}

func (fakeRuntimeProvisioner) Install(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{RuntimeKind: model.RuntimeMieru, ArtifactRef: fakeRuntimeArtifactRef, Version: "v3.35.0", State: "running"}, nil
}

func (fakeRuntimeProvisioner) Update(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{RuntimeKind: model.RuntimeMieru, ArtifactRef: fakeRuntimeArtifactRef, Version: "v3.35.0", State: "running"}, nil
}

func (fakeRuntimeProvisioner) Uninstall(context.Context, model.RuntimeKind) (provisioner.Result, error) {
	return provisioner.Result{RuntimeKind: model.RuntimeMieru, ArtifactRef: fakeRuntimeArtifactRef, Version: "v3.35.0", State: "removed"}, nil
}
