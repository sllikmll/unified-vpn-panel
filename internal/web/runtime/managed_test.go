package runtime

import (
	"errors"
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

	if _, err := local.Driver(model.RuntimeWireGuard); !errors.Is(err, driver.ErrUnsupportedRuntime) {
		t.Fatalf("wireguard driver err = %v, want ErrUnsupportedRuntime", err)
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
