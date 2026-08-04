package nodecommand

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type ProtocolVersion string

const ProtocolV1 ProtocolVersion = "v1"

const (
	MaxCommandIDLength      = 128
	MaxIdempotencyKeyLength = 128
	MaxTargetGUIDLength     = 128
	MaxClientIDLength       = 128
	MaxEndpointTagLength    = 128
	MaxEmailLength          = 254
	MaxEndpointID           = 1<<31 - 1
	MaxDesiredGeneration    = 1<<62 - 1
	MaxIssuedAtFutureSkew   = 2 * time.Minute
	MaxCommandLifetime      = 15 * time.Minute
	MaxSecretMaterialBytes  = 64 * 1024
	MaxSecretRefCount       = 16
	MaxSecretRefKeyLength   = 128
	MaxSecretRefValueLength = 4096
)

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

var supportedVersions = []ProtocolVersion{ProtocolV1}

type Operation string

const (
	OperationEndpointApply     Operation = "endpoint.apply"
	OperationEndpointDelete    Operation = "endpoint.delete"
	OperationEndpointEnable    Operation = "endpoint.enable"
	OperationEndpointDisable   Operation = "endpoint.disable"
	OperationEndpointStart     Operation = "endpoint.start"
	OperationEndpointStop      Operation = "endpoint.stop"
	OperationEndpointRestart   Operation = "endpoint.restart"
	OperationEndpointReconcile Operation = "endpoint.reconcile"
	OperationEndpointStatus    Operation = "endpoint.status"
	OperationEndpointHealth    Operation = "endpoint.health"
	OperationEndpointDetect    Operation = "endpoint.detect"
	OperationClientCreate      Operation = "client.create"
	OperationClientUpdate      Operation = "client.update"
	OperationClientDelete      Operation = "client.delete"
	OperationClientEnable      Operation = "client.enable"
	OperationClientDisable     Operation = "client.disable"
	OperationClientStatus      Operation = "client.status"
	OperationClientExport      Operation = "client.export"
)

type Payload interface {
	nodeCommandPayload()
}

type EndpointPayload struct {
	Tag    string `json:"tag,omitempty"`
	Enable *bool  `json:"enable,omitempty"`
}

func (EndpointPayload) nodeCommandPayload() {}

type ClientPayload struct {
	ClientID string `json:"clientId,omitempty"`
	Email    string `json:"email,omitempty"`
	Enable   *bool  `json:"enable,omitempty"`
}

func (ClientPayload) nodeCommandPayload() {}

type SecretInput struct {
	Material []byte
	Refs     map[string]string
}

type Request struct {
	Version            ProtocolVersion   `json:"version"`
	SupportedVersions  []ProtocolVersion `json:"supportedVersions"`
	CommandID          string            `json:"commandId"`
	IdempotencyKey     string            `json:"idempotencyKey"`
	NodeID             int               `json:"nodeId"`
	TargetGUID         string            `json:"targetGuid"`
	EndpointID         int               `json:"endpointId"`
	RuntimeKind        model.RuntimeKind `json:"runtimeKind"`
	Operation          Operation         `json:"operation"`
	DesiredGeneration  int64             `json:"desiredGeneration"`
	IssuedAt           time.Time         `json:"issuedAt"`
	ExpiresAt          time.Time         `json:"expiresAt"`
	Payload            Payload           `json:"-"`
	SecretInput        *SecretInput      `json:"-"`
	SealedPayload      string            `json:"sealedPayload,omitempty"`
	rawPayload         json.RawMessage
	negotiatedProtocol ProtocolVersion
}

type requestWire struct {
	Version           ProtocolVersion   `json:"version"`
	SupportedVersions []ProtocolVersion `json:"supportedVersions"`
	CommandID         string            `json:"commandId"`
	IdempotencyKey    string            `json:"idempotencyKey"`
	NodeID            int               `json:"nodeId"`
	TargetGUID        string            `json:"targetGuid"`
	EndpointID        int               `json:"endpointId"`
	RuntimeKind       model.RuntimeKind `json:"runtimeKind"`
	Operation         Operation         `json:"operation"`
	DesiredGeneration int64             `json:"desiredGeneration"`
	IssuedAt          time.Time         `json:"issuedAt"`
	ExpiresAt         time.Time         `json:"expiresAt"`
	Payload           json.RawMessage   `json:"payload,omitempty"`
	SealedPayload     string            `json:"sealedPayload,omitempty"`
}

func NegotiateVersion(offered []ProtocolVersion) (ProtocolVersion, error) {
	for i := len(supportedVersions) - 1; i >= 0; i-- {
		for _, version := range offered {
			if version == supportedVersions[i] {
				return version, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %v", ErrUnsupportedVersion, offered)
}

func (r Request) MarshalJSON() ([]byte, error) {
	var payload any
	switch p := r.Payload.(type) {
	case nil:
	case EndpointPayload:
		payload = p
	case ClientPayload:
		payload = p
	default:
		return nil, fmt.Errorf("%w: unsupported payload type %T", ErrPayloadMismatch, r.Payload)
	}
	return json.Marshal(struct {
		Version           ProtocolVersion   `json:"version"`
		SupportedVersions []ProtocolVersion `json:"supportedVersions"`
		CommandID         string            `json:"commandId"`
		IdempotencyKey    string            `json:"idempotencyKey"`
		NodeID            int               `json:"nodeId"`
		TargetGUID        string            `json:"targetGuid"`
		EndpointID        int               `json:"endpointId"`
		RuntimeKind       model.RuntimeKind `json:"runtimeKind"`
		Operation         Operation         `json:"operation"`
		DesiredGeneration int64             `json:"desiredGeneration"`
		IssuedAt          time.Time         `json:"issuedAt"`
		ExpiresAt         time.Time         `json:"expiresAt"`
		Payload           any               `json:"payload,omitempty"`
		SealedPayload     string            `json:"sealedPayload,omitempty"`
	}{
		Version:           r.Version,
		SupportedVersions: r.SupportedVersions,
		CommandID:         r.CommandID,
		IdempotencyKey:    r.IdempotencyKey,
		NodeID:            r.NodeID,
		TargetGUID:        r.TargetGUID,
		EndpointID:        r.EndpointID,
		RuntimeKind:       r.RuntimeKind,
		Operation:         r.Operation,
		DesiredGeneration: r.DesiredGeneration,
		IssuedAt:          r.IssuedAt,
		ExpiresAt:         r.ExpiresAt,
		Payload:           payload,
		SealedPayload:     r.SealedPayload,
	})
}

func (r Request) Validate(now time.Time) error {
	if r.Version != ProtocolV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, r.Version)
	}
	if _, err := NegotiateVersion(r.SupportedVersions); err != nil {
		return err
	}
	if strings.TrimSpace(r.CommandID) == "" {
		return fmt.Errorf("%w: commandId", ErrMissingField)
	}
	if !isSafeBoundedToken(r.CommandID, MaxCommandIDLength) {
		return fmt.Errorf("%w: commandId", ErrInvalidField)
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("%w: idempotencyKey", ErrMissingField)
	}
	if !isSafeBoundedToken(r.IdempotencyKey, MaxIdempotencyKeyLength) {
		return fmt.Errorf("%w: idempotencyKey", ErrInvalidField)
	}
	if r.NodeID <= 0 {
		return fmt.Errorf("%w: nodeId", ErrInvalidField)
	}
	if strings.TrimSpace(r.TargetGUID) == "" {
		return fmt.Errorf("%w: targetGuid", ErrMissingField)
	}
	if !isSafeBoundedToken(r.TargetGUID, MaxTargetGUIDLength) {
		return fmt.Errorf("%w: targetGuid", ErrInvalidField)
	}
	if r.EndpointID <= 0 || r.EndpointID > MaxEndpointID {
		return fmt.Errorf("%w: endpointId", ErrInvalidField)
	}
	if r.DesiredGeneration <= 0 || r.DesiredGeneration > MaxDesiredGeneration {
		return fmt.Errorf("%w: desiredGeneration", ErrInvalidField)
	}
	if r.IssuedAt.IsZero() {
		return fmt.Errorf("%w: issuedAt", ErrMissingField)
	}
	if r.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: expiresAt", ErrMissingField)
	}
	if !now.IsZero() {
		if r.IssuedAt.After(now.Add(MaxIssuedAtFutureSkew)) {
			return fmt.Errorf("%w: issuedAt", ErrNotYetValid)
		}
		if !now.Before(r.ExpiresAt) {
			return fmt.Errorf("%w: expiresAt", ErrExpired)
		}
	}
	if !r.IssuedAt.Before(r.ExpiresAt) {
		return fmt.Errorf("%w: issuedAt/expiresAt", ErrInvalidField)
	}
	if r.ExpiresAt.Sub(r.IssuedAt) > MaxCommandLifetime {
		return fmt.Errorf("%w: lifetime", ErrInvalidField)
	}
	if !isSupportedRuntime(r.RuntimeKind) {
		return fmt.Errorf("%w: %s", ErrUnsupportedRuntime, r.RuntimeKind)
	}
	if !isSupportedOperation(r.Operation) {
		return fmt.Errorf("%w: %s", ErrUnsupportedOperation, r.Operation)
	}
	if err := validatePayloadForOperation(r.Operation, r.Payload); err != nil {
		return err
	}
	if err := validateSecretInput(r.SecretInput); err != nil {
		return err
	}
	if r.SealedPayload != "" && !isSafeSealedPayload(r.SealedPayload) {
		return fmt.Errorf("%w: sealedPayload", ErrInvalidField)
	}
	return nil
}

func isSupportedRuntime(kind model.RuntimeKind) bool {
	switch kind {
	case model.RuntimeXray, model.RuntimeMTProto, model.RuntimeWireGuard, model.RuntimeAmneziaWG, model.RuntimeMieru, model.RuntimeNaiveProxy:
		return true
	default:
		return false
	}
}

func isSupportedOperation(operation Operation) bool {
	switch operation {
	case OperationEndpointApply, OperationEndpointDelete, OperationEndpointEnable, OperationEndpointDisable, OperationEndpointStart, OperationEndpointStop, OperationEndpointRestart, OperationEndpointReconcile, OperationEndpointStatus, OperationEndpointHealth, OperationEndpointDetect, OperationClientCreate, OperationClientUpdate, OperationClientDelete, OperationClientEnable, OperationClientDisable, OperationClientStatus, OperationClientExport:
		return true
	default:
		return false
	}
}

func validatePayloadForOperation(operation Operation, payload Payload) error {
	switch operation {
	case OperationEndpointApply:
		p, ok := payload.(EndpointPayload)
		if !ok {
			return fmt.Errorf("%w: %s requires endpoint payload", ErrPayloadMismatch, operation)
		}
		return validateEndpointPayload(p, true)
	case OperationEndpointEnable, OperationEndpointDisable:
		p, ok := payload.(EndpointPayload)
		if !ok {
			return fmt.Errorf("%w: %s requires endpoint payload", ErrPayloadMismatch, operation)
		}
		if strings.TrimSpace(p.Tag) != "" {
			return fmt.Errorf("%w: tag", ErrForbiddenField)
		}
		if p.Enable == nil {
			return fmt.Errorf("%w: enable", ErrMissingField)
		}
		if operation == OperationEndpointEnable && !*p.Enable {
			return fmt.Errorf("%w: enable", ErrInvalidField)
		}
		if operation == OperationEndpointDisable && *p.Enable {
			return fmt.Errorf("%w: enable", ErrInvalidField)
		}
		return nil
	case OperationEndpointDelete, OperationEndpointStart, OperationEndpointStop, OperationEndpointRestart, OperationEndpointReconcile, OperationEndpointStatus, OperationEndpointHealth, OperationEndpointDetect:
		if payload != nil {
			return fmt.Errorf("%w: %s rejects payload", ErrPayloadMismatch, operation)
		}
		return nil
	case OperationClientCreate, OperationClientUpdate, OperationClientDelete, OperationClientEnable, OperationClientDisable, OperationClientStatus, OperationClientExport:
		p, ok := payload.(ClientPayload)
		if !ok {
			return fmt.Errorf("%w: %s requires client payload", ErrPayloadMismatch, operation)
		}
		return validateClientPayload(operation, p)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedOperation, operation)
	}
}

func validateEndpointPayload(payload EndpointPayload, allowTag bool) error {
	if strings.TrimSpace(payload.Tag) == "" {
		return fmt.Errorf("%w: tag", ErrMissingField)
	}
	if payload.Enable != nil {
		return fmt.Errorf("%w: enable", ErrForbiddenField)
	}
	if !allowTag {
		return fmt.Errorf("%w: tag", ErrForbiddenField)
	}
	if !isSafeBoundedToken(payload.Tag, MaxEndpointTagLength) {
		return fmt.Errorf("%w: tag", ErrInvalidField)
	}
	return nil
}

func validateClientPayload(operation Operation, payload ClientPayload) error {
	if strings.TrimSpace(payload.ClientID) == "" {
		return fmt.Errorf("%w: clientId", ErrMissingField)
	}
	if !isSafeBoundedToken(payload.ClientID, MaxClientIDLength) {
		return fmt.Errorf("%w: clientId", ErrInvalidField)
	}
	switch operation {
	case OperationClientCreate:
		if strings.TrimSpace(payload.Email) == "" {
			return fmt.Errorf("%w: email", ErrMissingField)
		}
		return validateEmail(payload.Email)
	case OperationClientUpdate:
		if strings.TrimSpace(payload.Email) == "" && payload.Enable == nil {
			return fmt.Errorf("%w: client update field", ErrMissingField)
		}
		if strings.TrimSpace(payload.Email) != "" {
			return validateEmail(payload.Email)
		}
		return nil
	case OperationClientDelete, OperationClientStatus, OperationClientExport:
		if strings.TrimSpace(payload.Email) != "" || payload.Enable != nil {
			return fmt.Errorf("%w: client delete/status field", ErrForbiddenField)
		}
		return nil
	case OperationClientEnable, OperationClientDisable:
		if strings.TrimSpace(payload.Email) != "" {
			return fmt.Errorf("%w: email", ErrForbiddenField)
		}
		if payload.Enable != nil {
			if operation == OperationClientEnable && !*payload.Enable {
				return fmt.Errorf("%w: enable", ErrInvalidField)
			}
			if operation == OperationClientDisable && *payload.Enable {
				return fmt.Errorf("%w: enable", ErrInvalidField)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedOperation, operation)
	}
}

func validateEmail(email string) error {
	if len(email) > MaxEmailLength || strings.TrimSpace(email) != email || strings.ContainsAny(email, "\x00\r\n\t /\\") {
		return fmt.Errorf("%w: email", ErrInvalidField)
	}
	at := strings.Count(email, "@")
	if at != 1 || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return fmt.Errorf("%w: email", ErrInvalidField)
	}
	return nil
}

func validateSecretInput(secret *SecretInput) error {
	if secret == nil {
		return nil
	}
	if len(secret.Material) > MaxSecretMaterialBytes {
		return fmt.Errorf("%w: secret material", ErrInvalidField)
	}
	if len(secret.Refs) > MaxSecretRefCount {
		return fmt.Errorf("%w: secret refs", ErrInvalidField)
	}
	for key, value := range secret.Refs {
		if !isSafeBoundedToken(key, MaxSecretRefKeyLength) {
			return fmt.Errorf("%w: secret ref key", ErrInvalidField)
		}
		if len(value) > MaxSecretRefValueLength || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: secret ref value", ErrInvalidField)
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("%w: secret ref value", ErrInvalidField)
			}
		}
	}
	return nil
}

func isSafeSealedPayload(value string) bool {
	if len(value) > MaxSecretMaterialBytes*2 {
		return false
	}
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isSafeBoundedToken(value string, maxLen int) bool {
	if value == "" || len(value) > maxLen || strings.TrimSpace(value) != value {
		return false
	}
	if strings.Contains(value, "..") || strings.ContainsAny(value, "/\\") {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return safeIDPattern.MatchString(value)
}

func secretInputDigest(secret *SecretInput) string {
	if secret == nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte("material:"))
	materialDigest := sha256.Sum256(secret.Material)
	h.Write([]byte(hex.EncodeToString(materialDigest[:])))
	h.Write([]byte("\nrefs:"))
	keys := make([]string, 0, len(secret.Refs))
	for key := range secret.Refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		keyDigest := sha256.Sum256([]byte(key))
		valueDigest := sha256.Sum256([]byte(secret.Refs[key]))
		h.Write([]byte(hex.EncodeToString(keyDigest[:])))
		h.Write([]byte("="))
		h.Write([]byte(hex.EncodeToString(valueDigest[:])))
		h.Write([]byte(";"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
