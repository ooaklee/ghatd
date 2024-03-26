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
	"go.uber.org/zap"
)

// UserModel holds the methods of a valid user model
type UserModel interface {
	GetUserId() string
	IsAdmin() bool
	GetUserStatus() string
}

// Service holds and manages auth business logic
type Service struct {
	accessTokenSecret  string
	refreshTokenSecret string
}

// NewServiceRequest holds expected variable needed for
// a new auth service
// TODO: Use a different secret for each token type
type NewServiceRequest struct {
	AccessTokenSecret  string
	RefreshTokenSecret string
}

// NewService creates auth service
func NewService(request *NewServiceRequest) *Service {
	return &Service{
		accessTokenSecret:  request.AccessTokenSecret,
		refreshTokenSecret: request.RefreshTokenSecret,
	}
}

// CreateInitalToken creates a shortlived JWT token to be used to verify user
// name
// TODO: Create tests
func (s *Service) CreateInitalToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	var err error

	td := &TokenDetails{}
	td.EtExpires = generateTimeOfExpiryAsSeconds(initialTokenDefaultTTL)
	td.EtTTL = getTokenTimeToLive(td.EtExpires)
	td.GenerateEphemeralUUID()

	et := generateHS256Tokens(map[string]interface{}{
		tokenClaimKeyAuthorized: true,
		tokenClaimKeySub:        user.GetUserId(),
		tokenClaimKeyAccessUUID: td.EphemeralUUID,
		tokenClaimKeyAdmin:      user.IsAdmin(),
		tokenClaimKeyExp:        td.EtExpires,
	})

	td.EphemeralToken, err = et.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		return nil, err
	}
	return td, nil
}

// CreateEmailVerificationToken creates a shortlived JWT token to be used to verify user's
// email address. Gives user 10 minutes to verify account.
// TODO: Create tests
func (s *Service) CreateEmailVerificationToken(ctx context.Context, user UserModel) (*TokenDetails, error) {
	var err error

	td := &TokenDetails{}
	td.EvExpires = generateTimeOfExpiryAsSeconds(emailVerificationTokenDefaultTTL)
	td.EvTTL = getTokenTimeToLive(td.EvExpires)
	td.GenerateEmailVerificationUUID()

	evt := generateHS256Tokens(map[string]interface{}{
		tokenClaimKeyAuthorized: false,
		tokenClaimKeySub:        user.GetUserId(),
		tokenClaimKeyAccessUUID: td.EmailVerificationUUID,
		tokenClaimKeyAdmin:      user.IsAdmin(),
		tokenClaimKeyExp:        td.EvExpires,
	})

	td.EmailVerificationToken, err = evt.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		return nil, err
	}
	return td, nil

}

// CreateToken creates access and refresh JWT token to be used to
// access some endpoints
// TODO: Create tests
func (s *Service) CreateToken(ctx context.Context, user UserModel) (*TokenDetails, error) {

	var err error

	td := &TokenDetails{}
	td.AtExpires = generateTimeOfExpiryAsSeconds(accesstokenDefaultTTL)
	td.AtTTL = getTokenTimeToLive(td.AtExpires)
	td.RtExpires = generateTimeOfExpiryAsSeconds(refreshtokenDefaultTTL)
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

	td.AccessToken, err = at.SignedString([]byte(s.accessTokenSecret))
	if err != nil {
		return nil, err
	}

	// Create Refresh Token
	rt := generateHS256Tokens(map[string]interface{}{
		tokenClaimKeyRefreshUUID: td.RefreshUUID,
		tokenClaimKeySub:         user.GetUserId(),
		tokenClaimKeyExp:         td.RtExpires,
	})

	td.RefreshToken, err = rt.SignedString([]byte(s.refreshTokenSecret))
	if err != nil {
		return nil, err
	}
	return td, nil

}

// ExtractToken attempts to retrieve bearer token out of request
// Assume the token is passed in `Authorization` header. In the even
// no header is passed error is returned
// TODO: Create tests
func (s *Service) ExtractToken(ctx context.Context, r *http.Request) (string, error) {
	headers := r.Header

	_, ok := headers[httpHeaderKeyAuthorization]

	if ok {
		return getTokenFromHeaderBearerToken(r.Header.Get(httpHeaderKeyAuthorization)), nil
	}

	return "", errors.New(ErrKeyNoBearerHeaderFound)
}

// VerifyToken extracts and verifies token
// TODO: Create tests
func (s *Service) VerifyToken(ctx context.Context, r *http.Request) (*jwt.Token, error) {
	tokenString, err := s.ExtractToken(ctx, r)
	if err != nil {
		return nil, err
	}

	return s.ParseAccessTokenFromString(ctx, tokenString)
}

// ParseAccessTokenFromString parse string into token if valid, sure that the token is correctly signed.
// Makes sure that the token method conform to "SigningMethodHMAC"
// TODO: Create tests
func (s *Service) ParseAccessTokenFromString(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
	log := logger.AcquireFrom(ctx)

	token, err := jwt.Parse(tokenAsString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			log.Error("unexpected-signing-method:", zap.Any("method-used", token.Header[tokenHeaderKeyAlg]))
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
			log.Error("unknown-parsing-error:", zap.Error(err))
			return nil, errors.New(ErrKeyUnauthorizedParsedStringUnknown)
		}
	}
	return token, nil
}

// CheckTokenIsValid confirms if the token has expired/ is still
// used
// TODO: Create tests
func (s *Service) CheckTokenIsValid(ctx context.Context, r *http.Request) error {
	token, err := s.VerifyToken(ctx, r)
	if err != nil {
		return err
	}
	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		return err
	}
	return nil
}

// ExtractTokenMetadata retrieves token's meta data to be used to
// query against persistent storage
// TODO: Create tests
func (s *Service) ExtractTokenMetadata(ctx context.Context, r *http.Request) (*TokenAccessDetails, error) {
	token, err := s.VerifyToken(ctx, r)
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

// generateTimeOfExpiryAsSeconds returns the ToE (Time of expiry) duration as seconds. It is
// calculated by adding the expected duration to now
func generateTimeOfExpiryAsSeconds(ttlDuration time.Duration) int64 {
	return time.Now().Add(ttlDuration).Unix()
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
