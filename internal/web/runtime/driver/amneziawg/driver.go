package amneziawg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

type Driver struct {
	rt *awg.Runtime
}

func New(rt *awg.Runtime) *Driver {
	return &Driver{rt: rt}
}

func (d *Driver) Kind() model.RuntimeKind { return model.RuntimeAmneziaWG }

func (d *Driver) Capabilities() driver.Capabilities {
	return driver.Capabilities{EndpointLifecycle: true, ClientCRUD: false, Detect: true, Status: true, Health: true}
}

func (d *Driver) Create(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, inbound)
}

func (d *Driver) Update(ctx context.Context, _, newInbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, newInbound)
}

func (d *Driver) Delete(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	cfg, err := configFromInbound(inbound)
	if err != nil {
		return driver.EndpointResult{}, err
	}
	if err := d.rt.Delete(ctx, cfg.Server.InterfaceName); err != nil {
		return driver.EndpointResult{}, err
	}
	return endpointResult(inbound, model.EndpointDeleted), nil
}

func (d *Driver) Enable(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	in := *inbound
	cfg, err := configFromInbound(&in)
	if err != nil {
		return driver.EndpointResult{}, err
	}
	cfg.Server.Enable = true
	raw, _ := json.Marshal(cfg)
	in.Enable = true
	in.Settings = string(raw)
	return d.apply(ctx, &in)
}

func (d *Driver) Disable(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	cfg, err := configFromInbound(inbound)
	if err != nil {
		return driver.EndpointResult{}, err
	}
	if err := d.rt.Delete(ctx, cfg.Server.InterfaceName); err != nil {
		return driver.EndpointResult{}, err
	}
	return endpointResult(inbound, model.EndpointDisabled), nil
}

func (d *Driver) Restart(context.Context) error { return driver.ErrUnsupportedOperation }

func (d *Driver) Status(ctx context.Context, inbound *model.Inbound) (driver.StatusResult, error) {
	cfg, err := configFromInbound(inbound)
	if err != nil {
		return driver.StatusResult{}, err
	}
	st, err := d.rt.Observe(ctx, cfg.Server.InterfaceName)
	if err != nil {
		return driver.StatusResult{}, err
	}
	status := model.EndpointDisabled
	if st.State == awg.StateRunning || st.State == awg.StateApplied {
		status = model.EndpointActive
	}
	return driver.StatusResult{RuntimeKind: model.RuntimeAmneziaWG, InboundId: inbound.Id, Tag: inbound.Tag, Enabled: inbound.Enable, Status: status}, nil
}

func (d *Driver) Detect(ctx context.Context) (driver.DetectResult, error) {
	st, err := d.rt.Detect(ctx)
	return driver.DetectResult{RuntimeKind: model.RuntimeAmneziaWG, Available: err == nil && st.Backend != awg.BackendNone}, err
}

func (d *Driver) Health(ctx context.Context, inbound *model.Inbound) (driver.HealthResult, error) {
	st, err := d.Status(ctx, inbound)
	if err != nil {
		return driver.HealthResult{}, err
	}
	return driver.HealthResult{RuntimeKind: model.RuntimeAmneziaWG, InboundId: inbound.Id, Tag: inbound.Tag, Status: st.Status}, nil
}

func (d *Driver) Clients() driver.ClientDriver { return clientDriver{d: d} }

func (d *Driver) apply(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	cfg, err := configFromInbound(inbound)
	if err != nil {
		return driver.EndpointResult{}, err
	}
	if !inbound.Enable || !cfg.Server.Enable {
		return d.Disable(ctx, inbound)
	}
	if err := d.rt.Apply(ctx, cfg); err != nil {
		return driver.EndpointResult{}, err
	}
	return endpointResult(inbound, model.EndpointActive), nil
}

type clientDriver struct{ d *Driver }

func (c clientDriver) Create(ctx context.Context, inbound *model.Inbound, client model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, awg.ErrPeerOperationsUnsupported
}

func (c clientDriver) Update(ctx context.Context, inbound *model.Inbound, oldEmail string, client model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, awg.ErrPeerOperationsUnsupported
}

func (c clientDriver) Delete(ctx context.Context, _ *model.Inbound, email string) (driver.ClientResult, error) {
	return driver.ClientResult{}, awg.ErrPeerOperationsUnsupported
}

func (c clientDriver) Enable(ctx context.Context, inbound *model.Inbound, client model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, awg.ErrPeerOperationsUnsupported
}

func (c clientDriver) Disable(ctx context.Context, _ *model.Inbound, email string) (driver.ClientResult, error) {
	return driver.ClientResult{}, awg.ErrPeerOperationsUnsupported
}

func (c clientDriver) Status(ctx context.Context, _ *model.Inbound, email string) (driver.ClientStatusResult, error) {
	return driver.ClientStatusResult{}, awg.ErrPeerOperationsUnsupported
}

func (c clientDriver) Export(ctx context.Context, clientID string) (string, error) {
	return "", awg.ErrPeerOperationsUnsupported
}

func (c clientDriver) upsert(ctx context.Context, _ *model.Inbound, email string, client awg.Client) (driver.ClientResult, error) {
	if err := c.d.rt.UpsertPeer(ctx, client); err != nil {
		return driver.ClientResult{}, err
	}
	return driver.ClientResult{RuntimeKind: model.RuntimeAmneziaWG, Email: email, Enabled: client.Enable}, nil
}

func configFromInbound(inbound *model.Inbound) (awg.DesiredConfig, error) {
	if inbound == nil {
		return awg.DesiredConfig{}, driver.ErrNilInbound
	}
	if inbound.Protocol != model.Protocol("amneziawg") {
		return awg.DesiredConfig{}, driver.ErrProtocolRuntimeMismatch
	}
	var cfg awg.DesiredConfig
	if err := json.Unmarshal([]byte(inbound.Settings), &cfg); err != nil {
		return cfg, fmt.Errorf("decode amneziawg settings: %w", err)
	}
	if cfg.Server.InterfaceName == "" {
		cfg.Server.InterfaceName = "awg0"
	}
	if cfg.Server.ListenPort == 0 {
		cfg.Server.ListenPort = inbound.Port
	}
	return cfg, nil
}

func modelClientToAWG(c model.Client) awg.Client {
	id := c.Email
	if c.ID != "" {
		id = c.ID
	}
	allowed := first(c.AllowedIPs)
	return awg.Client{ID: id, Email: c.Email, PrivateKey: c.PrivateKey, PublicKey: c.PublicKey, PresharedKey: c.PreSharedKey, IPv4Address: allowed, AllowedIPs: allowed, ClientAllowedIPs: "0.0.0.0/0", PersistentKeepalive: c.KeepAlive, Enable: c.Enable}
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	if parts := strings.Split(v[0], ","); len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return strings.TrimSpace(v[0])
}

func endpointResult(inbound *model.Inbound, status model.EndpointStatus) driver.EndpointResult {
	return driver.EndpointResult{RuntimeKind: model.RuntimeAmneziaWG, InboundId: inbound.Id, Tag: inbound.Tag, Enabled: inbound.Enable, Status: status}
}
