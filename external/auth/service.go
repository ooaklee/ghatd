// Package auth implements JWT token signing, verification, and authentication
// services. It provides functionality for creating and validating access tokens,
// refresh tokens, and email verification tokens.
//
// The package supports standard JWT operations with HS256 signing and includes
// user context extraction and validation.
package auth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// UserModel holds the methods of a valid user model
type UserModel interface {
	// GetUserId returns the immutable stored identity, never a mutable handle.
	GetUserId() string
	// IsAdmin supplies an issuance-time role snapshot, not live authorization.
	IsAdmin() bool
	// GetUserStatus supplies the issuance-time account state.
	GetUserStatus() string
}

// Service manages JWT token creation, validation, and user authentication.
// It handles access tokens, refresh tokens, and ephemeral tokens for various
// authentication flows.
type Service struct {
	accessTokenSecret  string
	refreshTokenSecret string
	// issuer and audience are immutable trusted configuration copied at construction.
	issuer   string
	audience []string
}

// NewServiceRequest contains configuration for creating a new auth service.
//
// Both access and refresh token secrets should be cryptographically secure
// random strings of at least 32 bytes.
type NewServiceRequest struct {
	// AccessTokenSecret signs access, initial-login and email-verification tokens.
	AccessTokenSecret string
	// RefreshTokenSecret signs rotation credentials and should be a separate key.
	RefreshTokenSecret string
	// Issuer is signed and required on verification when non-empty. Enabling it
	// rejects older unbound credentials; plan a session rollover before rollout.
	Issuer string
	// Audience lists accepted recipients and is signed into new credentials.
	// Verification requires at least one exact match (JWT audience semantics).
	// Use separate services for narrower recipients; hosts may require exact sets.
	Audience []string
}

// NewService creates a new authentication service with the provided secrets.
//
// The service signs and verifies only HS256. The request must be non-nil; the
// caller owns secret provisioning. Audience values are copied at construction.
func NewService(request *NewServiceRequest) *Service {
	return &Service{
		accessTokenSecret:  request.AccessTokenSecret,
		refreshTokenSecret: request.RefreshTokenSecret,
		issuer:             request.Issuer,
		audience:           append([]string(nil), request.Audience...),
	}
}

// CreateInitalToken creates a short-lived JWT token for initial user verification.
// The token is valid for 5 minutes and contains basic user information.
func (s *Service) CreateInitalToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "create-initial-token")
	userID := user.GetUserId()
	logger.Debug("auth-initial-token-create-started", zap.String("user-id", userID), zap.Bool("admin", user.IsAdmin()))

	td := &TokenDetails{}
	td.EtExpires = toolbox.GenerateTimeOfExpiryAsSeconds(initialTokenDefaultTTL)
	td.EtTTL = getTokenTimeToLive(td.EtExpires)
	td.GenerateEphemeralUUID()

	claims, err := s.newClaims(user, td.EphemeralUUID, TokenUseLogin, td.EtExpires, time.Time{})
	if err != nil {
		return nil, err
	}
	et := generateTokenWithSigningMethodHS256(AccessClaims{Claims: claims, AccessUUID: td.EphemeralUUID, IsAdmin: user.IsAdmin(), IsAuthorized: true})
	td.EphemeralToken, err = et.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		logger.Error("auth-initial-token-signing-failed", zap.String("user-id", userID), zap.Error(err))
		return nil, fmt.Errorf("signing ephemeral token: %w", err)
	}

	logger.Debug("auth-initial-token-created", zap.String("user-id", userID), zap.Duration("ttl", td.EtTTL))
	return td, nil
}

// CreateEmailVerificationToken creates a short-lived JWT token for email verification.
// The token is valid for 10 minutes and must be used to verify the user's email address.
func (s *Service) CreateEmailVerificationToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "create-email-verification-token")
	userID := user.GetUserId()
	logger.Debug("auth-email-verification-token-create-started", zap.String("user-id", userID))

	td := &TokenDetails{}
	td.EvExpires = toolbox.GenerateTimeOfExpiryAsSeconds(emailVerificationTokenDefaultTTL)
	td.EvTTL = getTokenTimeToLive(td.EvExpires)
	td.GenerateEmailVerificationUUID()

	claims, err := s.newClaims(user, td.EmailVerificationUUID, TokenUseEmailVerification, td.EvExpires, time.Time{})
	if err != nil {
		return nil, err
	}
	evt := generateTokenWithSigningMethodHS256(AccessClaims{Claims: claims, AccessUUID: td.EmailVerificationUUID, IsAdmin: user.IsAdmin()})
	td.EmailVerificationToken, err = evt.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		logger.Error("auth-email-verification-token-signing-failed", zap.String("user-id", userID), zap.Error(err))
		return nil, fmt.Errorf("signing email verification token: %w", err)
	}

	logger.Debug("auth-email-verification-token-created", zap.String("user-id", userID), zap.Duration("ttl", td.EvTTL))
	return td, nil
}

// CreateToken creates access and refresh JWT tokens for user authentication.
//
// Access tokens are valid for 15 minutes and contain user session information.
// Refresh tokens are valid for 7 days and can be used to obtain new access tokens.
func (s *Service) CreateToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	return s.CreateTokenWithAuthenticationTime(ctx, user, time.Time{})
}

// CreateTokenWithAuthenticationTime carries the original signed login time through rotation.
func (s *Service) CreateTokenWithAuthenticationTime(ctx context.Context, user UserModel, authenticatedAt time.Time) (*TokenDetails, error) {
	if !authenticatedAt.IsZero() && (authenticatedAt.Unix() <= 0 || authenticatedAt.Unix() > time.Now().Unix()) {
		return nil, ErrUnauthorized
	}

	logger := logger.AcquireOperationFrom(ctx, "external/auth", "create-token")
	userID := user.GetUserId()
	logger.Debug("auth-token-create-started", zap.String("user-id", userID), zap.Bool("admin", user.IsAdmin()), zap.String("user-status", user.GetUserStatus()))

	td := &TokenDetails{}
	td.AtExpires = toolbox.GenerateTimeOfExpiryAsSeconds(accesstokenDefaultTTL)
	td.AtTTL = getTokenTimeToLive(td.AtExpires)
	td.RtExpires = toolbox.GenerateTimeOfExpiryAsSeconds(refreshtokenDefaultTTL)
	td.RtTTL = getTokenTimeToLive(td.RtExpires)
	td.GenerateRefreshUUID().GenerateAccessUUID()

	// Create Access Token
	accessClaims, err := s.newClaims(user, td.AccessUUID, TokenUseAccess, td.AtExpires, authenticatedAt)
	if err != nil {
		return nil, err
	}
	at := generateTokenWithSigningMethodHS256(AccessClaims{Claims: accessClaims, AccessUUID: td.AccessUUID, IsAdmin: user.IsAdmin(), IsAuthorized: user.GetUserStatus() == userStatusKeyForAuthorisation})
	td.AccessToken, err = at.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		logger.Error("auth-access-token-signing-failed", zap.String("user-id", userID), zap.Error(err))
		return nil, fmt.Errorf("signing access token: %w", err)
	}

	// Create Refresh Token
	refreshClaims, err := s.newClaims(user, td.RefreshUUID, TokenUseRefresh, td.RtExpires, authenticatedAt)
	if err != nil {
		return nil, err
	}
	rt := generateTokenWithSigningMethodHS256(RefreshClaims{Claims: refreshClaims, RefreshUUID: td.RefreshUUID})

	td.RefreshToken, err = rt.SignedString([]byte(s.refreshTokenSecret))
	if err != nil {
		logger.Error("auth-refresh-token-signing-failed", zap.String("user-id", userID), zap.Error(err))
		return nil, fmt.Errorf("signing refresh token: %w", err)
	}

	logger.Debug("auth-token-created", zap.String("user-id", userID), zap.Duration("access-token-ttl", td.AtTTL), zap.Duration("refresh-token-ttl", td.RtTTL))
	return td, nil
}

// ExtractToken retrieves the bearer token from the Authorization header.
//
// Returns an error if no Authorization header is present or if the header
// format is invalid.
func (s *Service) ExtractToken(ctx context.Context, r *http.Request) (string, error) {
	path := logger.RequestPath(r)
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "extract-token")
	authorization := ""
	if r != nil {
		authorization = r.Header.Get(httpHeaderKeyAuthorization)
	}
	if authorization == "" {
		logger.Debug("auth-bearer-header-missing", zap.String("path", path))
		return "", ErrNoBearerHeaderFound
	}

	token := getTokenFromHeaderBearerToken(authorization)
	logger.Debug("auth-bearer-token-extracted", zap.String("path", path), zap.Int("token-length", len(token)))
	return token, nil
}

// VerifyToken extracts and verifies the JWT token from the request.
//
// It validates the token signature and returns the parsed token if valid.
func (s *Service) VerifyToken(ctx context.Context, r *http.Request) (*jwt.Token, error) {
	path := logger.RequestPath(r)
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "verify-token")
	tokenString, err := s.ExtractToken(ctx, r)
	if err != nil {
		return nil, err
	}

	token, err := s.ParseAccessTokenFromString(ctx, tokenString)
	if err != nil {
		return nil, err
	}

	logger.Debug("auth-token-verified", zap.String("path", path))
	return token, nil
}

// ParseAccessTokenFromString parses and validates a JWT token string.
//
// It pins HS256, requires expiry and applies configured issuer/audience checks.
// Signature validity alone does not establish session purpose or live authority;
// callers need the extraction and current-session checks for that decision.
func (s *Service) ParseAccessTokenFromString(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "parse-access-token")
	unexpectedAlgorithm := ""

	token, err := jwt.Parse(tokenAsString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			unexpectedAlgorithm = token.Method.Alg()
			return nil, ErrUnauthorizedTokenUnexpectedSigningMethod
		}
		return []byte(s.accessTokenSecret), nil
	}, s.parserOptions()...)

	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			logger.Warn("access-token-expired")
			return nil, ErrUnauthorizedParsedStringTokenExpired
		case errors.Is(err, jwt.ErrTokenMalformed):
			logger.Warn("access-token-malformatted")
			return nil, ErrUnauthorizedMalformattedToken
		case errors.Is(err, ErrUnauthorizedTokenUnexpectedSigningMethod):
			logger.Warn("access-token-unexpected-signing-method", zap.String("algorithm", unexpectedAlgorithm))
			return nil, ErrUnauthorizedTokenUnexpectedSigningMethod
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			logger.Warn("access-token-signature-invalid")
			return nil, ErrUnauthorizedParsedStringUnknown
		default:
			logger.Error("token-parsing-error", zap.Error(err))
			return nil, ErrUnauthorizedParsedStringUnknown
		}
	}

	logger.Debug("access-token-parsed")
	return token, nil
}

// ParseRefreshTokenFromString parses and validates a refresh token string.
//
// Similar to ParseAccessTokenFromString but uses the refresh token secret.
func (s *Service) ParseRefreshTokenFromString(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "parse-refresh-token")
	unexpectedAlgorithm := ""

	token, err := jwt.Parse(tokenAsString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			unexpectedAlgorithm = token.Method.Alg()
			return nil, ErrUnauthorizedTokenUnexpectedSigningMethod
		}
		return []byte(s.refreshTokenSecret), nil
	}, s.parserOptions()...)

	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			logger.Warn("refresh-token-expired")
			return nil, ErrUnauthorizedParsedStringTokenExpired
		case errors.Is(err, jwt.ErrTokenMalformed):
			logger.Warn("refresh-token-malformatted")
			return nil, ErrUnauthorizedMalformattedToken
		case errors.Is(err, ErrUnauthorizedTokenUnexpectedSigningMethod):
			logger.Warn("refresh-token-unexpected-signing-method", zap.String("algorithm", unexpectedAlgorithm))
			return nil, ErrUnauthorizedTokenUnexpectedSigningMethod
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			logger.Warn("refresh-token-signature-invalid")
			return nil, ErrUnauthorizedParsedStringUnknown
		default:
			logger.Error("token-parsing-error", zap.Error(err))
			return nil, ErrUnauthorizedParsedStringUnknown
		}
	}

	logger.Debug("refresh-token-parsed")
	return token, nil
}

// CheckTokenIsValid verifies that the token is valid and has not expired.
func (s *Service) CheckTokenIsValid(ctx context.Context, r *http.Request) error {
	path := logger.RequestPath(r)
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "check-token-is-valid")
	token, err := s.VerifyToken(ctx, r)
	if err != nil {
		return err
	}

	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		logger.Warn("auth-token-invalid-claims", zap.String("path", path))
		return ErrUnauthorized
	}

	logger.Debug("auth-token-valid", zap.String("path", path))
	return nil
}

// ExtractTokenMetadata retrieves and validates token metadata from the request.
//
// Returns TokenAccessDetails containing user ID, access UUID, and authorization status.
func (s *Service) ExtractTokenMetadata(ctx context.Context, r *http.Request) (*TokenAccessDetails, error) {
	path := logger.RequestPath(r)
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "extract-token-metadata")
	token, err := s.VerifyToken(ctx, r)
	if err != nil {
		return nil, err
	}

	details, err := s.CheckAccessTokenValidityGetDetails(ctx, token)
	if err != nil {
		return nil, err
	}

	logger.Debug("auth-token-metadata-extracted", zap.String("path", path), zap.String("user-id", details.UserID), zap.Bool("admin", details.IsAdmin), zap.Bool("authorized", details.IsAuthorized))
	return details, nil
}

// ExtractRefreshTokenMetadataByString retrieves refresh token metadata from a token string.
func (s *Service) ExtractRefreshTokenMetadataByString(ctx context.Context, tokenAsString string) (*TokenRefreshDetails, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "extract-refresh-token-metadata")
	token, err := s.ParseRefreshTokenFromString(ctx, tokenAsString)
	if err != nil {
		return nil, err
	}

	details, err := s.GetRefreshTokenUUID(ctx, token)
	if err != nil {
		return nil, err
	}

	logger.Debug("auth-refresh-token-metadata-extracted", zap.String("user-id", details.UserID))
	return details, nil
}

// ExtractAccessTokenMetadataByString retrieves access token metadata from a token string.
func (s *Service) ExtractAccessTokenMetadataByString(ctx context.Context, tokenAsString string) (*TokenAccessDetails, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "extract-access-token-metadata")
	token, err := s.ParseAccessTokenFromString(ctx, tokenAsString)
	if err != nil {
		return nil, err
	}

	details, err := s.CheckAccessTokenValidityGetDetails(ctx, token)
	if err != nil {
		return nil, err
	}

	logger.Debug("auth-access-token-metadata-extracted", zap.String("user-id", details.UserID), zap.Bool("admin", details.IsAdmin), zap.Bool("authorized", details.IsAuthorized))
	return details, nil
}

// CheckAccessTokenValidityGetDetails extracts typed metadata from a token already
// verified by this service. It rejects malformed identity claims but does not
// verify a fabricated jwt.Token or consult live sessions/accounts. Prefer the
// string extraction API when receiving an untrusted credential.
func (s *Service) CheckAccessTokenValidityGetDetails(ctx context.Context, token *jwt.Token) (*TokenAccessDetails, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "check-access-token-validity-get-details")
	if token == nil || token.Method == nil {
		return nil, ErrUnauthorized
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if ok && token.Valid {
		accessUUID, ok := claims[tokenClaimKeyAccessUUID].(string)
		if !ok || accessUUID == "" {
			logger.Warn("auth-access-token-missing-access-uuid")
			return nil, ErrUnauthorizedNoTokenUUID
		}

		userID, ok := claims[tokenClaimKeySub].(string)
		if !ok || userID == "" {
			logger.Warn("auth-access-token-missing-user-id")
			return nil, ErrUnauthorizedNoUserIDFound
		}

		isAdmin, ok := claims[tokenClaimKeyAdmin].(bool)
		if !ok {
			logger.Warn("auth-access-token-missing-admin-claim", zap.String("user-id", userID))
			return nil, ErrUnauthorizedNoAdminInfoFound
		}

		// Check user if active
		isActive, ok := claims[tokenClaimKeyAuthorized].(bool)
		if !ok {
			logger.Warn("auth-access-token-missing-authorized-claim", zap.String("user-id", userID))
			return nil, ErrUnauthorizedNoAuthorizationInfoFound
		}

		logger.Debug("auth-access-token-details-valid", zap.String("user-id", userID), zap.Bool("admin", isAdmin), zap.Bool("authorized", isActive))
		authenticatedAt, err := tokenAuthenticationTime(claims)
		if err != nil {
			return nil, err
		}
		revision, err := tokenEmailRevision(claims)
		if err != nil {
			return nil, err
		}
		audience, err := tokenAudience(claims)
		if err != nil {
			return nil, ErrUnauthorized
		}
		userType, purpose, err := tokenIdentityContext(claims, accessUUID)
		if err != nil || purpose == TokenUseRefresh {
			return nil, ErrUnauthorized
		}
		issuer, err := claims.GetIssuer()
		if err != nil {
			return nil, ErrUnauthorized
		}
		return &TokenAccessDetails{
			UserType:           userType,
			TokenUse:           purpose,
			Issuer:             issuer,
			Audience:           append([]string(nil), audience...),
			SigningAlgorithm:   token.Method.Alg(),
			EmailRevision:      revision,
			AuthenticationTime: authenticatedAt,
			AccessUUID:         accessUUID,
			UserID:             userID,
			IsAdmin:            isAdmin,
			IsAuthorized:       isActive,
		}, nil
	}
	logger.Warn("auth-access-token-invalid")
	return nil, ErrUnauthorized

}

// tokenAudience rejects malformed present audience claims rather than allowing
// a JWT library's optional-claim handling to treat them as an absent audience.
// Absence stays valid for legacy sessions; hosts decide which audiences to accept.
func tokenAudience(claims jwt.MapClaims) ([]string, error) {
	if value, exists := claims["aud"]; exists {
		switch value.(type) {
		case string, []string, []interface{}:
		default:
			return nil, ErrUnauthorized
		}
	}
	return claims.GetAudience()
}

// VerifyRefreshToken validates the signature and configured JWT constraints.
// For compatibility this wrapper reports all parser failures as refresh expiry;
// use ParseRefreshTokenFromString when the specific classification is needed.
func (s *Service) VerifyRefreshToken(ctx context.Context, t string) (*jwt.Token, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "verify-refresh-token")
	token, err := s.ParseRefreshTokenFromString(ctx, t)
	if err != nil {
		return nil, ErrUnauthorizedRefreshTokenExpired
	}
	logger.Debug("refresh-token-verified")
	return token, nil
}

// CheckRefreshTokenIsValid is the compatibility signature-validation wrapper.
// It does not check rotation records, account status or resource permissions.
func (s *Service) CheckRefreshTokenIsValid(ctx context.Context, t string) (*jwt.Token, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "check-refresh-token-is-valid")
	token, err := s.VerifyRefreshToken(ctx, t)
	if err != nil {
		return nil, err
	}
	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		logger.Warn("refresh-token-invalid-claims")
		return nil, err
	}
	logger.Debug("refresh-token-valid")
	return token, nil
}

// GetRefreshTokenUUID extracts rotation metadata from an already verified token.
// Present purpose claims must identify refresh credentials; legacy absent claims
// remain absent. This does not check live rotation records or account authority.
func (s *Service) GetRefreshTokenUUID(ctx context.Context, token *jwt.Token) (*TokenRefreshDetails, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/auth", "get-refresh-token-uuid")

	var refreshDetails TokenRefreshDetails
	if token == nil || !token.Valid {
		return nil, ErrUnauthorized
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if ok && token.Valid {
		var err error
		refreshDetails.AuthenticationTime, err = tokenAuthenticationTime(claims)
		if err != nil {
			return nil, err
		}
		refreshDetails.EmailRevision, err = tokenEmailRevision(claims)
		if err != nil {
			return nil, err
		}
		refreshDetails.RefreshUUID, ok = claims[tokenClaimKeyRefreshUUID].(string)
		if !ok || refreshDetails.RefreshUUID == "" {
			logger.Warn("refresh-token-missing-refresh-uuid")
			return nil, ErrUnauthorizedNoTokenUUID
		}
		refreshDetails.UserID, ok = claims[tokenClaimKeySub].(string)
		if !ok || refreshDetails.UserID == "" {
			logger.Warn("refresh-token-missing-user-id")
			return nil, ErrUnauthorizedNoUserIDFound
		}
		refreshDetails.UserType, refreshDetails.TokenUse, err = tokenIdentityContext(claims, refreshDetails.RefreshUUID)
		if err != nil || (refreshDetails.TokenUse != "" && refreshDetails.TokenUse != TokenUseRefresh) {
			return nil, ErrUnauthorized
		}
		logger.Debug("refresh-token-uuid-extracted", zap.String("user-id", refreshDetails.UserID))
	} else {
		return nil, ErrUnauthorized
	}

	return &refreshDetails, nil

}

// getTokenTimeToLive returns the remaining amount of time of before the
// token expiry is reached
func getTokenTimeToLive(tokenExpiry int64) time.Duration {
	expiryUTC := time.Unix(tokenExpiry, 0)
	now := time.Now()

	return expiryUTC.Sub(now)
}

// getTokenFromHeaderBearerToken returns token passed  in bearer token  header (Authorization)
// value, returns an empty string
func getTokenFromHeaderBearerToken(bearerToken string) string {
	strArr := strings.Split(bearerToken, " ")
	if len(strArr) == 2 {
		return strArr[1]
	}
	return ""
}

// generateTokenWithSigningMethodHS256 returns token based on HS256 signing method
func generateTokenWithSigningMethodHS256(claims jwt.Claims) *jwt.Token {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

}

// tokenAuthenticationTime validates optional freshness without upgrading legacy sessions.
func tokenAuthenticationTime(claims jwt.MapClaims) (time.Time, error) {
	value, exists := claims["auth_time"]
	if !exists {
		return time.Time{}, nil
	}
	seconds, ok := value.(float64)
	if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > float64(time.Now().Unix()) || math.Trunc(seconds) != seconds {
		return time.Time{}, ErrUnauthorized
	}
	return time.Unix(int64(seconds), 0).UTC(), nil
}

// SupportsEmailRevision allows hosts to opt into disconnects without silently
// accepting a custom signer that cannot invalidate pre-change credentials.
func (s *Service) SupportsEmailRevision() bool { return true }

// userEmailRevision reads the optional account revision, retaining revision zero
// for legacy user models that do not expose it.
func userEmailRevision(user UserModel) int64 {
	if model, ok := user.(interface{ GetEmailRevision() int64 }); ok {
		return model.GetEmailRevision()
	}
	return 0
}

// tokenEmailRevision accepts only non-negative integer claims within JSON's
// exact integer range. A missing claim represents legacy revision zero.
func tokenEmailRevision(claims jwt.MapClaims) (int64, error) {
	value, exists := claims["email_revision"]
	if !exists {
		return 0, nil
	}
	switch n := value.(type) {
	case float64:
		if n >= 0 && n <= 9007199254740991 && n == math.Trunc(n) {
			return int64(n), nil
		}
	case int64:
		if n >= 0 && n <= 9007199254740991 {
			return n, nil
		}
	case int:
		if n >= 0 && int64(n) <= 9007199254740991 {
			return int64(n), nil
		}
	}
	return 0, ErrUnauthorized
}
