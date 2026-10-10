package partnermanager

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

// acquisitionStub records the selected owner and can revoke authority during I/O.
type acquisitionStub struct {
	eligible bool
	err      error
	calls    []string
	revoke   *authorityStub
}

// CanAcquirePartnerReferrals returns the case's current commercial decision.
func (s *acquisitionStub) CanAcquirePartnerReferrals(_ context.Context, customer string) (bool, error) {
	s.calls = append(s.calls, customer)
	if s.revoke != nil {
		s.revoke.deny = true
	}
	return s.eligible, s.err
}

// acquisitionLinkStub records issuance without hiding unexpected owner methods.
type acquisitionLinkStub struct {
	ReferralService
	issued int
}

// IssueLink records a new acquisition request reaching the owning service.
func (s *acquisitionLinkStub) IssueLink(_ context.Context, p referral.PartnerState) (referral.Link, error) {
	s.issued++
	return referral.Link{ID: "new-link", PartnerID: p.PartnerID}, nil
}

// TestAcquisitionAdmission refuses unpaid and unavailable enrollment/link
// requests and catches session revocation during the eligibility lookup.
func TestAcquisitionAdmission(t *testing.T) {
	for _, operation := range []string{"enroll", "link", "eligibility"} {
		for _, tc := range []struct {
			name             string
			eligible, revoke bool
			fail, want       error
		}{
			{name: "eligible", eligible: true},
			{name: "unpaid", want: ErrIneligible},
			{name: "dependency unavailable", fail: ErrUnavailable, want: ErrUnavailable},
			{name: "revoked during paid lookup", eligible: true, revoke: true, want: ErrDenied},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				m, p, _, _, a, _, _ := managerFixture(t)
				eligibility := &acquisitionStub{eligible: tc.eligible, err: tc.fail}
				if tc.revoke {
					eligibility.revoke = a
				}
				m.deps.AcquisitionEligibility = eligibility
				links := &acquisitionLinkStub{ReferralService: m.deps.Referral}
				m.deps.Referral = links
				var err error
				switch operation {
				case "enroll":
					_, err = m.EnrollSelf(context.Background(), "owner", p.cfg.TermsVersion)
				case "link":
					_, err = m.GetOrCreateLink(context.Background(), "owner")
				case "eligibility":
					var eligible bool
					eligible, err = m.AcquisitionEligible(context.Background(), "owner")
					if tc.want == ErrIneligible {
						require.NoError(t, err)
						require.False(t, eligible)
						return
					}
				}
				if tc.want == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, tc.want)
					require.Empty(t, p.enrollTerms)
					require.Zero(t, links.issued)
				}
				require.Equal(t, []string{"owner"}, eligibility.calls)
			})
		}
	}
}

// TestAcquisitionLapseKeepsHistoricalReadsAndRecovery separates new commercial
// admission from retained account balances and original-key rotation receipts.
func TestAcquisitionLapseKeepsHistoricalReadsAndRecovery(t *testing.T) {
	for _, operation := range []string{"overview", "rotation recovery", "signup recovery"} {
		t.Run(operation, func(t *testing.T) {
			m, _, r, _, _, identity, _ := managerFixture(t)
			eligibility := &acquisitionStub{}
			m.deps.AcquisitionEligibility = eligibility
			switch operation {
			case "overview":
				_, err := m.Overview(context.Background(), "owner")
				require.NoError(t, err)
				require.Empty(t, eligibility.calls)
			case "rotation recovery":
				owner := &rotationManagerReferral{}
				m.deps.Referral = owner
				_, err := m.RotateLink(context.Background(), "owner", referral.RotateLinkRequest{ExpectedLinkCode: "old", IdempotencyKey: "original", Reason: "rotate"})
				require.NoError(t, err)
				require.False(t, owner.request.Partner.CanAcquireReferrals)
				require.Equal(t, 1, owner.calls)
			case "signup recovery":
				at := m.deps.Clock.Now()
				token, _, err := m.deps.Evidence.Issue(r.link)
				require.NoError(t, err)
				identity.signup = SignupFact{ID: "signup", CustomerID: "referred-free-user", AttributionEvidence: token, CreatedAt: at, NewAccount: true, Individual: true}
				r.acceptedSignup = referral.Referral{ID: "original-attribution"}
				out, err := m.ConsumeSignup(context.Background(), "worker", "signup")
				require.NoError(t, err)
				require.Equal(t, "original-attribution", out.ID)
				require.Empty(t, eligibility.calls)
			}
		})
	}
}

// TestSignupEligibilityChecksReferrerNeverTheFreeReferredAccount verifies the
// worker's new attribution boundary, including expiry between visit and signup.
func TestSignupEligibilityChecksReferrerNeverTheFreeReferredAccount(t *testing.T) {
	for _, eligible := range []bool{true, false} {
		name := "expired referrer"
		if eligible {
			name = "paid referrer and free signup"
		}
		t.Run(name, func(t *testing.T) {
			m, _, r, _, _, identity, _ := managerFixture(t)
			port := &acquisitionStub{eligible: eligible}
			m.deps.AcquisitionEligibility = port
			at := m.deps.Clock.Now()
			token, _, err := m.deps.Evidence.Issue(r.link)
			require.NoError(t, err)
			identity.signup = SignupFact{ID: "signup", CustomerID: "free-customer", AttributionEvidence: token, CreatedAt: at, NewAccount: true, Individual: true}
			_, err = m.ConsumeSignup(context.Background(), "worker", "signup")
			if eligible {
				require.NoError(t, err)
				require.Equal(t, "free-customer", r.elig.ReferredCustomer)
			} else {
				require.ErrorIs(t, err, ErrIneligible)
				require.Empty(t, r.elig.SignupID)
			}
			require.Equal(t, []string{"owner"}, port.calls)
		})
	}
}

// acquisitionRotationOwner separates an existing receipt from new rotation
// admission. Native referral tests own the transactional receipt guarantees.
type acquisitionRotationOwner struct {
	ReferralService
	// receipt is an already committed original-key result.
	receipt bool
	// calls counts delegation even when current acquisition cannot be assessed.
	calls int
}

// RotateLink preserves original receipts before consulting new admission.
func (r *acquisitionRotationOwner) RotateLink(_ context.Context, req referral.RotateLinkRequest) (referral.Link, error) {
	r.calls++
	if r.receipt {
		return referral.Link{ID: "retained-link"}, nil
	}
	if !req.Partner.CanAcquireReferrals {
		return referral.Link{}, referral.ErrDenied
	}
	return referral.Link{ID: "new-link"}, nil
}

// TestRotationEligibilityRecovery preserves committed receipts through both
// paid-plan lapse and dependency outage, without allowing a new rotation.
func TestRotationEligibilityRecovery(t *testing.T) {
	for _, tc := range []struct {
		name          string
		receipt       bool
		failure, want error
	}{
		{"lapsed with receipt", true, nil, nil},
		{"outage with receipt", true, ErrUnavailable, nil},
		{"lapsed without receipt", false, nil, ErrIneligible},
		{"outage without receipt", false, ErrUnavailable, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			m.deps.AcquisitionEligibility = &acquisitionStub{err: tc.failure}
			owner := &acquisitionRotationOwner{receipt: tc.receipt}
			m.deps.Referral = owner
			out, err := m.RotateLink(context.Background(), "owner", referral.RotateLinkRequest{ExpectedLinkCode: "old", IdempotencyKey: "original", Reason: "rotation"})
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, 1, owner.calls)
			if tc.receipt {
				require.Equal(t, "retained-link", out.ID)
			} else {
				require.Empty(t, out.ID)
			}
		})
	}
}

// TestAttributionEligibilityRechecksAndPreservesReceipts checks the paid owner
// again at apply time while allowing original receipt recovery after lapse.
func TestAttributionEligibilityRechecksAndPreservesReceipts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		committed     bool
		failure, want error
	}{
		{"lapsed after preview", false, nil, ErrIneligible},
		{"outage after preview", false, ErrUnavailable, ErrUnavailable},
		{"committed before lapse", true, nil, nil},
		{"committed before outage", true, ErrUnavailable, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, owner, _, change := attributionFixture(t)
			port := &acquisitionStub{eligible: true}
			m.deps.AcquisitionEligibility = port
			ctx := context.Background()
			preview, err := m.PreviewAttribution(ctx, change)
			require.NoError(t, err)
			req := applyPreview(change, preview)
			if tc.committed {
				_, err = m.ApplyAttribution(ctx, req)
				require.NoError(t, err)
			}
			port.eligible, port.err, port.calls = false, tc.failure, nil
			out, err := m.ApplyAttribution(ctx, req)
			require.ErrorIs(t, err, tc.want)
			if tc.committed {
				require.Equal(t, "receipt", out.ID)
				require.Empty(t, port.calls)
				require.Equal(t, 1, owner.assignCalls)
			} else {
				require.Empty(t, out)
				require.Equal(t, []string{"owner"}, port.calls)
				require.Zero(t, owner.assignCalls)
			}
		})
	}
}
