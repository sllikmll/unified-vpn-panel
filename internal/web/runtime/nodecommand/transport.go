package nodecommand

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	MaxSessionChannelIDLength = 128
	MaxSessionPrincipalLength = 128
	MaxSessionLifetime        = 24 * time.Hour
)

type AuthenticatedSession struct {
	nodeID          int
	channelID       string
	principal       string
	authenticatedAt time.Time
	expiresAt       time.Time
}

func newAuthenticatedSession(nodeID int, principal, channelID string, authenticatedAt, expiresAt time.Time) AuthenticatedSession {
	return AuthenticatedSession{
		nodeID:          nodeID,
		channelID:       channelID,
		principal:       principal,
		authenticatedAt: authenticatedAt,
		expiresAt:       expiresAt,
	}
}

func (s AuthenticatedSession) NodeID() int {
	return s.nodeID
}

func (s AuthenticatedSession) ChannelID() string {
	return s.channelID
}

func (s AuthenticatedSession) Principal() string {
	return s.principal
}

func (s AuthenticatedSession) AuthenticatedAt() time.Time {
	return s.authenticatedAt
}

func (s AuthenticatedSession) ExpiresAt() time.Time {
	return s.expiresAt
}

func (s AuthenticatedSession) Valid() bool {
	return s.validate(time.Now().UTC(), s.nodeID) == nil
}

func (s AuthenticatedSession) validate(now time.Time, requestNodeID int) error {
	if s.nodeID <= 0 || strings.TrimSpace(s.channelID) == "" || strings.TrimSpace(s.principal) == "" || s.authenticatedAt.IsZero() || s.expiresAt.IsZero() {
		return ErrUnauthenticated
	}
	if !isSafeBoundedToken(s.channelID, MaxSessionChannelIDLength) || !isSafeBoundedToken(s.principal, MaxSessionPrincipalLength) {
		return ErrUnauthenticated
	}
	if requestNodeID <= 0 {
		return fmt.Errorf("%w: request nodeId", ErrInvalidField)
	}
	if s.nodeID != requestNodeID {
		return ErrNodeMismatch
	}
	if !s.authenticatedAt.Before(s.expiresAt) {
		return fmt.Errorf("%w: authenticatedAt/expiresAt", ErrUnauthenticated)
	}
	if s.expiresAt.Sub(s.authenticatedAt) > MaxSessionLifetime {
		return fmt.Errorf("%w: session lifetime", ErrUnauthenticated)
	}
	if !now.IsZero() {
		if s.authenticatedAt.After(now) {
			return fmt.Errorf("%w: authenticatedAt", ErrNotYetValid)
		}
		if !now.Before(s.expiresAt) {
			return fmt.Errorf("%w: session", ErrExpired)
		}
	}
	return nil
}

type Transport interface {
	Send(ctx context.Context, session AuthenticatedSession, req Request) (Response, error)
	nodeCommandTransport()
}

type TransportFunc func(context.Context, AuthenticatedSession, Request) (Response, error)

func (TransportFunc) nodeCommandTransport() {}

func (f TransportFunc) Send(ctx context.Context, session AuthenticatedSession, req Request) (Response, error) {
	if ctx == nil {
		return Response{}, ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	now := time.Now().UTC()
	if err := req.Validate(now); err != nil {
		return Response{}, err
	}
	if err := session.validate(now, req.NodeID); err != nil {
		return Response{}, err
	}
	if f == nil {
		return Response{}, fmt.Errorf("%w: transport func", ErrInvalidField)
	}
	resp, err := f(ctx, session, req)
	if err != nil {
		return Response{}, err
	}
	if err := resp.ValidateFor(req); err != nil {
		return Response{}, err
	}
	return resp, nil
}

type Executor interface {
	Execute(ctx context.Context, session AuthenticatedSession, req Request) (Response, error)
	nodeCommandExecutor()
}

type ExecutorFunc func(context.Context, AuthenticatedSession, Request) (Response, error)

func (ExecutorFunc) nodeCommandExecutor() {}

func (f ExecutorFunc) Execute(ctx context.Context, session AuthenticatedSession, req Request) (Response, error) {
	if ctx == nil {
		return Response{}, ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	now := time.Now().UTC()
	if err := req.Validate(now); err != nil {
		return Response{}, err
	}
	if err := session.validate(now, req.NodeID); err != nil {
		return Response{}, err
	}
	if f == nil {
		return Response{}, fmt.Errorf("%w: executor func", ErrInvalidField)
	}
	resp, err := f(ctx, session, req)
	if err != nil {
		return Response{}, err
	}
	if err := resp.ValidateFor(req); err != nil {
		return Response{}, err
	}
	return resp, nil
}
