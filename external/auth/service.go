// Package auth implements JWT token signing, verification, and authentication
// services. It provides functionality for creating and validating access tokens,
// refresh tokens, and email verification tokens.
//
// The package supports standard JWT operations with HMAC signing and includes
// user context extraction and validation.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// UserModel holds the methods of a valid user model
type UserModel interface {
	GetUserId() string
	IsAdmin() bool
	GetUserStatus() string
}

// Service manages JWT token creation, validation, and user authentication.
// It handles access tokens, refresh tokens, and ephemeral tokens for various
// authentication flows.
type Service struct {
	accessTokenSecret  string
	refreshTokenSecret string
	signingMethod      *jwt.SigningMethodHMAC
}

// NewServiceRequest contains configuration for creating a new auth service.
//
// Both access and refresh token secrets should be cryptographically secure
// random strings of at least 32 bytes.
type NewServiceRequest struct {
	AccessTokenSecret  string
	RefreshTokenSecret string
}

// NewService creates a new authentication service with the provided secrets.
//
// The service uses HS256 (HMAC with SHA-256) for token signing by default.
func NewService(request *NewServiceRequest) *Service {
	return &Service{
		accessTokenSecret:  request.AccessTokenSecret,
		refreshTokenSecret: request.RefreshTokenSecret,
		signingMethod:      jwt.SigningMethodHS256,
	}
}

// CreateInitalToken creates a short-lived JWT token for initial user verification.
// The token is valid for 5 minutes and contains basic user information.
func (s *Service) CreateInitalToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	td := &TokenDetails{}
	td.EtExpires = toolbox.GenerateTimeOfExpiryAsSeconds(initialTokenDefaultTTL)
	td.EtTTL = getTokenTimeToLive(td.EtExpires)
	td.GenerateEphemeralUUID()

	et := generateHS256Tokens(map[string]interface{}{
		tokenClaimKeyAuthorized: true,
		tokenClaimKeySub:        user.GetUserId(),
		tokenClaimKeyAccessUUID: td.EphemeralUUID,
		tokenClaimKeyAdmin:      user.IsAdmin(),
		tokenClaimKeyExp:        td.EtExpires,
	})

	var err error
	td.EphemeralToken, err = et.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		return nil, fmt.Errorf("signing ephemeral token: %w", err)
	}

	return td, nil
}

// CreateEmailVerificationToken creates a short-lived JWT token for email verification.
// The token is valid for 10 minutes and must be used to verify the user's email address.
func (s *Service) CreateEmailVerificationToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	td := &TokenDetails{}
	td.EvExpires = toolbox.GenerateTimeOfExpiryAsSeconds(emailVerificationTokenDefaultTTL)
	td.EvTTL = getTokenTimeToLive(td.EvExpires)
	td.GenerateEmailVerificationUUID()

	evt := generateHS256Tokens(map[string]interface{}{
		tokenClaimKeyAuthorized: false,
		tokenClaimKeySub:        user.GetUserId(),
		tokenClaimKeyAccessUUID: td.EmailVerificationUUID,
		tokenClaimKeyAdmin:      user.IsAdmin(),
		tokenClaimKeyExp:        td.EvExpires,
	})

	var err error
	td.EmailVerificationToken, err = evt.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		return nil, fmt.Errorf("signing email verification token: %w", err)
	}

	return td, nil
}

// CreateToken creates access and refresh JWT tokens for user authentication.
//
// Access tokens are valid for 15 minutes and contain user session information.
// Refresh tokens are valid for 7 days and can be used to obtain new access tokens.
func (s *Service) CreateToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	td := &TokenDetails{}
	td.AtExpires = toolbox.GenerateTimeOfExpiryAsSeconds(accesstokenDefaultTTL)
	td.AtTTL = getTokenTimeToLive(td.AtExpires)
	td.RtExpires = toolbox.GenerateTimeOfExpiryAsSeconds(refreshtokenDefaultTTL)
	td.RtTTL = getTokenTimeToLive(td.RtExpires)
	td.GenerateRefreshUUID().GenerateAccessUUID()

	// Create Access Token
	at := generateHS256Tokens(mapAccessTokenClaims(&mapAccessTokenClaimsRequest{
		UserStatus:            user.GetUserStatus(),
		AccessTokenUUID:       td.AccessUUID,
		UserID:                user.GetUserId(),
		IsAdmin:               user.IsAdmin(),
		AccessTokenTTLSeconds: td.AtExpires,
	}))

	var err error
	td.AccessToken, err = at.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		return nil, fmt.Errorf("signing access token: %w", err)
	}

	// Create Refresh Token
	rt := generateHS256Tokens(map[string]interface{}{
		tokenClaimKeyRefreshUUID: td.RefreshUUID,
		tokenClaimKeySub:         user.GetUserId(),
		tokenClaimKeyExp:         td.RtExpires,
	})

	td.RefreshToken, err = rt.SignedString([]byte(s.refreshTokenSecret))
	if err != nil {
		return nil, fmt.Errorf("signing refresh token: %w", err)
	}

	return td, nil
}

// ExtractToken retrieves the bearer token from the Authorization header.
//
// Returns an error if no Authorization header is present or if the header
// format is invalid.
func (s *Service) ExtractToken(ctx context.Context, r *http.Request) (string, error) {
	authorization := r.Header.Get(httpHeaderKeyAuthorization)
	if authorization == "" {
		return "", errors.New(ErrKeyNoBearerHeaderFound)
	}

	return getTokenFromHeaderBearerToken(authorization), nil
}

// VerifyToken extracts and verifies the JWT token from the request.
//
// It validates the token signature and returns the parsed token if valid.
func (s *Service) VerifyToken(ctx context.Context, r *http.Request) (*jwt.Token, error) {
	tokenString, err := s.ExtractToken(ctx, r)
	if err != nil {
		return nil, err
	}

	return s.ParseAccessTokenFromString(ctx, tokenString)
}

// ParseAccessTokenFromString parses and validates a JWT token string.
//
// It ensures the token uses HMAC signing and returns detailed errors for
// expiration, malformed tokens, and other validation failures.
func (s *Service) ParseAccessTokenFromString(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
	log := logger.Get(ctx)

	token, err := jwt.Parse(tokenAsString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			log.Error("unexpected-signing-method", zap.Any("method", token.Header[tokenHeaderKeyAlg]))
			return nil, errors.New(ErrKeyUnauthorizedTokenUnexpectedSigningMethod)
		}
		return []byte(s.accessTokenSecret), nil
	})

	if err != nil {
		 switch err.Error() {
		case "Token is expired":
			return nil, errors.New(ErrKeyUnauthorizedParsedStringTokenExpired)
		case "token contains an invalid number of segments":
			return nil, errors.New(ErrKeyUnauthorizedMalformattedToken)
		default:
			log.Error("token-parsing-error", zap.Error(err))
			return nil, errors.New(ErrKeyUnauthorizedParsedStringUnknown)
		}
	}

	return token, nil
}

// ParseRefreshTokenFromString parses and validates a refresh token string.
//
// Similar to ParseAccessTokenFromString but uses the refresh token secret.
func (s *Service) ParseRefreshTokenFromString(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
	log := logger.Get(ctx)

	token, err := jwt.Parse(tokenAsString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			log.Error("unexpected-signing-method", zap.Any("method", token.Header[tokenHeaderKeyAlg]))
			return nil, errors.New(ErrKeyUnauthorizedTokenUnexpectedSigningMethod)
		}
		return []byte(s.refreshTokenSecret), nil
	})

	if err != nil {
		switch err.Error() {
		case "Token is expired":
			return nil, errors.New(ErrKeyUnauthorizedParsedStringTokenExpired)
		case "token contains an invalid number of segments":
			return nil, errors.New(ErrKeyUnauthorizedMalformattedToken)
		default:
			log.Error("token-parsing-error", zap.Error(err))
			return nil, errors.New(ErrKeyUnauthorizedParsedStringUnknown)
		}
	}

	return token, nil
}

// CheckTokenIsValid verifies that the token is valid and has not expired.
func (s *Service) CheckTokenIsValid(ctx context.Context, r *http.Request) error {
	token, err := s.VerifyToken(ctx, r)
	if err != nil {
		return err
	}

	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		return errors.New(ErrKeyUnauthorized)
	}

	return nil
}

// ExtractTokenMetadata retrieves and validates token metadata from the request.
//
// Returns TokenAccessDetails containing user ID, access UUID, and authorization status.
func (s *Service) ExtractTokenMetadata(ctx context.Context, r *http.Request) (*TokenAccessDetails, error) {
	token, err := s.VerifyToken(ctx, r)
	if err != nil {
		return nil, err
	}

	return s.CheckAccessTokenValidityGetDetails(ctx, token)
}

// ExtractRefreshTokenMetadataByString retrieves refresh token metadata from a token string.
func (s *Service) ExtractRefreshTokenMetadataByString(ctx context.Context, tokenAsString string) (*TokenRefreshDetails, error) {
	token, err := s.ParseRefreshTokenFromString(ctx, tokenAsString)
	if err != nil {
		return nil, err
	}

	return s.GetRefreshTokenUUID(ctx, token)
}

// ExtractAccessTokenMetadataByString retrieves access token metadata from a token string.
func (s *Service) ExtractAccessTokenMetadataByString(ctx context.Context, tokenAsString string) (*TokenAccessDetails, error) {
	token, err := s.ParseAccessTokenFromString(ctx, tokenAsString)
	if err != nil {
		return nil, err
	}

	return s.CheckAccessTokenValidityGetDetails(ctx, token)
}

// CheckAccessTokenValidityGetDetails return details of a valid acess token
// TODO: Create tests
func (s *Service) CheckAccessTokenValidityGetDetails(ctx context.Context, token *jwt.Token) (*TokenAccessDetails, error) {

	claims, ok := token.Claims.(jwt.MapClaims)
	if ok && token.Valid {
		accessUUID, ok := claims[tokenClaimKeyAccessUUID].(string)
		if !ok {
			return nil, errors.New(ErrKeyUnauthorizedNoTokenUUID)
		}

		userID, ok := claims[tokenClaimKeySub].(string)
		if !ok {
			return nil, errors.New(ErrKeyUnauthorizedNoUserIDFound)
		}

		isAdmin, ok := claims[tokenClaimKeyAdmin].(bool)
		if !ok {
			return nil, errors.New(ErrKeyUnauthorizedNoAdminInfoFound)
		}

		// Check user if active
		isActive, ok := claims[tokenClaimKeyAuthorized].(bool)
		if !ok {
			return nil, errors.New(ErrKeyUnauthorizedNoAuthorizationInfoFound)
		}

		return &TokenAccessDetails{
			AccessUUID:   accessUUID,
			UserID:       userID,
			IsAdmin:      isAdmin,
			IsAuthorized: isActive,
		}, nil
	}
	return nil, errors.New(ErrKeyUnauthorized)

}

// VerifyRefreshToken makes sure that the refresh token is correctly signed. Make sure that
// the token method conform to "SigningMethodHMAC"
// TODO: Create tests
func (s *Service) VerifyRefreshToken(ctx context.Context, t string) (*jwt.Token, error) {

	token, err := jwt.Parse(t, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected-signing-method: %v", token.Header[tokenHeaderKeyAlg])
		}
		return []byte(s.refreshTokenSecret), nil
	})
	if err != nil {
		return nil, errors.New(ErrKeyUnauthorizedRefreshTokenExpired)
	}
	return token, nil
}

// CheckRefreshTokenIsValid confirms if the refresh token has not expired and is still
// usable
// TODO: Create tests
func (s *Service) CheckRefreshTokenIsValid(ctx context.Context, t string) (*jwt.Token, error) {
	token, err := s.VerifyRefreshToken(ctx, t)
	if err != nil {
		return nil, err
	}
	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		return nil, err
	}
	return token, nil
}

// GetRefreshTokenUUID grabs the UUID for refresh token
// Only if the token is valid i.e. the token claims should conform to
// TODO: Create tests
func (s *Service) GetRefreshTokenUUID(ctx context.Context, token *jwt.Token) (*TokenRefreshDetails, error) {

	var refreshDetails TokenRefreshDetails

	claims, ok := token.Claims.(jwt.MapClaims)
	if ok && token.Valid {
		refreshDetails.RefreshUUID, ok = claims[tokenClaimKeyRefreshUUID].(string)
		if !ok {
			return nil, errors.New(ErrKeyUnauthorizedNoTokenUUID)
		}
		refreshDetails.UserID, ok = claims[tokenClaimKeySub].(string)
		if !ok {
			return nil, errors.New(ErrKeyUnauthorizedNoUserIDFound)
		}
	}

	return &refreshDetails, nil

}

// generateHS256Tokens returns HS256 signed equivalent of passed claims
func generateHS256Tokens(claims map[string]interface{}) *jwt.Token {
	tokenClaims := jwt.MapClaims{}

	for key, value := range claims {
		tokenClaims[key] = value
	}

	return generateTokenWithSigningMethodHS256(tokenClaims)
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
