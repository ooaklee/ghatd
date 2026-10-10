package revenuestore

import (
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Audit disposition: additive encrypted codec compatibility and rejection of
// malformed private recovery identity, independent of provider/hash algorithms.
func TestRevenueObservationRecoveryCodec(t *testing.T) {
	cases := []struct {
		name, recovery, original string
		want                     error
	}{
		{name: "legacy_quarantine_without_optional_field"},
		{name: "legacy_resolution_without_optional_field", original: "source"},
		{name: "versioned_resolution_roundtrip", original: "source", recovery: "provider-v2:stable"},
		{name: "generic_provider_identity_is_not_stripe_specific", original: "source", recovery: "another-provider-identity"},
		{name: "recovery_identity_requires_resolution", recovery: "provider-v2:stable", want: billing.ErrRevenueUnavailable},
		{name: "recovery_identity_must_be_trimmed", original: "source", recovery: " provider-v2:stable", want: billing.ErrRevenueUnavailable},
		{name: "recovery_identity_is_bounded", original: "source", recovery: strings.Repeat("x", 257), want: billing.ErrRevenueUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := billing.RevenueObservation{ID: "observation", Fingerprint: "accepted-fingerprint", AcceptedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), ResolutionOf: tc.original}
			row, err := recordstore.NewRecord(kindObservation, v.ID, partition, 1, persistedObservation{Observation: v, RecoveryFingerprint: tc.recovery, SourceFingerprint: "original-private-source"})
			require.NoError(t, err)
			decoded, err := decodeObservation(row)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, decoded.ID)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.recovery, decoded.RecoveryFingerprint)
			require.Equal(t, "original-private-source", decoded.SourceFingerprint)
		})
	}
}
