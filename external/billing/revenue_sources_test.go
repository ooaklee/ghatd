package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

// Audit disposition: new reconciliation cases extend the revenue owning-service
// suite. Storage/concurrency proof is separately exercised against real Mongo.
func TestRevenueQuarantineResolution(t *testing.T) {
	type testCase struct {
		name         string
		mutate       func(*ResolveRevenueRequest)
		noFacts      bool
		uncertain    bool
		writeFailure int
		want         error
	}
	cases := []testCase{
		{name: "recovered_facts_and_resolution_are_one_acceptance"},
		{name: "stable_recovery_identity_is_retained", mutate: func(r *ResolveRevenueRequest) { r.RecoveryFingerprint = "versioned-source-identity" }},
		{name: "malformed_recovery_identity_is_invalid", mutate: func(r *ResolveRevenueRequest) { r.RecoveryFingerprint = " untrimmed" }, want: ErrRevenueInvalid},
		{name: "oversized_recovery_identity_is_invalid", mutate: func(r *ResolveRevenueRequest) { r.RecoveryFingerprint = strings.Repeat("x", 257) }, want: ErrRevenueInvalid},
		{name: "reasoned_no_revenue_is_a_durable_resolution", noFacts: true},
		{name: "expected_source_fingerprint_is_mandatory", mutate: func(r *ResolveRevenueRequest) { r.ExpectedFingerprint = "" }, want: ErrRevenueInvalid},
		{name: "changed_source_fingerprint_conflicts", mutate: func(r *ResolveRevenueRequest) { r.ExpectedFingerprint = "changed" }, want: ErrRevenueConflict},
		{name: "missing_verified_actor_is_invalid", mutate: func(r *ResolveRevenueRequest) { r.ActorID = "" }, want: ErrRevenueInvalid},
		{name: "cross_account_facts_are_invalid", mutate: func(r *ResolveRevenueRequest) { r.Facts[0].Scope.AccountID = "other" }, want: ErrRevenueInvalid},
		{name: "resolution_write_failure_rolls_back_recovered_facts", writeFailure: 2, want: ErrRevenueUnavailable},
		{name: "uncertain_commit_reconciles_exact_receipt", uncertain: true, want: ErrRevenueUncertain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRevenueTestRepo()
			svc, err := NewRevenueService(repo, revenueTestClock{time.Now().UTC()})
			require.NoError(t, err)
			_, _, fixtureRequest := revenueFixture(t)
			fact := fixtureRequest.Facts[0]
			original, err := svc.AcceptVerified(context.Background(), VerifiedRevenueRequest{Scope: fact.Scope, EnvelopeID: "quarantined-delivery", QuarantineReason: "historical_payer_plan_pending"})
			require.NoError(t, err)
			req := ResolveRevenueRequest{ObservationID: original.ID, ExpectedFingerprint: original.Fingerprint, Facts: []RevenueFact{fact}, Reason: "authenticated_source_recovered", ActorID: "authorized-worker"}
			if tc.noFacts {
				req.Facts = nil
				req.Reason = "no_subscription_revenue"
			}
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			repo.failAt = tc.writeFailure
			repo.uncertain = tc.uncertain
			resolution, err := svc.ResolveQuarantinedRevenue(context.Background(), req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, resolution.ID)
				if !tc.uncertain {
					require.Len(t, repo.observations, 1)
					require.Empty(t, repo.facts)
					return
				}
			}
			repo.failAt = 0
			repo.uncertain = false
			replay, err := svc.ResolveQuarantinedRevenue(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, original.ID, replay.ResolutionOf)
			require.Equal(t, req.ActorID, replay.ResolutionBy)
			require.Equal(t, req.Reason, replay.ResolutionReason)
			require.Equal(t, req.RecoveryFingerprint, replay.RecoveryFingerprint)
			if req.RecoveryFingerprint == "" {
				// Existing empty-field receipts must retain their pre-extension hash.
				canonical := make([]RevenueFact, len(req.Facts))
				for i, f := range req.Facts {
					canonical[i], err = canonicalRevenueFact(f)
					require.NoError(t, err)
				}
				encoded, err := json.Marshal(struct {
					Original, Fingerprint string
					Facts                 []RevenueFact
					Reason, Actor         string
				}{original.ID, original.Fingerprint, canonical, req.Reason, req.ActorID})
				require.NoError(t, err)
				hash := sha256.Sum256(encoded)
				require.Equal(t, hex.EncodeToString(hash[:]), replay.Fingerprint)
			}
			require.Len(t, repo.observations, 2)
			if tc.noFacts {
				require.Empty(t, repo.facts)
			} else {
				require.Len(t, repo.facts, 1)
			}
			storedOriginal := repo.observations[original.ID]
			require.Equal(t, original, storedOriginal)
			changedRecovery := req
			changedRecovery.RecoveryFingerprint = "different-recovery-identity"
			_, err = svc.ResolveQuarantinedRevenue(context.Background(), changedRecovery)
			require.ErrorIs(t, err, ErrRevenueConflict)
			req.Reason = "different_resolution"
			_, err = svc.ResolveQuarantinedRevenue(context.Background(), req)
			require.ErrorIs(t, err, ErrRevenueConflict)
		})
	}
}

func TestRevenueStrictAbsenceAndConstruction(t *testing.T) {
	type testCase struct {
		name  string
		repo  RevenueRepository
		clock RevenueClock
		want  error
	}
	var nilRepo *revenueTestRepo
	cases := []testCase{{name: "typed_nil_repository", repo: nilRepo, clock: revenueTestClock{}, want: ErrRevenueUnavailable}, {name: "missing_clock", repo: newRevenueTestRepo(), want: ErrRevenueUnavailable}, {name: "complete_dependencies", repo: newRevenueTestRepo(), clock: revenueTestClock{}}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRevenueService(tc.repo, tc.clock)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}
	causes := []struct {
		name   string
		err    error
		absent bool
	}{{"single absence", ErrRevenueNotFound, true}, {"wrapped absence", errors.New("different"), false}, {"joined absence and outage", errors.Join(ErrRevenueNotFound, ErrRevenueUnavailable), false}}
	for _, tc := range causes {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.absent, singleRevenueCause(tc.err, ErrRevenueNotFound)) })
	}
}
