package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named current authorization, owning evidence, stale
// preview and original-receipt boundaries. Actual persistence has Mongo tests.
type attributionIdentity struct {
	Identity
	principals map[string]Principal
	fact       SignupFact
	fail       error
}

func (i *attributionIdentity) GetPartnerPrincipal(_ context.Context, id string) (Principal, error) {
	return i.principals[id], i.fail
}
func (i *attributionIdentity) GetSignupFact(context.Context, string) (SignupFact, error) {
	return i.fact, i.fail
}

type attributionProgram struct {
	*programStub
	signupAt time.Time
}

func (p *attributionProgram) ResolveReferralTerms(ctx context.Context, v partnerprogram.Partner, g []string, at, signupAt time.Time) (partnerprogram.EffectiveTerms, error) {
	p.signupAt = signupAt
	out, err := p.ResolveTerms(ctx, v, g, at)
	out.ProgramID = partnerprogram.ProgramID
	out.PartnerID = v.ID
	return out, err
}

type attributionAuthority struct {
	deny    bool
	calls   int
	targets []string
}

func (a *attributionAuthority) CheckPartners(_ context.Context, actor, cap, target string) error {
	a.calls++
	a.targets = append(a.targets, target)
	if a.deny || actor != "operator" || cap != CapabilityAttribution || target != "referred" {
		return ErrDenied
	}
	return nil
}

type attributionReferral struct {
	ReferralService
	state             referral.AttributionSnapshot
	receipt           referral.Referral
	key               string
	assignCalls       int
	last              referral.CorrectionRequest
	readErr, applyErr error
	revoke            *attributionAuthority
}

func (r *attributionReferral) GetAttributionSnapshot(context.Context, string) (referral.AttributionSnapshot, error) {
	if r.revoke != nil {
		r.revoke.deny = true
	}
	return r.state, r.readErr
}
func (r *attributionReferral) FindCorrection(_ context.Context, actor, customer, key string) (referral.Referral, error) {
	if r.readErr != nil {
		return referral.Referral{}, r.readErr
	}
	if r.receipt.ID != "" && actor == r.receipt.CorrectionBy && customer == r.receipt.ReferredCustomer && key == r.key {
		if r.revoke != nil {
			r.revoke.deny = true
		}
		return r.receipt, nil
	}
	return referral.Referral{}, referral.ErrNotFound
}
func (r *attributionReferral) AssignAttribution(_ context.Context, req referral.CorrectionRequest) (referral.Referral, error) {
	r.assignCalls++
	r.last = req
	r.key = req.IdempotencyKey
	r.receipt = referral.Referral{ID: "receipt", Revision: req.ExpectedRevision + 1, ProgramID: referral.ProgramID, PartnerID: req.Partner.PartnerID, ReferredCustomer: req.ReferredCustomer, SignupID: req.SignupID, CorrectionBy: req.ActorID, CorrectionReason: req.Reason, TermsSnapshot: req.Terms, Correction: &referral.CorrectionReceipt{PreviewFingerprint: req.PreviewFingerprint, SnapshotFingerprint: req.ExpectedSnapshotFingerprint, ExpectedRevision: req.ExpectedRevision, ExpectedReferralID: req.ExpectedReferralID, SignupID: req.SignupID, Mode: req.Mode}}
	return r.receipt, r.applyErr
}
func attributionFixture(t *testing.T) (*Manager, *attributionIdentity, *attributionProgram, *attributionReferral, *attributionAuthority, AttributionChange) {
	t.Helper()
	m, p, _, _, _, _, _ := managerFixture(t)
	i := &attributionIdentity{principals: map[string]Principal{"referred": {ID: "referred", Active: true, Individual: true}, "owner": {ID: "owner", Active: true, Individual: true, EmailVerified: true, RegionEligible: true}}, fact: SignupFact{ID: "signup", CustomerID: "referred", CreatedAt: m.deps.Clock.Now().Add(-time.Hour), NewAccount: true, Individual: true, AttributionEvidence: "private-owning-evidence"}}
	ap := &attributionProgram{programStub: p}
	p.cfg.CurrencyExponent = 2
	oldEnd := m.deps.Clock.Now().AddDate(0, 1, 0)
	original := referral.Referral{ID: "original", Revision: 1, ProgramID: referral.ProgramID, PartnerID: "prior-partner", ReferredCustomer: "referred", SignupID: "signup", TermsSnapshot: referral.TermsSnapshot{RecurrenceEndsAt: &oldEnd}}
	r := &attributionReferral{state: referral.AttributionSnapshot{ReferredCustomer: "referred", Head: original, History: []referral.Referral{original}, Fingerprint: "owning-snapshot"}}
	a := &attributionAuthority{}
	m.deps.Identity = i
	m.deps.Program = ap
	m.deps.Referral = r
	m.deps.Authority = a
	return m, i, ap, r, a, AttributionChange{ActorID: "operator", ReferredCustomer: "referred", PartnerID: p.partner.ID, SignupID: "signup", Reason: "owning evidence reviewed", Mode: referral.CorrectionProspective}
}
func applyPreview(r AttributionChange, p AttributionPreview) ApplyAttributionRequest {
	return ApplyAttributionRequest{AttributionChange: r, ExpectedRevision: p.Original.Revision, ExpectedReferralID: p.Original.ID, SnapshotFingerprint: p.SnapshotFingerprint, PreviewFingerprint: p.Fingerprint, IdempotencyKey: "correction-key"}
}

func TestAttributionPreviewOwnsEligibilityAndCurrentScope(t *testing.T) {
	cases := []struct {
		name, change string
		want         error
	}{
		{name: "eligible_reviewed_prospective_change"},
		{name: "reporting_actor_cannot_change_ownership", change: "actor", want: ErrDenied},
		{name: "target_scoped_permission_cannot_be_reused", change: "target", want: ErrDenied},
		{name: "self_referral", change: "self", want: referral.ErrSelfReferral},
		{name: "seat_creation_fact", change: "seat_fact", want: ErrDenied},
		{name: "legacy_account_is_not_new_creation_evidence", change: "legacy", want: ErrDenied},
		{name: "source_customer_mismatch", change: "source_owner", want: ErrDenied},
		{name: "wrong_current_target_identity", change: "principal", want: ErrDenied},
		{name: "unverified_proposed_partner_owner", change: "verification", want: ErrDenied},
		{name: "disabled_partner_acquisition", change: "acquire", want: ErrDenied},
		{name: "owning_terms_wrong_currency", change: "currency", want: ErrUnavailable},
		{name: "owning_snapshot_contains_another_customer", change: "snapshot_owner", want: ErrUnavailable},
		{name: "owning_binding_contains_another_customer", change: "binding_owner", want: ErrUnavailable},
		{name: "joined_source_absence_outage", change: "outage", want: ErrUnavailable},
		{name: "unsupported_historical_mode", change: "mode", want: ErrInvalid},
		{name: "revoked_during_owning_read_returns_no_private_preview", change: "revoked", want: ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, i, p, r, a, req := attributionFixture(t)
			switch tc.change {
			case "actor":
				req.ActorID = "reporting-operator"
			case "target":
				req.ReferredCustomer = "other-customer"
			case "self":
				p.partner.CustomerID = "referred"
			case "seat_fact":
				i.fact.Individual = false
			case "legacy":
				i.fact.NewAccount = false
			case "source_owner":
				i.fact.CustomerID = "other-customer"
			case "principal":
				v := i.principals["referred"]
				v.ID = "other"
				i.principals["referred"] = v
			case "verification":
				v := i.principals["owner"]
				v.EmailVerified = false
				i.principals["owner"] = v
			case "acquire":
				p.partner.CanAcquireReferrals = false
			case "currency":
				p.terms.Currency = "USD"
			case "snapshot_owner":
				r.state.History[0].ReferredCustomer = "other-customer"
			case "binding_owner":
				r.state.Bindings = append(r.state.Bindings, referral.PaymentAttribution{ProgramID: referral.ProgramID, ReferredCustomer: "other-customer"})
			case "outage":
				i.fail = errors.Join(ErrNotFound, ErrUnavailable)
			case "mode":
				req.Mode = "compensated"
			case "revoked":
				r.revoke = a
			}
			out, err := m.PreviewAttribution(context.Background(), req)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.Equal(t, req.ReferredCustomer, out.ReferredCustomer)
				require.Equal(t, referral.CorrectionProspective, out.Mode)
				require.Zero(t, out.HistoricalCommissionDeltaMinor)
				require.Zero(t, out.HistoricalPayoutDeltaMinor)
				require.Equal(t, i.fact.CreatedAt, p.signupAt)
				require.Equal(t, r.state.History[0].TermsSnapshot.RecurrenceEndsAt, out.ProposedTerms.RecurrenceEndsAt)
				wire, err := json.Marshal(out)
				require.NoError(t, err)
				require.NotContains(t, string(wire), i.fact.AttributionEvidence)
			}
			require.Zero(t, r.assignCalls)
			for _, target := range a.targets {
				require.Equal(t, req.ReferredCustomer, target)
			}
		})
	}
}

func TestAttributionApplyRecomputesReviewedSources(t *testing.T) {
	cases := []struct {
		name, change string
		want         error
	}{
		{name: "unchanged_review_applies_once"},
		{name: "clock_progress_without_new_policy_is_stable", change: "clock"},
		{name: "new_terms_require_fresh_review", change: "terms", want: referral.ErrStaleWrite},
		{name: "new_owning_source_requires_fresh_review", change: "source", want: referral.ErrStaleWrite},
		{name: "binding_snapshot_changed_requires_review", change: "snapshot", want: referral.ErrStaleWrite},
		{name: "expected_head_changed_requires_review", change: "head", want: referral.ErrStaleWrite},
		{name: "browser_changes_proposed_owner", change: "partner", want: ErrUnavailable},
		{name: "revoked_during_recheck_prevents_append", change: "revoked", want: ErrDenied},
		{name: "joined_receipt_absence_outage_cannot_admit", change: "outage", want: ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, i, p, r, a, change := attributionFixture(t)
			ctx := context.Background()
			preview, err := m.PreviewAttribution(ctx, change)
			require.NoError(t, err)
			req := applyPreview(change, preview)
			switch tc.change {
			case "clock":
				m.deps.Clock = managerClock{m.deps.Clock.Now().Add(time.Hour)}
			case "terms":
				p.terms.RateBasisPoints++
			case "source":
				i.fact.AttributionEvidence = "changed-owning-evidence"
			case "snapshot":
				r.state.Fingerprint = "different-snapshot"
			case "head":
				req.ExpectedRevision++
			case "partner":
				req.PartnerID = "unrelated-partner"
			case "revoked":
				r.revoke = a
			case "outage":
				r.readErr = errors.Join(referral.ErrNotFound, ErrUnavailable)
			}
			out, err := m.ApplyAttribution(ctx, req)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				require.Zero(t, r.assignCalls)
			} else {
				require.Equal(t, 1, r.assignCalls)
				require.Equal(t, change.ActorID, r.last.ActorID)
				require.Equal(t, preview.Fingerprint, r.last.PreviewFingerprint)
				require.Equal(t, i.fact.CreatedAt, r.last.SignupCreatedAt)
			}
		})
	}
}

func TestAttributionReceiptSurvivesLostReplyAndPolicyPause(t *testing.T) {
	cases := []struct {
		name, change string
		want         error
	}{
		{name: "original_receipt_recovers_without_current_policy_or_source"},
		{name: "changed_original_reason_conflicts", change: "reason", want: referral.ErrStaleWrite},
		{name: "changed_review_fingerprint_conflicts", change: "preview", want: referral.ErrStaleWrite},
		{name: "currently_revoked_actor_cannot_recover", change: "revoked", want: ErrDenied},
		{name: "revoked_during_receipt_read_returns_no_private_receipt", change: "revoked_read", want: ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, i, p, r, a, change := attributionFixture(t)
			ctx := context.Background()
			preview, err := m.PreviewAttribution(ctx, change)
			require.NoError(t, err)
			req := applyPreview(change, preview)
			r.applyErr = referral.ErrUncertain
			out, err := m.ApplyAttribution(ctx, req)
			require.ErrorIs(t, err, referral.ErrUncertain)
			require.Empty(t, out)
			accepted := r.receipt
			m.deps.Controls.Attribution = false
			i.fail = ErrUnavailable
			p.lookupErr = ErrUnavailable
			p.terms.RateBasisPoints++
			switch tc.change {
			case "reason":
				req.Reason = "changed reason"
			case "preview":
				req.PreviewFingerprint = "changed preview"
			case "revoked":
				a.deny = true
			case "revoked_read":
				r.revoke = a
			}
			out, err = m.ApplyAttribution(ctx, req)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, accepted, out)
			} else {
				require.Empty(t, out)
			}
			require.Equal(t, 1, r.assignCalls)
		})
	}
}

func TestAttributionPreviewNeverExtendsFrozenRecurrence(t *testing.T) {
	cases := []struct {
		name                       string
		oldDays, newDays           int
		unlimitedOld, unlimitedNew bool
		wantDays                   int
	}{
		{name: "finite_old_to_unlimited_retains_old_end", oldDays: 30, unlimitedNew: true, wantDays: 30},
		{name: "later_finite_policy_cannot_extend_prior_end", oldDays: 30, newDays: 60, wantDays: 30},
		{name: "earlier_new_policy_end_is_kept", oldDays: 30, newDays: 15, wantDays: 15},
		{name: "unlimited_old_can_admit_current_finite_end", unlimitedOld: true, newDays: 15, wantDays: 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, p, r, _, req := attributionFixture(t)
			at := m.deps.Clock.Now()
			if tc.unlimitedOld {
				r.state.History[0].TermsSnapshot.RecurrenceEndsAt = nil
			} else {
				v := at.Add(time.Duration(tc.oldDays) * 24 * time.Hour)
				r.state.History[0].TermsSnapshot.RecurrenceEndsAt = &v
			}
			if tc.unlimitedNew {
				p.terms.RecurrenceEndsAt = nil
			} else {
				v := at.Add(time.Duration(tc.newDays) * 24 * time.Hour)
				p.terms.RecurrenceEndsAt = &v
			}
			out, err := m.PreviewAttribution(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, at.Add(time.Duration(tc.wantDays)*24*time.Hour), *out.ProposedTerms.RecurrenceEndsAt)
		})
	}
}

func TestAttributionActorCannotBeDecodedFromTransport(t *testing.T) {
	cases := []struct{ name, payload string }{
		{name: "unexported_transport_actor", payload: `{"ActorID":"forged"}`},
		{name: "embedded_change_actor", payload: `{"actor_id":"forged","ReferredCustomer":"referred"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req ApplyAttributionRequest
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &req))
			require.Empty(t, req.ActorID)
		})
	}
}
