package oauth

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
	"github.com/go-redis/redis/v7"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transactionMemory struct {
	mu   sync.Mutex
	data map[string]*StoredTransaction
}

func (s *transactionMemory) Save(_ context.Context, txn *StoredTransaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[txn.State] = txn
	return nil
}
func (s *transactionMemory) Consume(_ context.Context, id string) (*StoredTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	txn := s.data[id]
	delete(s.data, id)
	if txn == nil {
		return nil, ErrSecureTransactionNotFound
	}
	return txn, nil
}
func testStore() *transactionMemory { return &transactionMemory{data: map[string]*StoredTransaction{}} }
func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}
func testJWKS(key *rsa.PrivateKey) map[string]interface{} {
	return map[string]interface{}{"keys": []interface{}{map[string]interface{}{"kid": "known", "kty": "RSA", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}}
}
func testSignedToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "known"
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	return raw
}
func testClaims(issuer, nonce string) jwt.MapClaims {
	return jwt.MapClaims{"iss": issuer, "sub": "stable-subject", "aud": "client", "exp": time.Now().Add(time.Minute).Unix(), "nonce": nonce, "email": "verified@example.test", "email_verified": true}
}

func TestIDTokenValidation(t *testing.T) {
	key := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(testJWKS(key)) }))
	defer server.Close()
	cache := newJWKSCache(server.URL, server.Client(), time.Now)
	for _, test := range []struct {
		name   string
		modify func(jwt.MapClaims)
		valid  bool
	}{
		{"valid", func(c jwt.MapClaims) {}, true},
		{"google documented alias", func(c jwt.MapClaims) { c["iss"] = "accounts.google.com" }, true},
		{"http issuer", func(c jwt.MapClaims) { c["iss"] = "http://accounts.google.com" }, false},
		{"issuer slash", func(c jwt.MapClaims) { c["iss"] = GoogleSecureIssuer + "/" }, false},
		{"wrong audience", func(c jwt.MapClaims) { c["aud"] = "attacker" }, false},
		{"missing expiry", func(c jwt.MapClaims) { delete(c, "exp") }, false},
		{"expired", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-2 * time.Minute).Unix() }, false},
		{"missing subject", func(c jwt.MapClaims) { delete(c, "sub") }, false},
		{"wrong nonce", func(c jwt.MapClaims) { c["nonce"] = "other" }, false},
		{"missing nonce", func(c jwt.MapClaims) { delete(c, "nonce") }, false},
		{"missing multi audience azp", func(c jwt.MapClaims) { c["aud"] = []string{"client", "other"} }, false},
		{"wrong single audience azp", func(c jwt.MapClaims) { c["azp"] = "other" }, false},
		{"valid multi audience azp", func(c jwt.MapClaims) { c["aud"] = []string{"client", "other"}; c["azp"] = "client" }, true},
		{"Apple string verification", func(c jwt.MapClaims) { c["email_verified"] = "true" }, true},
		{"invalid verification type", func(c jwt.MapClaims) { c["email_verified"] = 1 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := testClaims(GoogleSecureIssuer, "nonce")
			test.modify(claims)
			_, err := ValidateIDToken(context.Background(), cache, testSignedToken(t, key, claims), &IDTokenExpectations{Issuer: GoogleSecureIssuer, Audience: "client", Nonce: "nonce", RequireAZPMatching: true})
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrSecureIDTokenInvalid)
			}
		})
	}
	unsigned := jwt.NewWithClaims(jwt.SigningMethodHS256, testClaims(GoogleSecureIssuer, "nonce"))
	unsigned.Header["kid"] = "known"
	raw, err := unsigned.SignedString([]byte("wrong"))
	require.NoError(t, err)
	_, err = ValidateIDToken(context.Background(), cache, raw, &IDTokenExpectations{Issuer: GoogleSecureIssuer, Audience: "client", Nonce: "nonce"})
	require.ErrorIs(t, err, ErrSecureIDTokenInvalid)
}

func TestJWKSRefreshIsBoundedAndCancellable(t *testing.T) {
	key := testKey(t)
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		_ = json.NewEncoder(w).Encode(testJWKS(key))
	}))
	defer server.Close()
	cache := newJWKSCache(server.URL, server.Client(), time.Now)
	done := make(chan error)
	go func() { _, err := cache.lookup(context.Background(), "known"); done <- err }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cache.lookup(ctx, "unknown")
	require.ErrorIs(t, err, context.Canceled)
	close(release)
	require.NoError(t, <-done)
	for i := 0; i < 20; i++ {
		_, err = cache.lookup(context.Background(), "unknown")
		require.Error(t, err)
	}
	require.Equal(t, int32(1), calls.Load())
	cache.mu.Lock()
	cache.fetchedAt = time.Now().Add(-time.Hour)
	cache.mu.Unlock()
	_, err = cache.lookup(context.Background(), "known")
	require.Error(t, err)
}

func TestGoogleSecureFlowAndStateOrdering(t *testing.T) {
	key := testKey(t)
	store := testStore()
	var nonce, verifier string
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/keys" {
			_ = json.NewEncoder(w).Encode(testJWKS(key))
			return
		}
		exchanges.Add(1)
		require.NoError(t, r.ParseForm())
		require.Equal(t, verifier, r.Form.Get("code_verifier"))
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": testSignedToken(t, key, testClaims(GoogleSecureIssuer, nonce))})
	}))
	defer server.Close()
	provider, err := NewGoogleSecureProvider(&NewGoogleSecureProviderRequest{ClientID: "client", ClientSecret: "secret", RedirectURL: "http://localhost/callback", Store: store, HTTPClient: server.Client()})
	require.NoError(t, err)
	provider.tokenURL = server.URL + "/token"
	provider.jwks.jwksURL = server.URL + "/keys"
	txn, err := provider.BeginSecureTransactionWithOptions(context.Background(), "/app?test=1#section", SecureFlowOptions{Browser: true})
	require.NoError(t, err)
	stored := store.data[txn.TransactionID]
	nonce = stored.Nonce
	verifier = stored.PKCEVerifier
	parsed, err := url.Parse(txn.AuthorisationURL)
	require.NoError(t, err)
	digest := sha256.Sum256([]byte(verifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(digest[:]), parsed.Query().Get("code_challenge"))
	require.Equal(t, "S256", parsed.Query().Get("code_challenge_method"))
	callback := &SecureCallbackRequest{Method: http.MethodGet, TransactionID: txn.TransactionID, Query: url.Values{"state": {"wrong"}, "code": {"code"}}}
	_, err = provider.CompleteSecureTransaction(context.Background(), callback)
	require.Error(t, err)
	require.Contains(t, store.data, txn.TransactionID)
	callback.Query.Set("state", txn.TransactionID)
	callback.Query["code"] = []string{"one", "two"}
	_, err = provider.CompleteSecureTransaction(context.Background(), callback)
	require.Error(t, err)
	require.Contains(t, store.data, txn.TransactionID)
	callback.Query.Set("code", "code")
	result, err := provider.CompleteSecureTransaction(context.Background(), callback)
	require.NoError(t, err)
	require.True(t, result.Transaction.Options.Browser)
	require.Equal(t, "/app?test=1#section", result.ReturnPath)
	require.Equal(t, "stable-subject", result.UserInfo.(IdentityUserInfo).GetProviderSubject())
	require.Equal(t, int32(1), exchanges.Load())
	_, err = provider.CompleteSecureTransaction(context.Background(), callback)
	require.ErrorIs(t, err, ErrSecureTransactionNotFound)
	require.Equal(t, int32(1), exchanges.Load())
}

func TestAppleFormPostFirstAndRepeatProfile(t *testing.T) {
	rsaKey := testKey(t)
	appleKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(appleKey)
	require.NoError(t, err)
	store := testStore()
	var nonce string
	repeat := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/keys" {
			_ = json.NewEncoder(w).Encode(testJWKS(rsaKey))
			return
		}
		require.NoError(t, r.ParseForm())
		secret, err := jwt.ParseWithClaims(r.Form.Get("client_secret"), &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
			require.Equal(t, "apple-key", token.Header["kid"])
			return &appleKey.PublicKey, nil
		}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithIssuer("team"), jwt.WithAudience(AppleIssuer), jwt.WithSubject("client"))
		require.NoError(t, err)
		require.True(t, secret.Valid)
		require.Empty(t, r.Form.Get("code_verifier"))
		claims := testClaims(AppleIssuer, nonce)
		claims["email_verified"] = "true"
		if repeat {
			delete(claims, "email")
			delete(claims, "email_verified")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": testSignedToken(t, rsaKey, claims)})
	}))
	defer server.Close()
	provider, err := NewAppleProvider(&NewAppleProviderRequest{ClientID: "client", TeamID: "team", KeyID: "apple-key", PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), RedirectURL: "https://app.example/callback", Store: store, HTTPClient: server.Client()})
	require.NoError(t, err)
	provider.tokenURL = server.URL + "/token"
	provider.jwks.jwksURL = server.URL + "/keys"
	for i := 0; i < 2; i++ {
		txn, err := provider.BeginSecureTransaction(context.Background(), "/app")
		require.NoError(t, err)
		nonce = store.data[txn.TransactionID].Nonce
		require.Contains(t, txn.AuthorisationURL, "response_mode=form_post")
		callback := &SecureCallbackRequest{TransactionID: txn.TransactionID, Method: http.MethodPost, Query: url.Values{"state": {txn.TransactionID}, "code": {"code"}}}
		if i == 0 {
			callback.Query.Set("user", `{"email":"attacker@example.test","name":{"firstName":"First","lastName":""}}`)
		} else {
			repeat = true
		}
		result, err := provider.CompleteSecureTransaction(context.Background(), callback)
		require.NoError(t, err)
		if i == 0 {
			require.Equal(t, "verified@example.test", result.UserInfo.GetUserEmail())
			require.Equal(t, "First", result.UserInfo.GetUserFirstName())
		} else {
			require.Empty(t, result.UserInfo.GetUserEmail())
			require.Empty(t, result.UserInfo.GetUserFirstName())
		}
	}
	txn, err := provider.BeginSecureTransaction(context.Background(), "/app")
	require.NoError(t, err)
	result, err := provider.CompleteSecureTransaction(context.Background(), &SecureCallbackRequest{TransactionID: txn.TransactionID, Method: http.MethodPost, Query: url.Values{"state": {txn.TransactionID}, "error": {"access_denied"}}})
	require.ErrorIs(t, err, ErrProviderCancelled)
	require.NotNil(t, result.Transaction)
	require.NotContains(t, store.data, txn.TransactionID)
}

func TestReturnPathPolicy(t *testing.T) {
	for _, path := range []string{"/app?tab=x#part", "/app/settings", ""} {
		_, err := ValidateSecureReturnPath(path)
		require.NoError(t, err)
	}
	for _, path := range []string{"//evil.test", "https://evil.test", "/\\evil", "/%2f%2fevil.test", "/%5cevil", "/app\r\nLocation: evil"} {
		_, err := ValidateSecureReturnPath(path)
		require.Error(t, err)
	}
}

func TestRedisTransactionConsumptionIntegration(t *testing.T) {
	addr := os.Getenv("GHATD_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GHATD_TEST_REDIS_ADDR for real Redis integration")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	store := NewRedisTransactionStore(client, t.Name())
	state, err := NewOpaqueTransactionID()
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), &StoredTransaction{State: state, Nonce: "nonce", Provider: "google"}))
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Consume(context.Background(), state)
			if err == nil {
				wins.Add(1)
			} else {
				require.ErrorIs(t, err, ErrSecureTransactionNotFound)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), wins.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Consume(ctx, state)
	require.ErrorIs(t, err, context.Canceled)
}
