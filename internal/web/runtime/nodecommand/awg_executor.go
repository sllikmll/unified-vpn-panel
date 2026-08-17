package nodecommand

import (
	"context"
	"encoding/json"
	"errors"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/mieru"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/provisioner"
)

type DriverProvider interface {
	Driver(kind model.RuntimeKind) (driver.Driver, error)
}

type ProvisionerProvider interface {
	Provisioner() provisioner.Provisioner
}

type AWGExecutor struct {
	Provider        DriverProvider
	Provisioners    ProvisionerProvider
	ResponseSealKey []byte
}

func (e AWGExecutor) nodeCommandExecutor() {}

func (e AWGExecutor) Execute(ctx context.Context, session AuthenticatedSession, req Request) (Response, error) {
	return ExecutorFunc(e.execute).Execute(ctx, session, req)
}

func (e AWGExecutor) execute(ctx context.Context, _ AuthenticatedSession, req Request) (Response, error) {
	resp := baseResponse(req)
	switch req.Operation {
	case OperationRuntimeInstallPlan:
		return e.runtimePlan(ctx, req)
	case OperationRuntimeInstall, OperationRuntimeUpdate, OperationRuntimeUninstall:
		return e.runtimeLifecycle(ctx, req)
	}
	if e.Provider == nil {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnavailable
		resp.SummaryCode = SummaryUnavailable
		return resp, nil
	}
	d, err := e.Provider.Driver(req.RuntimeKind)
	if err != nil {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnsupportedRuntime
		resp.SummaryCode = SummaryUnsupportedOperation
		return resp, nil
	}
	switch req.Operation {
	case OperationEndpointApply:
		inbound, err := inboundFromRequest(req)
		if err != nil {
			return validationResponse(req), nil
		}
		if _, err := d.Create(ctx, inbound); err != nil {
			resp.Status = StatusFailed
			resp.ErrorCode = ErrorCodeRuntimeFailed
			resp.SummaryCode = SummaryRuntimeFailed
			return resp, nil
		}
		resp.Status = StatusSucceeded
		resp.SummaryCode = SummaryApplied
		resp.Result = Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, State: ResultStateApplied}
		return resp, nil
	case OperationEndpointDelete:
		inbound, err := inboundFromRefs(req)
		if err != nil {
			return validationResponse(req), nil
		}
		if _, err := d.Delete(ctx, inbound); err != nil {
			resp.Status = StatusFailed
			resp.ErrorCode = ErrorCodeRuntimeFailed
			resp.SummaryCode = SummaryRuntimeFailed
			return resp, nil
		}
		resp.Status = StatusSucceeded
		resp.SummaryCode = SummaryDeleted
		resp.Result = Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, State: ResultStateDeleted}
		return resp, nil
	case OperationEndpointStatus, OperationEndpointHealth:
		var inbound *model.Inbound
		var err error
		if req.SecretInput != nil && len(req.SecretInput.Material) > 0 {
			inbound, err = inboundFromRequest(req)
		} else {
			inbound, err = inboundFromRefs(req)
		}
		if err != nil {
			return validationResponse(req), nil
		}
		status, err := d.Status(ctx, inbound)
		if err != nil {
			resp.Status = StatusFailed
			resp.ErrorCode = ErrorCodeRuntimeFailed
			resp.SummaryCode = SummaryRuntimeFailed
			return resp, nil
		}
		state := ResultStateStopped
		if status.Status == model.EndpointActive {
			state = ResultStateRunning
		}
		resp.Status = StatusSucceeded
		resp.SummaryCode = SummaryStatusAvailable
		result := Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, State: state}
		if observer, ok := d.(driver.PeerObserver); ok {
			peers, peerErr := observer.PeerStatuses(ctx, inbound)
			if peerErr != nil {
				resp.Status = StatusFailed
				resp.ErrorCode = ErrorCodeRuntimeFailed
				resp.SummaryCode = SummaryRuntimeFailed
				return resp, nil
			}
			result.Peers = peers
		}
		resp.Result = result
		return resp, nil
	case OperationEndpointDetect:
		detect, err := d.Detect(ctx)
		if err != nil {
			resp.Status = StatusFailed
			resp.ErrorCode = ErrorCodeRuntimeFailed
			resp.SummaryCode = SummaryRuntimeFailed
			return resp, nil
		}
		state := ResultStateStopped
		if detect.Available {
			state = ResultStateRunning
		}
		resp.Status = StatusSucceeded
		resp.SummaryCode = SummaryStatusAvailable
		resp.Result = Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, State: state}
		return resp, nil
	case OperationClientCreate, OperationClientUpdate, OperationClientDelete, OperationClientEnable, OperationClientDisable, OperationClientStatus, OperationClientExport:
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnsupportedOperation
		resp.SummaryCode = SummaryUnsupportedOperation
		return resp, nil
	default:
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnsupportedOperation
		resp.SummaryCode = SummaryUnsupportedOperation
		return resp, nil
	}
}

func (e AWGExecutor) runtimePlan(_ context.Context, req Request) (Response, error) {
	resp := baseResponse(req)
	p, err := e.provisioner()
	if err != nil {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnavailable
		resp.SummaryCode = SummaryUnavailable
		return resp, nil
	}
	plan := p.Plan(req.RuntimeKind)
	resp.Result = Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, ArtifactRef: plan.ArtifactRef, ArtifactVersion: plan.Version}
	if plan.Blocked || !plan.Supported {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeRuntimeFailed
		resp.SummaryCode = SummaryBlocked
		resp.Result.State = ResultStateStopped
		return resp, nil
	}
	resp.Status = StatusSucceeded
	resp.SummaryCode = SummaryStatusAvailable
	resp.Result.State = ResultStateHealthy
	return resp, nil
}

func (e AWGExecutor) runtimeLifecycle(ctx context.Context, req Request) (Response, error) {
	resp := baseResponse(req)
	payload, ok := req.Payload.(RuntimePayload)
	if !ok || payload.RuntimeKind != req.RuntimeKind {
		return validationResponse(req), nil
	}
	p, err := e.provisioner()
	if err != nil {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnavailable
		resp.SummaryCode = SummaryUnavailable
		return resp, nil
	}
	plan := p.Plan(req.RuntimeKind)
	if req.Operation == OperationRuntimeUninstall {
		if payload.ArtifactRef != "" {
			return validationResponse(req), nil
		}
	} else if payload.ArtifactRef != plan.ArtifactRef {
		return validationResponse(req), nil
	}
	var result provisioner.Result
	switch req.Operation {
	case OperationRuntimeInstall:
		result, err = p.Install(ctx, req.RuntimeKind)
	case OperationRuntimeUpdate:
		result, err = p.Update(ctx, req.RuntimeKind)
	case OperationRuntimeUninstall:
		result, err = p.Uninstall(ctx, req.RuntimeKind)
	}
	resp.Result = Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, ArtifactRef: result.ArtifactRef, ArtifactVersion: result.Version, State: ResultStateRunning}
	if req.Operation == OperationRuntimeUninstall {
		resp.Result.State = ResultStateDeleted
	}
	if result.State == "blocked" {
		resp.Result.State = ResultStateStopped
	}
	if err != nil {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeRuntimeFailed
		resp.SummaryCode = SummaryRuntimeFailed
		if errors.Is(err, provisioner.ErrArtifactBlocked) {
			resp.SummaryCode = SummaryBlocked
		}
		return resp, nil
	}
	resp.Status = StatusSucceeded
	switch req.Operation {
	case OperationRuntimeInstall:
		resp.SummaryCode = SummaryInstalled
	case OperationRuntimeUpdate:
		resp.SummaryCode = SummaryUpdated
	case OperationRuntimeUninstall:
		resp.SummaryCode = SummaryUninstalled
	}
	return resp, nil
}

func (e AWGExecutor) provisioner() (provisioner.Provisioner, error) {
	if e.Provisioners != nil {
		p := e.Provisioners.Provisioner()
		if p != nil {
			return p, nil
		}
	}
	return provisioner.NewLocal(provisioner.Config{}), nil
}

func baseResponse(req Request) Response {
	return Response{Version: ProtocolV1, CommandID: req.CommandID, IdempotencyKey: req.IdempotencyKey, NodeID: req.NodeID, TargetGUID: req.TargetGUID, Operation: req.Operation, DesiredGeneration: req.DesiredGeneration}
}

func validationResponse(req Request) Response {
	resp := baseResponse(req)
	resp.Status = StatusFailed
	resp.ErrorCode = ErrorCodeValidationFailed
	resp.SummaryCode = SummaryValidationFailed
	return resp
}

func inboundFromRequest(req Request) (*model.Inbound, error) {
	if req.SecretInput == nil || len(req.SecretInput.Material) == 0 {
		return nil, errors.New("missing sealed material")
	}
	payload, _ := req.Payload.(EndpointPayload)
	switch req.RuntimeKind {
	case model.RuntimeAmneziaWG:
		var desired awg.DesiredConfig
		if err := json.Unmarshal(req.SecretInput.Material, &desired); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(desired)
		tag := payload.Tag
		if tag == "" {
			tag = desired.Server.InterfaceName
		}
		return &model.Inbound{Id: req.EndpointID, Tag: tag, Port: desired.Server.ListenPort, Protocol: model.Protocol("amneziawg"), Enable: desired.Server.Enable, Settings: string(raw)}, nil
	case model.RuntimeMieru:
		var desired mieru.ServerConfig
		if err := json.Unmarshal(req.SecretInput.Material, &desired); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(desired)
		port := 0
		if len(desired.PortBindings) > 0 {
			port = desired.PortBindings[0].Port
		}
		return &model.Inbound{Id: req.EndpointID, Tag: payload.Tag, Port: port, Protocol: model.Protocol("mieru"), Enable: true, Settings: string(raw)}, nil
	case model.RuntimeNaiveProxy:
		var desired struct {
			Endpoint naiveproxy.Endpoint `json:"endpoint"`
			Users    []naiveproxy.User   `json:"users"`
		}
		if err := json.Unmarshal(req.SecretInput.Material, &desired); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(desired)
		return &model.Inbound{Id: req.EndpointID, Tag: payload.Tag, Port: desired.Endpoint.Port, Protocol: model.Protocol("naiveproxy"), Enable: true, Settings: string(raw)}, nil
	default:
		return nil, driver.ErrUnsupportedRuntime
	}
}

func inboundFromRefs(req Request) (*model.Inbound, error) {
	if req.RuntimeKind == model.RuntimeMieru {
		return &model.Inbound{Id: req.EndpointID, Tag: "mieru", Port: 0, Protocol: model.Protocol("mieru"), Enable: true, Settings: `{"portBindings":[{"port":1,"protocol":"TCP"}]}`}, nil
	}
	if req.RuntimeKind == model.RuntimeNaiveProxy {
		return &model.Inbound{Id: req.EndpointID, Tag: "naiveproxy", Port: 443, Protocol: model.Protocol("naiveproxy"), Enable: true, Settings: `{"endpoint":{"Domain":"example.test","ListenIP":"127.0.0.1","Port":443},"users":[{"ID":"placeholder","Username":"placeholder","Password":"placeholder-password","Enabled":true}]}`}, nil
	}
	if req.SecretInput == nil || req.SecretInput.Refs == nil || req.SecretInput.Refs["interfaceName"] == "" {
		return nil, errors.New("missing interface ref")
	}
	server := awg.DefaultServer(req.SecretInput.Refs["interfaceName"], 51820)
	server.PrivateKey = "redacted-private-placeholder"
	server.PublicKey = "redacted-public-placeholder"
	raw, _ := json.Marshal(awg.DesiredConfig{Server: server})
	return &model.Inbound{Id: req.EndpointID, Tag: req.SecretInput.Refs["interfaceName"], Port: server.ListenPort, Protocol: model.Protocol("amneziawg"), Enable: true, Settings: string(raw)}, nil
}
