package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

func TestLocalManagedRuntimeDrivers(t *testing.T) {
	local := NewLocal(LocalDeps{})

	xray, err := local.Driver(model.RuntimeXray)
	if err != nil {
		t.Fatalf("xray driver: %v", err)
	}
	if xray.Kind() != model.RuntimeXray {
		t.Fatalf("xray kind = %q, want %q", xray.Kind(), model.RuntimeXray)
	}

	mtproto, err := local.Driver(model.RuntimeMTProto)
	if err != nil {
		t.Fatalf("mtproto driver: %v", err)
	}
	if mtproto.Kind() != model.RuntimeMTProto {
		t.Fatalf("mtproto kind = %q, want %q", mtproto.Kind(), model.RuntimeMTProto)
	}

	awg, err := local.Driver(model.RuntimeAmneziaWG)
	if err != nil {
		t.Fatalf("amneziawg driver: %v", err)
	}
	if awg.Kind() != model.RuntimeAmneziaWG {
		t.Fatalf("amneziawg kind = %q, want %q", awg.Kind(), model.RuntimeAmneziaWG)
	}
	mieru, err := local.Driver(model.RuntimeMieru)
	if err != nil {
		t.Fatalf("mieru driver: %v", err)
	}
	if mieru.Kind() != model.RuntimeMieru || !mieru.Capabilities().EndpointLifecycle || !mieru.Capabilities().Detect {
		t.Fatalf("mieru driver/capabilities = %q/%+v", mieru.Kind(), mieru.Capabilities())
	}
	naive, err := local.Driver(model.RuntimeNaiveProxy)
	if err != nil {
		t.Fatalf("naiveproxy driver: %v", err)
	}
	if naive.Kind() != model.RuntimeNaiveProxy || !naive.Capabilities().EndpointLifecycle || !naive.Capabilities().Detect {
		t.Fatalf("naiveproxy driver/capabilities = %q/%+v", naive.Kind(), naive.Capabilities())
	}
	if _, err := local.Driver(model.RuntimeWireGuard); !errors.Is(err, driver.ErrUnsupportedRuntime) {
		t.Fatalf("wireguard driver err = %v, want ErrUnsupportedRuntime", err)
	}
}

func TestManagedSecretRefsForRemoteAWGDelete(t *testing.T) {
	const private = "SHOULD_NOT_LEAVE_MASTER"
	inbound := &model.Inbound{Settings: `{"server":{"interfaceName":"awg0","privateKey":"` + private + `"}}`}
	refs, err := managedSecretRefs(model.RuntimeAmneziaWG, inbound)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs["interfaceName"] != "awg0" {
		t.Fatalf("refs = %#v", refs)
	}
	raw, _ := json.Marshal(refs)
	if strings.Contains(string(raw), private) {
		t.Fatalf("secret leaked into refs: %s", raw)
	}
}

func TestRemoteManagedRuntimeDriversUseLegacyRuntimeOnly(t *testing.T) {
	remote := &Remote{}

	for _, kind := range []model.RuntimeKind{model.RuntimeXray, model.RuntimeMTProto} {
		got, err := remote.Driver(kind)
		if err != nil {
			t.Fatalf("%s driver: %v", kind, err)
		}
		if got.Kind() != kind {
			t.Fatalf("%s driver kind = %q", kind, got.Kind())
		}
	}

	if _, err := remote.Driver(model.RuntimeMieru); !errors.Is(err, driver.ErrUnsupportedRuntime) {
		t.Fatalf("mieru driver err = %v, want ErrUnsupportedRuntime", err)
	}
}
