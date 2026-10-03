package auth

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// TokenUseAccess identifies an authenticated session credential.
	TokenUseAccess = "access"
	// TokenUseRefresh identifies a session-rotation credential, not API access.
	TokenUseRefresh = "refresh"
	// TokenUseLogin identifies a short-lived initial-login proof.
	TokenUseLogin = "login"
	// TokenUseEmailVerification identifies a short-lived email-ownership proof.
	TokenUseEmailVerification = "email_verification"
)

// UserTypeProvider is an optional trusted user-model capability. It deliberately
// differs from resource GetType methods and does not enlarge UserModel's contract.
type UserTypeProvider interface {
	// GetUserType returns the stored account type, or empty for a legacy account.
	GetUserType() string
}

// Claims combines standard JWT identity with narrowly typed account context.
// Claims are signed, not encrypted: never place secrets or arbitrary user
// extensions here. Type, issuer and audience are context, not permissions.
type Claims struct {
	// RegisteredClaims carries issuer, stable user subject, audience, expiry,
	// issuance time and unique token ID. Subject never embeds an account type.
	jwt.RegisteredClaims
	// UserType snapshots the trusted account's persisted configuration type.
	UserType string `json:"user_type,omitempty"`
	// TokenUse binds new credentials to their intended authentication operation.
	TokenUse string `json:"token_use,omitempty"`
	// EmailRevision invalidates credentials after security-sensitive email changes.
	EmailRevision int64 `json:"email_revision"`
	// AuthenticationTime is the original verified login time in Unix seconds;
	// zero is omitted and is never replaced by the token's issuance time.
	AuthenticationTime int64 `json:"auth_time,omitempty"`
}

// AccessClaims preserves existing access-family wire fields while sharing typed
// identity claims. Login and email-verification proofs also use this wire shape;
// their TokenUse must be checked before treating them as authenticated sessions.
type AccessClaims struct {
	Claims
	// AccessUUID is the live-store identifier, equal to the registered token ID.
	AccessUUID string `json:"access_uuid"`
	// IsAdmin snapshots the account role; current policy must still authorize work.
	IsAdmin bool `json:"admin"`
	// IsAuthorized snapshots account activeness at issuance, not current authority.
	IsAuthorized bool `json:"authorized"`
}

// RefreshClaims carries only the identity required for session rotation.
type RefreshClaims struct {
	Claims
	// RefreshUUID identifies the single-use rotation record and equals token ID.
	RefreshUUID string `json:"refresh_uuid"`
}

// newClaims derives reserved identity fields only from trusted configuration,
// a stored user and server-generated session data. Hosts cannot merge a client
// claim map over reserved fields. Missing legacy user types are kept missing.
func (s *Service) newClaims(user UserModel, id, purpose string, expires int64, authenticatedAt time.Time) (Claims, error) {
	userType := ""
	if provider, ok := user.(UserTypeProvider); ok {
		userType = provider.GetUserType()
	}
	if userType != "" && !validUserType(userType) {
		return Claims{}, ErrUnauthorized
	}
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: s.issuer, Subject: user.GetUserId(), Audience: append(jwt.ClaimStrings(nil), s.audience...),
			ExpiresAt: jwt.NewNumericDate(time.Unix(expires, 0)), IssuedAt: jwt.NewNumericDate(time.Now()), ID: id,
		},
		UserType: userType, TokenUse: purpose, EmailRevision: userEmailRevision(user),
	}
	if !authenticatedAt.IsZero() {
		claims.AuthenticationTime = authenticatedAt.Unix()
	}
	return claims, nil
}

// validUserType permits bounded UTF-8, non-whitespace custom identifiers without
// limiting hosts to built-in presets. Invalid UTF-8 must not be silently replaced
// during JSON encoding, which would change the signed account classification.
func validUserType(value string) bool {
	return utf8.ValidString(value) && len(value) <= 128 && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// tokenIdentityContext validates optional added claims without upgrading legacy
// tokens. A malformed present value is never treated as a missing value.
func tokenIdentityContext(claims jwt.MapClaims, id string) (userType, purpose string, err error) {
	if raw, exists := claims["user_type"]; exists {
		var ok bool
		userType, ok = raw.(string)
		if !ok || userType == "" || !validUserType(userType) {
			return "", "", ErrUnauthorized
		}
	}
	if raw, exists := claims["token_use"]; exists {
		purpose, _ = raw.(string)
		switch purpose {
		case TokenUseAccess, TokenUseRefresh, TokenUseLogin, TokenUseEmailVerification:
		default:
			return "", "", ErrUnauthorized
		}
	}
	if raw, exists := claims["jti"]; exists && (raw != id || id == "") {
		return "", "", ErrUnauthorized
	}
	return userType, purpose, nil
}

// parserOptions applies explicitly configured trust boundaries. Empty issuer or
// audience retains legacy unbound compatibility; callers must opt in deliberately.
// All credentials still require an expiry, and key callbacks pin HS256.
func (s *Service) parserOptions() []jwt.ParserOption {
	options := []jwt.ParserOption{jwt.WithExpirationRequired(), jwt.WithIssuedAt()}
	if s.issuer != "" {
		options = append(options, jwt.WithIssuer(s.issuer))
	}
	if len(s.audience) > 0 {
		options = append(options, jwt.WithAudience(s.audience...))
	}
	return options
}

// IsSessionCredential distinguishes access sessions from short-lived proofs.
// Missing purpose remains compatible with old tokens; it does not assert a type
// or fresh login. Live session and account checks remain the caller's duty.
func (t *TokenAccessDetails) IsSessionCredential() bool {
	return t != nil && (t.TokenUse == "" || t.TokenUse == TokenUseAccess)
}

// MatchesUserType rejects a changed signed account type. An absent legacy claim
// remains absent rather than being backfilled from the current account.
func MatchesUserType(signedType string, user UserTypeProvider) bool {
	return signedType == "" || (user != nil && signedType == user.GetUserType())
}
