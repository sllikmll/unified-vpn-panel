package nodecommand

import "errors"

var (
	ErrUnsupportedVersion   = errors.New("node command version unsupported")
	ErrUnsupportedRuntime   = errors.New("node command runtime unsupported")
	ErrUnsupportedOperation = errors.New("node command operation unsupported")
	ErrUnknownField         = errors.New("node command unknown field")
	ErrDuplicateField       = errors.New("node command duplicate field")
	ErrTrailingJSON         = errors.New("node command trailing json")
	ErrForbiddenField       = errors.New("node command forbidden field")
	ErrPayloadMismatch      = errors.New("node command payload mismatch")
	ErrPayloadTooLarge      = errors.New("node command payload too large")
	ErrInvalidJSON          = errors.New("node command invalid json")
	ErrMissingField         = errors.New("node command missing field")
	ErrInvalidField         = errors.New("node command invalid field")
	ErrExpired              = errors.New("node command expired")
	ErrNotYetValid          = errors.New("node command not yet valid")
	ErrUnauthenticated      = errors.New("node command unauthenticated")
	ErrNodeMismatch         = errors.New("node command node mismatch")
	ErrTargetMismatch       = errors.New("node command target mismatch")
	ErrInvalidContext       = errors.New("node command invalid context")
	ErrUnsafeResponse       = errors.New("node command unsafe response")
	ErrReplayInProgress     = errors.New("node command replay in progress")
	ErrReplayKeyConflict    = errors.New("node command replay key conflict")
	ErrReplayMissingEntry   = errors.New("node command replay missing entry")
	ErrReplayCapacity       = errors.New("node command replay capacity exhausted")
)
