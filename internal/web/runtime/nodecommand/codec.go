package nodecommand

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const DefaultMaxRequestBytes int64 = 1 << 20

var allowedJSONFields = map[string]struct{}{
	"version": {}, "supportedVersions": {}, "commandId": {}, "idempotencyKey": {},
	"nodeId": {}, "targetGuid": {}, "endpointId": {}, "runtimeKind": {}, "operation": {},
	"desiredGeneration": {}, "issuedAt": {}, "expiresAt": {}, "payload": {},
	"tag": {}, "enable": {}, "clientId": {}, "email": {},
}

type DecodeOptions struct {
	MaxBytes int64
	Now      func() time.Time
}

func DecodeRequest(r io.Reader, opts DecodeOptions) (Request, error) {
	limit := opts.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxRequestBytes
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return Request{}, err
	}
	if int64(len(raw)) > limit {
		return Request{}, ErrPayloadTooLarge
	}
	if err := scanStrictJSON(raw); err != nil {
		return Request{}, err
	}

	var wire requestWire
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return Request{}, classifyJSONError(err)
	}
	if dec.More() {
		return Request{}, ErrTrailingJSON
	}
	var payload Payload
	if len(wire.Payload) > 0 && string(wire.Payload) != "null" {
		decoded, err := decodePayload(wire.Operation, wire.Payload)
		if err != nil {
			return Request{}, err
		}
		payload = decoded
	}
	negotiated, err := NegotiateVersion(wire.SupportedVersions)
	if err != nil {
		return Request{}, err
	}
	req := Request{
		Version:            wire.Version,
		SupportedVersions:  wire.SupportedVersions,
		CommandID:          wire.CommandID,
		IdempotencyKey:     wire.IdempotencyKey,
		NodeID:             wire.NodeID,
		TargetGUID:         wire.TargetGUID,
		EndpointID:         wire.EndpointID,
		RuntimeKind:        wire.RuntimeKind,
		Operation:          wire.Operation,
		DesiredGeneration:  wire.DesiredGeneration,
		IssuedAt:           wire.IssuedAt,
		ExpiresAt:          wire.ExpiresAt,
		Payload:            payload,
		rawPayload:         append(json.RawMessage(nil), wire.Payload...),
		negotiatedProtocol: negotiated,
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now()
	}
	if err := req.Validate(now); err != nil {
		return Request{}, err
	}
	return req, nil
}

func decodePayload(operation Operation, raw json.RawMessage) (Payload, error) {
	if strings.HasPrefix(string(operation), "client.") {
		var payload ClientPayload
		if err := decodeStrict(raw, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
	var payload EndpointPayload
	if err := decodeStrict(raw, &payload); err != nil {
		if payloadLooksClient(raw) {
			return nil, fmt.Errorf("%w: %s received client payload", ErrPayloadMismatch, operation)
		}
		return nil, err
	}
	return payload, nil
}

func payloadLooksClient(raw json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	_, hasClientID := probe["clientId"]
	_, hasEmail := probe["email"]
	return hasClientID || hasEmail
}

func decodeStrict(raw []byte, target any) error {
	if err := scanStrictJSON(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return classifyJSONError(err)
	}
	if token, err := dec.Token(); err != io.EOF {
		if err != nil {
			return classifyJSONError(err)
		}
		return fmt.Errorf("%w: %v", ErrTrailingJSON, token)
	}
	return nil
}

func scanStrictJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := scanValue(dec); err != nil {
		return err
	}
	if token, err := dec.Token(); err != io.EOF {
		if err != nil {
			return classifyJSONError(err)
		}
		return fmt.Errorf("%w: %v", ErrTrailingJSON, token)
	}
	return nil
}

func scanValue(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return classifyJSONError(err)
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			seen := map[string]struct{}{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return classifyJSONError(err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return ErrInvalidJSON
				}
				if isForbiddenJSONField(key) {
					return fmt.Errorf("%w: %s", ErrForbiddenField, key)
				}
				if _, allowed := allowedJSONFields[key]; !allowed {
					return fmt.Errorf("%w: %s", ErrUnknownField, key)
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("%w: %s", ErrDuplicateField, key)
				}
				seen[key] = struct{}{}
				if err := scanValue(dec); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil {
				return classifyJSONError(err)
			}
			if end != json.Delim('}') {
				return ErrInvalidJSON
			}
		case '[':
			for dec.More() {
				if err := scanValue(dec); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil {
				return classifyJSONError(err)
			}
			if end != json.Delim(']') {
				return ErrInvalidJSON
			}
		default:
			return ErrInvalidJSON
		}
	}
	return nil
}

func isForbiddenJSONField(key string) bool {
	switch strings.ToLower(key) {
	case "command", "args", "env", "stderr", "stdout", "privatekey", "sshprivatekey", "sshpassword", "sshprivatekeypassphrase", "password":
		return true
	default:
		return false
	}
}

func classifyJSONError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "unknown field") {
		return fmt.Errorf("%w: %v", ErrUnknownField, err)
	}
	if err == io.EOF {
		return fmt.Errorf("%w: eof", ErrInvalidJSON)
	}
	return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
}
