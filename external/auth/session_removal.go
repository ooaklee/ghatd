package auth

import (
	"context"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionRemovalDetails is verified deletion authority for one signed session
// record. It is deliberately not TokenAccessDetails: expired credentials may
// authorize removal, never authentication, refresh, or account administration.
type SessionRemovalDetails struct {
	// UserID is the immutable signed owner of the record to remove.
	UserID string
	// TokenID identifies only the supplied credential, not its session family.
	TokenID string
	// TokenUse is the verified access/refresh purpose selected by the caller.
	TokenUse string
}

// SessionRemovalVerifier is the narrow cryptographic port used by cleanup-only
// managers. Implementations must validate signature, purpose and configured
// trust boundaries without consulting live account status or issuing tokens.
type SessionRemovalVerifier interface {
	ExtractSessionRemovalMetadata(context.Context, string, string) (*SessionRemovalDetails, error)
}

// removalClaims relaxes only a well-formed, already elapsed expiration during
// deletion verification. Missing/invalid expiry and future iat/nbf still fail.
// It is private and never used by access or refresh admission parsers.
type removalClaims struct {
	jwt.MapClaims
	now time.Time
}

func (c removalClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	expires, err := c.MapClaims.GetExpirationTime()
	if err == nil && expires != nil && expires.Before(c.now) {
		return jwt.NewNumericDate(c.now.Add(time.Minute)), nil
	}
	return expires, err
}

// ExtractSessionRemovalMetadata verifies one access/refresh credential for
// deletion only. Expiry alone does not prevent cleanup. HS256, required expiry,
// issuer/audience, other temporal claims, purpose and identity remain validated.
// No JWT or general authenticated-session context escapes this method.
func (s *Service) ExtractSessionRemovalMetadata(ctx context.Context, raw, use string) (*SessionRemovalDetails, error) {
	if ctx == nil || s == nil {
		return nil, ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	secret := s.accessTokenSecret
	if use == TokenUseRefresh {
		secret = s.refreshTokenSecret
	} else if use != TokenUseAccess {
		return nil, ErrUnauthorized
	}
	if raw == "" || secret == "" {
		return nil, ErrUnauthorized
	}
	// Signature verification precedes the explicitly scoped claims validation.
	// The unvalidated token never leaves this function.
	token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthorizedTokenUnexpectedSigningMethod
		}
		return []byte(secret), nil
	}, jwt.WithoutClaimsValidation(), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || token == nil || !token.Valid {
		return nil, ErrUnauthorized
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrUnauthorized
	}
	if err := jwt.NewValidator(s.parserOptions()...).Validate(removalClaims{MapClaims: claims, now: time.Now()}); err != nil {
		return nil, ErrUnauthorized
	}
	var owner, id string
	if use == TokenUseAccess {
		value, err := s.CheckAccessTokenValidityGetDetails(ctx, token)
		if err != nil {
			return nil, err
		}
		if !value.IsSessionCredential() {
			return nil, ErrUnauthorized
		}
		owner, id = value.UserID, value.AccessUUID
	} else {
		value, err := s.GetRefreshTokenUUID(ctx, token)
		if err != nil {
			return nil, err
		}
		owner, id = value.UserID, value.RefreshUUID
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(id) == "" || strings.ContainsAny(owner+id, ":") {
		return nil, ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &SessionRemovalDetails{UserID: owner, TokenID: id, TokenUse: use}, nil
}
