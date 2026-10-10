package recordstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: named nil/absolute/precision/zone/zero expiry cases test
// opt-in cleanup metadata without shortening the owning selected lifetime.
func TestRecordExpirationPrecisionAndCompatibility(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, mode string
		at, wantAt time.Time
		want       error
	}{
		{name: "nil_expiration_preserves_original_AAD", mode: "nil"},
		{name: "exact_millisecond_is_not_extended", at: base.Add(time.Millisecond), wantAt: base.Add(time.Millisecond)},
		{name: "fractional_millisecond_rounds_up", at: base.Add(time.Nanosecond), wantAt: base.Add(time.Millisecond)},
		{name: "past_expiration_is_valid_cleanup_metadata", at: base.Add(-time.Hour), wantAt: base.Add(-time.Hour)},
		{name: "non_UTC_input_normalizes_without_shortening", at: time.Date(2026, 10, 7, 13, 0, 0, 123, time.FixedZone("fixture", 3600)), wantAt: base.Add(time.Millisecond)},
		{name: "explicit_zero_is_not_a_delete_all_policy", want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			store := &MongoStore{cipher: cipher}
			original, err := NewRecord("fixture", "identity", "scope", 1, map[string]string{"private": "fixture"})
			require.NoError(t, err)
			r := original
			if tc.mode != "nil" {
				r, err = r.WithExpiration(tc.at)
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, r)
					return
				}
				require.Equal(t, tc.wantAt, *r.ExpiresAt)
				require.False(t, r.ExpiresAt.Before(tc.at))
				require.Equal(t, time.UTC, r.ExpiresAt.Location())
			}
			require.Nil(t, original.ExpiresAt, "builder must not mutate caller metadata")
			stored, err := store.encode(r)
			require.NoError(t, err)
			if tc.mode == "nil" {
				expected, err := json.Marshal([]any{"ghatd-owned-record-v1", "fixture", "identity", "scope", int64(1), int64(0), ""})
				require.NoError(t, err)
				require.Equal(t, expected, aad(stored))
				require.Nil(t, stored.ExpiresAt)
			}
			// A real BSON roundtrip has only millisecond precision. Sealing must use
			// exactly those stored values, otherwise payload authentication would fail.
			encoded, err := bson.Marshal(stored)
			require.NoError(t, err)
			var persisted storedRecord
			require.NoError(t, bson.Unmarshal(encoded, &persisted))
			out, err := store.decode(persisted)
			require.NoError(t, err)
			require.Equal(t, r, out)
		})
	}
}

func TestRecordExpirationAuthenticatedMetadata(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, mode string
		initial    bool
		want       error
	}{
		{"unchanged_expiration", "", true, nil},
		{"changed_expiration_invalidates_payload", "changed", true, encryption.ErrInvalidPayload},
		{"removed_expiration_invalidates_payload", "removed", true, encryption.ErrInvalidPayload},
		{"added_expiration_invalidates_legacy_payload", "added", false, encryption.ErrInvalidPayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			store := &MongoStore{cipher: cipher}
			r, err := NewRecord("fixture", "id", "scope", 1, struct{}{})
			require.NoError(t, err)
			if tc.initial {
				r, err = r.WithExpiration(base)
				require.NoError(t, err)
			}
			persisted, err := store.encode(r)
			require.NoError(t, err)
			switch tc.mode {
			case "changed", "added":
				changed := base.Add(time.Millisecond)
				persisted.ExpiresAt = &changed
			case "removed":
				persisted.ExpiresAt = nil
			}
			out, err := store.decode(persisted)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.Equal(t, r, out)
			}
		})
	}
}
