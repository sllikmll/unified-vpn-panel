package amneziawg

import (
	"context"
	"errors"
	"strings"
	"testing"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

func TestDriverAppliesInboundSettingsAndRedactsResults(t *testing.T) {
	be := &awg.FakeBackend{DockerAvailable: true}
	d := New(awg.NewRuntime(be, awg.MemoryStore{}))
	in := inboundFixture()
	res, err := d.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.RuntimeKind != model.RuntimeAmneziaWG || res.InboundId != 9 || !res.Enabled {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(be.LastConfig, "PrivateKey = SERVER_PRIVATE") {
		t.Fatal("backend did not receive rendered config")
	}
	if _, err := d.Clients().Status(context.Background(), in, "u@example.test"); !errors.Is(err, awg.ErrPeerOperationsUnsupported) {
		t.Fatalf("client status err = %v, want ErrPeerOperationsUnsupported", err)
	}
}

func TestDriverRejectsInvalidAWGParams(t *testing.T) {
	in := inboundFixture()
	in.Settings = strings.Replace(in.Settings, `"s3":12`, `"s3":65`, 1)
	d := New(awg.NewRuntime(&awg.FakeBackend{DockerAvailable: true}, awg.MemoryStore{}))
	if _, err := d.Create(context.Background(), in); err == nil {
		t.Fatal("Create succeeded, want invalid AWG params error")
	}
}

func TestDriverCapabilities(t *testing.T) {
	d := New(awg.NewRuntime(&awg.FakeBackend{}, awg.MemoryStore{}))
	if d.Kind() != model.RuntimeAmneziaWG {
		t.Fatalf("kind = %q", d.Kind())
	}
	caps := d.Capabilities()
	if !caps.EndpointLifecycle || caps.ClientCRUD || !caps.Detect || !caps.Status {
		t.Fatalf("capabilities = %+v", caps)
	}
	if err := d.Restart(context.Background()); !errors.Is(err, driver.ErrUnsupportedOperation) {
		t.Fatalf("Restart err = %v, want unsupported", err)
	}
}

func inboundFixture() *model.Inbound {
	return &model.Inbound{
		Id:       9,
		Tag:      "awg-home",
		Remark:   "AWG home",
		Enable:   true,
		Protocol: model.Protocol("amneziawg"),
		Port:     51820,
		Settings: `{"server":{"enable":true,"interfaceName":"awg0","listenPort":51820,"mtu":1420,"privateKey":"SERVER_PRIVATE","publicKey":"SERVER_PUBLIC","ipv4Address":"10.66.66.1/24","ipv4Pool":"10.66.66.0/24","dns":"1.1.1.1","endpoint":"vpn.example.test","jc":4,"jmin":40,"jmax":120,"s1":15,"s2":80,"s3":12,"s4":8,"h1":"1000-3000","h2":"4000-6000","h3":"7000-9000","h4":"10000-12000","i1":"<r 64>"},"clients":[{"id":"client-1","email":"u@example.test","privateKey":"CLIENT_PRIVATE","publicKey":"CLIENT_PUBLIC","presharedKey":"PSK","ipv4Address":"10.66.66.2/32","allowedIPs":"10.66.66.2/32","clientAllowedIPs":"0.0.0.0/0","persistentKeepalive":25,"enable":true}]}`,
	}
}
