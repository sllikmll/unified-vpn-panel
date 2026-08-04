package driver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type fakeRuntime struct {
	err error

	addInboundCalls    []*model.Inbound
	delInboundCalls    []*model.Inbound
	updateInboundCalls []updateInboundCall
	addClientCalls     []clientCall
	updateUserCalls    []updateUserCall
	deleteUserCalls    []deleteUserCall
	restartCalls       int
}

type updateInboundCall struct {
	oldIb *model.Inbound
	newIb *model.Inbound
}

type clientCall struct {
	ib     *model.Inbound
	client model.Client
}

type updateUserCall struct {
	ib       *model.Inbound
	oldEmail string
	client   model.Client
}

type deleteUserCall struct {
	ib    *model.Inbound
	email string
}

func (f *fakeRuntime) Name() string { return "fake" }

func (f *fakeRuntime) AddInbound(_ context.Context, ib *model.Inbound) error {
	f.addInboundCalls = append(f.addInboundCalls, ib)
	return f.err
}

func (f *fakeRuntime) DelInbound(_ context.Context, ib *model.Inbound) error {
	f.delInboundCalls = append(f.delInboundCalls, ib)
	return f.err
}

func (f *fakeRuntime) UpdateInbound(_ context.Context, oldIb, newIb *model.Inbound) error {
	f.updateInboundCalls = append(f.updateInboundCalls, updateInboundCall{oldIb: oldIb, newIb: newIb})
	return f.err
}

func (f *fakeRuntime) AddUser(context.Context, *model.Inbound, map[string]any) error { return nil }
func (f *fakeRuntime) RemoveUser(context.Context, *model.Inbound, string) error      { return nil }

func (f *fakeRuntime) UpdateUser(_ context.Context, ib *model.Inbound, oldEmail string, payload model.Client) error {
	f.updateUserCalls = append(f.updateUserCalls, updateUserCall{ib: ib, oldEmail: oldEmail, client: payload})
	return f.err
}

func (f *fakeRuntime) DeleteUser(_ context.Context, ib *model.Inbound, email string) error {
	f.deleteUserCalls = append(f.deleteUserCalls, deleteUserCall{ib: ib, email: email})
	return f.err
}

func (f *fakeRuntime) AddClient(_ context.Context, ib *model.Inbound, client model.Client) error {
	f.addClientCalls = append(f.addClientCalls, clientCall{ib: ib, client: client})
	return f.err
}

func (f *fakeRuntime) DeleteClient(context.Context, string) error { return nil }

func (f *fakeRuntime) RestartXray(context.Context) error {
	f.restartCalls++
	return f.err
}

func (f *fakeRuntime) ResetClientTraffic(context.Context, *model.Inbound, string) error { return nil }
func (f *fakeRuntime) ResetInboundTraffic(context.Context, *model.Inbound) error        { return nil }
func (f *fakeRuntime) ResetAllTraffics(context.Context) error                           { return nil }

func TestXrayAdapterDelegatesLifecycleExactlyOnce(t *testing.T) {
	rt := &fakeRuntime{}
	driver := NewXrayAdapter(rt)
	oldIb := &model.Inbound{Id: 7, Tag: "old", Protocol: model.VLESS, Enable: false}
	newIb := &model.Inbound{Id: 7, Tag: "new", Protocol: model.VLESS, Enable: true}

	if _, err := driver.Create(context.Background(), newIb); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := driver.Update(context.Background(), oldIb, newIb); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := driver.Enable(context.Background(), oldIb); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if _, err := driver.Disable(context.Background(), newIb); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, err := driver.Delete(context.Background(), oldIb); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := driver.Restart(context.Background()); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	if len(rt.addInboundCalls) != 1 || rt.addInboundCalls[0] != newIb {
		t.Fatalf("AddInbound calls = %#v, want exactly new inbound", rt.addInboundCalls)
	}
	if len(rt.updateInboundCalls) != 3 {
		t.Fatalf("UpdateInbound call count = %d, want 3", len(rt.updateInboundCalls))
	}
	if rt.updateInboundCalls[0].oldIb != oldIb || rt.updateInboundCalls[0].newIb != newIb {
		t.Fatalf("Update delegated wrong inbounds: %#v", rt.updateInboundCalls[0])
	}
	if !rt.updateInboundCalls[1].newIb.Enable || rt.updateInboundCalls[1].newIb == oldIb {
		t.Fatalf("Enable must delegate one enabled copy, got %#v", rt.updateInboundCalls[1].newIb)
	}
	if rt.updateInboundCalls[2].newIb.Enable || rt.updateInboundCalls[2].newIb == newIb {
		t.Fatalf("Disable must delegate one disabled copy, got %#v", rt.updateInboundCalls[2].newIb)
	}
	if len(rt.delInboundCalls) != 1 || rt.delInboundCalls[0] != oldIb {
		t.Fatalf("DelInbound calls = %#v, want exactly old inbound", rt.delInboundCalls)
	}
	if rt.restartCalls != 1 {
		t.Fatalf("RestartXray calls = %d, want 1", rt.restartCalls)
	}
}

func TestXrayAdapterDelegatesClientOperationsAndPropagatesErrors(t *testing.T) {
	wantErr := errors.New("runtime failed")
	rt := &fakeRuntime{err: wantErr}
	driver := NewXrayAdapter(rt)
	ib := &model.Inbound{Id: 2, Tag: "in", Protocol: model.VLESS}
	client := model.Client{Email: "a@example.test", ID: "uuid", Enable: true}

	if _, err := driver.Clients().Create(context.Background(), ib, client); !errors.Is(err, wantErr) {
		t.Fatalf("client create err = %v, want runtime error", err)
	}
	if _, err := driver.Clients().Update(context.Background(), ib, "old@example.test", client); !errors.Is(err, wantErr) {
		t.Fatalf("client update err = %v, want runtime error", err)
	}
	if _, err := driver.Clients().Delete(context.Background(), ib, client.Email); !errors.Is(err, wantErr) {
		t.Fatalf("client delete err = %v, want runtime error", err)
	}

	if len(rt.addClientCalls) != 1 || rt.addClientCalls[0].ib != ib || rt.addClientCalls[0].client.Email != client.Email {
		t.Fatalf("AddClient calls = %#v, want exact delegation", rt.addClientCalls)
	}
	if len(rt.updateUserCalls) != 1 || rt.updateUserCalls[0].oldEmail != "old@example.test" || rt.updateUserCalls[0].client.Email != client.Email {
		t.Fatalf("UpdateUser calls = %#v, want exact delegation", rt.updateUserCalls)
	}
	if len(rt.deleteUserCalls) != 1 || rt.deleteUserCalls[0].email != client.Email {
		t.Fatalf("DeleteUser calls = %#v, want exact delegation", rt.deleteUserCalls)
	}
}

func TestMTProtoAdapterDelegatesOnlySupportedInboundOperations(t *testing.T) {
	rt := &fakeRuntime{}
	driver := NewMTProtoAdapter(rt)
	oldIb := &model.Inbound{Id: 9, Tag: "mt-old", Protocol: model.MTProto, Enable: true}
	newIb := &model.Inbound{Id: 9, Tag: "mt-new", Protocol: model.MTProto, Enable: true}

	if _, err := driver.Create(context.Background(), newIb); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := driver.Update(context.Background(), oldIb, newIb); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := driver.Delete(context.Background(), oldIb); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if len(rt.addInboundCalls) != 1 || len(rt.updateInboundCalls) != 1 || len(rt.delInboundCalls) != 1 {
		t.Fatalf("supported delegation counts add/update/del = %d/%d/%d, want 1/1/1", len(rt.addInboundCalls), len(rt.updateInboundCalls), len(rt.delInboundCalls))
	}
	if err := driver.Restart(context.Background()); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("MTProto Restart err = %v, want ErrUnsupportedOperation", err)
	}
	if _, err := driver.Clients().Create(context.Background(), oldIb, model.Client{Email: "a@example.test"}); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("MTProto client create err = %v, want ErrUnsupportedOperation", err)
	}
	if len(rt.addClientCalls) != 0 || len(rt.updateUserCalls) != 0 || len(rt.deleteUserCalls) != 0 || rt.restartCalls != 0 {
		t.Fatalf("unsupported operations delegated unexpectedly: addClient=%d updateUser=%d deleteUser=%d restart=%d", len(rt.addClientCalls), len(rt.updateUserCalls), len(rt.deleteUserCalls), rt.restartCalls)
	}
}

func TestAdaptersHandleCancellationAndNilDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt := &fakeRuntime{}
	driver := NewXrayAdapter(rt)
	if _, err := driver.Create(ctx, &model.Inbound{Protocol: model.VLESS}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Create canceled err = %v, want context.Canceled", err)
	}
	if len(rt.addInboundCalls) != 0 {
		t.Fatalf("canceled call delegated %d times, want 0", len(rt.addInboundCalls))
	}
	if _, err := driver.Create(nil, &model.Inbound{Protocol: model.VLESS}); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Create nil context err = %v, want ErrNilContext", err)
	}
	if len(rt.addInboundCalls) != 0 {
		t.Fatalf("nil context delegated %d times, want 0", len(rt.addInboundCalls))
	}

	nilDriver := NewXrayAdapter(nil)
	if _, err := nilDriver.Create(context.Background(), &model.Inbound{Protocol: model.VLESS}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("nil runtime err = %v, want ErrNilRuntime", err)
	}
	var typedNilRuntime *fakeRuntime
	typedNilDriver := NewXrayAdapter(typedNilRuntime)
	if _, err := typedNilDriver.Create(context.Background(), &model.Inbound{Protocol: model.VLESS}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("typed nil runtime err = %v, want ErrNilRuntime", err)
	}
	if _, err := driver.Create(context.Background(), nil); !errors.Is(err, ErrNilInbound) {
		t.Fatalf("nil inbound err = %v, want ErrNilInbound", err)
	}
}

func TestLegacyAdaptersStatusHealthDetectAreUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name    string
		driver  Driver
		inbound *model.Inbound
	}{
		{name: "xray", driver: NewXrayAdapter(&fakeRuntime{}), inbound: &model.Inbound{Protocol: model.VLESS, Enable: true}},
		{name: "mtproto", driver: NewMTProtoAdapter(&fakeRuntime{}), inbound: &model.Inbound{Protocol: model.MTProto, Enable: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.driver.Status(context.Background(), tc.inbound); !errors.Is(err, ErrUnsupportedOperation) {
				t.Fatalf("Status err = %v, want ErrUnsupportedOperation", err)
			}
			if _, err := tc.driver.Health(context.Background(), tc.inbound); !errors.Is(err, ErrUnsupportedOperation) {
				t.Fatalf("Health err = %v, want ErrUnsupportedOperation", err)
			}
			if _, err := tc.driver.Detect(context.Background()); !errors.Is(err, ErrUnsupportedOperation) {
				t.Fatalf("Detect err = %v, want ErrUnsupportedOperation", err)
			}
		})
	}
}

func TestProtocolRuntimeMismatchDoesNotDelegate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		driver  Driver
		inbound *model.Inbound
		old     *model.Inbound
		next    *model.Inbound
		rt      *fakeRuntime
	}{
		{
			name:    "xray rejects mtproto",
			rt:      &fakeRuntime{},
			inbound: &model.Inbound{Protocol: model.MTProto, Tag: "mt"},
			old:     &model.Inbound{Protocol: model.VLESS, Tag: "old"},
			next:    &model.Inbound{Protocol: model.MTProto, Tag: "next"},
		},
		{
			name:    "mtproto rejects non-mtproto",
			rt:      &fakeRuntime{},
			inbound: &model.Inbound{Protocol: model.VLESS, Tag: "vless"},
			old:     &model.Inbound{Protocol: model.MTProto, Tag: "old"},
			next:    &model.Inbound{Protocol: model.VLESS, Tag: "next"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.HasPrefix(tc.name, "xray") {
				tc.driver = NewXrayAdapter(tc.rt)
			} else {
				tc.driver = NewMTProtoAdapter(tc.rt)
			}

			if _, err := tc.driver.Create(context.Background(), tc.inbound); !errors.Is(err, ErrProtocolRuntimeMismatch) {
				t.Fatalf("Create err = %v, want ErrProtocolRuntimeMismatch", err)
			}
			if _, err := tc.driver.Update(context.Background(), tc.old, tc.next); !errors.Is(err, ErrProtocolRuntimeMismatch) {
				t.Fatalf("Update err = %v, want ErrProtocolRuntimeMismatch", err)
			}
			if _, err := tc.driver.Clients().Create(context.Background(), tc.inbound, model.Client{Email: "a@example.test"}); !errors.Is(err, ErrProtocolRuntimeMismatch) {
				t.Fatalf("client Create err = %v, want ErrProtocolRuntimeMismatch", err)
			}
			if len(tc.rt.addInboundCalls) != 0 || len(tc.rt.updateInboundCalls) != 0 || len(tc.rt.addClientCalls) != 0 {
				t.Fatalf("mismatch delegated add/update/client = %d/%d/%d, want 0/0/0", len(tc.rt.addInboundCalls), len(tc.rt.updateInboundCalls), len(tc.rt.addClientCalls))
			}
		})
	}
}

func TestCapabilities(t *testing.T) {
	xray := NewXrayAdapter(&fakeRuntime{}).Capabilities()
	wantXray := Capabilities{EndpointLifecycle: true, Restart: true, ClientCRUD: true}
	if xray != wantXray {
		t.Fatalf("xray capabilities = %#v, want %#v", xray, wantXray)
	}

	mtproto := NewMTProtoAdapter(&fakeRuntime{}).Capabilities()
	wantMTProto := Capabilities{EndpointLifecycle: true}
	if mtproto != wantMTProto {
		t.Fatalf("mtproto capabilities = %#v, want %#v", mtproto, wantMTProto)
	}
}

func TestLegacyClientEnableDisableAreRuntimeActivationOnly(t *testing.T) {
	rt := &fakeRuntime{}
	driver := NewXrayAdapter(rt)
	ib := &model.Inbound{Id: 12, Tag: "in", Protocol: model.VLESS}

	if _, err := driver.Clients().Enable(context.Background(), ib, model.Client{Email: "a@example.test", Enable: false}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if _, err := driver.Clients().Disable(context.Background(), ib, "a@example.test"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if len(rt.addClientCalls) != 1 || !rt.addClientCalls[0].client.Enable {
		t.Fatalf("Enable AddClient calls = %#v, want one enabled runtime activation and no DB mutation here", rt.addClientCalls)
	}
	if len(rt.deleteUserCalls) != 1 || rt.deleteUserCalls[0].email != "a@example.test" {
		t.Fatalf("Disable DeleteUser calls = %#v, want one runtime deactivation and no DB mutation here", rt.deleteUserCalls)
	}
}

func TestResultJSONDoesNotSurfaceWarningsMessagesSecretsOrRawConfig(t *testing.T) {
	results := []any{
		EndpointResult{RuntimeKind: model.RuntimeMTProto, InboundId: 3, Tag: "mt", Enabled: true, Status: model.EndpointActive},
		StatusResult{RuntimeKind: model.RuntimeXray, InboundId: 4, Tag: "xr", Enabled: true, Status: model.EndpointActive},
		DetectResult{RuntimeKind: model.RuntimeXray, Available: true},
		HealthResult{RuntimeKind: model.RuntimeXray, InboundId: 5, Tag: "xr", Status: model.EndpointActive, CheckedAt: 1},
		ClientResult{RuntimeKind: model.RuntimeXray, InboundId: 6, Tag: "xr", Email: "a@example.test", Enabled: true},
		ClientStatusResult{RuntimeKind: model.RuntimeXray, InboundId: 6, Tag: "xr", Email: "a@example.test", Enabled: true},
	}

	for _, result := range results {
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal %T: %v", result, err)
		}
		lower := strings.ToLower(string(data))
		for _, forbidden := range []string{"warning", "message", "secret", "stderr", "settings", "streamsettings", "raw", "config", "password", "privatekey"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%T JSON %s contains forbidden surface %q", result, data, forbidden)
			}
		}
	}
}
