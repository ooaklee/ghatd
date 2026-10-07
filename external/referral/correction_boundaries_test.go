package referral

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named receipt, impact and dependency boundaries with a
// fresh owning transaction fixture per case; no historical money is moved.
func TestCorrectionReceiptRecoveryAndPreconditions(t *testing.T) {
	cases := []struct {
		name, change string
		want         error
	}{
		{name: "same_request_replays_after_later_head_and_acquisition_pause"},
		{name: "changed_reason_conflicts", change: "reason", want: ErrStaleWrite},
		{name: "changed_terms_conflict", change: "terms", want: ErrStaleWrite},
		{name: "changed_preview_conflicts", change: "preview", want: ErrStaleWrite},
		{name: "changed_signup_source_conflicts", change: "signup", want: ErrStaleWrite},
		{name: "another_actor_cannot_replay", change: "actor", want: ErrStaleWrite},
		{name: "another_key_cannot_replay", change: "key", want: ErrStaleWrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, p, l, e := fixture(t)
			ctx := context.Background()
			first, err := s.LockAttribution(ctx, p, l, e)
			require.NoError(t, err)
			s.clock = fakeClock{e.At.Add(time.Hour)}
			req := reviewedCorrection(t, s, CorrectionRequest{Partner: PartnerState{"new", "new-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: "operator", Reason: "approved correction", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: fixtureTerms()})
			accepted, err := s.AssignAttribution(ctx, req)
			require.NoError(t, err)
			s.clock = fakeClock{e.At.Add(2 * time.Hour)}
			later := reviewedCorrection(t, s, CorrectionRequest{Partner: PartnerState{"later", "later-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: "operator", Reason: "later correction", ExpectedRevision: accepted.Revision, ExpectedReferralID: accepted.ID, Terms: fixtureTerms()})
			_, err = s.AssignAttribution(ctx, later)
			require.NoError(t, err)
			s.clock = fakeClock{} // Recovery does not need a fresh cutover clock.
			if tc.change == "" {
				req.Partner.CanAcquireReferrals = false
			}
			switch tc.change {
			case "reason":
				req.Reason = "different reason"
			case "terms":
				req.Terms.RateBasisPoints++
			case "preview":
				req.PreviewFingerprint = later.PreviewFingerprint
			case "signup":
				req.SignupID = "different-source"
			case "actor":
				req.ActorID = "another-operator"
			case "key":
				req.IdempotencyKey = "another-key"
			}
			out, err := s.AssignAttribution(ctx, req)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, accepted, out)
			} else {
				require.Empty(t, out)
			}
			require.Len(t, repo.history[ProgramID+":"+e.ReferredCustomer], 3)
			if tc.change == "actor" || tc.change == "key" {
				_, err = s.FindCorrection(ctx, req.ActorID, req.ReferredCustomer, req.IdempotencyKey)
				require.ErrorIs(t, err, ErrNotFound)
			}
			wire, err := json.Marshal(accepted)
			require.NoError(t, err)
			require.NotContains(t, string(wire), accepted.Correction.RequestFingerprint)
		})
	}
}

func TestCorrectionSnapshotDetectsChangedImpact(t *testing.T) {
	cases := []struct {
		name, change string
		want         error
	}{
		{name: "reviewed_existing_future_binding_remains_immutable"},
		{name: "new_binding_after_review_requires_new_preview", change: "binding", want: ErrStaleWrite},
		{name: "missing_snapshot_is_denied", change: "fingerprint", want: ErrInvalid},
		{name: "unsupported_historical_mode_is_denied", change: "mode", want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, p, l, e := fixture(t)
			ctx := context.Background()
			first, err := s.LockAttribution(ctx, p, l, e)
			require.NoError(t, err)
			s.clock = fakeClock{e.At.Add(time.Hour)}
			future, err := s.BindPayment(ctx, e.ReferredCustomer, "already-bound-future", e.At.Add(5*time.Hour))
			require.NoError(t, err)
			req := reviewedCorrection(t, s, CorrectionRequest{Partner: PartnerState{"new", "new-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: "operator", Reason: "reviewed correction", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: fixtureTerms()})
			switch tc.change {
			case "binding":
				_, err = s.BindPayment(ctx, e.ReferredCustomer, "later-binding", e.At.Add(2*time.Hour))
				require.NoError(t, err)
			case "fingerprint":
				req.ExpectedSnapshotFingerprint = ""
			case "mode":
				req.Mode = "compensated"
			}
			out, err := s.AssignAttribution(ctx, req)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				require.Len(t, repo.history[ProgramID+":"+e.ReferredCustomer], 1)
			}
			got, err := s.BindPayment(ctx, e.ReferredCustomer, future.PaymentID, future.EffectiveAt)
			require.NoError(t, err)
			require.Equal(t, future, got)
			if tc.want == nil {
				state, err := s.GetAttributionSnapshot(ctx, e.ReferredCustomer)
				require.NoError(t, err)
				require.Equal(t, []PaymentAttribution{future}, state.Bindings)
				require.Equal(t, "partner", future.PartnerID)
			}
		})
	}
}

type correctionFaultRepo struct {
	Repository
	historyErr   error
	wrongBinding bool
}

func (r *correctionFaultRepo) WithAttributionTransaction(ctx context.Context, p, c string, fn func(Repository) error) error {
	return r.Repository.WithAttributionTransaction(ctx, p, c, func(tx Repository) error {
		return fn(&correctionFaultRepo{Repository: tx, historyErr: r.historyErr, wrongBinding: r.wrongBinding})
	})
}
func (r *correctionFaultRepo) ListReferralHistory(ctx context.Context, p, c string) ([]Referral, error) {
	if r.historyErr != nil {
		return nil, r.historyErr
	}
	return r.Repository.ListReferralHistory(ctx, p, c)
}
func (r *correctionFaultRepo) ListPaymentAttributionsByCustomer(ctx context.Context, p, c string) ([]PaymentAttribution, error) {
	rows, err := r.Repository.ListPaymentAttributionsByCustomer(ctx, p, c)
	if r.wrongBinding {
		rows = append(rows, PaymentAttribution{ProgramID: p, ReferredCustomer: "another-customer"})
	}
	return rows, err
}
func TestCorrectionReadFailuresNeverAdmitOrExposePartialResult(t *testing.T) {
	cases := []struct {
		name                                        string
		joined, wrongBinding, nilContext, cancelled bool
		want                                        error
	}{
		{name: "joined_receipt_absence_and_outage", joined: true, want: ErrUnavailable},
		{name: "cross_customer_binding_snapshot", wrongBinding: true, want: ErrUnavailable},
		{name: "nil_context", nilContext: true, want: ErrInvalid},
		{name: "cancelled_context", cancelled: true, want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, p, l, e := fixture(t)
			ctx := context.Background()
			first, err := s.LockAttribution(ctx, p, l, e)
			require.NoError(t, err)
			s.clock = fakeClock{e.At.Add(time.Hour)}
			req := reviewedCorrection(t, s, CorrectionRequest{Partner: PartnerState{"new", "new-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: "operator", Reason: "reviewed correction", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: fixtureTerms()})
			fault := &correctionFaultRepo{Repository: repo, wrongBinding: tc.wrongBinding}
			if tc.joined {
				fault.historyErr = errors.Join(ErrNotFound, ErrUnavailable)
			}
			s.repo = fault
			if tc.nilContext {
				ctx = nil
			}
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := s.AssignAttribution(ctx, req)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
			require.Len(t, repo.history[ProgramID+":"+e.ReferredCustomer], 1)
			state, err := s.GetAttributionSnapshot(ctx, e.ReferredCustomer)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, state)
		})
	}
}
