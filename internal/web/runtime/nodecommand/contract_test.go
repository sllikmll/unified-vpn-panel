package nodecommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestNegotiateVersion(t *testing.T) {
	tests := []struct {
		name      string
		offered   []ProtocolVersion
		want      ProtocolVersion
		wantError error
	}{
		{name: "v1", offered: []ProtocolVersion{ProtocolV1}, want: ProtocolV1},
		{name: "highest common", offered: []ProtocolVersion{"v0", ProtocolV1}, want: ProtocolV1},
		{name: "unsupported", offered: []ProtocolVersion{"v0"}, wantError: ErrUnsupportedVersion},
		{name: "empty", offered: nil, wantError: ErrUnsupportedVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NegotiateVersion(tt.offered)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if got != tt.want {
				t.Fatalf("version = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeRequestStrictValidation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	valid := fmt.Sprintf(`{
		"version":"v1",
		"supportedVersions":["v1"],
		"commandId":"cmd-1",
		"idempotencyKey":"idem-1",
		"nodeId":7,
		"targetGuid":"550e8400-e29b-41d4-a716-446655440000",
		"endpointId":9,
		"runtimeKind":"wireguard",
		"operation":"client.create",
		"desiredGeneration":11,
		"issuedAt":"%s",
		"expiresAt":"%s",
		"payload":{"clientId":"client-1","email":"a@example.com","enable":true}
	}`, now.Format(time.RFC3339), now.Add(time.Minute).Format(time.RFC3339))

	tests := []struct {
		name      string
		body      string
		wantError error
	}{
		{name: "valid", body: valid},
		{name: "unknown envelope field", body: replace(valid, `"payload":`, `"extra":1,"payload":`), wantError: ErrUnknownField},
		{name: "duplicate field", body: replace(valid, `"nodeId":7`, `"nodeId":7,"nodeId":8`), wantError: ErrDuplicateField},
		{name: "case alias node id", body: replace(valid, `"nodeId":7`, `"nodeId":7,"nodeID":8`), wantError: ErrUnknownField},
		{name: "case alias target guid mixed", body: replace(valid, `"targetGuid":"550e8400-e29b-41d4-a716-446655440000"`, `"targetGuid":"550e8400-e29b-41d4-a716-446655440000","TargetGuid":"550e8400-e29b-41d4-a716-446655440001"`), wantError: ErrUnknownField},
		{name: "case alias target guid acronym", body: replace(valid, `"targetGuid":"550e8400-e29b-41d4-a716-446655440000"`, `"targetGuid":"550e8400-e29b-41d4-a716-446655440000","targetGUID":"550e8400-e29b-41d4-a716-446655440001"`), wantError: ErrUnknownField},
		{name: "case alias command id", body: replace(valid, `"commandId":"cmd-1"`, `"commandId":"cmd-1","CommandId":"cmd-2"`), wantError: ErrUnknownField},
		{name: "case alias payload client id", body: replace(valid, `"clientId":"client-1"`, `"clientId":"client-1","ClientID":"client-2"`), wantError: ErrUnknownField},
		{name: "trailing json", body: valid + `{}`, wantError: ErrTrailingJSON},
		{name: "payload mismatch", body: strings.Replace(valid, `"operation":"client.create"`, `"operation":"endpoint.apply"`, 1), wantError: ErrPayloadMismatch},
		{name: "raw shell command field", body: replace(valid, `"email":"a@example.com"`, `"email":"a@example.com","command":"rm -rf /"`), wantError: ErrForbiddenField},
		{name: "raw shell args field", body: replace(valid, `"email":"a@example.com"`, `"email":"a@example.com","args":["-rf"]`), wantError: ErrForbiddenField},
		{name: "raw env field", body: replace(valid, `"email":"a@example.com"`, `"email":"a@example.com","env":{"A":"B"}`), wantError: ErrForbiddenField},
		{name: "ssh credential field", body: replace(valid, `"email":"a@example.com"`, `"email":"a@example.com","sshPrivateKey":"secret"`), wantError: ErrForbiddenField},
		{name: "unsupported runtime", body: strings.Replace(valid, `"runtimeKind":"wireguard"`, `"runtimeKind":"ssh"`, 1), wantError: ErrUnsupportedRuntime},
		{name: "unsupported operation", body: strings.Replace(valid, `"operation":"client.create"`, `"operation":"client.exec"`, 1), wantError: ErrUnsupportedOperation},
		{name: "expired", body: strings.Replace(valid, now.Add(time.Minute).Format(time.RFC3339), now.Add(-time.Minute).Format(time.RFC3339), 1), wantError: ErrExpired},
		{name: "oversized", body: valid, wantError: ErrPayloadTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := int64(1 << 20)
			if tt.name == "oversized" {
				limit = 8
			}
			got, err := DecodeRequest(strings.NewReader(tt.body), DecodeOptions{
				MaxBytes: limit,
				Now:      func() time.Time { return now },
			})
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if tt.wantError == nil {
				payload, ok := got.Payload.(ClientPayload)
				if !ok {
					t.Fatalf("payload type = %T, want ClientPayload", got.Payload)
				}
				if payload.Email != "a@example.com" {
					t.Fatalf("email = %q, want a@example.com", payload.Email)
				}
			}
		})
	}
}

func TestValidateRequestExactFailures(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	base := Request{
		Version:           ProtocolV1,
		SupportedVersions: []ProtocolVersion{ProtocolV1},
		CommandID:         "cmd-1",
		IdempotencyKey:    "idem-1",
		NodeID:            7,
		TargetGUID:        "550e8400-e29b-41d4-a716-446655440000",
		EndpointID:        9,
		RuntimeKind:       model.RuntimeWireGuard,
		Operation:         OperationEndpointStatus,
		DesiredGeneration: 1,
		IssuedAt:          now,
		ExpiresAt:         now.Add(time.Minute),
	}

	tests := []struct {
		name      string
		mutate    func(*Request)
		wantError error
	}{
		{name: "valid", mutate: func(*Request) {}},
		{name: "empty command id", mutate: func(r *Request) { r.CommandID = "" }, wantError: ErrMissingField},
		{name: "empty idempotency", mutate: func(r *Request) { r.IdempotencyKey = "" }, wantError: ErrMissingField},
		{name: "negative node", mutate: func(r *Request) { r.NodeID = -1 }, wantError: ErrInvalidField},
		{name: "zero node", mutate: func(r *Request) { r.NodeID = 0 }, wantError: ErrInvalidField},
		{name: "empty target guid", mutate: func(r *Request) { r.TargetGUID = "" }, wantError: ErrMissingField},
		{name: "target guid path-like", mutate: func(r *Request) { r.TargetGUID = "../target" }, wantError: ErrInvalidField},
		{name: "target guid too long", mutate: func(r *Request) { r.TargetGUID = strings.Repeat("a", MaxTargetGUIDLength+1) }, wantError: ErrInvalidField},
		{name: "zero endpoint", mutate: func(r *Request) { r.EndpointID = 0 }, wantError: ErrInvalidField},
		{name: "endpoint too large", mutate: func(r *Request) { r.EndpointID = MaxEndpointID + 1 }, wantError: ErrInvalidField},
		{name: "zero generation", mutate: func(r *Request) { r.DesiredGeneration = 0 }, wantError: ErrInvalidField},
		{name: "generation too large", mutate: func(r *Request) { r.DesiredGeneration = MaxDesiredGeneration + 1 }, wantError: ErrInvalidField},
		{name: "command id whitespace", mutate: func(r *Request) { r.CommandID = "cmd 1" }, wantError: ErrInvalidField},
		{name: "idempotency path-like", mutate: func(r *Request) { r.IdempotencyKey = "../idem" }, wantError: ErrInvalidField},
		{name: "issued too far future", mutate: func(r *Request) {
			r.IssuedAt = now.Add(MaxIssuedAtFutureSkew + time.Second)
			r.ExpiresAt = r.IssuedAt.Add(time.Second)
		}, wantError: ErrNotYetValid},
		{name: "lifetime too long", mutate: func(r *Request) { r.ExpiresAt = r.IssuedAt.Add(MaxCommandLifetime + time.Second) }, wantError: ErrInvalidField},
		{name: "issued after expires", mutate: func(r *Request) { r.IssuedAt = now.Add(time.Minute); r.ExpiresAt = now.Add(30 * time.Second) }, wantError: ErrInvalidField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			tt.mutate(&req)
			err := req.Validate(now)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateRequestOperationPayloadRules(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	base := Request{
		Version:           ProtocolV1,
		SupportedVersions: []ProtocolVersion{ProtocolV1},
		CommandID:         "cmd-1",
		IdempotencyKey:    "idem-1",
		NodeID:            7,
		TargetGUID:        "550e8400-e29b-41d4-a716-446655440000",
		EndpointID:        9,
		RuntimeKind:       model.RuntimeWireGuard,
		DesiredGeneration: 1,
		IssuedAt:          now,
		ExpiresAt:         now.Add(time.Minute),
	}
	enableTrue := true
	enableFalse := false
	tests := []struct {
		name      string
		operation Operation
		payload   Payload
		wantError error
	}{
		{name: "endpoint apply requires endpoint payload", operation: OperationEndpointApply, wantError: ErrPayloadMismatch},
		{name: "endpoint apply rejects empty payload", operation: OperationEndpointApply, payload: EndpointPayload{}, wantError: ErrMissingField},
		{name: "endpoint apply accepts tag", operation: OperationEndpointApply, payload: EndpointPayload{Tag: "wg-home"}},
		{name: "endpoint apply rejects enable", operation: OperationEndpointApply, payload: EndpointPayload{Tag: "wg-home", Enable: &enableTrue}, wantError: ErrForbiddenField},
		{name: "endpoint apply rejects oversized tag", operation: OperationEndpointApply, payload: EndpointPayload{Tag: strings.Repeat("a", MaxEndpointTagLength+1)}, wantError: ErrInvalidField},
		{name: "endpoint status rejects payload", operation: OperationEndpointStatus, payload: EndpointPayload{Tag: "wg-home"}, wantError: ErrPayloadMismatch},
		{name: "endpoint enable requires true", operation: OperationEndpointEnable, payload: EndpointPayload{Enable: &enableFalse}, wantError: ErrInvalidField},
		{name: "endpoint enable accepts true", operation: OperationEndpointEnable, payload: EndpointPayload{Enable: &enableTrue}},
		{name: "client create requires payload", operation: OperationClientCreate, wantError: ErrPayloadMismatch},
		{name: "client create requires email", operation: OperationClientCreate, payload: ClientPayload{ClientID: "client-1"}, wantError: ErrMissingField},
		{name: "client create accepts client email", operation: OperationClientCreate, payload: ClientPayload{ClientID: "client-1", Email: "a@example.com"}},
		{name: "client delete rejects email", operation: OperationClientDelete, payload: ClientPayload{ClientID: "client-1", Email: "a@example.com"}, wantError: ErrForbiddenField},
		{name: "client enable requires true when present", operation: OperationClientEnable, payload: ClientPayload{ClientID: "client-1", Enable: &enableFalse}, wantError: ErrInvalidField},
		{name: "client disable accepts false", operation: OperationClientDisable, payload: ClientPayload{ClientID: "client-1", Enable: &enableFalse}},
		{name: "client id rejects whitespace", operation: OperationClientStatus, payload: ClientPayload{ClientID: "client 1"}, wantError: ErrInvalidField},
		{name: "client email bounded", operation: OperationClientCreate, payload: ClientPayload{ClientID: "client-1", Email: strings.Repeat("a", MaxEmailLength+1)}, wantError: ErrInvalidField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.Operation = tt.operation
			req.Payload = tt.payload
			err := req.Validate(now)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestDecodeRequestOperationPayloadStrictExtras(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	body := fmt.Sprintf(`{
		"version":"v1",
		"supportedVersions":["v1"],
		"commandId":"cmd-1",
		"idempotencyKey":"idem-1",
		"nodeId":7,
		"targetGuid":"550e8400-e29b-41d4-a716-446655440000",
		"endpointId":9,
		"runtimeKind":"wireguard",
		"operation":"client.delete",
		"desiredGeneration":11,
		"issuedAt":"%s",
		"expiresAt":"%s",
		"payload":{"clientId":"client-1","email":"a@example.com"}
	}`, now.Format(time.RFC3339), now.Add(time.Minute).Format(time.RFC3339))
	_, err := DecodeRequest(strings.NewReader(body), DecodeOptions{Now: func() time.Time { return now }})
	if !errors.Is(err, ErrForbiddenField) {
		t.Fatalf("error = %v, want ErrForbiddenField", err)
	}
}

func TestRequestSecretInputDoesNotSerialize(t *testing.T) {
	req := Request{
		Version:           ProtocolV1,
		SupportedVersions: []ProtocolVersion{ProtocolV1},
		CommandID:         "cmd-1",
		IdempotencyKey:    "idem-1",
		NodeID:            7,
		TargetGUID:        "550e8400-e29b-41d4-a716-446655440000",
		EndpointID:        9,
		RuntimeKind:       model.RuntimeWireGuard,
		Operation:         OperationEndpointApply,
		DesiredGeneration: 1,
		IssuedAt:          time.Unix(100, 0).UTC(),
		ExpiresAt:         time.Unix(200, 0).UTC(),
		Payload:           EndpointPayload{Tag: "wg-home"},
		SecretInput:       &SecretInput{Material: []byte("private-key")},
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte("private-key")) {
		t.Fatalf("serialized request leaked secret: %s", raw)
	}
	if bytes.Contains(raw, []byte("SecretInput")) {
		t.Fatalf("serialized request exposed secret field: %s", raw)
	}
}

func TestSealedSecretInputRoundTripsRefsWithoutPlaintext(t *testing.T) {
	key := []byte("token")
	secret := &SecretInput{
		Material: []byte("private-config"),
		Refs:     map[string]string{"interfaceName": "awg0"},
	}
	sealed, err := SealSecretInput(key, secret)
	if err != nil {
		t.Fatalf("SealSecretInput: %v", err)
	}
	if strings.Contains(sealed, "private-config") || strings.Contains(sealed, "interfaceName") || strings.Contains(sealed, "awg0") {
		t.Fatalf("sealed secret leaked plaintext: %q", sealed)
	}
	opened, err := OpenSealedSecretInput(key, sealed)
	if err != nil {
		t.Fatalf("OpenSealedSecretInput: %v", err)
	}
	if string(opened.Material) != "private-config" || opened.Refs["interfaceName"] != "awg0" {
		t.Fatalf("opened secret = %+v", opened)
	}
}

func TestClientExportOperationValidation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	req := validRequest(now)
	req.Operation = OperationClientExport
	req.Payload = ClientPayload{ClientID: "client-1"}
	if err := req.Validate(now); err != nil {
		t.Fatalf("client export request: %v", err)
	}

	resp := validResponse(req)
	resp.SummaryCode = SummaryExported
	resp.Result.State = ResultStateExported
	resp.SealedResult = "abc123_-"
	if err := resp.ValidateFor(req); err != nil {
		t.Fatalf("client export response: %v", err)
	}

	resp.SealedResult = "abc/123"
	if err := resp.ValidateFor(req); !errors.Is(err, ErrUnsafeResponse) {
		t.Fatalf("unsafe sealed result error = %v, want ErrUnsafeResponse", err)
	}
}

func TestRequestSecretInputBounds(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	base := validRequest(now)
	tests := []struct {
		name      string
		secret    *SecretInput
		wantError error
	}{
		{name: "nil"},
		{name: "material bounded", secret: &SecretInput{Material: bytes.Repeat([]byte("a"), MaxSecretMaterialBytes)}},
		{name: "material too large", secret: &SecretInput{Material: bytes.Repeat([]byte("a"), MaxSecretMaterialBytes+1)}, wantError: ErrInvalidField},
		{name: "refs bounded", secret: &SecretInput{Refs: map[string]string{"ref-1": strings.Repeat("v", MaxSecretRefValueLength)}}},
		{name: "too many refs", secret: &SecretInput{Refs: manySecretRefs(MaxSecretRefCount + 1)}, wantError: ErrInvalidField},
		{name: "bad ref key", secret: &SecretInput{Refs: map[string]string{"../key": "value"}}, wantError: ErrInvalidField},
		{name: "ref value too large", secret: &SecretInput{Refs: map[string]string{"ref-1": strings.Repeat("v", MaxSecretRefValueLength+1)}}, wantError: ErrInvalidField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.SecretInput = tt.secret
			err := req.Validate(now)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if tt.wantError == nil {
				if _, err := requestReplayHashErr(req); err != nil {
					t.Fatalf("hash valid secret: %v", err)
				}
			}
		})
	}
}

func TestResponseRejectsUnsafeErrorDetails(t *testing.T) {
	enabled := true
	req := validRequest(time.Unix(100, 0).UTC())
	tests := []struct {
		name      string
		response  Response
		wantError error
	}{
		{name: "safe failed", response: Response{Version: ProtocolV1, CommandID: req.CommandID, IdempotencyKey: req.IdempotencyKey, NodeID: req.NodeID, TargetGUID: req.TargetGUID, Status: StatusFailed, Operation: req.Operation, DesiredGeneration: req.DesiredGeneration, ErrorCode: ErrorCodeUnsupportedOperation, SummaryCode: SummaryUnsupportedOperation}},
		{name: "unknown summary code", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusFailed, ErrorCode: ErrorCodeRuntimeFailed, SummaryCode: SummaryCode("stderr: private key failed")}, wantError: ErrUnsafeResponse},
		{name: "unknown code", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusFailed, ErrorCode: ErrorCode("panic stack"), SummaryCode: SummaryRuntimeFailed}, wantError: ErrUnsafeResponse},
		{name: "unknown state", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusSucceeded, Result: Result{State: ResultState("running; cat /etc/passwd")}}, wantError: ErrUnsafeResponse},
		{name: "bad result runtime", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusSucceeded, Result: Result{RuntimeKind: model.RuntimeKind("ssh"), EndpointID: 9, State: ResultStateRunning}}, wantError: ErrUnsafeResponse},
		{name: "bad result client id", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusSucceeded, Result: Result{RuntimeKind: model.RuntimeWireGuard, EndpointID: 9, ClientID: "../client", Enabled: &enabled, State: ResultStateRunning}}, wantError: ErrUnsafeResponse},
		{name: "failed requires error code", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusFailed, SummaryCode: SummaryRuntimeFailed}, wantError: ErrUnsafeResponse},
		{name: "failed requires failure summary", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusFailed, ErrorCode: ErrorCodeRuntimeFailed, SummaryCode: SummaryApplied}, wantError: ErrUnsafeResponse},
		{name: "success rejects error code", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusSucceeded, ErrorCode: ErrorCodeRuntimeFailed, SummaryCode: SummaryApplied}, wantError: ErrUnsafeResponse},
		{name: "success rejects failure summary", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusSucceeded, SummaryCode: SummaryRuntimeFailed}, wantError: ErrUnsafeResponse},
		{name: "accepted requires accepted summary", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusAccepted, SummaryCode: SummaryApplied}, wantError: ErrUnsafeResponse},
		{name: "replayed requires compatible summary", response: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: StatusReplayed, SummaryCode: SummaryUnavailable}, wantError: ErrUnsafeResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.response.Validate()
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestResponseValidateForRequiresExactCorrelation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	req := validRequest(now)
	valid := validResponse(req)
	tests := []struct {
		name      string
		mutate    func(*Response)
		wantError error
	}{
		{name: "valid", mutate: func(*Response) {}},
		{name: "wrong command", mutate: func(r *Response) { r.CommandID = "cmd-2" }, wantError: ErrUnsafeResponse},
		{name: "wrong key", mutate: func(r *Response) { r.IdempotencyKey = "idem-2" }, wantError: ErrUnsafeResponse},
		{name: "missing key", mutate: func(r *Response) { r.IdempotencyKey = "" }, wantError: ErrUnsafeResponse},
		{name: "wrong node", mutate: func(r *Response) { r.NodeID = req.NodeID + 1 }, wantError: ErrUnsafeResponse},
		{name: "missing node", mutate: func(r *Response) { r.NodeID = 0 }, wantError: ErrUnsafeResponse},
		{name: "wrong target guid", mutate: func(r *Response) { r.TargetGUID = "550e8400-e29b-41d4-a716-446655440001" }, wantError: ErrUnsafeResponse},
		{name: "missing target guid", mutate: func(r *Response) { r.TargetGUID = "" }, wantError: ErrUnsafeResponse},
		{name: "wrong operation", mutate: func(r *Response) { r.Operation = OperationEndpointStatus }, wantError: ErrUnsafeResponse},
		{name: "missing operation", mutate: func(r *Response) { r.Operation = "" }, wantError: ErrUnsafeResponse},
		{name: "wrong desired generation", mutate: func(r *Response) { r.DesiredGeneration = req.DesiredGeneration + 1 }, wantError: ErrUnsafeResponse},
		{name: "missing desired generation", mutate: func(r *Response) { r.DesiredGeneration = 0 }, wantError: ErrUnsafeResponse},
		{name: "wrong result runtime", mutate: func(r *Response) { r.Result.RuntimeKind = model.RuntimeXray }, wantError: ErrUnsafeResponse},
		{name: "missing result runtime", mutate: func(r *Response) { r.Result.RuntimeKind = "" }, wantError: ErrUnsafeResponse},
		{name: "wrong result endpoint", mutate: func(r *Response) { r.Result.EndpointID = req.EndpointID + 1 }, wantError: ErrUnsafeResponse},
		{name: "missing result endpoint", mutate: func(r *Response) { r.Result.EndpointID = 0 }, wantError: ErrUnsafeResponse},
		{name: "wrong result client", mutate: func(r *Response) { r.Result.ClientID = "client-2" }, wantError: ErrUnsafeResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := valid
			tt.mutate(&resp)
			err := resp.ValidateFor(req)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestResponseValidateForRejectsClientResultOnEndpointOperation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	req := validRequest(now)
	req.Operation = OperationEndpointStatus
	req.Payload = nil
	resp := validResponse(req)
	resp.Result.ClientID = "client-1"
	if err := resp.ValidateFor(req); !errors.Is(err, ErrUnsafeResponse) {
		t.Fatalf("error = %v, want ErrUnsafeResponse", err)
	}
}

func TestExecutorRequiresAuthenticatedSession(t *testing.T) {
	now := time.Now().UTC()
	req := validRequest(now)
	executor := ExecutorFunc(func(context.Context, AuthenticatedSession, Request) (Response, error) {
		return Response{
			Version:           ProtocolV1,
			CommandID:         "cmd-1",
			IdempotencyKey:    "idem-1",
			NodeID:            req.NodeID,
			TargetGUID:        req.TargetGUID,
			Status:            StatusAccepted,
			Operation:         OperationClientCreate,
			DesiredGeneration: 1,
			SummaryCode:       SummaryAccepted,
		}, nil
	})

	_, err := executor.Execute(context.Background(), AuthenticatedSession{}, req)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("empty session error = %v, want ErrUnauthenticated", err)
	}

	resp, err := executor.Execute(context.Background(), newAuthenticatedSession(7, req.TargetGUID, "node-7", "mtls:node-7", now.Add(-time.Second), now.Add(time.Minute)), req)
	if err != nil {
		t.Fatalf("authenticated execute: %v", err)
	}
	if resp.Status != StatusAccepted {
		t.Fatalf("status = %q, want %q", resp.Status, StatusAccepted)
	}
}

func TestAuthenticatedSessionAccessorsAndValidation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	session := newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute))
	if session.NodeID() != 7 || session.TargetGUID() != "550e8400-e29b-41d4-a716-446655440000" || session.Principal() != "node-7" || session.ChannelID() != "channel-1" {
		t.Fatalf("session accessors returned wrong binding")
	}
	if !session.AuthenticatedAt().Equal(now.Add(-time.Second)) || !session.ExpiresAt().Equal(now.Add(time.Minute)) {
		t.Fatalf("session time accessors returned wrong binding")
	}
	if err := session.validate(now, 7, "550e8400-e29b-41d4-a716-446655440000"); err != nil {
		t.Fatalf("valid session: %v", err)
	}
	tests := []struct {
		name      string
		session   AuthenticatedSession
		nodeID    int
		target    string
		wantError error
	}{
		{name: "zero", session: AuthenticatedSession{}, nodeID: 7, target: "550e8400-e29b-41d4-a716-446655440000", wantError: ErrUnauthenticated},
		{name: "node mismatch", session: session, nodeID: 8, target: "550e8400-e29b-41d4-a716-446655440000", wantError: ErrNodeMismatch},
		{name: "target mismatch", session: session, nodeID: 7, target: "550e8400-e29b-41d4-a716-446655440001", wantError: ErrTargetMismatch},
		{name: "future", session: newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(time.Second), now.Add(time.Minute)), nodeID: 7, target: "550e8400-e29b-41d4-a716-446655440000", wantError: ErrNotYetValid},
		{name: "expired", session: newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(-time.Minute), now), nodeID: 7, target: "550e8400-e29b-41d4-a716-446655440000", wantError: ErrExpired},
		{name: "zero node", session: newAuthenticatedSession(0, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute)), nodeID: 7, target: "550e8400-e29b-41d4-a716-446655440000", wantError: ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.session.validate(now, tt.nodeID, tt.target)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestTransportAndExecutorValidationGates(t *testing.T) {
	now := time.Now().UTC()
	session := newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute))
	req := validRequest(now)
	tests := []struct {
		name      string
		ctx       context.Context
		session   AuthenticatedSession
		req       Request
		resp      Response
		wantError error
	}{
		{name: "nil context", ctx: nil, session: session, req: req, resp: validResponse(req), wantError: ErrInvalidContext},
		{name: "canceled context", ctx: canceledContext(), session: session, req: req, resp: validResponse(req), wantError: context.Canceled},
		{name: "invalid session", ctx: context.Background(), session: AuthenticatedSession{}, req: req, resp: validResponse(req), wantError: ErrUnauthenticated},
		{name: "node mismatch", ctx: context.Background(), session: newAuthenticatedSession(8, req.TargetGUID, "node-8", "channel-8", now.Add(-time.Second), now.Add(time.Minute)), req: req, resp: validResponse(req), wantError: ErrNodeMismatch},
		{name: "target mismatch", ctx: context.Background(), session: newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440001", "node-7", "channel-7", now.Add(-time.Second), now.Add(time.Minute)), req: req, resp: validResponse(req), wantError: ErrTargetMismatch},
		{name: "invalid request", ctx: context.Background(), session: session, req: Request{CommandID: "cmd-1"}, resp: validResponse(req), wantError: ErrUnsupportedVersion},
		{name: "unsafe response", ctx: context.Background(), session: session, req: req, resp: Response{Version: ProtocolV1, CommandID: "cmd-1", Status: Status("raw")}, wantError: ErrUnsafeResponse},
		{name: "wrong-command response", ctx: context.Background(), session: session, req: req, resp: func() Response {
			resp := validResponse(req)
			resp.CommandID = "cmd-2"
			return resp
		}(), wantError: ErrUnsafeResponse},
		{name: "valid", ctx: context.Background(), session: session, req: req, resp: validResponse(req)},
	}
	for _, tt := range tests {
		t.Run(tt.name+" transport", func(t *testing.T) {
			transport := TransportFunc(func(context.Context, AuthenticatedSession, Request) (Response, error) {
				return tt.resp, nil
			})
			_, err := transport.Send(tt.ctx, tt.session, tt.req)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
		t.Run(tt.name+" executor", func(t *testing.T) {
			executor := ExecutorFunc(func(context.Context, AuthenticatedSession, Request) (Response, error) {
				return tt.resp, nil
			})
			_, err := executor.Execute(tt.ctx, tt.session, tt.req)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}

	_, err := (TransportFunc(nil)).Send(context.Background(), session, req)
	if !errors.Is(err, ErrInvalidField) {
		t.Fatalf("nil transport func error = %v, want ErrInvalidField", err)
	}
	_, err = (ExecutorFunc(nil)).Execute(context.Background(), session, req)
	if !errors.Is(err, ErrInvalidField) {
		t.Fatalf("nil executor func error = %v, want ErrInvalidField", err)
	}
}

func TestSessionTargetMismatchBlocksCallback(t *testing.T) {
	now := time.Now().UTC()
	req := validRequest(now)
	session := newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440001", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute))
	called := false
	transport := TransportFunc(func(context.Context, AuthenticatedSession, Request) (Response, error) {
		called = true
		return validResponse(req), nil
	})

	_, err := transport.Send(context.Background(), session, req)
	if !errors.Is(err, ErrTargetMismatch) {
		t.Fatalf("error = %v, want ErrTargetMismatch", err)
	}
	if called {
		t.Fatal("transport callback was called for target mismatch")
	}
}

func TestMemoryReplayGuard(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	guard := NewMemoryReplayGuard(2, time.Minute, func() time.Time { return now })
	req := validRequest(now)
	req.ExpiresAt = now.Add(10 * time.Minute)
	resp := validResponse(req)

	got, replayed, err := guard.Begin(context.Background(), req)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if replayed {
		t.Fatal("first begin replayed")
	}
	if got != nil {
		t.Fatalf("first response = %#v, want nil", got)
	}
	if err := guard.Commit(context.Background(), req, resp); err != nil {
		t.Fatalf("commit: %v", err)
	}
	got, replayed, err = guard.Begin(context.Background(), req)
	if err != nil {
		t.Fatalf("replay begin: %v", err)
	}
	if !replayed || got == nil || got.Status != StatusSucceeded {
		t.Fatalf("replay = (%#v, %v), want stored success", got, replayed)
	}

	now = now.Add(2 * time.Minute)
	got, replayed, err = guard.Begin(context.Background(), req)
	if err != nil {
		t.Fatalf("expired begin: %v", err)
	}
	if replayed || got != nil {
		t.Fatalf("expired replay = (%#v, %v), want new execution", got, replayed)
	}
}

func TestMemoryReplayGuardConcurrentSingleWinner(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	guard := NewMemoryReplayGuard(64, time.Minute, func() time.Time { return now })
	req := validRequest(now)
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	inflight := 0

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, replayed, err := guard.Begin(context.Background(), req)
			if errors.Is(err, ErrReplayInProgress) {
				mu.Lock()
				inflight++
				mu.Unlock()
				return
			}
			if err != nil {
				t.Errorf("begin: %v", err)
				return
			}
			if replayed || resp != nil {
				t.Errorf("unexpected replay before commit: %#v %v", resp, replayed)
				return
			}
			mu.Lock()
			winners++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if winners != 1 {
		t.Fatalf("winners = %d, want 1", winners)
	}
	if inflight != 31 {
		t.Fatalf("inflight = %d, want 31", inflight)
	}
}

func TestMemoryReplayGuardRejectsBeforeInsertAndCommitValidates(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	guard := NewMemoryReplayGuard(1, time.Minute, func() time.Time { return now })
	_, _, err := guard.Begin(nil, validRequest(now)) //nolint:staticcheck
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil context error = %v, want ErrInvalidContext", err)
	}
	_, _, err = guard.Begin(context.Background(), Request{CommandID: "cmd-1", IdempotencyKey: "idem-1"})
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("invalid request error = %v, want ErrUnsupportedVersion", err)
	}
	req := validRequest(now)
	err = guard.Commit(context.Background(), req, validResponse(req))
	if !errors.Is(err, ErrReplayMissingEntry) {
		t.Fatalf("missing commit error = %v, want ErrReplayMissingEntry", err)
	}
	if _, _, err := guard.Begin(context.Background(), req); err != nil {
		t.Fatalf("begin: %v", err)
	}
	err = guard.Commit(context.Background(), req, Response{Version: ProtocolV1, CommandID: "cmd-1", Status: Status("unsafe")})
	if !errors.Is(err, ErrUnsafeResponse) {
		t.Fatalf("unsafe commit error = %v, want ErrUnsafeResponse", err)
	}
	err = guard.Commit(context.Background(), req, func() Response {
		resp := validResponse(req)
		resp.CommandID = "cmd-2"
		return resp
	}())
	if !errors.Is(err, ErrUnsafeResponse) {
		t.Fatalf("wrong command commit error = %v, want ErrUnsafeResponse", err)
	}
}

func TestMemoryReplayGuardAbortValidation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	guard := NewMemoryReplayGuard(4, time.Minute, func() time.Time { return now })
	req := validRequest(now)
	if _, _, err := guard.Begin(context.Background(), req); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := guard.Abort(nil, req); !errors.Is(err, ErrInvalidContext) { //nolint:staticcheck
		t.Fatalf("nil abort error = %v, want ErrInvalidContext", err)
	}
	if _, _, err := guard.Begin(context.Background(), req); !errors.Is(err, ErrReplayInProgress) {
		t.Fatalf("nil abort mutated entry: %v", err)
	}
	if err := guard.Abort(canceledContext(), req); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled abort error = %v, want context.Canceled", err)
	}
	if _, _, err := guard.Begin(context.Background(), req); !errors.Is(err, ErrReplayInProgress) {
		t.Fatalf("canceled abort mutated entry: %v", err)
	}
	forged := req
	forged.Payload = ClientPayload{ClientID: "client-1", Email: "b@example.com"}
	if err := guard.Abort(context.Background(), forged); !errors.Is(err, ErrReplayKeyConflict) {
		t.Fatalf("forged abort error = %v, want ErrReplayKeyConflict", err)
	}
	if _, _, err := guard.Begin(context.Background(), req); !errors.Is(err, ErrReplayInProgress) {
		t.Fatalf("forged abort removed entry: %v", err)
	}
	missing := req
	missing.IdempotencyKey = "idem-missing"
	if err := guard.Abort(context.Background(), missing); !errors.Is(err, ErrReplayMissingEntry) {
		t.Fatalf("missing abort error = %v, want ErrReplayMissingEntry", err)
	}
	if err := guard.Abort(context.Background(), req); err != nil {
		t.Fatalf("abort valid: %v", err)
	}
	if _, _, err := guard.Begin(context.Background(), req); err != nil {
		t.Fatalf("begin after valid abort: %v", err)
	}
}

func TestMemoryReplayGuardReplayHashConflictsIncludePayloadTimeVersionsAndSecrets(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	base := validRequest(now)
	base.SecretInput = &SecretInput{Material: []byte("secret-a"), Refs: map[string]string{"k": "ref-a"}}
	guard := NewMemoryReplayGuard(8, time.Minute, func() time.Time { return now })
	if _, _, err := guard.Begin(context.Background(), base); err != nil {
		t.Fatalf("begin: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "version", mutate: func(r *Request) { r.SupportedVersions = []ProtocolVersion{ProtocolV1, "v-next"} }},
		{name: "target guid", mutate: func(r *Request) { r.TargetGUID = "550e8400-e29b-41d4-a716-446655440001" }},
		{name: "payload", mutate: func(r *Request) { r.Payload = ClientPayload{ClientID: "client-1", Email: "b@example.com"} }},
		{name: "issued", mutate: func(r *Request) { r.IssuedAt = r.IssuedAt.Add(time.Second); r.ExpiresAt = r.ExpiresAt.Add(time.Second) }},
		{name: "secret material", mutate: func(r *Request) {
			r.SecretInput = &SecretInput{Material: []byte("secret-b"), Refs: map[string]string{"k": "ref-a"}}
		}},
		{name: "secret refs", mutate: func(r *Request) {
			r.SecretInput = &SecretInput{Material: []byte("secret-a"), Refs: map[string]string{"k": "ref-b"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			tt.mutate(&req)
			_, _, err := guard.Begin(context.Background(), req)
			if !errors.Is(err, ErrReplayKeyConflict) {
				t.Fatalf("error = %v, want ErrReplayKeyConflict", err)
			}
		})
	}
	hash, err := requestReplayHashErr(base)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if strings.Contains(hash, "secret-a") || strings.Contains(hash, "ref-a") {
		t.Fatalf("replay hash leaked secret material/ref: %q", hash)
	}
}

func TestAuthenticatedSessionBounds(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	targetGUID := "550e8400-e29b-41d4-a716-446655440000"
	valid := newAuthenticatedSession(7, targetGUID, "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute))
	tests := []struct {
		name      string
		session   AuthenticatedSession
		wantError error
	}{
		{name: "valid", session: valid},
		{name: "target guid too long", session: newAuthenticatedSession(7, strings.Repeat("a", MaxTargetGUIDLength+1), "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute)), wantError: ErrUnauthenticated},
		{name: "bad target guid token", session: newAuthenticatedSession(7, "../target", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute)), wantError: ErrUnauthenticated},
		{name: "channel too long", session: newAuthenticatedSession(7, targetGUID, "node-7", strings.Repeat("a", MaxSessionChannelIDLength+1), now.Add(-time.Second), now.Add(time.Minute)), wantError: ErrUnauthenticated},
		{name: "principal too long", session: newAuthenticatedSession(7, targetGUID, strings.Repeat("a", MaxSessionPrincipalLength+1), "channel-1", now.Add(-time.Second), now.Add(time.Minute)), wantError: ErrUnauthenticated},
		{name: "bad channel token", session: newAuthenticatedSession(7, targetGUID, "node-7", "../channel", now.Add(-time.Second), now.Add(time.Minute)), wantError: ErrUnauthenticated},
		{name: "bad principal token", session: newAuthenticatedSession(7, targetGUID, "node 7", "channel-1", now.Add(-time.Second), now.Add(time.Minute)), wantError: ErrUnauthenticated},
		{name: "lifetime too long", session: newAuthenticatedSession(7, targetGUID, "node-7", "channel-1", now, now.Add(MaxSessionLifetime+time.Second)), wantError: ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.session.validate(now, 7, targetGUID)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestMemoryReplayGuardInflightCapacityAndExpiry(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	guard := NewMemoryReplayGuard(1, time.Minute, func() time.Time { return now })
	req := validRequest(now)
	if _, _, err := guard.Begin(context.Background(), req); err != nil {
		t.Fatalf("begin: %v", err)
	}
	now = now.Add(2 * time.Minute)
	_, _, err := guard.Begin(context.Background(), req)
	if !errors.Is(err, ErrReplayInProgress) {
		t.Fatalf("expired inflight duplicate error = %v, want ErrReplayInProgress", err)
	}
	other := validRequest(now)
	other.CommandID = "cmd-2"
	other.IdempotencyKey = "idem-2"
	other.IssuedAt = now
	other.ExpiresAt = now.Add(time.Minute)
	_, _, err = guard.Begin(context.Background(), other)
	if !errors.Is(err, ErrReplayCapacity) {
		t.Fatalf("full inflight capacity error = %v, want ErrReplayCapacity", err)
	}
	if err := guard.Commit(context.Background(), req, validResponse(req)); err != nil {
		t.Fatalf("commit: %v", err)
	}
	_, _, err = guard.Begin(context.Background(), other)
	if err != nil {
		t.Fatalf("completed entry should evict for new begin: %v", err)
	}
}

func validRequest(now time.Time) Request {
	return Request{
		Version:           ProtocolV1,
		SupportedVersions: []ProtocolVersion{ProtocolV1},
		CommandID:         "cmd-1",
		IdempotencyKey:    "idem-1",
		NodeID:            7,
		TargetGUID:        "550e8400-e29b-41d4-a716-446655440000",
		EndpointID:        9,
		RuntimeKind:       model.RuntimeWireGuard,
		Operation:         OperationClientCreate,
		DesiredGeneration: 1,
		IssuedAt:          now,
		ExpiresAt:         now.Add(time.Minute),
		Payload:           ClientPayload{ClientID: "client-1", Email: "a@example.com"},
	}
}

func validResponse(req Request) Response {
	return Response{
		Version:           ProtocolV1,
		CommandID:         req.CommandID,
		IdempotencyKey:    req.IdempotencyKey,
		NodeID:            req.NodeID,
		TargetGUID:        req.TargetGUID,
		Status:            StatusSucceeded,
		Operation:         req.Operation,
		DesiredGeneration: req.DesiredGeneration,
		Result:            Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, State: ResultStateApplied},
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func replace(s, old, new string) string {
	return strings.Replace(s, old, new, 1)
}

func manySecretRefs(count int) map[string]string {
	refs := make(map[string]string, count)
	for i := 0; i < count; i++ {
		refs[fmt.Sprintf("ref-%d", i)] = "value"
	}
	return refs
}
