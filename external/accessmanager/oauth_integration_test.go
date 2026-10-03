package accessmanager_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/go-redis/redis/v7"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
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
	suite, err := middleware.NewSuite(&middleware.NewSuiteRequest{Service: service, EphemeralStore: redisRuntime.Store, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh", CookieDomain: "app.example"})
	require.NoError(t, err)
	accessmanager.AttachRoutes(&accessmanager.AttachRoutesRequest{Router: httpRouter, Handler: handler, ActiveOnlyMiddleware: suite.ActiveOnly, HardenedRateLimitMiddleware: suite.HardenedRateLimit})
	require.NoError(t, httpRouter.ValidateRoutePolicies(), "integration fixtures must install required protections")
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

	t.Run("native handoff reuses provider identities and normal sessions", func(t *testing.T) {
		const callbackURI = "boasi.io.bedrock:/oauth/callback"
		const otherCallbackURI = "other.example.app:/oauth/callback"
		post := func(path string, body map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
			raw, marshalErr := json.Marshal(body)
			require.NoError(t, marshalErr)
			req := httptest.NewRequest(http.MethodPost, "https://app.example"+path, strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			for _, c := range cookies {
				req.AddCookie(c)
			}
			out := httptest.NewRecorder()
			httpRouter.GetRouter().ServeHTTP(out, req)
			return out
		}
		data := func(out *httptest.ResponseRecorder) map[string]interface{} {
			var envelope struct {
				Data map[string]interface{} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &envelope))
			return envelope.Data
		}
		discovery := serve(http.MethodGet, "/api/v1/ams/oauth/mobile/providers", "")
		require.Empty(t, data(discovery)["providers"])
		require.NoError(t, handler.ConfigureMobileOAuth(accessmanager.MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{callbackURI, otherCallbackURI}, Store: accessmanager.NewRedisMobileOAuthStore(redisRuntime.Client, dbName)}))
		discovery = serve(http.MethodGet, "/api/v1/ams/oauth/mobile/providers", "")
		require.Len(t, data(discovery)["providers"], 2)
		type attempt struct {
			verifier, state, authURL string
			cookie                   *http.Cookie
		}
		begin := func(provider string, linking bool, session *http.Cookie) attempt {
			verifier := base64.RawURLEncoding.EncodeToString([]byte(toolbox.GenerateUuidV4()[:32]))
			state := base64.RawURLEncoding.EncodeToString([]byte(toolbox.GenerateUuidV4()[:32]))
			digest := sha256.Sum256([]byte(verifier))
			body := map[string]string{"redirect_uri": callbackURI, "state": state, "code_challenge": base64.RawURLEncoding.EncodeToString(digest[:]), "code_challenge_method": "S256"}
			path := "/api/v1/ams/oauth/" + provider + "/mobile/login"
			cookies := []*http.Cookie{}
			if linking {
				path = "/api/v1/ams/oauth/" + provider + "/mobile/link"
				if session != nil {
					cookies = append(cookies, session)
				}
			}
			out := post(path, body, cookies...)
			require.Equal(t, 200, out.Code, out.Body.String())
			require.Empty(t, out.Result().Cookies())
			authorization, parseErr := url.Parse(data(out)["authorization_url"].(string))
			require.NoError(t, parseErr)
			require.Equal(t, "app.example", authorization.Host)
			browserStart := serve(http.MethodGet, authorization.RequestURI(), "")
			require.Equal(t, 302, browserStart.Code, browserStart.Body.String())
			require.Len(t, browserStart.Result().Cookies(), 1)
			repeated := serve(http.MethodGet, authorization.RequestURI(), "")
			require.Equal(t, 400, repeated.Code)
			return attempt{verifier: verifier, state: state, authURL: browserStart.Header().Get("Location"), cookie: browserStart.Result().Cookies()[0]}
		}
		finish := func(provider string, flow attempt, subject, email string) string {
			params := transport.prepare(t, flow.authURL, subject, email, email != "")
			if provider == "apple" && email != "" {
				params.Set("user", `{"name":{"firstName":"Relay","lastName":"Person"}}`)
			}
			out := complete(provider, params, flow.cookie)
			require.Equal(t, 303, out.Code, out.Body.String())
			for _, c := range out.Result().Cookies() {
				require.NotEqual(t, "access", c.Name)
				require.NotEqual(t, "refresh", c.Name)
			}
			uri, parseErr := url.Parse(out.Header().Get("Location"))
			require.NoError(t, parseErr)
			require.Equal(t, callbackURI, uri.Scheme+":"+uri.Path)
			require.Equal(t, flow.state, uri.Query().Get("state"))
			require.Empty(t, uri.Query().Get("error"))
			require.Len(t, uri.Query().Get("code"), 43)
			require.NotContains(t, out.Body.String(), "access_token")
			return uri.Query().Get("code")
		}
		redeem := func(code string, flow attempt, cookies ...*http.Cookie) *httptest.ResponseRecorder {
			return post("/api/v1/ams/oauth/mobile/exchange", map[string]string{"code": code, "code_verifier": flow.verifier, "redirect_uri": callbackURI, "state": flow.state}, cookies...)
		}
		flow := begin("google", false, nil)
		code := finish("google", flow, "native-google", "native@example.test")
		_, err := users.GetUserByOAuthIdentity(ctx, &user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: "native-google"})
		require.ErrorIs(t, err, user.ErrUserNotFound, "no account mutation before verifier proof")
		invalid := flow
		invalid.verifier = strings.Repeat("x", 43)
		require.Equal(t, 400, redeem(code, invalid).Code)
		invalid = flow
		invalid.state = strings.Repeat("y", 43)
		require.Equal(t, 400, redeem(code, invalid).Code)
		wrongApp := post("/api/v1/ams/oauth/mobile/exchange", map[string]string{"code": code, "code_verifier": flow.verifier, "redirect_uri": otherCallbackURI, "state": flow.state})
		require.Equal(t, 400, wrongApp.Code)
		out := redeem(code, flow)
		require.Equal(t, 200, out.Code, out.Body.String())
		nativeAccess, nativeRefresh := authCookies(out)
		require.Equal(t, false, data(out)["linked"])
		require.Equal(t, "google", data(out)["provider"])
		require.Equal(t, 400, redeem(code, flow).Code)
		nativeDetails, err := authService.ExtractAccessTokenMetadataByString(ctx, nativeAccess.Value)
		require.NoError(t, err)
		nativeAccount, err := users.GetUserByID(ctx, &user.GetUserByIDRequest{ID: nativeDetails.UserID})
		require.NoError(t, err)
		require.True(t, nativeAccount.User.Verification.EmailVerified)
		require.Equal(t, "ACTIVE", nativeAccount.User.Status)
		owner, err := redisRuntime.Store.FetchAuth(ctx, nativeDetails)
		require.NoError(t, err)
		require.Equal(t, nativeDetails.UserID, owner)

		// The same web subject and native subject identify the same account.
		webCookie, webAuthorization := start("google", "/app", true)
		webReply := complete("google", transport.prepare(t, webAuthorization, "native-google", "native@example.test", true), webCookie)
		webAccess, _ := authCookies(webReply)
		webDetails, err := authService.ExtractAccessTokenMetadataByString(ctx, webAccess.Value)
		require.NoError(t, err)
		require.Equal(t, nativeDetails.UserID, webDetails.UserID)

		// Browser callback cookie stays mandatory, even with mobile state.
		missingCookieFlow := begin("google", false, nil)
		params := transport.prepare(t, missingCookieFlow.authURL, "no-cookie", "no-cookie@example.test", true)
		missingCookie := serve(http.MethodGet, "/api/v1/ams/oauth/google/callback?"+params.Encode(), "")
		require.Equal(t, 400, missingCookie.Code)
		require.Empty(t, missingCookie.Header().Get("Location"))

		// Mobile provider cancellation returns only fixed error and matching app state.
		cancelled := begin("apple", false, nil)
		appleAuthorization, err := url.Parse(cancelled.authURL)
		require.NoError(t, err)
		cancelledReply := complete("apple", url.Values{"state": {appleAuthorization.Query().Get("state")}, "error": {"access_denied"}}, cancelled.cookie)
		cancelURI, err := url.Parse(cancelledReply.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, callbackURI, cancelURI.Scheme+":"+cancelURI.Path)
		require.Equal(t, "cancelled", cancelURI.Query().Get("error"))
		require.Equal(t, cancelled.state, cancelURI.Query().Get("state"))

		// Apple relay/profile survives repeated native authorization with no email/name.
		appleFlow := begin("apple", false, nil)
		appleCode := finish("apple", appleFlow, "native-apple", "mobile@privaterelay.appleid.com")
		appleReply := redeem(appleCode, appleFlow)
		require.Equal(t, 200, appleReply.Code, appleReply.Body.String())
		appleAccess, _ := authCookies(appleReply)
		appleDetails, err := authService.ExtractAccessTokenMetadataByString(ctx, appleAccess.Value)
		require.NoError(t, err)
		repeat := begin("apple", false, nil)
		repeatedReply := redeem(finish("apple", repeat, "native-apple", ""), repeat)
		repeatedAccess, _ := authCookies(repeatedReply)
		repeatedDetails, err := authService.ExtractAccessTokenMetadataByString(ctx, repeatedAccess.Value)
		require.NoError(t, err)
		require.Equal(t, appleDetails.UserID, repeatedDetails.UserID)
		relayAccount, err := users.GetUserByID(ctx, &user.GetUserByIDRequest{ID: appleDetails.UserID})
		require.NoError(t, err)
		require.Equal(t, "mobile@privaterelay.appleid.com", relayAccount.User.Email)

		// A callback cannot link without the initiating app's fresh session and verifier.
		linking := begin("apple", true, nativeAccess)
		linkCode := finish("apple", linking, "native-linked-apple", "linked@privaterelay.appleid.com")
		_, err = users.GetUserByOAuthIdentity(ctx, &user.OAuthIdentity{Provider: "apple", Issuer: oauth.AppleIssuer, Subject: "native-linked-apple"})
		require.ErrorIs(t, err, user.ErrUserNotFound)
		wrongSession := redeem(linkCode, linking, webAccess)
		require.Equal(t, 403, wrongSession.Code)
		require.Equal(t, "reauth_required", data(wrongSession)["error"])
		_, err = users.GetUserByOAuthIdentity(ctx, &user.OAuthIdentity{Provider: "apple", Issuer: oauth.AppleIssuer, Subject: "native-linked-apple"})
		require.ErrorIs(t, err, user.ErrUserNotFound)
		linking = begin("apple", true, nativeAccess)
		linkedReply := redeem(finish("apple", linking, "native-linked-apple", "linked@privaterelay.appleid.com"), linking, nativeAccess)
		require.Equal(t, 200, linkedReply.Code, linkedReply.Body.String())
		require.Equal(t, true, data(linkedReply)["linked"])
		require.Empty(t, linkedReply.Result().Cookies())
		linkedUser, err := users.GetUserByOAuthIdentity(ctx, &user.OAuthIdentity{Provider: "apple", Issuer: oauth.AppleIssuer, Subject: "native-linked-apple"})
		require.NoError(t, err)
		require.Equal(t, nativeDetails.UserID, linkedUser.ID)
		require.Equal(t, "native@example.test", linkedUser.Email)

		// Account restrictions are checked at redemption, not just at callback time.
		restricted := begin("google", false, nil)
		restrictedCode := finish("google", restricted, "native-google", "native@example.test")
		_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": nativeDetails.UserID}, bson.M{"$set": bson.M{"status": "SUSPENDED"}})
		require.NoError(t, err)
		restrictedReply := redeem(restrictedCode, restricted)
		require.Equal(t, 403, restrictedReply.Code)
		require.Equal(t, "restricted", data(restrictedReply)["error"])
		require.Empty(t, restrictedReply.Result().Cookies())
		_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": nativeDetails.UserID}, bson.M{"$set": bson.M{"status": "ACTIVE"}})
		require.NoError(t, err)

		// Only one of simultaneous native redemptions wins, with one account.
		concurrent := begin("google", false, nil)
		concurrentCode := finish("google", concurrent, "native-concurrent", "concurrent-native@example.test")
		outcomes := make(chan int, 8)
		for i := 0; i < 8; i++ {
			go func() { outcomes <- redeem(concurrentCode, concurrent).Code }()
		}
		winners := 0
		for i := 0; i < 8; i++ {
			status := <-outcomes
			if status == 200 {
				winners++
			} else {
				require.Equal(t, 400, status)
			}
		}
		require.Equal(t, 1, winners)
		count, err := db.Collection("users").CountDocuments(ctx, bson.M{"email": "concurrent-native@example.test"})
		require.NoError(t, err)
		require.Equal(t, int64(1), count)

		// The exchanged session uses the unchanged refresh and logout implementation.
		nativeRotated, err := service.RefreshToken(ctx, &accessmanager.RefreshTokenRequest{RefreshToken: nativeRefresh.Value, AccessToken: nativeAccess.Value})
		require.NoError(t, err)
		nativeRotatedDetails, err := authService.ExtractAccessTokenMetadataByString(ctx, nativeRotated.AccessToken)
		require.NoError(t, err)
		require.True(t, nativeRotatedDetails.AuthenticationTime.Equal(nativeDetails.AuthenticationTime))
		nativeLogout := httptest.NewRequest(http.MethodGet, "https://app.example/api/v1/ams/logout", nil)
		nativeLogout.Header.Set("Authorization", "Bearer "+nativeRotated.AccessToken)
		require.NoError(t, service.LogoutUser(ctx, nativeLogout))
		_, err = redisRuntime.Store.FetchAuth(ctx, nativeRotatedDetails)
		require.Error(t, err)
	})

}

type oauthAuditStub struct{}

func (oauthAuditStub) LogAuditEvent(context.Context, *audit.LogAuditEventRequest) error { return nil }
