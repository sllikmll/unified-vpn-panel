package driver

import (
	"context"
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type legacyAdapter struct {
	kind       model.RuntimeKind
	rt         legacyRuntime
	restart    bool
	clientCRUD bool
}

func NewXrayAdapter(rt legacyRuntime) Driver {
	return &legacyAdapter{
		kind:       model.RuntimeXray,
		rt:         rt,
		restart:    true,
		clientCRUD: true,
	}
}

func NewMTProtoAdapter(rt legacyRuntime) Driver {
	return &legacyAdapter{
		kind: model.RuntimeMTProto,
		rt:   rt,
	}
}

func (a *legacyAdapter) Kind() model.RuntimeKind { return a.kind }

func (a *legacyAdapter) Capabilities() Capabilities {
	return Capabilities{
		EndpointLifecycle: true,
		Restart:           a.restart,
		ClientCRUD:        a.clientCRUD,
	}
}

func (a *legacyAdapter) Create(ctx context.Context, inbound *model.Inbound) (EndpointResult, error) {
	if err := a.beforeEndpoint(ctx, inbound); err != nil {
		return EndpointResult{}, err
	}
	if err := a.rt.AddInbound(ctx, inbound); err != nil {
		return EndpointResult{}, err
	}
	return endpointResult(a.kind, inbound), nil
}

func (a *legacyAdapter) Update(ctx context.Context, oldInbound, newInbound *model.Inbound) (EndpointResult, error) {
	if err := checkContext(ctx); err != nil {
		return EndpointResult{}, err
	}
	if err := requireRuntime(a.rt); err != nil {
		return EndpointResult{}, err
	}
	if err := requireInbound(oldInbound); err != nil {
		return EndpointResult{}, err
	}
	if err := requireInbound(newInbound); err != nil {
		return EndpointResult{}, err
	}
	if err := requireProtocol(a.kind, oldInbound); err != nil {
		return EndpointResult{}, err
	}
	if err := requireProtocol(a.kind, newInbound); err != nil {
		return EndpointResult{}, err
	}
	if err := a.rt.UpdateInbound(ctx, oldInbound, newInbound); err != nil {
		return EndpointResult{}, err
	}
	return endpointResult(a.kind, newInbound), nil
}

func (a *legacyAdapter) Delete(ctx context.Context, inbound *model.Inbound) (EndpointResult, error) {
	if err := a.beforeEndpoint(ctx, inbound); err != nil {
		return EndpointResult{}, err
	}
	if err := a.rt.DelInbound(ctx, inbound); err != nil {
		return EndpointResult{}, err
	}
	return endpointResult(a.kind, inbound), nil
}

func (a *legacyAdapter) Enable(ctx context.Context, inbound *model.Inbound) (EndpointResult, error) {
	if err := a.beforeEndpoint(ctx, inbound); err != nil {
		return EndpointResult{}, err
	}
	next := *inbound
	next.Enable = true
	if err := a.rt.UpdateInbound(ctx, inbound, &next); err != nil {
		return EndpointResult{}, err
	}
	return endpointResult(a.kind, &next), nil
}

func (a *legacyAdapter) Disable(ctx context.Context, inbound *model.Inbound) (EndpointResult, error) {
	if err := a.beforeEndpoint(ctx, inbound); err != nil {
		return EndpointResult{}, err
	}
	next := *inbound
	next.Enable = false
	if err := a.rt.UpdateInbound(ctx, inbound, &next); err != nil {
		return EndpointResult{}, err
	}
	return endpointResult(a.kind, &next), nil
}

func (a *legacyAdapter) Restart(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := requireRuntime(a.rt); err != nil {
		return err
	}
	if !a.restart {
		return fmt.Errorf("%w: %s restart", ErrUnsupportedOperation, a.kind)
	}
	return a.rt.RestartXray(ctx)
}

func (a *legacyAdapter) Status(ctx context.Context, inbound *model.Inbound) (StatusResult, error) {
	if err := a.beforeEndpoint(ctx, inbound); err != nil {
		return StatusResult{}, err
	}
	return StatusResult{}, fmt.Errorf("%w: %s status", ErrUnsupportedOperation, a.kind)
}

func (a *legacyAdapter) Detect(ctx context.Context) (DetectResult, error) {
	if err := checkContext(ctx); err != nil {
		return DetectResult{}, err
	}
	if err := requireRuntime(a.rt); err != nil {
		return DetectResult{}, err
	}
	return DetectResult{}, fmt.Errorf("%w: %s detect", ErrUnsupportedOperation, a.kind)
}

func (a *legacyAdapter) Health(ctx context.Context, inbound *model.Inbound) (HealthResult, error) {
	if err := a.beforeEndpoint(ctx, inbound); err != nil {
		return HealthResult{}, err
	}
	return HealthResult{}, fmt.Errorf("%w: %s health", ErrUnsupportedOperation, a.kind)
}

func (a *legacyAdapter) Clients() ClientDriver {
	if !a.clientCRUD {
		return unsupportedClientDriver{kind: a.kind}
	}
	return legacyClientAdapter{kind: a.kind, rt: a.rt}
}

func (a *legacyAdapter) beforeEndpoint(ctx context.Context, inbound *model.Inbound) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := requireRuntime(a.rt); err != nil {
		return err
	}
	if err := requireInbound(inbound); err != nil {
		return err
	}
	return requireProtocol(a.kind, inbound)
}

type legacyClientAdapter struct {
	kind model.RuntimeKind
	rt   legacyRuntime
}

func (a legacyClientAdapter) Create(ctx context.Context, inbound *model.Inbound, client model.Client) (ClientResult, error) {
	if err := a.before(ctx, inbound); err != nil {
		return ClientResult{}, err
	}
	if err := a.rt.AddClient(ctx, inbound, client); err != nil {
		return ClientResult{}, err
	}
	return clientResult(a.kind, inbound, client.Email, client.Enable), nil
}

func (a legacyClientAdapter) Update(ctx context.Context, inbound *model.Inbound, oldEmail string, client model.Client) (ClientResult, error) {
	if err := a.before(ctx, inbound); err != nil {
		return ClientResult{}, err
	}
	if err := a.rt.UpdateUser(ctx, inbound, oldEmail, client); err != nil {
		return ClientResult{}, err
	}
	return clientResult(a.kind, inbound, client.Email, client.Enable), nil
}

func (a legacyClientAdapter) Delete(ctx context.Context, inbound *model.Inbound, email string) (ClientResult, error) {
	if err := a.before(ctx, inbound); err != nil {
		return ClientResult{}, err
	}
	if err := a.rt.DeleteUser(ctx, inbound, email); err != nil {
		return ClientResult{}, err
	}
	return clientResult(a.kind, inbound, email, false), nil
}

func (a legacyClientAdapter) Enable(ctx context.Context, inbound *model.Inbound, client model.Client) (ClientResult, error) {
	client.Enable = true
	return a.Create(ctx, inbound, client)
}

func (a legacyClientAdapter) Disable(ctx context.Context, inbound *model.Inbound, email string) (ClientResult, error) {
	return a.Delete(ctx, inbound, email)
}

func (a legacyClientAdapter) Status(ctx context.Context, inbound *model.Inbound, email string) (ClientStatusResult, error) {
	if err := a.before(ctx, inbound); err != nil {
		return ClientStatusResult{}, err
	}
	return ClientStatusResult{}, fmt.Errorf("%w: %s client status", ErrUnsupportedOperation, a.kind)
}

func (a legacyClientAdapter) before(ctx context.Context, inbound *model.Inbound) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := requireRuntime(a.rt); err != nil {
		return err
	}
	if err := requireInbound(inbound); err != nil {
		return err
	}
	return requireProtocol(a.kind, inbound)
}

type unsupportedClientDriver struct {
	kind model.RuntimeKind
}

func (d unsupportedClientDriver) Create(ctx context.Context, inbound *model.Inbound, _ model.Client) (ClientResult, error) {
	return d.unsupported(ctx, inbound, "client create")
}

func (d unsupportedClientDriver) Update(ctx context.Context, inbound *model.Inbound, _ string, _ model.Client) (ClientResult, error) {
	return d.unsupported(ctx, inbound, "client update")
}

func (d unsupportedClientDriver) Delete(ctx context.Context, inbound *model.Inbound, _ string) (ClientResult, error) {
	return d.unsupported(ctx, inbound, "client delete")
}

func (d unsupportedClientDriver) Enable(ctx context.Context, inbound *model.Inbound, _ model.Client) (ClientResult, error) {
	return d.unsupported(ctx, inbound, "client enable")
}

func (d unsupportedClientDriver) Disable(ctx context.Context, inbound *model.Inbound, _ string) (ClientResult, error) {
	return d.unsupported(ctx, inbound, "client disable")
}

func (d unsupportedClientDriver) Status(ctx context.Context, inbound *model.Inbound, _ string) (ClientStatusResult, error) {
	if err := checkContext(ctx); err != nil {
		return ClientStatusResult{}, err
	}
	if err := requireInbound(inbound); err != nil {
		return ClientStatusResult{}, err
	}
	if err := requireProtocol(d.kind, inbound); err != nil {
		return ClientStatusResult{}, err
	}
	return ClientStatusResult{}, fmt.Errorf("%w: %s client status", ErrUnsupportedOperation, d.kind)
}

func (d unsupportedClientDriver) unsupported(ctx context.Context, inbound *model.Inbound, operation string) (ClientResult, error) {
	if err := checkContext(ctx); err != nil {
		return ClientResult{}, err
	}
	if err := requireInbound(inbound); err != nil {
		return ClientResult{}, err
	}
	if err := requireProtocol(d.kind, inbound); err != nil {
		return ClientResult{}, err
	}
	return ClientResult{}, fmt.Errorf("%w: %s %s", ErrUnsupportedOperation, d.kind, operation)
}
