package nodecommand

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	awgdriver "github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver/amneziawg"
)

type awgProvider struct {
	driver driver.Driver
}

func (p awgProvider) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	if kind != model.RuntimeAmneziaWG {
		return nil, driver.ErrUnsupportedRuntime
	}
	return p.driver, nil
}

func TestAWGExecutorEndpointAndClientLifecycle(t *testing.T) {
	now := time.Now().UTC()
	rt := awg.NewRuntime(&awg.FakeBackend{DockerAvailable: true}, awg.MemoryStore{})
	exec := AWGExecutor{Provider: awgProvider{driver: awgdriver.New(rt)}, ResponseSealKey: []byte("token")}
	session := newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute))

	desired := awg.DesiredConfig{Server: awgServerFixture(), Clients: []awg.Client{awgClientFixture()}}
	applyReq := awgRequest(now, OperationEndpointApply, EndpointPayload{Tag: "awg-home"}, mustJSON(t, desired))
	resp, err := exec.Execute(context.Background(), session, applyReq)
	if err != nil {
		t.Fatalf("endpoint apply execute: %v", err)
	}
	if resp.Status != StatusSucceeded || resp.Result.State != ResultStateApplied {
		t.Fatalf("endpoint apply response = %+v", resp)
	}

	statusReq := awgRequest(now, OperationClientStatus, ClientPayload{ClientID: "client-1"}, nil)
	resp, err = exec.Execute(context.Background(), session, statusReq)
	if err != nil {
		t.Fatalf("client status execute: %v", err)
	}
	if resp.Status != StatusFailed || resp.ErrorCode != ErrorCodeUnsupportedOperation {
		t.Fatalf("client status response = %+v", resp)
	}

	deleteReq := awgRequest(now, OperationClientDelete, ClientPayload{ClientID: "client-1"}, nil)
	resp, err = exec.Execute(context.Background(), session, deleteReq)
	if err != nil {
		t.Fatalf("client delete execute: %v", err)
	}
	if resp.Status != StatusFailed || resp.ErrorCode != ErrorCodeUnsupportedOperation {
		t.Fatalf("client delete response = %+v", resp)
	}
}

func TestAWGExecutorClientCreateUpdateExportUnsupported(t *testing.T) {
	now := time.Now().UTC()
	rt := awg.NewRuntime(&awg.FakeBackend{DockerAvailable: true}, awg.MemoryStore{})
	if err := rt.Apply(context.Background(), awg.DesiredConfig{Server: awgServerFixture()}); err != nil {
		t.Fatalf("prime runtime: %v", err)
	}
	key := []byte("token")
	exec := AWGExecutor{Provider: awgProvider{driver: awgdriver.New(rt)}, ResponseSealKey: key}
	session := newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute))

	client := awgClientFixture()
	createReq := awgRequest(now, OperationClientCreate, ClientPayload{ClientID: client.ID, Email: client.Email}, mustJSON(t, client))
	resp, err := exec.Execute(context.Background(), session, createReq)
	if err != nil {
		t.Fatalf("client create execute: %v", err)
	}
	if resp.Status != StatusFailed || resp.ErrorCode != ErrorCodeUnsupportedOperation {
		t.Fatalf("client create response = %+v", resp)
	}

	client.PersistentKeepalive = 15
	updateReq := awgRequest(now, OperationClientUpdate, ClientPayload{ClientID: client.ID, Email: client.Email}, mustJSON(t, client))
	resp, err = exec.Execute(context.Background(), session, updateReq)
	if err != nil {
		t.Fatalf("client update execute: %v", err)
	}
	if resp.Status != StatusFailed || resp.ErrorCode != ErrorCodeUnsupportedOperation {
		t.Fatalf("client update response = %+v", resp)
	}

	exportReq := awgRequest(now, OperationClientExport, ClientPayload{ClientID: client.ID}, nil)
	resp, err = exec.Execute(context.Background(), session, exportReq)
	if err != nil {
		t.Fatalf("client export execute: %v", err)
	}
	if resp.Status != StatusFailed || resp.ErrorCode != ErrorCodeUnsupportedOperation || resp.SealedResult != "" || len(resp.SecretOutput) != 0 {
		t.Fatalf("client export response = %+v", resp)
	}
}

func awgRequest(now time.Time, op Operation, payload Payload, material []byte) Request {
	return Request{
		Version:           ProtocolV1,
		SupportedVersions: []ProtocolVersion{ProtocolV1},
		CommandID:         "cmd-" + strings.ReplaceAll(string(op), ".", "-"),
		IdempotencyKey:    "idem-" + strings.ReplaceAll(string(op), ".", "-"),
		NodeID:            7,
		TargetGUID:        "550e8400-e29b-41d4-a716-446655440000",
		EndpointID:        9,
		RuntimeKind:       model.RuntimeAmneziaWG,
		Operation:         op,
		DesiredGeneration: 1,
		IssuedAt:          now,
		ExpiresAt:         now.Add(time.Minute),
		Payload:           payload,
		SecretInput:       &SecretInput{Material: material, Refs: map[string]string{"interfaceName": "awg0"}},
	}
}

func awgServerFixture() awg.Server {
	server := awg.DefaultServer("awg0", 51820)
	server.PrivateKey = "SERVER_PRIVATE"
	server.PublicKey = "SERVER_PUBLIC"
	return server
}

func awgClientFixture() awg.Client {
	return awg.Client{
		ID:                  "client-1",
		Email:               "u@example.test",
		PrivateKey:          "CLIENT_PRIVATE",
		PublicKey:           "CLIENT_PUBLIC",
		PresharedKey:        "PSK",
		IPv4Address:         "10.66.66.2/32",
		AllowedIPs:          "10.66.66.2/32",
		ClientAllowedIPs:    "0.0.0.0/0",
		PersistentKeepalive: 25,
		Enable:              true,
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}
