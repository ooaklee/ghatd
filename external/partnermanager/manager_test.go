package partnermanager

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type managerClock struct{ at time.Time }

func (c managerClock) Now() time.Time { return c.at }

type authorityStub struct {
	deny  bool
	calls []string
}

func (a *authorityStub) CheckPartners(ctx context.Context, actor, cap, target string) error {
	a.calls = append(a.calls, cap)
	if a.deny {
		return ErrDenied
	}
	return nil
}

type identityStub struct {
	principal Principal
	signup    SignupFact
	fail      error
}

func (i *identityStub) GetPartnerPrincipal(context.Context, string) (Principal, error) {
	return i.principal, i.fail
}
func (i *identityStub) GetSignupFact(context.Context, string) (SignupFact, error) {
	return i.signup, i.fail
}

type groupsStub struct {
	ids  []string
	fail error
}

func (g *groupsStub) PartnerGroupIDs(context.Context, string) ([]string, error) { return g.ids, g.fail }

type programStub struct {
	ProgramService
	partner                   partnerprogram.Partner
	cfg                       partnerprogram.Config
	terms                     partnerprogram.EffectiveTerms
	lookupErr, destinationErr error
	enrollTerms               string
	resolvedGroups            []string
	resolvedAt                time.Time
}

func (p *programStub) Config() partnerprogram.Config { return p.cfg }
func (p *programStub) GetPartnerForCustomer(context.Context, string) (partnerprogram.Partner, error) {
	return p.partner, p.lookupErr
}
func (p *programStub) GetPartner(context.Context, string) (partnerprogram.Partner, error) {
	return p.partner, p.lookupErr
}
func (p *programStub) Enroll(ctx context.Context, r partnerprogram.EnrollRequest) (partnerprogram.Partner, error) {
	p.enrollTerms = r.AcceptedTermsVersion
	if r.AcceptedTermsVersion != p.cfg.TermsVersion {
		return partnerprogram.Partner{}, partnerprogram.ErrDenied
	}
	return p.partner, nil
}
func (p *programStub) ResolveTerms(ctx context.Context, partner partnerprogram.Partner, groups []string, at time.Time) (partnerprogram.EffectiveTerms, error) {
	p.resolvedGroups = groups
	p.resolvedAt = at
	return p.terms, nil
}
func (p *programStub) GetPayoutDestination(context.Context, string) (partnerprogram.Destination, error) {
	return partnerprogram.Destination{}, p.destinationErr
}

type referralStub struct {
	ReferralService
	binding                      referral.PaymentAttribution
	link                         referral.Link
	elig                         referral.Eligibility
	boundAt                      time.Time
	boundPayment, boundPrincipal string
	acceptedSignup               referral.Referral
	lookupSignupErr              error
}

func (r *referralStub) LookupSignup(context.Context, string, string, referral.Evidence, time.Time) (referral.Referral, error) {
	if r.lookupSignupErr != nil {
		return referral.Referral{}, r.lookupSignupErr
	}
	if r.acceptedSignup.ID != "" {
		return r.acceptedSignup, nil
	}
	return referral.Referral{}, referral.ErrNotFound
}

func (r *referralStub) BindPayment(ctx context.Context, customer, payment string, at time.Time) (referral.PaymentAttribution, error) {
	r.boundAt = at
	r.boundPayment = payment
	r.boundPrincipal = customer
	return r.binding, nil
}
func (r *referralStub) GetLinkByCode(context.Context, string) (referral.Link, error) {
	return r.link, nil
}
func (r *referralStub) LockAttribution(ctx context.Context, p referral.PartnerState, l referral.Link, e referral.Eligibility) (referral.Referral, error) {
	r.elig = e
	return referral.Referral{ID: "signup-attribution", TermsSnapshot: e.Terms}, nil
}

type earningsStub struct {
	EarningsService
	accrual       *partnerearnings.AccrualRequest
	refund        *partnerearnings.ReversalRequest
	recordCalls   int
	acceptedEntry *partnerearnings.Entry
	acceptedError error
	dispute       *partnerearnings.DisputeRequest
}

func (e *earningsStub) Accrue(ctx context.Context, r partnerearnings.AccrualRequest) (partnerearnings.Entry, error) {
	e.accrual = &r
	return partnerearnings.Entry{ID: "durable-accrual", AmountMinor: 2000}, nil
}
func (e *earningsStub) Reverse(ctx context.Context, r partnerearnings.ReversalRequest) ([]partnerearnings.Entry, error) {
	e.refund = &r
	return []partnerearnings.Entry{{ID: "durable-reversal"}}, nil
}
func (e *earningsStub) Balances(context.Context, string) (partnerearnings.Balances, error) {
	return partnerearnings.Balances{}, nil
}
func (e *earningsStub) RecordPayment(ctx context.Context, r partnerearnings.RecordPaymentRequest) (partnerearnings.Claim, error) {
	e.recordCalls++
	return partnerearnings.Claim{}, nil
}

type revenueStub struct {
	facts map[string]billing.RevenueFact
	fail  error
}

func (b *revenueStub) GetRevenueFact(ctx context.Context, id string) (billing.RevenueFact, error) {
	if b.fail != nil {
		return billing.RevenueFact{}, b.fail
	}
	f, ok := b.facts[id]
	if !ok {
		return billing.RevenueFact{}, billing.ErrRevenueNotFound
	}
	return f, nil
}
func managerFixture(t *testing.T) (*Manager, *programStub, *referralStub, *earningsStub, *authorityStub, *identityStub, *revenueStub) {
	t.Helper()
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	p := &programStub{partner: partnerprogram.Partner{ID: "partner", ProgramID: partnerprogram.ProgramID, CustomerID: "owner", CanAcquireReferrals: true, CanAccrue: true, CanRequestPayouts: true}, cfg: partnerprogram.Config{Currency: "EUR", TermsVersion: "fixture-terms-v1"}, destinationErr: partnerprogram.ErrNotFound, terms: partnerprogram.EffectiveTerms{RateBasisPoints: 8000, HoldDuration: 14 * 24 * time.Hour, Currency: "EUR", CurrencyExponent: 2, EligiblePlanIDs: []string{"plan"}, TermsVersion: "fixture-new-terms", PolicyVersionIDs: []string{"new-policy"}}}
	r := &referralStub{binding: referral.PaymentAttribution{PartnerID: "partner", ReferralID: "frozen-referral", Terms: referral.TermsSnapshot{RateBasisPoints: 2000, HoldDurationDays: 7, Currency: "EUR", CurrencyExponent: 2, TermsVersion: "fixture-terms-v1", EligiblePlanIDs: []string{"plan"}, PolicyVersionIDs: []string{"old-policy"}}}, link: referral.Link{ID: "link", ProgramID: referral.ProgramID, PartnerID: "partner", Code: "CaseSensitiveCode", CreatedAt: at.Add(-time.Hour)}}
	e := &earningsStub{}
	a := &authorityStub{}
	i := &identityStub{principal: Principal{ID: "owner", Active: true, EmailVerified: true, Individual: true, RegionEligible: true}}
	b := &revenueStub{facts: map[string]billing.RevenueFact{}}
	signer, err := referral.NewEvidenceSigner(referral.EvidenceConfig{ProgramID: referral.ProgramID, ActiveKeyID: "fixture", Keys: map[string][]byte{"fixture": []byte("01234567890123456789012345678901")}, Window: 7 * 24 * time.Hour}, managerClock{at})
	require.NoError(t, err)
	m, err := NewManager(Dependencies{Program: p, Referral: r, Earnings: e, Identity: i, Authority: a, Groups: &groupsStub{ids: []string{"verified-group"}}, Revenue: b, Evidence: signer, Clock: managerClock{at}, Controls: Controls{true, true, true, true, true}})
	require.NoError(t, err)
	return m, p, r, e, a, i, b
}
func TestEnrollmentAdmissionAndConsent(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*Manager, *identityStub, *authorityStub)
		terms string
		want  error
	}{
		{"explicit current consent", func(m *Manager, i *identityStub, a *authorityStub) {}, "fixture-terms-v1", nil},
		{"missing consent", func(m *Manager, i *identityStub, a *authorityStub) {}, "", partnerprogram.ErrDenied},
		{"disabled enrollment", func(m *Manager, i *identityStub, a *authorityStub) { m.deps.Controls.Enrollment = false }, "fixture-terms-v1", ErrDenied},
		{"unverified customer", func(m *Manager, i *identityStub, a *authorityStub) { i.principal.EmailVerified = false }, "fixture-terms-v1", ErrDenied},
		{"organization seat", func(m *Manager, i *identityStub, a *authorityStub) { i.principal.Individual = false }, "fixture-terms-v1", ErrDenied},
		{"unapproved region", func(m *Manager, i *identityStub, a *authorityStub) { i.principal.RegionEligible = false }, "fixture-terms-v1", ErrDenied},
		{"different identity snapshot", func(m *Manager, i *identityStub, a *authorityStub) { i.principal.ID = "other" }, "fixture-terms-v1", ErrDenied},
		{"revoked authority", func(m *Manager, i *identityStub, a *authorityStub) { a.deny = true }, "fixture-terms-v1", ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, a, i, _ := managerFixture(t)
			tc.alter(m, i, a)
			_, err := m.EnrollSelf(context.Background(), "owner", tc.terms)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
func TestOverviewPreservesDependencyErrors(t *testing.T) {
	cases := []struct {
		name                string
		lookup, destination error
		want                error
	}{
		{"expected enrollment absence", partnerprogram.ErrNotFound, nil, ErrNotPartner},
		{"partner outage", partnerprogram.ErrUnavailable, nil, partnerprogram.ErrUnavailable},
		{"destination outage", nil, partnerprogram.ErrUnavailable, partnerprogram.ErrUnavailable},
		{"expected destination absence", nil, partnerprogram.ErrNotFound, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, _, _, _, _, _ := managerFixture(t)
			p.lookupErr = tc.lookup
			p.destinationErr = tc.destination
			_, err := m.Overview(context.Background(), "owner")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"verified-group"}, p.resolvedGroups)
			}
		})
	}
}
func TestRevenueUsesFrozenAllocation(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		paused   bool
		disabled bool
		currency string
		want     error
	}{
		{"payment ignores new current rate", billing.RevenuePayment, false, false, "EUR", nil},
		{"refund recovers while accrual paused", billing.RevenueRefund, true, true, "EUR", nil},
		{"disabled accrual", billing.RevenuePayment, false, true, "EUR", ErrDenied},
		{"partner accrual paused", billing.RevenuePayment, true, false, "EUR", ErrDenied},
		{"foreign currency", billing.RevenuePayment, false, false, "USD", partnerearnings.ErrCurrencyMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, r, e, _, _, b := managerFixture(t)
			p.partner.CanAccrue = !tc.paused
			m.deps.Controls.Accrual = !tc.disabled
			at := m.deps.Clock.Now().Add(-time.Hour)
			original := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account", LiveMode: false}, Kind: billing.RevenuePayment, PaymentID: "payment", InvoiceID: "invoice", AllocationID: "line", PrincipalID: "payer", SubscriptionID: "subscription", PlanID: "plan", CostID: "cost", Currency: tc.currency, CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: at}
			original.ID = original.PaymentFactID()
			b.facts[original.ID] = original
			fact := original
			if tc.kind == billing.RevenueRefund {
				fact.Kind = tc.kind
				fact.ID = "refund-fact"
				fact.AdjustmentID = "refund"
				fact.EffectiveAt = at.Add(time.Hour)
				fact.CumulativeRefundedMinor = 2500
				b.facts[fact.ID] = fact
			}
			_, err := m.ProcessRevenueFact(context.Background(), "worker", fact.ID)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, e.accrual)
				require.Nil(t, e.refund)
				return
			}
			require.NoError(t, err)
			require.Equal(t, at, r.boundAt)
			require.Equal(t, "payer", r.boundPrincipal)
			if tc.kind == billing.RevenuePayment {
				require.Equal(t, 2000, e.accrual.RateBasisPoints)
				require.Equal(t, 7*24*time.Hour, e.accrual.HoldDuration)
				require.Equal(t, "frozen-referral", e.accrual.ReferralID)
				require.Equal(t, original.ID, e.accrual.PaymentID)
			} else {
				require.EqualValues(t, 2500, e.refund.CumulativeRefundedMinor)
				require.Equal(t, original.ID, e.refund.PaymentID)
			}
		})
	}
}
func TestSignupReadsOwningEvidence(t *testing.T) {
	cases := []struct {
		name     string
		existing bool
		tampered bool
		want     error
	}{
		{"new account", false, false, nil},
		{"existing account", true, false, ErrDenied},
		{"tampered evidence", false, true, referral.ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, r, _, _, i, _ := managerFixture(t)
			token, _, err := m.deps.Evidence.Issue(r.link)
			require.NoError(t, err)
			i.signup = SignupFact{ID: "signup", CustomerID: "payer", CreatedAt: m.deps.Clock.Now(), NewAccount: !tc.existing, Individual: true, AttributionEvidence: token}
			if tc.tampered {
				i.signup.AttributionEvidence = token + "changed"
			}
			_, err = m.ConsumeSignup(context.Background(), "worker", "signup")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			require.Equal(t, i.signup.CreatedAt, r.elig.At)
			require.Equal(t, "payer", r.elig.ReferredCustomer)
			require.Equal(t, i.signup.CreatedAt, p.resolvedAt)
			require.Equal(t, []string{"verified-group"}, p.resolvedGroups)
			require.Equal(t, 8000, r.elig.Terms.RateBasisPoints)
		})
	}
}
func TestManualRecordingChecksCurrentAuthority(t *testing.T) {
	cases := []struct {
		name              string
		revoked, disabled bool
		want              error
	}{
		{"allowed operator", false, false, nil},
		{"revoked operator replay denied", true, false, ErrDenied},
		{"paused handling preserves already attempted recording", false, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, e, a, _, _ := managerFixture(t)
			a.deny = tc.revoked
			m.deps.Controls.ManualRecording = !tc.disabled
			_, err := m.AdminRecordPayment(context.Background(), partnerearnings.RecordPaymentRequest{ClaimID: "claim", ActorID: "operator", IdempotencyKey: "same-key", ExpectedRevision: 2})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, e.recordCalls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, e.recordCalls)
				require.Equal(t, []string{CapabilityRecordPayment}, a.calls)
			}
		})
	}
}

func (e *earningsStub) AcceptedAccrual(context.Context, string, string) (partnerearnings.Entry, error) {
	if e.acceptedError != nil {
		return partnerearnings.Entry{}, e.acceptedError
	}
	if e.acceptedEntry != nil {
		return *e.acceptedEntry, nil
	}
	return partnerearnings.Entry{}, partnerearnings.ErrNotFound
}
func (e *earningsStub) Dispute(_ context.Context, r partnerearnings.DisputeRequest) (partnerearnings.DisputeResult, error) {
	e.dispute = &r
	return partnerearnings.DisputeResult{Entries: []partnerearnings.Entry{{ID: "durable-dispute-decision"}}}, nil
}
