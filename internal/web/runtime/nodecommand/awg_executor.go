package nodecommand

import (
	"context"
	"encoding/json"
	"errors"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

type DriverProvider interface {
	Driver(kind model.RuntimeKind) (driver.Driver, error)
}

type AWGExecutor struct {
	Provider        DriverProvider
	ResponseSealKey []byte
}

func (e AWGExecutor) nodeCommandExecutor() {}

func (e AWGExecutor) Execute(ctx context.Context, session AuthenticatedSession, req Request) (Response, error) {
	return ExecutorFunc(e.execute).Execute(ctx, session, req)
}

func (e AWGExecutor) execute(ctx context.Context, _ AuthenticatedSession, req Request) (Response, error) {
	resp := baseResponse(req)
	if req.RuntimeKind != model.RuntimeAmneziaWG {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnsupportedRuntime
		resp.SummaryCode = SummaryUnsupportedOperation
		return resp, nil
	}
	if e.Provider == nil {
		resp.Status = StatusFailed
		resp.ErrorCode = ErrorCodeUnavailable
		resp.SummaryCode = SummaryUnavailable
		return resp, nil
	}
	d, err := e.Provider.Driver(model.RuntimeAmneziaWG)
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
		inbound, err := inboundFromRefs(req)
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
		resp.Result = Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, State: state}
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

func clientResult(req Request, clientID string, enabled bool) Result {
	state := ResultStateDisabled
	if enabled {
		state = ResultStateEnabled
	}
	return Result{RuntimeKind: req.RuntimeKind, EndpointID: req.EndpointID, ClientID: clientID, Enabled: &enabled, State: state}
}

func clientFromRequest(req Request) (model.Client, error) {
	if req.SecretInput == nil || len(req.SecretInput.Material) == 0 {
		return model.Client{}, errors.New("missing sealed material")
	}
	var client awg.Client
	if err := json.Unmarshal(req.SecretInput.Material, &client); err != nil {
		return model.Client{}, err
	}
	payload, _ := req.Payload.(ClientPayload)
	if client.ID == "" {
		client.ID = payload.ClientID
	}
	if client.Email == "" {
		client.Email = payload.Email
	}
	if err := awg.ValidateClient(client); err != nil {
		return model.Client{}, err
	}
	return model.Client{
		ID:           client.ID,
		Email:        client.Email,
		PrivateKey:   client.PrivateKey,
		PublicKey:    client.PublicKey,
		PreSharedKey: client.PresharedKey,
		AllowedIPs:   []string{client.IPv4Address},
		KeepAlive:    client.PersistentKeepalive,
		Enable:       client.Enable,
	}, nil
}

func inboundFromRequest(req Request) (*model.Inbound, error) {
	if req.SecretInput == nil || len(req.SecretInput.Material) == 0 {
		return nil, errors.New("missing sealed material")
	}
	var desired awg.DesiredConfig
	if err := json.Unmarshal(req.SecretInput.Material, &desired); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(desired)
	payload, _ := req.Payload.(EndpointPayload)
	tag := payload.Tag
	if tag == "" {
		tag = desired.Server.InterfaceName
	}
	return &model.Inbound{Id: req.EndpointID, Tag: tag, Port: desired.Server.ListenPort, Protocol: model.Protocol("amneziawg"), Enable: desired.Server.Enable, Settings: string(raw)}, nil
}

func inboundFromRefs(req Request) (*model.Inbound, error) {
	if req.SecretInput == nil || req.SecretInput.Refs == nil || req.SecretInput.Refs["interfaceName"] == "" {
		return nil, errors.New("missing interface ref")
	}
	server := awg.DefaultServer(req.SecretInput.Refs["interfaceName"], 51820)
	server.PrivateKey = "redacted-private-placeholder"
	server.PublicKey = "redacted-public-placeholder"
	raw, _ := json.Marshal(awg.DesiredConfig{Server: server})
	return &model.Inbound{Id: req.EndpointID, Tag: req.SecretInput.Refs["interfaceName"], Port: server.ListenPort, Protocol: model.Protocol("amneziawg"), Enable: true, Settings: string(raw)}, nil
}
