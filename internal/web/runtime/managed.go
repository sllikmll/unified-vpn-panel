package runtime

import (
	"fmt"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	awgdriver "github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver/amneziawg"
)

type ManagedRuntime interface {
	Runtime
	Driver(kind model.RuntimeKind) (driver.Driver, error)
}

func (l *Local) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	if kind == model.RuntimeAmneziaWG {
		return awgdriver.New(awg.NewRuntime(awg.NewCommandBackend(), nil)), nil
	}
	return legacyDriverFor(kind, l)
}

func (r *Remote) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	return legacyDriverFor(kind, r)
}

func legacyDriverFor(kind model.RuntimeKind, rt Runtime) (driver.Driver, error) {
	switch kind {
	case model.RuntimeXray:
		return driver.NewXrayAdapter(rt), nil
	case model.RuntimeMTProto:
		return driver.NewMTProtoAdapter(rt), nil
	default:
		return nil, fmt.Errorf("%w: %s", driver.ErrUnsupportedRuntime, kind)
	}
}
