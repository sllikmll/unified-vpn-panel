package driver

import (
	"context"
	"errors"
	"reflect"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

var (
	ErrDuplicateRuntime        = errors.New("runtime driver already registered")
	ErrNilDriver               = errors.New("runtime driver is nil")
	ErrNilRuntime              = errors.New("runtime dependency is nil")
	ErrNilContext              = errors.New("context is nil")
	ErrNilInbound              = errors.New("inbound is nil")
	ErrUnsupportedRuntime      = errors.New("runtime kind is unsupported")
	ErrUnsupportedOperation    = errors.New("runtime operation is unsupported")
	ErrProtocolRuntimeMismatch = errors.New("inbound protocol does not match runtime driver")
)

type Capabilities struct {
	EndpointLifecycle bool
	Restart           bool
	ClientCRUD        bool
	Detect            bool
	Status            bool
	Health            bool
}

type EndpointResult struct {
	RuntimeKind model.RuntimeKind    `json:"runtimeKind"`
	InboundId   int                  `json:"inboundId,omitempty"`
	Tag         string               `json:"tag,omitempty"`
	Enabled     bool                 `json:"enabled"`
	Status      model.EndpointStatus `json:"status,omitempty"`
}

type StatusResult struct {
	RuntimeKind model.RuntimeKind    `json:"runtimeKind"`
	InboundId   int                  `json:"inboundId,omitempty"`
	Tag         string               `json:"tag,omitempty"`
	Enabled     bool                 `json:"enabled"`
	Status      model.EndpointStatus `json:"status"`
}

type DetectResult struct {
	RuntimeKind model.RuntimeKind `json:"runtimeKind"`
	Available   bool              `json:"available"`
}

type HealthResult struct {
	RuntimeKind model.RuntimeKind    `json:"runtimeKind"`
	InboundId   int                  `json:"inboundId,omitempty"`
	Tag         string               `json:"tag,omitempty"`
	Status      model.EndpointStatus `json:"status"`
	CheckedAt   int64                `json:"checkedAt,omitempty"`
}

type ClientResult struct {
	RuntimeKind model.RuntimeKind `json:"runtimeKind"`
	InboundId   int               `json:"inboundId,omitempty"`
	Tag         string            `json:"tag,omitempty"`
	Email       string            `json:"email,omitempty"`
	Enabled     bool              `json:"enabled"`
}

type ClientStatusResult struct {
	RuntimeKind model.RuntimeKind `json:"runtimeKind"`
	InboundId   int               `json:"inboundId,omitempty"`
	Tag         string            `json:"tag,omitempty"`
	Email       string            `json:"email,omitempty"`
	Enabled     bool              `json:"enabled"`
}

type Driver interface {
	Kind() model.RuntimeKind
	Capabilities() Capabilities
	Create(ctx context.Context, inbound *model.Inbound) (EndpointResult, error)
	Update(ctx context.Context, oldInbound, newInbound *model.Inbound) (EndpointResult, error)
	Delete(ctx context.Context, inbound *model.Inbound) (EndpointResult, error)
	Enable(ctx context.Context, inbound *model.Inbound) (EndpointResult, error)
	Disable(ctx context.Context, inbound *model.Inbound) (EndpointResult, error)
	Restart(ctx context.Context) error
	Status(ctx context.Context, inbound *model.Inbound) (StatusResult, error)
	Detect(ctx context.Context) (DetectResult, error)
	Health(ctx context.Context, inbound *model.Inbound) (HealthResult, error)
	Clients() ClientDriver
}

type Stopper interface {
	Stop(ctx context.Context, inbound *model.Inbound) error
}

type ClientDriver interface {
	Create(ctx context.Context, inbound *model.Inbound, client model.Client) (ClientResult, error)
	Update(ctx context.Context, inbound *model.Inbound, oldEmail string, client model.Client) (ClientResult, error)
	Delete(ctx context.Context, inbound *model.Inbound, email string) (ClientResult, error)
	Enable(ctx context.Context, inbound *model.Inbound, client model.Client) (ClientResult, error)
	Disable(ctx context.Context, inbound *model.Inbound, email string) (ClientResult, error)
	Status(ctx context.Context, inbound *model.Inbound, email string) (ClientStatusResult, error)
}

type legacyRuntime interface {
	Name() string
	AddInbound(ctx context.Context, ib *model.Inbound) error
	DelInbound(ctx context.Context, ib *model.Inbound) error
	UpdateInbound(ctx context.Context, oldIb, newIb *model.Inbound) error
	AddUser(ctx context.Context, ib *model.Inbound, userMap map[string]any) error
	RemoveUser(ctx context.Context, ib *model.Inbound, email string) error
	UpdateUser(ctx context.Context, ib *model.Inbound, email string, payload model.Client) error
	DeleteUser(ctx context.Context, ib *model.Inbound, email string) error
	AddClient(ctx context.Context, ib *model.Inbound, client model.Client) error
	DeleteClient(ctx context.Context, email string) error
	RestartXray(ctx context.Context) error
	ResetClientTraffic(ctx context.Context, ib *model.Inbound, email string) error
	ResetInboundTraffic(ctx context.Context, ib *model.Inbound) error
	ResetAllTraffics(ctx context.Context) error
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	return ctx.Err()
}

func requireRuntime(rt legacyRuntime) error {
	if isNilInterface(rt) {
		return ErrNilRuntime
	}
	return nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func requireInbound(inbound *model.Inbound) error {
	if inbound == nil {
		return ErrNilInbound
	}
	return nil
}

func requireProtocol(kind model.RuntimeKind, inbound *model.Inbound) error {
	if kind == model.RuntimeMTProto {
		if inbound.Protocol != model.MTProto {
			return ErrProtocolRuntimeMismatch
		}
		return nil
	}
	if kind == model.RuntimeXray && inbound.Protocol == model.MTProto {
		return ErrProtocolRuntimeMismatch
	}
	return nil
}

func endpointResult(kind model.RuntimeKind, inbound *model.Inbound) EndpointResult {
	status := model.EndpointActive
	if !inbound.Enable {
		status = model.EndpointDisabled
	}
	return EndpointResult{
		RuntimeKind: kind,
		InboundId:   inbound.Id,
		Tag:         inbound.Tag,
		Enabled:     inbound.Enable,
		Status:      status,
	}
}

func clientResult(kind model.RuntimeKind, inbound *model.Inbound, email string, enabled bool) ClientResult {
	return ClientResult{
		RuntimeKind: kind,
		InboundId:   inbound.Id,
		Tag:         inbound.Tag,
		Email:       email,
		Enabled:     enabled,
	}
}
