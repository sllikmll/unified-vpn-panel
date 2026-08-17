package nodecommand

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/mieru"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	awgdriver "github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver/amneziawg"
)

type awgProvider struct {
	driver driver.Driver
}

func (p awgProvider) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	if p.driver == nil || kind != p.driver.Kind() {
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

	endpointDeleteReq := awgRequest(now, OperationEndpointDelete, nil, nil)
	endpointDeleteReq.SecretInput = &SecretInput{Refs: map[string]string{"interfaceName": "awg0"}}
	resp, err = exec.Execute(context.Background(), session, endpointDeleteReq)
	if err != nil {
		t.Fatalf("endpoint delete execute: %v", err)
	}
	if resp.Status != StatusSucceeded || resp.Result.State != ResultStateDeleted {
		t.Fatalf("endpoint delete response = %+v", resp)
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

func TestExecutorAppliesMieruAndNaiveProxyFullDesired(t *testing.T) {
	now := time.Now().UTC()
	session := newAuthenticatedSession(7, "550e8400-e29b-41d4-a716-446655440000", "node-7", "channel-1", now.Add(-time.Second), now.Add(time.Minute))

	for _, tt := range []struct {
		name string
		kind model.RuntimeKind
		body []byte
	}{
		{
			name: "mieru",
			kind: model.RuntimeMieru,
			body: mustJSON(t, mieru.ServerConfig{
				PortBindings: []mieru.PortBinding{{Port: 2999, Protocol: mieru.TransportTCP}},
				Users:        []mieru.User{{Name: "alice", Password: "secret-123456"}},
			}),
		},
		{
			name: "naiveproxy",
			kind: model.RuntimeNaiveProxy,
			body: mustJSON(t, map[string]any{
				"endpoint": naiveproxy.Endpoint{Domain: "example.test", ListenIP: "127.0.0.1", Port: 443, ACMEEmail: "ops@example.test"},
				"users":    []naiveproxy.User{{ID: "u1", Username: "alice", Password: "secret-123456", Enabled: true}},
			}),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := &captureDriver{kind: tt.kind}
			exec := AWGExecutor{Provider: awgProvider{driver: d}, ResponseSealKey: []byte("token")}
			req := awgRequest(now, OperationEndpointApply, EndpointPayload{Tag: tt.name}, tt.body)
			req.RuntimeKind = tt.kind
			resp, err := exec.Execute(context.Background(), session, req)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if resp.Status != StatusSucceeded {
				t.Fatalf("response = %+v", resp)
			}
			if !strings.Contains(d.settings, "secret-123456") {
				t.Fatalf("full desired config was not passed to driver: %s", d.settings)
			}
		})
	}
}

type captureDriver struct {
	kind     model.RuntimeKind
	settings string
}

func (d *captureDriver) Kind() model.RuntimeKind { return d.kind }
func (d *captureDriver) Capabilities() driver.Capabilities {
	return driver.Capabilities{EndpointLifecycle: true}
}

func (d *captureDriver) Create(_ context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	d.settings = inbound.Settings
	return driver.EndpointResult{RuntimeKind: d.kind, InboundId: inbound.Id, Tag: inbound.Tag, Status: model.EndpointActive}, nil
}

func (d *captureDriver) Update(context.Context, *model.Inbound, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, nil
}

func (d *captureDriver) Delete(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, nil
}

func (d *captureDriver) Enable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, nil
}

func (d *captureDriver) Disable(context.Context, *model.Inbound) (driver.EndpointResult, error) {
	return driver.EndpointResult{}, nil
}
func (d *captureDriver) Restart(context.Context) error { return nil }
func (d *captureDriver) Status(context.Context, *model.Inbound) (driver.StatusResult, error) {
	return driver.StatusResult{}, nil
}

func (d *captureDriver) Detect(context.Context) (driver.DetectResult, error) {
	return driver.DetectResult{}, nil
}

func (d *captureDriver) Health(context.Context, *model.Inbound) (driver.HealthResult, error) {
	return driver.HealthResult{}, nil
}
func (d *captureDriver) Clients() driver.ClientDriver { return managedNoopClient{} }

type managedNoopClient struct{}

func (managedNoopClient) Create(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (managedNoopClient) Update(context.Context, *model.Inbound, string, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (managedNoopClient) Delete(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (managedNoopClient) Enable(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (managedNoopClient) Disable(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (managedNoopClient) Status(context.Context, *model.Inbound, string) (driver.ClientStatusResult, error) {
	return driver.ClientStatusResult{}, driver.ErrUnsupportedOperation
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
