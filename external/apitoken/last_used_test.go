package apitoken

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// touchRepository implements only the atomic port so any accidental legacy
// list/read/replace call fails immediately instead of hiding in a broad mock.
type touchRepository struct {
	ApitokenRespository
	calls            int
	tokenID, ownerID string
	digest           []byte
	at               time.Time
	err              error
}

func (r *touchRepository) TouchAPIToken(_ context.Context, tokenID, ownerID string, digest []byte, at time.Time) error {
	r.calls++
	r.tokenID, r.ownerID, r.at = tokenID, ownerID, at
	r.digest = append([]byte(nil), digest...)
	digest[0] ^= 0xff
	return r.err
}

func TestLastUsedDelegatesOnlyExactAtomicUpdate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tokenID, ownerID              string
		nilRequest, badDigest, storeFailure bool
		wantCalls                           int
		wantError                           bool
	}{
		{name: "verified", tokenID: "credential", ownerID: "owner", wantCalls: 1},
		{name: "nil request", nilRequest: true, wantError: true},
		{name: "missing token ID", ownerID: "owner", wantError: true},
		{name: "missing owner", tokenID: "credential", wantError: true},
		{name: "invalid digest", tokenID: "credential", ownerID: "owner", badDigest: true, wantError: true},
		{name: "store failure", tokenID: "credential", ownerID: "owner", storeFailure: true, wantCalls: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digest := sha256.Sum256([]byte("test-secret"))
			req := &UpdateAPITokenLastUsedAtRequest{TokenID: tc.tokenID, ClientID: tc.ownerID, APITokenEncoded: append([]byte(nil), digest[:]...)}
			if tc.nilRequest {
				req = nil
			}
			if tc.badDigest {
				req.APITokenEncoded = []byte("invalid")
			}
			repo := &touchRepository{}
			if tc.storeFailure {
				repo.err = errors.New("store unavailable")
			}
			before := time.Now().UTC()
			err := NewService(repo).UpdateAPITokenLastUsedAt(context.Background(), req)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantCalls, repo.calls)
			if tc.wantCalls > 0 {
				require.Equal(t, tc.tokenID, repo.tokenID)
				require.Equal(t, tc.ownerID, repo.ownerID)
				require.Equal(t, digest[:], repo.digest)
				require.Equal(t, digest[:], req.APITokenEncoded, "store must not mutate caller digest")
				require.False(t, repo.at.Before(before))
				require.False(t, repo.at.After(time.Now().UTC()))
				require.Same(t, time.UTC, repo.at.Location())
			}
		})
	}
}
