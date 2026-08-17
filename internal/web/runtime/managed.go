package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/mieru"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	awgdriver "github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver/amneziawg"
	mierudriver "github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver/mieru"
	naivedriver "github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver/naiveproxy"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/nodecommand"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/provisioner"
)

type ManagedRuntime interface {
	Runtime
	Driver(kind model.RuntimeKind) (driver.Driver, error)
	Provisioner() provisioner.Provisioner
}

func (l *Local) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	switch kind {
	case model.RuntimeAmneziaWG:
		backend := awg.NewCommandBackend()
		profile := awg.DockerBackendProfile()
		profile.ContainerName = provisioner.AWG2ContainerName
		backend.DockerProfile = profile
		return awgdriver.New(awg.NewRuntime(backend, nil)), nil
	case model.RuntimeMieru:
		return mierudriver.New(mieru.NewRuntime(mieru.OSRunner{}, mieru.OSFileSystem{})), nil
	case model.RuntimeNaiveProxy:
		rt, err := naiveproxy.NewRuntime(naiveproxy.NewOSRunner(), naiveproxy.NewOSConfigStore(), naiveproxy.NewHTTPSHealthVerifier(30*time.Second))
		if err != nil {
			return nil, err
		}
		return naivedriver.New(rt), nil
	}
	return legacyDriverFor(kind, l)
}

func (l *Local) Provisioner() provisioner.Provisioner {
	return provisioner.NewLocal(provisioner.Config{})
}

func (r *Remote) Driver(kind model.RuntimeKind) (driver.Driver, error) {
	switch kind {
	case model.RuntimeAmneziaWG, model.RuntimeMieru, model.RuntimeNaiveProxy:
		if r.node == nil || r.node.Id <= 0 || r.node.Guid == "" {
			return nil, fmt.Errorf("%w: remote node identity unavailable", driver.ErrUnsupportedRuntime)
		}
		return remoteManagedDriver{remote: r, kind: kind}, nil
	}
	return legacyDriverFor(kind, r)
}

func (r *Remote) Provisioner() provisioner.Provisioner {
	return remoteProvisioner{remote: r}
}

type remoteProvisioner struct {
	remote *Remote
}

func (p remoteProvisioner) Plan(kind model.RuntimeKind) provisioner.Plan {
	local := provisioner.NewLocal(provisioner.Config{}).Plan(kind)
	if p.remote == nil || p.remote.node == nil || p.remote.node.Id <= 0 || p.remote.node.Guid == "" {
		local.Supported = false
		local.Blocked = true
		local.Reason = "remote node identity unavailable"
	}
	return local
}

func (p remoteProvisioner) Install(ctx context.Context, kind model.RuntimeKind) (provisioner.Result, error) {
	return p.send(ctx, nodecommand.OperationRuntimeInstall, kind)
}

func (p remoteProvisioner) Update(ctx context.Context, kind model.RuntimeKind) (provisioner.Result, error) {
	return p.send(ctx, nodecommand.OperationRuntimeUpdate, kind)
}

func (p remoteProvisioner) Uninstall(ctx context.Context, kind model.RuntimeKind) (provisioner.Result, error) {
	return p.send(ctx, nodecommand.OperationRuntimeUninstall, kind)
}

func (p remoteProvisioner) send(ctx context.Context, op nodecommand.Operation, kind model.RuntimeKind) (provisioner.Result, error) {
	if p.remote == nil || p.remote.node == nil {
		return provisioner.Result{}, driver.ErrNilRuntime
	}
	plan := p.Plan(kind)
	if op != nodecommand.OperationRuntimeUninstall && (plan.Blocked || !plan.Supported) {
		return provisioner.Result{RuntimeKind: kind, ArtifactRef: plan.ArtifactRef, Version: plan.Version, State: "blocked", SummaryCode: "blocked"}, provisioner.ErrArtifactBlocked
	}
	node := p.remote.node
	now := time.Now().UTC()
	req := nodecommand.Request{
		Version:           nodecommand.ProtocolV1,
		SupportedVersions: []nodecommand.ProtocolVersion{nodecommand.ProtocolV1},
		CommandID:         fmt.Sprintf("runtime-%s-%s-%d", kind, op, now.UnixNano()),
		IdempotencyKey:    fmt.Sprintf("runtime-%s-%s-%d", kind, op, now.UnixNano()),
		NodeID:            node.Id,
		TargetGUID:        node.Guid,
		EndpointID:        1,
		RuntimeKind:       kind,
		Operation:         op,
		DesiredGeneration: now.UnixNano(),
		IssuedAt:          now,
		ExpiresAt:         now.Add(5 * time.Minute),
		Payload:           nodecommand.RuntimePayload{RuntimeKind: kind},
	}
	if op != nodecommand.OperationRuntimeUninstall {
		req.Payload = nodecommand.RuntimePayload{RuntimeKind: kind, ArtifactRef: plan.ArtifactRef}
	}
	session := nodecommand.NewAuthenticatedSession(node.Id, node.Guid, fmt.Sprintf("node-%d", node.Id), "node-command-v1", now.Add(-time.Second), now.Add(10*time.Minute))
	resp, err := p.remote.Send(ctx, session, req)
	if err != nil {
		return provisioner.Result{}, err
	}
	if resp.Status != nodecommand.StatusSucceeded {
		return provisioner.Result{RuntimeKind: kind, ArtifactRef: plan.ArtifactRef, Version: resp.Result.ArtifactVersion, State: string(resp.Result.State), SummaryCode: string(resp.SummaryCode)}, fmt.Errorf("remote runtime provision failed: %s", resp.SummaryCode)
	}
	return provisioner.Result{RuntimeKind: kind, ArtifactRef: resp.Result.ArtifactRef, Version: resp.Result.ArtifactVersion, State: string(resp.Result.State), SummaryCode: string(resp.SummaryCode)}, nil
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

type remoteManagedDriver struct {
	remote *Remote
	kind   model.RuntimeKind
}

func (d remoteManagedDriver) Kind() model.RuntimeKind { return d.kind }

func (d remoteManagedDriver) Capabilities() driver.Capabilities {
	return driver.Capabilities{EndpointLifecycle: true, Restart: true, Detect: true, Status: true, Health: true}
}

func (d remoteManagedDriver) Create(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, nodecommand.OperationEndpointApply, inbound, true)
}

func (d remoteManagedDriver) Update(ctx context.Context, _, newInbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, nodecommand.OperationEndpointApply, newInbound, true)
}

func (d remoteManagedDriver) Delete(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, nodecommand.OperationEndpointDelete, inbound, false)
}

func (d remoteManagedDriver) Enable(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, nodecommand.OperationEndpointApply, inbound, true)
}

func (d remoteManagedDriver) Disable(ctx context.Context, inbound *model.Inbound) (driver.EndpointResult, error) {
	return d.apply(ctx, nodecommand.OperationEndpointApply, inbound, true)
}

func (d remoteManagedDriver) Restart(ctx context.Context) error {
	_, err := d.send(ctx, nodecommand.OperationEndpointRestart, nil, nil, 1)
	return err
}

func (d remoteManagedDriver) Status(ctx context.Context, inbound *model.Inbound) (driver.StatusResult, error) {
	material := []byte(nil)
	if inbound != nil && strings.TrimSpace(inbound.Settings) != "" {
		material = []byte(inbound.Settings)
	}
	resp, err := d.send(ctx, nodecommand.OperationEndpointStatus, inbound, material, 1)
	if err != nil {
		return driver.StatusResult{}, err
	}
	status := model.EndpointDisabled
	if resp.Result.State == nodecommand.ResultStateRunning || resp.Result.State == nodecommand.ResultStateApplied {
		status = model.EndpointActive
	}
	return driver.StatusResult{RuntimeKind: d.kind, InboundId: inbound.Id, Tag: inbound.Tag, Enabled: inbound.Enable, Status: status}, nil
}

func (d remoteManagedDriver) Detect(ctx context.Context) (driver.DetectResult, error) {
	resp, err := d.send(ctx, nodecommand.OperationEndpointDetect, nil, nil, 1)
	return driver.DetectResult{RuntimeKind: d.kind, Available: err == nil && resp.Status == nodecommand.StatusSucceeded}, err
}

func (d remoteManagedDriver) Health(ctx context.Context, inbound *model.Inbound) (driver.HealthResult, error) {
	st, err := d.Status(ctx, inbound)
	if err != nil {
		return driver.HealthResult{}, err
	}
	return driver.HealthResult{RuntimeKind: d.kind, InboundId: inbound.Id, Tag: inbound.Tag, Status: st.Status}, nil
}

func (d remoteManagedDriver) PeerStatuses(ctx context.Context, inbound *model.Inbound) ([]driver.PeerStatusResult, error) {
	material := []byte(nil)
	if inbound != nil && strings.TrimSpace(inbound.Settings) != "" {
		material = []byte(inbound.Settings)
	}
	resp, err := d.send(ctx, nodecommand.OperationEndpointStatus, inbound, material, 1)
	if err != nil {
		return nil, err
	}
	if resp.Status != nodecommand.StatusSucceeded {
		return nil, fmt.Errorf("remote managed runtime failed: %s", resp.SummaryCode)
	}
	return resp.Result.Peers, nil
}

func (d remoteManagedDriver) Clients() driver.ClientDriver { return remoteUnsupportedClient{} }

func (d remoteManagedDriver) apply(ctx context.Context, op nodecommand.Operation, inbound *model.Inbound, withDesired bool) (driver.EndpointResult, error) {
	var material []byte
	if withDesired {
		material = []byte(inbound.Settings)
	}
	generation := int64(1)
	if len(material) > 0 {
		sum := sha256.Sum256(material)
		generation = int64(binary.BigEndian.Uint64(sum[:8]) & 0x3fffffffffffffff)
		if generation == 0 {
			generation = 1
		}
	}
	resp, err := d.send(ctx, op, inbound, material, generation)
	if err != nil {
		return driver.EndpointResult{}, err
	}
	status := model.EndpointActive
	if op == nodecommand.OperationEndpointDelete {
		status = model.EndpointDeleted
	} else if !inbound.Enable {
		status = model.EndpointDisabled
	}
	if resp.Status != nodecommand.StatusSucceeded {
		return driver.EndpointResult{}, fmt.Errorf("remote managed runtime failed: %s", resp.SummaryCode)
	}
	return driver.EndpointResult{RuntimeKind: d.kind, InboundId: inbound.Id, Tag: inbound.Tag, Enabled: inbound.Enable, Status: status}, nil
}

func (d remoteManagedDriver) send(ctx context.Context, op nodecommand.Operation, inbound *model.Inbound, material []byte, generation int64) (nodecommand.Response, error) {
	if d.remote == nil || d.remote.node == nil {
		return nodecommand.Response{}, driver.ErrNilRuntime
	}
	node := d.remote.node
	endpointID := 1
	tag := "managed"
	enable := true
	if inbound != nil {
		endpointID = inbound.Id
		tag = inbound.Tag
		enable = inbound.Enable
	}
	if generation <= 0 {
		generation = time.Now().UnixNano()
	}
	now := time.Now().UTC()
	req := nodecommand.Request{
		Version:           nodecommand.ProtocolV1,
		SupportedVersions: []nodecommand.ProtocolVersion{nodecommand.ProtocolV1},
		CommandID:         fmt.Sprintf("managed-%d-%s-%d", endpointID, op, generation),
		IdempotencyKey:    fmt.Sprintf("managed-%d-%s-%d", endpointID, op, generation),
		NodeID:            node.Id,
		TargetGUID:        node.Guid,
		EndpointID:        endpointID,
		RuntimeKind:       d.kind,
		Operation:         op,
		DesiredGeneration: generation,
		IssuedAt:          now,
		ExpiresAt:         now.Add(5 * time.Minute),
	}
	switch op {
	case nodecommand.OperationEndpointApply:
		req.Payload = nodecommand.EndpointPayload{Tag: tag}
	case nodecommand.OperationEndpointEnable, nodecommand.OperationEndpointDisable:
		req.Payload = nodecommand.EndpointPayload{Enable: &enable}
	}
	if len(material) > 0 {
		req.SecretInput = &nodecommand.SecretInput{Material: material}
	}
	session := nodecommand.NewAuthenticatedSession(node.Id, node.Guid, fmt.Sprintf("node-%d", node.Id), "node-command-v1", now.Add(-time.Second), now.Add(10*time.Minute))
	return d.remote.Send(ctx, session, req)
}

type remoteUnsupportedClient struct{}

func (remoteUnsupportedClient) Create(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (remoteUnsupportedClient) Update(context.Context, *model.Inbound, string, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (remoteUnsupportedClient) Delete(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (remoteUnsupportedClient) Enable(context.Context, *model.Inbound, model.Client) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (remoteUnsupportedClient) Disable(context.Context, *model.Inbound, string) (driver.ClientResult, error) {
	return driver.ClientResult{}, driver.ErrUnsupportedOperation
}

func (remoteUnsupportedClient) Status(context.Context, *model.Inbound, string) (driver.ClientStatusResult, error) {
	return driver.ClientStatusResult{}, driver.ErrUnsupportedOperation
}
