package accessmanager_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/go-redis/redis/v7"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/repository"
	helpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// signedProviderTransport simulates only provider HTTPS endpoints using real signed JWTs.
type signedProviderTransport struct {
	mu     sync.Mutex
	key    *rsa.PrivateKey
	tokens map[string]jwt.MapClaims
	calls  int
}

func (f *signedProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body interface{}
	if r.Method == http.MethodGet {
		body = map[string]interface{}{"keys": []interface{}{map[string]interface{}{"kid": "fixture", "kty": "RSA", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}}
	} else {
		f.calls++
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		claims := f.tokens[r.Form.Get("code")]
		if claims == nil {
			return nil, fmt.Errorf("unknown fixture code")
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "fixture"
		raw, err := token.SignedString(f.key)
		if err != nil {
			return nil, err
		}
		body = map[string]string{"id_token": raw}
	}
	encoded, _ := json.Marshal(body)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(encoded))), Request: r}, nil
}
func (f *signedProviderTransport) prepare(t *testing.T, authorisation, subject, email string, verified bool) url.Values {
	t.Helper()
	parsed, err := url.Parse(authorisation)
	require.NoError(t, err)
	q := parsed.Query()
	code := toolbox.GenerateUuidV4()
	issuer := oauth.GoogleSecureIssuer
	if parsed.Hostname() == "appleid.apple.com" {
		issuer = oauth.AppleIssuer
	}
	claims := jwt.MapClaims{"iss": issuer, "sub": subject, "aud": "client", "exp": time.Now().Add(time.Minute).Unix(), "nonce": q.Get("nonce")}
	if email != "" {
		claims["email"] = email
		claims["email_verified"] = verified
	}
	f.mu.Lock()
	f.tokens[code] = claims
	f.mu.Unlock()
	return url.Values{"state": {q.Get("state")}, "code": {code}}
}

func TestOAuthBrowserLifecycleIntegration(t *testing.T) {
	mongoURI, redisAddr := os.Getenv("GHATD_TEST_MONGO_URI"), os.Getenv("GHATD_TEST_REDIS_ADDR")
	if mongoURI == "" || redisAddr == "" {
		t.Skip("set GHATD_TEST_MONGO_URI and GHATD_TEST_REDIS_ADDR for full OAuth integration")
	}
	ctx := context.Background()
	dbName := "oauth_flow_" + toolbox.GenerateUuidV4()
	mongoHandler, err := helpers.NewHandler(helpers.DefaultConfig(mongoURI, dbName))
	require.NoError(t, err)
	defer mongoHandler.Close(ctx)
	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	defer db.Drop(ctx)
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	users := user.NewService(user.NewRepository(store), nil, user.DefaultUserConfig(), &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
	redisRuntime, err := ephemeral.NewRedisRuntime(ctx, &ephemeral.NewRedisRuntimeRequest{Options: &redis.Options{Addr: redisAddr}, Component: dbName, Environment: "test"})
	require.NoError(t, err)
	defer redisRuntime.Close(ctx)
	transactions := oauth.NewRedisTransactionStore(redisRuntime.Client, dbName)
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	transport := &signedProviderTransport{key: signingKey, tokens: map[string]jwt.MapClaims{}}
	httpClient := &http.Client{Transport: transport, Timeout: time.Second}
	google, err := oauth.NewGoogleSecureProvider(&oauth.NewGoogleSecureProviderRequest{ClientID: "client", ClientSecret: "fixture", RedirectURL: "https://app.example/api/v1/ams/oauth/google/callback", HTTPClient: httpClient, Store: transactions})
	require.NoError(t, err)
	appleKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(appleKey)
	require.NoError(t, err)
	apple, err := oauth.NewAppleProvider(&oauth.NewAppleProviderRequest{ClientID: "client", TeamID: "team", KeyID: "key", PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), RedirectURL: "https://app.example/api/v1/ams/oauth/apple/callback", HTTPClient: httpClient, Store: transactions})
	require.NoError(t, err)
	authService := auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "test-access-secret", RefreshTokenSecret: "test-refresh-secret"})
	service := accessmanager.NewService(&accessmanager.NewServiceRequest{AuditService: oauthAuditStub{}, UserService: users, AuthService: authService, EphemeralStore: redisRuntime.Store, OauthServices: []accessmanager.OauthService{google, apple}})
	handler := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{Service: service, Validator: validator.NewValidator(), ErrorMaps: []reply.ErrorManifest{accessmanager.AccessmanagerErrorMap}, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh", CookieDomain: "app.example", OAuthOrigin: "https://app.example"})

	httpRouter := router.NewRouter(nil, nil)
	accessmanager.AttachRoutes(&accessmanager.AttachRoutesRequest{Router: httpRouter, Handler: handler})
	serve := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "https://app.example"+path, strings.NewReader(body))
		request.Header.Set("Origin", "https://app.example")
		if method == http.MethodPost {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		httpRouter.GetRouter().ServeHTTP(response, request)
		return response
	}
	start := func(provider, path string, browser bool) (*http.Cookie, string) {
		q := url.Values{"request_url": {path}}
		if browser {
			q.Set("browser", "true")
		}
		response := serve(http.MethodGet, "/api/v1/ams/oauth/"+provider+"/login?"+q.Encode(), "")
		require.Equal(t, http.StatusFound, response.Code)
		cookies := response.Result().Cookies()
		require.Len(t, cookies, 1)
		cookie := cookies[0]
		require.True(t, cookie.HttpOnly)
		require.True(t, cookie.Secure)
		require.Empty(t, cookie.Domain)
		require.Equal(t, "/api/v1/ams/oauth/"+provider+"/callback", cookie.Path)
		if provider == "apple" {
			require.Equal(t, http.SameSiteNoneMode, cookie.SameSite)
		} else {
			require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		}
		return cookie, response.Header().Get("Location")
	}
	complete := func(provider string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		path := "/api/v1/ams/oauth/" + provider + "/callback"
		if provider == "apple" {
			return serve(http.MethodPost, path, values.Encode(), cookie)
		}
		return serve(http.MethodGet, path+"?"+values.Encode(), "", cookie)
	}
	authCookies := func(response *httptest.ResponseRecorder) (*http.Cookie, *http.Cookie) {
		var access, refresh *http.Cookie
		for _, cookie := range response.Result().Cookies() {
			switch cookie.Name {
			case "access":
				access = cookie
			case "refresh":
				refresh = cookie
			}
		}
		require.NotNil(t, access)
		require.NotNil(t, refresh)
		return access, refresh
	}
	discovery := serve(http.MethodGet, "/api/v1/ams/oauth/providers", "")
	require.Equal(t, 200, discovery.Code)
	require.Contains(t, discovery.Body.String(), `"google"`)
	require.Contains(t, discovery.Body.String(), `"apple"`)
	cookie, authorisation := start("google", "/app?from=sign-in#section", true)
	values := transport.prepare(t, authorisation, "google-subject", "person@example.test", true)
	response := complete("google", values, cookie)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Equal(t, "/app?from=sign-in#section", response.Header().Get("Location"))
	access, refresh := authCookies(response)
	require.Equal(t, http.SameSiteStrictMode, access.SameSite)
	require.True(t, access.HttpOnly)
	require.NotContains(t, response.Body.String(), access.Value)
	details, err := authService.ExtractAccessTokenMetadataByString(ctx, access.Value)
	require.NoError(t, err)
	require.False(t, details.AuthenticationTime.IsZero())
	owner, err := redisRuntime.Store.FetchAuth(ctx, details)
	require.NoError(t, err)
	require.Equal(t, details.UserID, owner)
	account, err := users.GetUserByID(ctx, &user.GetUserByIDRequest{ID: details.UserID})
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", account.User.Status)
	require.True(t, account.User.Verification.EmailVerified)
	replay := complete("google", values, cookie)
	require.Equal(t, http.StatusBadRequest, replay.Code)
	for _, cookie := range replay.Result().Cookies() {
		require.NotEqual(t, "access", cookie.Name)
	}
	// Matching email does not automatically connect a different provider subject.
	cookie, authorisation = start("google", "/app", true)
	response = complete("google", transport.prepare(t, authorisation, "different-subject", "person@example.test", true), cookie)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Contains(t, response.Header().Get("Location"), "oauth_error=link_required")
	// Unverified provider email never creates a user or a session.
	cookie, authorisation = start("google", "/app", true)
	response = complete("google", transport.prepare(t, authorisation, "unverified-subject", "unverified@example.test", false), cookie)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Contains(t, response.Header().Get("Location"), "oauth_error=unverified_email")
	count, err := db.Collection("users").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	// An explicit same-origin link survives Apple's POST with only the transient cookie.
	linkRequest := httptest.NewRequest(http.MethodPost, "https://app.example/api/v1/ams/oauth/apple/link", strings.NewReader(`{"request_url":"/settings#account","browser":true}`))
	linkRequest.Header.Set("Origin", "https://app.example")
	linkRequest.Header.Set("Content-Type", "application/json")
	linkRequest.AddCookie(access)
	linkedStart := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(linkedStart, linkRequest)
	require.Equal(t, http.StatusOK, linkedStart.Code)
	var linkBody struct {
		Data struct {
			RedirectURL string `json:"redirect_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(linkedStart.Body.Bytes(), &linkBody))
	require.NotEmpty(t, linkBody.Data.RedirectURL)
	linkedCookie := linkedStart.Result().Cookies()[0]
	response = complete("apple", transport.prepare(t, linkBody.Data.RedirectURL, "apple-subject", "relay@privaterelay.appleid.com", true), linkedCookie)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Equal(t, "/settings?oauth_linked=apple#account", response.Header().Get("Location"))
	for _, cookie := range response.Result().Cookies() {
		require.NotEqual(t, "access", cookie.Name)
	}
	linkedAccount, err := users.GetUserByOAuthIdentity(ctx, &user.OAuthIdentity{Provider: "apple", Issuer: oauth.AppleIssuer, Subject: "apple-subject"})
	require.NoError(t, err)
	require.Equal(t, details.UserID, linkedAccount.ID)
	require.Equal(t, "person@example.test", linkedAccount.Email)
	// Subsequent Apple login accepts absent first-only profile/email and retains the original account.
	cookie, authorisation = start("apple", "/app", true)
	response = complete("apple", transport.prepare(t, authorisation, "apple-subject", "", false), cookie)
	require.Equal(t, http.StatusSeeOther, response.Code)
	newAccess, _ := authCookies(response)
	repeatDetails, err := authService.ExtractAccessTokenMetadataByString(ctx, newAccess.Value)
	require.NoError(t, err)
	require.Equal(t, details.UserID, repeatDetails.UserID)
	// A stale session cannot borrow another session's fresh login timestamp.
	staleTokens, err := authService.CreateTokenWithAuthenticationTime(ctx, linkedAccount, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.NoError(t, redisRuntime.Store.CreateAuth(ctx, linkedAccount.ID, staleTokens))
	_, err = service.OAuthLink(ctx, &accessmanager.OauthLoginRequest{Provider: "google", RequestUrl: "/settings#account", Browser: true}, staleTokens.AccessToken)
	require.ErrorIs(t, err, accessmanager.ErrOAuthReauthenticationRequired)
	// Provider sessions use the normal refresh and logout paths, preserving auth_time.
	rotated, err := service.RefreshToken(ctx, &accessmanager.RefreshTokenRequest{RefreshToken: refresh.Value, AccessToken: access.Value})
	require.NoError(t, err)
	rotatedDetails, err := authService.ExtractAccessTokenMetadataByString(ctx, rotated.AccessToken)
	require.NoError(t, err)
	require.True(t, rotatedDetails.AuthenticationTime.Equal(details.AuthenticationTime))
	_, err = redisRuntime.Store.FetchAuth(ctx, details)
	require.Error(t, err)
	logoutRequest := httptest.NewRequest(http.MethodGet, "https://app.example/api/v1/ams/users/logout", nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+rotated.AccessToken)
	require.NoError(t, service.LogoutUser(ctx, logoutRequest))
	_, err = redisRuntime.Store.FetchAuth(ctx, rotatedDetails)
	require.Error(t, err)
	// A restricted account stays restricted and receives no provider session.
	for _, status := range []string{"PROVISIONED", "SUSPENDED", "DEACTIVATED", "LOCKED_OUT", "RECOVERY", "DELETED"} {
		_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": details.UserID}, bson.M{"$set": bson.M{"status": status}})
		require.NoError(t, err)
		cookie, authorisation = start("google", "/app", true)
		response = complete("google", transport.prepare(t, authorisation, "google-subject", "person@example.test", true), cookie)
		require.Equal(t, http.StatusSeeOther, response.Code)
		require.Contains(t, response.Header().Get("Location"), "oauth_error=restricted")
		for _, cookie := range response.Result().Cookies() {
			require.NotEqual(t, "access", cookie.Name)
		}
	}
	// Cancellation consumes the bound transaction and preserves the trusted return path.
	cookie, authorisation = start("apple", "/settings#account", true)
	parsed, err := url.Parse(authorisation)
	require.NoError(t, err)
	response = complete("apple", url.Values{"state": {parsed.Query().Get("state")}, "error": {"access_denied"}}, cookie)
	require.Equal(t, http.StatusSeeOther, response.Code)
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "cancelled", location.Query().Get("oauth_error"))
	require.Equal(t, "/settings#account", location.Query().Get("request_url"))
	// API clients retain a 200 token-metadata response when browser mode is absent.
	cookie, authorisation = start("google", "/app", false)
	response = complete("google", transport.prepare(t, authorisation, "api-subject", "api@example.test", true), cookie)
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, response.Header().Get("Location"))
	authCookies(response)
	// Cross-origin link POSTs are rejected before credentials can initiate a transaction.
	crossOrigin := httptest.NewRequest(http.MethodPost, "https://app.example/api/v1/ams/oauth/google/link", strings.NewReader(`{"request_url":"/settings#account","browser":true}`))
	crossOrigin.Header.Set("Origin", "https://evil.example")
	crossOrigin.AddCookie(newAccess)
	crossReply := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(crossReply, crossOrigin)
	require.Equal(t, http.StatusForbidden, crossReply.Code)
}

type oauthAuditStub struct{}

func (oauthAuditStub) LogAuditEvent(context.Context, *audit.LogAuditEventRequest) error { return nil }
