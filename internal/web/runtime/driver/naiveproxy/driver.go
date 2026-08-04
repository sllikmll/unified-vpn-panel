package naiveproxy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	core "github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

type Driver struct {
	rt *core.Runtime
}

func New(rt *core.Runtime) *Driver { return &Driver{rt: rt} }

func (d *Driver) Kind() model.RuntimeKind { return model.RuntimeNaiveProxy }

func (d *Driver) Capabilities() driver.Capabilities {
	return driver.Capabilities{EndpointLifecycle: true, Restart: true, ClientCRUD: false, Detect: true, Status: true, Health: true}
}

func (d *Driver) Create(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, inbound)
}

func (d *Driver) Update(ctx context.Context, _, newInbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, newInbound)
}

func (d *Driver) Delete(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	if err := require(inbound); err != nil {
		return driver.EndpointResult{}, err
	}
	if d.rt == nil {
		return driver.EndpointResult{}, driver.ErrNilRuntime
	}
	if err := d.rt.Delete(ctx); err != nil {
		return driver.EndpointResult{}, err
	}
	return result(inbound, model.EndpointDeleted), nil
}

func (d *Driver) Enable(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	if _, err := d.apply(ctx, inbound); err != nil {
		return driver.EndpointResult{}, err
	}
	return result(inbound, model.EndpointActive), d.rt.Start(ctx)
}

func (d *Driver) Disable(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	if err := require(inbound); err != nil {
		return driver.EndpointResult{}, err
	}
	if d.rt == nil {
		return driver.EndpointResult{}, driver.ErrNilRuntime
	}
	if err := d.rt.Stop(ctx); err != nil {
		return driver.EndpointResult{}, err
	}
	return result(inbound, model.EndpointDisabled), nil
}

func (d *Driver) Stop(ctx context.Context, inbound *model.Inbound) error {
	if err := require(inbound); err != nil {
		return err
	}
	if d.rt == nil {
		return driver.ErrNilRuntime
	}
	return d.rt.Stop(ctx)
}

func (d *Driver) Restart(ctx context.Context) error {
	if d.rt == nil {
		return driver.ErrNilRuntime
	}
	return d.rt.Restart(ctx)
}

func (d *Driver) Status(ctx context.Context, inbound *model.Inbound) (driver.StatusResult, error) {
	if err := require(inbound); err != nil {
		return driver.StatusResult{}, err
	}
	if d.rt == nil {
		return driver.StatusResult{}, driver.ErrNilRuntime
	}
	obs, err := d.rt.Observe(ctx)
	if err != nil {
		return driver.StatusResult{}, err
	}
	status := model.EndpointDisabled
	if obs.State == core.BackendActive || obs.State == core.BackendReloading {
		status = model.EndpointActive
	}
	return driver.StatusResult{RuntimeKind: model.RuntimeNaiveProxy, InboundId: inbound.Id, Tag: inbound.Tag, Enabled: inbound.Enable, Status: status}, nil
}

func (d *Driver) Detect(ctx context.Context) (driver.DetectResult, error) {
	if d.rt == nil {
		return driver.DetectResult{}, driver.ErrNilRuntime
	}
	obs, err := d.rt.Detect(ctx)
	return driver.DetectResult{RuntimeKind: model.RuntimeNaiveProxy, Available: err == nil && obs.State != core.BackendNotFound}, err
}

func (d *Driver) Health(ctx context.Context, inbound *model.Inbound) (driver.HealthResult, error) {
	st, err := d.Status(ctx, inbound)
	if err != nil {
		return driver.HealthResult{}, err
	}
	return driver.HealthResult{RuntimeKind: model.RuntimeNaiveProxy, InboundId: inbound.Id, Tag: inbound.Tag, Status: st.Status}, nil
}

func (d *Driver) Clients() driver.ClientDriver { return unsupported{} }

func (d *Driver) apply(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	if err := require(inbound); err != nil {
		return driver.EndpointResult{}, err
	}
	if d.rt == nil {
		return driver.EndpointResult{}, driver.ErrNilRuntime
	}
	var payload struct {
		Endpoint core.Endpoint `json:"endpoint"`
		Users    []core.User   `json:"users"`
	}
	if err := json.Unmarshal([]byte(inbound.Settings), &payload); err != nil {
		return driver.EndpointResult{}, fmt.Errorf("decode naiveproxy settings: %w", err)
	}
	server := core.Server{Endpoint: payload.Endpoint, Users: payload.Users}
	if err := d.rt.Apply(ctx, server); err != nil {
		return driver.EndpointResult{}, err
	}
	return result(inbound, model.EndpointActive), nil
}

func require(inbound *model.Inbound) error {
	if inbound == nil {
		return driver.ErrNilInbound
	}
	if inbound.Protocol != model.Protocol("naiveproxy") {
		return driver.ErrProtocolRuntimeMismatch
	}
	return nil
}

func result(inbound *model.Inbound, status model.EndpointStatus) driver.EndpointResult {
	return driver.EndpointResult{RuntimeKind: model.RuntimeNaiveProxy, InboundId: inbound.Id, Tag: inbound.Tag, Enabled: inbound.Enable, Status: status}
}

type unsupported struct{}

func (unsupported) Create(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (unsupported) Update(context.Context, *model.Inbound, string, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (unsupported) Delete(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (unsupported) Enable(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (unsupported) Disable(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (unsupported) Status(context.Context, *model.Inbound, string) (driver.ClientStatusResult, error) {
	return driver.ClientStatusResult{}, driver.ErrUnsupportedOperation
}
