package apitoken

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/stretchr/testify/require"
)

// verificationRepository rejects accidental list-based lookup by leaving the
// embedded interface nil; only the exact digest lookup is implemented.
type verificationRepository struct {
	ApitokenRespository
	token  *UserAPIToken
	err    error
	calls  int
	nanoID string
	digest []byte
}

func (r *verificationRepository) GetAPITokenByDigest(_ context.Context, nanoID string, digest []byte) (*UserAPIToken, error) {
	r.calls++
	r.nanoID = nanoID
	r.digest = append([]byte(nil), digest...)
	return r.token, r.err
}

func TestVerifyAPITokenIdentityAndExpiry(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, tc := range []struct {
		name        string
		headers     []string
		status      string
		expiry      string
		owner       string
		nano        string
		id          string
		wrongDigest bool
		nilRecord   bool
		storeError  bool
		wantValid   bool
		wantLookup  bool
	}{
		{name: "permanent", wantValid: true, wantLookup: true},
		{name: "future expiry", expiry: now.Add(time.Hour).Format(time.RFC3339Nano), wantValid: true, wantLookup: true},
		{name: "past expiry", expiry: now.Add(-time.Hour).Format(time.RFC3339Nano), wantLookup: true},
		{name: "invalid expiry", expiry: "not-a-date", wantLookup: true},
		{name: "revoked", status: UserTokenStatusKeyRevoked, wantLookup: true},
		{name: "unknown status", status: "UNKNOWN", wantLookup: true},
		{name: "missing owner", owner: "missing", wantLookup: true},
		{name: "missing token id", id: "missing", wantLookup: true},
		{name: "wrong namespace", nano: "other", wantLookup: true},
		{name: "wrong digest", wrongDigest: true, wantLookup: true},
		{name: "nil record", nilRecord: true, wantLookup: true},
		{name: "store failure", storeError: true, wantLookup: true},
		{name: "missing header", headers: []string{}},
		{name: "empty prefix", headers: []string{".test-secret"}},
		{name: "empty secret", headers: []string{"member."}},
		{name: "extra segment", headers: []string{"member.test.secret"}},
		{name: "duplicate header", headers: []string{"member.test-secret", "member.test-secret"}},
		{name: "comma combined", headers: []string{"member.test-secret,member.other"}},
		{name: "whitespace", headers: []string{"member.test secret"}},
		{name: "invisible prefix", headers: []string{"mem\u200bber.test-secret"}},
		{name: "invisible secret", headers: []string{"member.test\u200b-secret"}},
		{name: "invalid UTF8", headers: []string{"member." + string([]byte{0xff})}},
		{name: "oversized prefix", headers: []string{strings.Repeat("x", 257) + ".test-secret"}},
		{name: "oversized secret", headers: []string{"member." + strings.Repeat("x", 513)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digest := sha256.Sum256([]byte("test-secret"))
			token := &UserAPIToken{ID: "credential", CreatedByID: "user", CreatedByNanoId: "member", Status: UserTokenStatusKeyActive, ValueSHA: digest[:], TtlExpiresAt: tc.expiry}
			if tc.status != "" {
				token.Status = tc.status
			}
			if tc.nano != "" {
				token.CreatedByNanoId = tc.nano
			}
			if tc.owner == "missing" {
				token.CreatedByID = ""
			}
			if tc.id == "missing" {
				token.ID = ""
			}
			if tc.wrongDigest {
				token.ValueSHA = []byte("wrong")
			}
			if tc.nilRecord {
				token = nil
			}
			repo := &verificationRepository{token: token}
			if tc.storeError {
				repo.err = errors.New("store failure")
			}
			request := httptest.NewRequest("GET", "/", nil)
			headers := tc.headers
			if headers == nil {
				headers = []string{"member.test-secret"}
			}
			for _, h := range headers {
				request.Header.Add(common.SystemWideXApiToken, h)
			}
			got, err := NewService(repo).ExtractValidateUserAPITokenMetadata(context.Background(), request)
			if tc.wantValid {
				require.NoError(t, err)
				require.True(t, got.IsValid)
				require.Equal(t, "credential", got.TokenID)
				require.Equal(t, "user", got.UserID)
				require.Equal(t, "member", got.NanoId)
				require.Empty(t, got.UserAPIToken)
				require.Equal(t, digest[:], got.UserAPITokenEncoded)
			} else {
				require.Error(t, err)
				require.Nil(t, got)
			}
			if tc.wantLookup {
				require.Equal(t, 1, repo.calls)
				require.Equal(t, "member", repo.nanoID)
				require.Equal(t, digest[:], repo.digest)
			} else {
				require.Zero(t, repo.calls)
			}
		})
	}
}

func TestGenerateAPITokenConcurrentSecrets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		workers int
	}{{"one caller", 1}, {"concurrent callers", 32}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tokens := make(chan *UserAPIToken, tc.workers)
			var workers sync.WaitGroup
			for i := 0; i < tc.workers; i++ {
				workers.Add(1)
				go func() { defer workers.Done(); tokens <- new(UserAPIToken).Generate() }()
			}
			workers.Wait()
			close(tokens)
			seen := map[string]bool{}
			for token := range tokens {
				raw, err := base64.RawURLEncoding.DecodeString(token.Value)
				require.NoError(t, err)
				require.Len(t, raw, 32)
				digest := sha256.Sum256([]byte(token.Value))
				require.Equal(t, digest[:], token.ValueSHA)
				require.False(t, seen[token.Value])
				seen[token.Value] = true
			}
		})
	}
}
