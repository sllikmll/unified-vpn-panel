package nodecommand

import (
	"fmt"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type Status string

const (
	StatusAccepted  Status = "accepted"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusReplayed  Status = "replayed"
)

type ErrorCode string

const (
	ErrorCodeNone                 ErrorCode = ""
	ErrorCodeUnsupportedVersion   ErrorCode = "unsupported_version"
	ErrorCodeUnsupportedRuntime   ErrorCode = "unsupported_runtime"
	ErrorCodeUnsupportedOperation ErrorCode = "unsupported_operation"
	ErrorCodeValidationFailed     ErrorCode = "validation_failed"
	ErrorCodeExpired              ErrorCode = "expired"
	ErrorCodeReplayConflict       ErrorCode = "replay_conflict"
	ErrorCodeRuntimeFailed        ErrorCode = "runtime_failed"
	ErrorCodeUnavailable          ErrorCode = "unavailable"
	ErrorCodeUnauthorized         ErrorCode = "unauthorized"
)

type SummaryCode string

const (
	SummaryNone                 SummaryCode = ""
	SummaryAccepted             SummaryCode = "accepted"
	SummaryApplied              SummaryCode = "applied"
	SummaryDeleted              SummaryCode = "deleted"
	SummaryExported             SummaryCode = "exported"
	SummaryStatusAvailable      SummaryCode = "status_available"
	SummaryUnsupportedOperation SummaryCode = "unsupported_operation"
	SummaryValidationFailed     SummaryCode = "validation_failed"
	SummaryExpired              SummaryCode = "expired"
	SummaryReplayConflict       SummaryCode = "replay_conflict"
	SummaryRuntimeFailed        SummaryCode = "runtime_failed"
	SummaryUnavailable          SummaryCode = "unavailable"
	SummaryUnauthorized         SummaryCode = "unauthorized"
)

type ResultState string

const (
	ResultStateUnknown   ResultState = ""
	ResultStatePending   ResultState = "pending"
	ResultStateRunning   ResultState = "running"
	ResultStateStopped   ResultState = "stopped"
	ResultStateApplied   ResultState = "applied"
	ResultStateDeleted   ResultState = "deleted"
	ResultStateExported  ResultState = "exported"
	ResultStateHealthy   ResultState = "healthy"
	ResultStateUnhealthy ResultState = "unhealthy"
	ResultStateEnabled   ResultState = "enabled"
	ResultStateDisabled  ResultState = "disabled"
)

type Response struct {
	Version            ProtocolVersion `json:"version"`
	CommandID          string          `json:"commandId"`
	IdempotencyKey     string          `json:"idempotencyKey,omitempty"`
	NodeID             int             `json:"nodeId"`
	TargetGUID         string          `json:"targetGuid"`
	Status             Status          `json:"status"`
	Operation          Operation       `json:"operation,omitempty"`
	DesiredGeneration  int64           `json:"desiredGeneration,omitempty"`
	ObservedGeneration int64           `json:"observedGeneration,omitempty"`
	ErrorCode          ErrorCode       `json:"errorCode,omitempty"`
	SummaryCode        SummaryCode     `json:"summaryCode,omitempty"`
	Result             Result          `json:"result,omitempty"`
	SealedResult       string          `json:"sealedResult,omitempty"`
	SecretOutput       []byte          `json:"-"`
}

type Result struct {
	RuntimeKind model.RuntimeKind `json:"runtimeKind,omitempty"`
	EndpointID  int               `json:"endpointId,omitempty"`
	ClientID    string            `json:"clientId,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	State       ResultState       `json:"state,omitempty"`
}

func (r Response) Validate() error {
	if r.Version != ProtocolV1 {
		return fmt.Errorf("%w: version", ErrUnsafeResponse)
	}
	if strings.TrimSpace(r.CommandID) == "" {
		return fmt.Errorf("%w: commandId", ErrUnsafeResponse)
	}
	if !isSafeBoundedToken(r.CommandID, MaxCommandIDLength) {
		return fmt.Errorf("%w: commandId", ErrUnsafeResponse)
	}
	if r.IdempotencyKey != "" && !isSafeBoundedToken(r.IdempotencyKey, MaxIdempotencyKeyLength) {
		return fmt.Errorf("%w: idempotencyKey", ErrUnsafeResponse)
	}
	if r.NodeID <= 0 {
		return fmt.Errorf("%w: nodeId", ErrUnsafeResponse)
	}
	if !isSafeBoundedToken(r.TargetGUID, MaxTargetGUIDLength) {
		return fmt.Errorf("%w: targetGuid", ErrUnsafeResponse)
	}
	if !isAllowedStatus(r.Status) {
		return fmt.Errorf("%w: status", ErrUnsafeResponse)
	}
	if !isAllowedErrorCode(r.ErrorCode) {
		return fmt.Errorf("%w: errorCode", ErrUnsafeResponse)
	}
	if !isAllowedSummaryCode(r.SummaryCode) {
		return fmt.Errorf("%w: summaryCode", ErrUnsafeResponse)
	}
	if r.Operation != "" && !isSupportedOperation(r.Operation) {
		return fmt.Errorf("%w: operation", ErrUnsafeResponse)
	}
	if r.DesiredGeneration < 0 || r.DesiredGeneration > MaxDesiredGeneration {
		return fmt.Errorf("%w: desiredGeneration", ErrUnsafeResponse)
	}
	if r.ObservedGeneration < 0 || r.ObservedGeneration > MaxDesiredGeneration {
		return fmt.Errorf("%w: observedGeneration", ErrUnsafeResponse)
	}
	if err := r.Result.Validate(); err != nil {
		return err
	}
	if r.SealedResult != "" && !isSafeSealedPayload(r.SealedResult) {
		return fmt.Errorf("%w: sealedResult", ErrUnsafeResponse)
	}
	return validateResponseStatusConsistency(r)
}

func (r Response) ValidateFor(req Request) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.CommandID != req.CommandID {
		return fmt.Errorf("%w: commandId correlation", ErrUnsafeResponse)
	}
	if r.IdempotencyKey != req.IdempotencyKey {
		return fmt.Errorf("%w: idempotencyKey correlation", ErrUnsafeResponse)
	}
	if r.NodeID != req.NodeID {
		return fmt.Errorf("%w: nodeId correlation", ErrUnsafeResponse)
	}
	if r.TargetGUID != req.TargetGUID {
		return fmt.Errorf("%w: targetGuid correlation", ErrUnsafeResponse)
	}
	if r.Operation != req.Operation {
		return fmt.Errorf("%w: operation correlation", ErrUnsafeResponse)
	}
	if r.DesiredGeneration != req.DesiredGeneration {
		return fmt.Errorf("%w: desiredGeneration correlation", ErrUnsafeResponse)
	}
	if r.Result.hasFields() {
		if r.Result.RuntimeKind != req.RuntimeKind {
			return fmt.Errorf("%w: result.runtimeKind correlation", ErrUnsafeResponse)
		}
		if r.Result.EndpointID != req.EndpointID {
			return fmt.Errorf("%w: result.endpointId correlation", ErrUnsafeResponse)
		}
	}
	if strings.HasPrefix(string(req.Operation), "client.") {
		payload, ok := req.Payload.(ClientPayload)
		if !ok {
			return fmt.Errorf("%w: client payload correlation", ErrUnsafeResponse)
		}
		if r.Result.ClientID != "" && r.Result.ClientID != payload.ClientID {
			return fmt.Errorf("%w: result.clientId correlation", ErrUnsafeResponse)
		}
	} else if r.Result.ClientID != "" {
		return fmt.Errorf("%w: result.clientId forbidden", ErrUnsafeResponse)
	}
	return nil
}

func isAllowedStatus(status Status) bool {
	switch status {
	case StatusAccepted, StatusSucceeded, StatusFailed, StatusReplayed:
		return true
	default:
		return false
	}
}

func isAllowedErrorCode(code ErrorCode) bool {
	switch code {
	case ErrorCodeNone, ErrorCodeUnsupportedVersion, ErrorCodeUnsupportedRuntime, ErrorCodeUnsupportedOperation, ErrorCodeValidationFailed, ErrorCodeExpired, ErrorCodeReplayConflict, ErrorCodeRuntimeFailed, ErrorCodeUnavailable, ErrorCodeUnauthorized:
		return true
	default:
		return false
	}
}

func isAllowedSummaryCode(code SummaryCode) bool {
	switch code {
	case SummaryNone, SummaryAccepted, SummaryApplied, SummaryDeleted, SummaryExported, SummaryStatusAvailable, SummaryUnsupportedOperation, SummaryValidationFailed, SummaryExpired, SummaryReplayConflict, SummaryRuntimeFailed, SummaryUnavailable, SummaryUnauthorized:
		return true
	default:
		return false
	}
}

func validateResponseStatusConsistency(r Response) error {
	switch r.Status {
	case StatusFailed:
		if r.ErrorCode == ErrorCodeNone {
			return fmt.Errorf("%w: errorCode required", ErrUnsafeResponse)
		}
		if !isFailureSummaryCode(r.SummaryCode) {
			return fmt.Errorf("%w: summaryCode failed", ErrUnsafeResponse)
		}
		return nil
	case StatusAccepted:
		if r.ErrorCode != ErrorCodeNone {
			return fmt.Errorf("%w: errorCode forbidden", ErrUnsafeResponse)
		}
		if r.SummaryCode != SummaryNone && r.SummaryCode != SummaryAccepted {
			return fmt.Errorf("%w: summaryCode accepted", ErrUnsafeResponse)
		}
		if r.Result.State != ResultStateUnknown && r.Result.State != ResultStatePending {
			return fmt.Errorf("%w: result.state accepted", ErrUnsafeResponse)
		}
		return nil
	case StatusSucceeded, StatusReplayed:
		if r.ErrorCode != ErrorCodeNone {
			return fmt.Errorf("%w: errorCode forbidden", ErrUnsafeResponse)
		}
		if !isSuccessSummaryCode(r.SummaryCode) {
			return fmt.Errorf("%w: summaryCode success", ErrUnsafeResponse)
		}
		if !isSuccessResultState(r.Result.State) {
			return fmt.Errorf("%w: result.state success", ErrUnsafeResponse)
		}
		return nil
	default:
		return fmt.Errorf("%w: status", ErrUnsafeResponse)
	}
}

func isFailureSummaryCode(code SummaryCode) bool {
	switch code {
	case SummaryUnsupportedOperation, SummaryValidationFailed, SummaryExpired, SummaryReplayConflict, SummaryRuntimeFailed, SummaryUnavailable, SummaryUnauthorized:
		return true
	default:
		return false
	}
}

func isSuccessSummaryCode(code SummaryCode) bool {
	switch code {
	case SummaryNone, SummaryAccepted, SummaryApplied, SummaryDeleted, SummaryExported, SummaryStatusAvailable:
		return true
	default:
		return false
	}
}

func isSuccessResultState(state ResultState) bool {
	switch state {
	case ResultStateUnknown, ResultStatePending, ResultStateRunning, ResultStateStopped, ResultStateApplied, ResultStateDeleted, ResultStateExported, ResultStateHealthy, ResultStateUnhealthy, ResultStateEnabled, ResultStateDisabled:
		return true
	default:
		return false
	}
}

func (r Result) Validate() error {
	if r.RuntimeKind != "" && !isSupportedRuntime(r.RuntimeKind) {
		return fmt.Errorf("%w: result.runtimeKind", ErrUnsafeResponse)
	}
	if r.EndpointID < 0 || r.EndpointID > MaxEndpointID {
		return fmt.Errorf("%w: result.endpointId", ErrUnsafeResponse)
	}
	if r.ClientID != "" && !isSafeBoundedToken(r.ClientID, MaxClientIDLength) {
		return fmt.Errorf("%w: result.clientId", ErrUnsafeResponse)
	}
	if !isAllowedResultState(r.State) {
		return fmt.Errorf("%w: result.state", ErrUnsafeResponse)
	}
	return nil
}

func (r Result) hasFields() bool {
	return r.RuntimeKind != "" || r.EndpointID != 0 || r.ClientID != "" || r.Enabled != nil || r.State != ResultStateUnknown
}

func isAllowedResultState(state ResultState) bool {
	switch state {
	case ResultStateUnknown, ResultStatePending, ResultStateRunning, ResultStateStopped, ResultStateApplied, ResultStateDeleted, ResultStateExported, ResultStateHealthy, ResultStateUnhealthy, ResultStateEnabled, ResultStateDisabled:
		return true
	default:
		return false
	}
}
