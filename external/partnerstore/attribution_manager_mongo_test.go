package partnerstore

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

type correctionReplyStore struct {
	recordstore.Store
	lost atomic.Bool
}
type correctionReplyTx struct {
	recordstore.Tx
	wrote *bool
}

func (t correctionReplyTx) Insert(ctx context.Context, row recordstore.Record) error {
	if err := t.Tx.Insert(ctx, row); err != nil {
		return err
	}
	if row.Kind == kindReferralRevision {
		*t.wrote = true
	}
	return nil
}
func (s *correctionReplyStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	wrote := false
	err := s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { wrote = false; return fn(correctionReplyTx{Tx: tx, wrote: &wrote}) })
	if err == nil && wrote && s.lost.CompareAndSwap(false, true) {
		return recordstore.ErrUncertain
	}
	return err
}
func correctionAccount(t *testing.T, f *workerFixture, email string) string {
	t.Helper()
	created, err := f.users.CreateUser(f.ctx, &user.CreateUserRequest{Email: email, FirstName: "Fixture", LastName: "Member", GenerateUUID: true})
	require.NoError(t, err)
	_, err = f.users.VerifyUserEmail(f.ctx, &user.VerifyUserEmailRequest{ID: created.User.ID})
	require.NoError(t, err)
	_, err = f.users.UpdateUserStatus(f.ctx, &user.UpdateUserStatusRequest{ID: created.User.ID, DesiredStatus: "ACTIVE"})
	require.NoError(t, err)
	principal, err := f.identity.GetPartnerPrincipal(f.ctx, created.User.ID)
	require.NoError(t, err)
	require.True(t, principal.Active, "owning current principal: %+v", principal)
	require.True(t, principal.EmailVerified, "owning current principal: %+v", principal)
	fact, err := f.identity.GetSignupFact(f.ctx, created.User.ID)
	require.NoError(t, err)
	require.True(t, fact.NewAccount && fact.Individual)
	return created.User.ID
}
func enrollCorrectionPartner(t *testing.T, f *workerFixture, email string) partnerprogram.Partner {
	t.Helper()
	id := correctionAccount(t, f, email)
	p, err := f.program.Enroll(f.ctx, partnerprogram.EnrollRequest{CustomerID: id, AcceptedTermsVersion: "fixture-terms"})
	require.NoError(t, err)
	return p
}
func managerCorrectionRequest(change partnermanager.AttributionChange, p partnermanager.AttributionPreview, key string) partnermanager.ApplyAttributionRequest {
	return partnermanager.ApplyAttributionRequest{AttributionChange: change, ExpectedRevision: p.Original.Revision, ExpectedReferralID: p.Original.ID, SnapshotFingerprint: p.SnapshotFingerprint, PreviewFingerprint: p.Fingerprint, IdempotencyKey: key}
}

// Audit disposition: named full owning identity/program/referral transactions,
// using actual captures and encrypted Mongo receipts, not synthetic signup flags.
func TestMongoManagerAttributionPreviewApplyAndRecovery(t *testing.T) {
	cases := []struct {
		name                                         string
		lost, changedPolicy, changedBinding, revoked bool
		want                                         error
	}{
		{name: "reviewed_prospective_change_keeps_future_binding"},
		{name: "committed_lost_response_recovers_after_pause_and_new_policy", lost: true},
		{name: "current_policy_change_requires_fresh_review", changedPolicy: true, want: referral.ErrStaleWrite},
		{name: "new_payment_binding_requires_fresh_review", changedBinding: true, want: referral.ErrStaleWrite},
		{name: "revoked_current_operator_cannot_apply", revoked: true, want: partnermanager.ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			ctx := f.ctx
			originalPartner := enrollCorrectionPartner(t, f, "original-owner@example.test")
			newPartner := enrollCorrectionPartner(t, f, "new-owner@example.test")
			customer := correctionAccount(t, f, "referred@example.test")
			m := f.manager(t, false)
			firstChange := partnermanager.AttributionChange{ActorID: "fixture-operator", ReferredCustomer: customer, PartnerID: originalPartner.ID, SignupID: customer, Reason: "reviewed owning new-account capture; prospective assignment", Mode: referral.CorrectionProspective}
			firstPreview, err := m.PreviewAttribution(ctx, firstChange)
			require.NoError(t, err)
			first, err := m.ApplyAttribution(ctx, managerCorrectionRequest(firstChange, firstPreview, "first"))
			require.NoError(t, err)
			future, err := f.referrals.BindPayment(ctx, customer, "already-bound-future", f.clock.Now().Add(5*time.Hour))
			require.NoError(t, err)
			f.clock.now = f.clock.now.Add(time.Hour)
			change := firstChange
			change.PartnerID = newPartner.ID
			change.Reason = "reviewed prospective owner correction"
			preview, err := m.PreviewAttribution(ctx, change)
			require.NoError(t, err)
			require.Equal(t, first, preview.Original)
			require.Equal(t, []referral.PaymentAttribution{future}, preview.UnchangedBindings)
			require.Zero(t, preview.HistoricalCommissionDeltaMinor)
			require.Zero(t, preview.HistoricalPayoutDeltaMinor)
			req := managerCorrectionRequest(change, preview, "correct")
			if tc.changedPolicy {
				_, err = f.program.PublishPolicy(ctx, partnerprogram.PublishPolicyRequest{ActorID: "fixture-operator", ExpectedRevision: 1, Draft: partnerprogram.PolicyDraft{Scope: "global", RateBasisPoints: 3000, HoldDays: 7, Currency: "EUR", EligiblePlanIDs: []string{"fixture-plan"}, TermsVersion: "fixture-terms", EffectiveFrom: f.clock.Now()}})
				require.NoError(t, err)
			}
			if tc.changedBinding {
				_, err = f.referrals.BindPayment(ctx, customer, "new-binding", f.clock.Now())
				require.NoError(t, err)
			}
			if tc.revoked {
				f.authority.revoked.Store(true)
			}
			if tc.lost {
				repo, err := NewReferralRepository(&correctionReplyStore{Store: f.store})
				require.NoError(t, err)
				f.referrals, err = referral.NewService(repo, f.clock, randomIDs{}, 30*24*time.Hour)
				if err == nil {
					f.referrals, err = f.referrals.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
				}
				require.NoError(t, err)
				m = f.manager(t, false)
			}
			out, err := m.ApplyAttribution(ctx, req)
			if tc.lost {
				require.ErrorIs(t, err, referral.ErrUncertain)
				require.Empty(t, out)
				var healthy *ReferralRepository
				healthy, err = NewReferralRepository(f.store)
				require.NoError(t, err)
				f.referrals, err = referral.NewService(healthy, f.clock, randomIDs{}, 30*24*time.Hour)
				if err == nil {
					f.referrals, err = f.referrals.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
				}
				require.NoError(t, err)
				_, err = f.program.PublishPolicy(ctx, partnerprogram.PublishPolicyRequest{ActorID: "fixture-operator", ExpectedRevision: 1, Draft: partnerprogram.PolicyDraft{Scope: "global", RateBasisPoints: 3000, HoldDays: 7, Currency: "EUR", EligiblePlanIDs: []string{"fixture-plan"}, TermsVersion: "fixture-terms", EffectiveFrom: f.clock.Now()}})
				require.NoError(t, err)
				// A later correction must not hide this original immutable receipt.
				later := change
				later.PartnerID = originalPartner.ID
				later.Reason = "later reviewed prospective correction"
				var lp partnermanager.AttributionPreview
				lp, err = f.manager(t, false).PreviewAttribution(ctx, later)
				require.NoError(t, err)
				_, err = f.manager(t, false).ApplyAttribution(ctx, managerCorrectionRequest(later, lp, "later"))
				require.NoError(t, err)
				out, err = f.manager(t, true).ApplyAttribution(ctx, req)
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				head, err := f.referrals.GetReferralForCustomer(ctx, customer)
				require.NoError(t, err)
				require.Equal(t, first, head)
			} else {
				require.Equal(t, newPartner.ID, out.PartnerID)
				require.Equal(t, first.ID, out.CorrectionOf)
				require.NotNil(t, out.Correction)
				recovered, err := f.manager(t, true).ApplyAttribution(ctx, req)
				require.NoError(t, err)
				require.Equal(t, out, recovered)
				f.authority.revoked.Store(true)
				_, err = f.manager(t, true).ApplyAttribution(ctx, req)
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			binding, err := f.referrals.BindPayment(ctx, customer, future.PaymentID, future.EffectiveAt)
			require.NoError(t, err)
			require.Equal(t, future, binding)
		})
	}
}
