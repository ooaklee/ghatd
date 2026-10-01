package accessmanager

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/oauth"
)

func newMobileStoreTestClient(t *testing.T) (*redis.Client, MobileOAuthStore) {
	t.Helper()
	address := os.Getenv("GHATD_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set GHATD_TEST_REDIS_ADDR to an isolated Redis for mobile store integration")
	}
	client := redis.NewClient(&redis.Options{Addr: address, DialTimeout: time.Second})
	if err := client.Ping().Err(); err != nil {
		_ = client.Close()
		t.Fatalf("isolated Redis unavailable: %v", err)
	}
	namespace := fmt.Sprintf("mobile-store-test:%s:%d", t.Name(), time.Now().UnixNano())
	t.Cleanup(func() {
		keys, err := client.Keys("oauth:mobile:" + namespace + ":*").Result()
		if err == nil && len(keys) > 0 {
			_ = client.Del(keys...).Err()
		}
		_ = client.Close()
	})
	return client, NewRedisMobileOAuthStore(client, namespace)
}

// This client must never be called when input or context is rejected.
type unusedMobileRedis struct{}

func (unusedMobileRedis) SetNX(string, interface{}, time.Duration) *redis.BoolCmd {
	panic("unexpected Redis write")
}
func (unusedMobileRedis) Eval(string, []string, ...interface{}) *redis.Cmd {
	panic("unexpected Redis read")
}

func valid43() string { return "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij1234567" } // 43 chars

func TestMobileOAuthStoreSaveStartNilClientFailsClosed(t *testing.T) {
	store := NewRedisMobileOAuthStore(nil, "nil-check")
	if err := store.SaveStart(context.Background(), valid43(), []byte("x"), time.Second); err == nil {
		t.Fatal("expected error for nil client, got nil")
	}
	if _, err := store.ConsumeStart(context.Background(), valid43()); err == nil {
		t.Fatal("expected error for nil client consume, got nil")
	}
}

func TestMobileOAuthStoreRejectsInvalidIDs(t *testing.T) {
	store := NewRedisMobileOAuthStore(unusedMobileRedis{}, "input-check")
	ctx := context.Background()
	if err := store.SaveStart(ctx, "short", []byte("x"), time.Second); err == nil {
		t.Fatal("expected invalid ticket to be rejected")
	}
	if err := store.SaveStart(ctx, valid43()+"/", []byte("x"), time.Second); err == nil {
		t.Fatal("expected non-base64url ticket to be rejected")
	}
	if err := store.SaveGrant(ctx, "bad", valid43(), "uri", valid43(), nil, time.Second); err == nil {
		t.Fatal("expected invalid code to be rejected")
	}
	if _, err := store.ConsumeGrant(ctx, "bad", valid43(), "uri", valid43()); err == nil {
		t.Fatal("expected invalid code consume to be rejected")
	}
}

func TestMobileOAuthStoreRejectsBadTTL(t *testing.T) {
	store := NewRedisMobileOAuthStore(unusedMobileRedis{}, "input-check")
	ctx := context.Background()
	if err := store.SaveStart(ctx, valid43(), []byte("x"), 0); err == nil {
		t.Fatal("expected zero ttl to be rejected")
	}
	if err := store.SaveStart(ctx, valid43(), []byte("x"), -time.Second); err == nil {
		t.Fatal("expected negative ttl to be rejected")
	}
	if err := store.SaveGrant(ctx, valid43(), valid43(), "uri", valid43(), nil, 0); err == nil {
		t.Fatal("expected zero ttl grant to be rejected")
	}
}

func TestMobileOAuthStoreRespectsContextCancellation(t *testing.T) {
	store := NewRedisMobileOAuthStore(unusedMobileRedis{}, "input-check")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ConsumeStart(ctx, valid43()); err == nil {
		t.Fatal("expected cancelled context error")
	}
	if _, err := store.ConsumeGrant(ctx, valid43(), valid43(), "uri", valid43()); err == nil {
		t.Fatal("expected cancelled context error")
	}
}

func TestMobileOAuthStoreStartOneUse(t *testing.T) {
	_, store := newMobileStoreTestClient(t)
	ctx := context.Background()
	ticket := valid43()
	payload := []byte(`{"provider":"google"}`)

	if err := store.SaveStart(ctx, ticket, payload, time.Minute); err != nil {
		t.Fatalf("SaveStart: %v", err)
	}
	// Duplicate save must fail closed.
	if err := store.SaveStart(ctx, ticket, payload, time.Minute); err == nil {
		t.Fatal("expected duplicate SaveStart to fail")
	}
	got, err := store.ConsumeStart(ctx, ticket)
	if err != nil {
		t.Fatalf("ConsumeStart: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
	// Second consume must miss.
	if _, err := store.ConsumeStart(ctx, ticket); err != oauth.ErrSecureTransactionNotFound {
		t.Fatalf("expected ErrSecureTransactionNotFound on replay, got %v", err)
	}
}

func TestMobileOAuthStoreConsumeGrantProofSemantics(t *testing.T) {
	_, store := newMobileStoreTestClient(t)
	ctx := context.Background()
	code, challenge, state := valid43(), valid43(), valid43()
	other := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" // 43 chars, != valid43()
	uri := "boasi.io.bedrock:/oauth/callback"
	payload := []byte(`{"sub":"abc"}`)

	if err := store.SaveGrant(ctx, code, challenge, uri, state, payload, time.Minute); err != nil {
		t.Fatalf("SaveGrant: %v", err)
	}
	if err := store.SaveGrant(ctx, code, challenge, uri, state, payload, time.Minute); err == nil {
		t.Fatal("expected duplicate SaveGrant to fail")
	}

	// Wrong proof must NOT consume the grant.
	for _, tc := range []struct {
		name        string
		challenge   string
		redirectURI string
		state       string
	}{
		{"wrong challenge", other, uri, state},
		{"wrong redirect", challenge, "other:/cb", state},
		{"wrong state", challenge, uri, other},
	} {
		if _, err := store.ConsumeGrant(ctx, code, tc.challenge, tc.redirectURI, tc.state); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}

	// Correct proof redeems exactly once.
	got, err := store.ConsumeGrant(ctx, code, challenge, uri, state)
	if err != nil {
		t.Fatalf("ConsumeGrant with correct proof: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
	// Replay must miss.
	if _, err := store.ConsumeGrant(ctx, code, challenge, uri, state); err != oauth.ErrSecureTransactionNotFound {
		t.Fatalf("expected ErrSecureTransactionNotFound on replay, got %v", err)
	}
	// Unknown code must miss with non-leaking error.
	if _, err := store.ConsumeGrant(ctx, challenge, challenge, uri, state); err != oauth.ErrSecureTransactionNotFound {
		t.Fatalf("expected ErrSecureTransactionNotFound for unknown code, got %v", err)
	}
}

func TestMobileOAuthStoreExpiresAndCapsArtifacts(t *testing.T) {
	client, store := newMobileStoreTestClient(t)
	redisStore := store.(*RedisMobileOAuthStore)
	ctx := context.Background()
	id, uri := valid43(), "boasi.io.bedrock:/oauth/callback"
	requireTTL := func(key string, cap time.Duration) {
		t.Helper()
		ttl, err := client.PTTL(key).Result()
		if err != nil || ttl <= 0 || ttl > cap {
			t.Fatalf("unexpected artifact TTL: %v, %v", ttl, err)
		}
	}
	if err := store.SaveStart(ctx, id, []byte("start"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGrant(ctx, id, id, uri, id, []byte("grant"), time.Hour); err != nil {
		t.Fatal(err)
	}
	requireTTL(redisStore.startKey(id), 2*time.Minute)
	requireTTL(redisStore.grantKey(id), time.Minute)
	// Expire the actual Redis keys without making the test sleep for a minute.
	for _, key := range []string{redisStore.startKey(id), redisStore.grantKey(id)} {
		if err := client.PExpire(key, -time.Millisecond).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ConsumeStart(ctx, id); err != oauth.ErrSecureTransactionNotFound {
		t.Fatalf("expired start was redeemable: %v", err)
	}
	if _, err := store.ConsumeGrant(ctx, id, id, uri, id); err != oauth.ErrSecureTransactionNotFound {
		t.Fatalf("expired grant was redeemable: %v", err)
	}
}
