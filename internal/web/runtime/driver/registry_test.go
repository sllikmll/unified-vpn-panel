package driver

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type stubDriver struct {
	kind model.RuntimeKind
}

func (d stubDriver) Kind() model.RuntimeKind { return d.kind }
func (d stubDriver) Capabilities() Capabilities {
	return Capabilities{}
}

func (d stubDriver) Create(context.Context, *model.Inbound) (EndpointResult, error) {
	return EndpointResult{}, nil
}

func (d stubDriver) Update(context.Context, *model.Inbound, *model.Inbound) (EndpointResult, error) {
	return EndpointResult{}, nil
}

func (d stubDriver) Delete(context.Context, *model.Inbound) (EndpointResult, error) {
	return EndpointResult{}, nil
}

func (d stubDriver) Enable(context.Context, *model.Inbound) (EndpointResult, error) {
	return EndpointResult{}, nil
}

func (d stubDriver) Disable(context.Context, *model.Inbound) (EndpointResult, error) {
	return EndpointResult{}, nil
}
func (d stubDriver) Restart(context.Context) error { return nil }
func (d stubDriver) Status(context.Context, *model.Inbound) (StatusResult, error) {
	return StatusResult{}, nil
}
func (d stubDriver) Detect(context.Context) (DetectResult, error) { return DetectResult{}, nil }
func (d stubDriver) Health(context.Context, *model.Inbound) (HealthResult, error) {
	return HealthResult{}, nil
}
func (d stubDriver) Clients() ClientDriver { return nil }

func TestRegistryLookupKindsAndDuplicates(t *testing.T) {
	reg := NewRegistry()

	if _, err := reg.Driver(model.RuntimeXray); !errors.Is(err, ErrUnsupportedRuntime) {
		t.Fatalf("missing driver err = %v, want ErrUnsupportedRuntime", err)
	}

	xray := stubDriver{kind: model.RuntimeXray}
	mtproto := stubDriver{kind: model.RuntimeMTProto}
	if err := reg.Register(mtproto); err != nil {
		t.Fatalf("register mtproto: %v", err)
	}
	if err := reg.Register(xray); err != nil {
		t.Fatalf("register xray: %v", err)
	}
	if err := reg.Register(stubDriver{kind: model.RuntimeXray}); !errors.Is(err, ErrDuplicateRuntime) {
		t.Fatalf("duplicate register err = %v, want ErrDuplicateRuntime", err)
	}

	got, err := reg.Driver(model.RuntimeXray)
	if err != nil {
		t.Fatalf("lookup xray: %v", err)
	}
	if got.Kind() != model.RuntimeXray {
		t.Fatalf("lookup kind = %q, want xray", got.Kind())
	}
	if kinds := reg.Kinds(); !reflect.DeepEqual(kinds, []model.RuntimeKind{model.RuntimeMTProto, model.RuntimeXray}) {
		t.Fatalf("Kinds = %#v, want deterministic sorted order", kinds)
	}
}

func TestRegistryRejectsNilAndEmptyKind(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(nil); !errors.Is(err, ErrNilDriver) {
		t.Fatalf("nil register err = %v, want ErrNilDriver", err)
	}
	if err := reg.Register(stubDriver{}); !errors.Is(err, ErrUnsupportedRuntime) {
		t.Fatalf("empty-kind register err = %v, want ErrUnsupportedRuntime", err)
	}
}

type nilableStubDriver struct {
	stubDriver
}

func TestRegistryZeroValueAndTypedNilDriverAreSafe(t *testing.T) {
	var reg Registry

	if kinds := reg.Kinds(); len(kinds) != 0 {
		t.Fatalf("zero-value Kinds = %#v, want empty", kinds)
	}
	if _, err := reg.Driver(model.RuntimeXray); !errors.Is(err, ErrUnsupportedRuntime) {
		t.Fatalf("zero-value Driver err = %v, want ErrUnsupportedRuntime", err)
	}

	var typedNil *nilableStubDriver
	if err := reg.Register(typedNil); !errors.Is(err, ErrNilDriver) {
		t.Fatalf("typed nil register err = %v, want ErrNilDriver", err)
	}

	if err := reg.Register(stubDriver{kind: model.RuntimeXray}); err != nil {
		t.Fatalf("zero-value register xray: %v", err)
	}
	got, err := reg.Driver(model.RuntimeXray)
	if err != nil {
		t.Fatalf("zero-value lookup xray: %v", err)
	}
	if got.Kind() != model.RuntimeXray {
		t.Fatalf("zero-value lookup kind = %q, want xray", got.Kind())
	}
}
