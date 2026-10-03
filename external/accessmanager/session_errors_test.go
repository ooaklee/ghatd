package accessmanager_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

type cyclicSessionError struct{}

func (e *cyclicSessionError) Error() string { return "cycle" }
func (e *cyclicSessionError) Unwrap() error { return e }

type ambiguousSessionError struct{}

func (ambiguousSessionError) Error() string { return "ambiguous" }
func (ambiguousSessionError) Is(target error) bool {
	return target == auth.ErrUnauthorized || target == auth.ErrUnauthorizedParsedStringTokenExpired
}

type sliceSessionError []string

func (sliceSessionError) Error() string { return "uncomparable" }

type nilSessionError struct{ cause error }

func (e *nilSessionError) Error() string { return e.cause.Error() }
func (e *nilSessionError) Unwrap() error { return e.cause }

// misleadingSessionError proves custom Is methods cannot override a leaf cause.
type misleadingSessionError struct{ cause error }

func (e misleadingSessionError) Error() string      { return "opaque wrapper" }
func (e misleadingSessionError) Unwrap() error      { return e.cause }
func (misleadingSessionError) Is(target error) bool { return target == auth.ErrNoBearerHeaderFound }

func TestClassifySessionError(t *testing.T) {
	deep := error(auth.ErrUnauthorized)
	for i := 0; i < 65; i++ {
		deep = fmt.Errorf("wrapper: %w", deep)
	}
	cases := []struct {
		name string
		err  error
		want accessmanager.SessionErrorKind
	}{
		{"nil", nil, accessmanager.SessionErrorUnknown},
		{"outage", errors.New("database unavailable"), accessmanager.SessionErrorUnknown},
		{"lookalike", errors.New(auth.ErrUnauthorized.Error()), accessmanager.SessionErrorUnknown},
		{"canceled", context.Canceled, accessmanager.SessionErrorUnknown},
		{"deadline", context.DeadlineExceeded, accessmanager.SessionErrorUnknown},
		{"misconfigured", accessmanager.ErrSessionVerificationUnavailable, accessmanager.SessionErrorUnknown},
		{"rotation timeout", accessmanager.ErrRefreshTemporarilyUnavailable, accessmanager.SessionErrorUnknown},
		{"missing header", auth.ErrNoBearerHeaderFound, accessmanager.SessionErrorRefreshable},
		{"expired access", auth.ErrUnauthorizedParsedStringTokenExpired, accessmanager.SessionErrorRefreshable},
		{"missing access record", accessmanager.ErrUnauthorizedTokenNotFoundInStore, accessmanager.SessionErrorRefreshable},
		{"malformed", auth.ErrUnauthorizedMalformattedToken, accessmanager.SessionErrorInvalidCredential},
		{"invalid", auth.ErrUnauthorized, accessmanager.SessionErrorInvalidCredential},
		{"expired refresh", auth.ErrUnauthorizedRefreshTokenExpired, accessmanager.SessionErrorInvalidCredential},
		{"missing refresh record", accessmanager.ErrUnauthorizedRefreshTokenCacheDeletionFailure, accessmanager.SessionErrorInvalidCredential},
		{"empty refresh", accessmanager.ErrEmptyRefreshToken, accessmanager.SessionErrorInvalidCredential},
		{"invalid refresh", accessmanager.ErrInvalidRefreshToken, accessmanager.SessionErrorInvalidCredential},
		{"revision", accessmanager.ErrOAuthReauthenticationRequired, accessmanager.SessionErrorInvalidCredential},
		{"missing user", user.ErrUserNotFound, accessmanager.SessionErrorInvalidCredential},
		{"inactive", accessmanager.ErrUnauthorizedNonActiveStatus, accessmanager.SessionErrorDenied},
		{"not admin", accessmanager.ErrUnauthorizedAdminAccessAttempted, accessmanager.SessionErrorDenied},
		{"wrapped expiry", fmt.Errorf("parse: %w", auth.ErrUnauthorizedParsedStringTokenExpired), accessmanager.SessionErrorRefreshable},
		{"wrapped denial", fmt.Errorf("policy: %w", accessmanager.ErrUnauthorizedNonActiveStatus), accessmanager.SessionErrorDenied},
		{"joined expiry and outage", errors.Join(auth.ErrUnauthorizedParsedStringTokenExpired, errors.New("outage")), accessmanager.SessionErrorUnknown},
		{"joined single invalid", errors.Join(auth.ErrUnauthorized), accessmanager.SessionErrorUnknown},
		{"deep", deep, accessmanager.SessionErrorUnknown},
		{"cycle", &cyclicSessionError{}, accessmanager.SessionErrorUnknown},
		{"ambiguous Is", ambiguousSessionError{}, accessmanager.SessionErrorUnknown},
		{"uncomparable", sliceSessionError{"opaque"}, accessmanager.SessionErrorUnknown},
		{"typed nil", (*nilSessionError)(nil), accessmanager.SessionErrorUnknown},
		{"wrapped typed nil", fmt.Errorf("adapter: %w", (*nilSessionError)(nil)), accessmanager.SessionErrorUnknown},
		{"custom Is cannot hide outage", misleadingSessionError{context.DeadlineExceeded}, accessmanager.SessionErrorUnknown},
		{"custom Is cannot override expiry", misleadingSessionError{auth.ErrUnauthorizedParsedStringTokenExpired}, accessmanager.SessionErrorRefreshable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, accessmanager.ClassifySessionError(tc.err))
		})
	}
}
