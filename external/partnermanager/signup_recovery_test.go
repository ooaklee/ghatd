package partnermanager

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

func TestSignupRecoveryRequiresCurrentAuthority(t *testing.T) {
	cases := []struct {
		name                      string
		paused, accepted, revoked bool
		lookupErr, want           error
	}{
		{name: "already committed while paused", paused: true, accepted: true},
		{name: "new admission while paused", paused: true, want: ErrDenied},
		{name: "revoked worker cannot replay", paused: true, accepted: true, revoked: true, want: ErrDenied},
		{name: "lookup outage remains retryable", accepted: true, lookupErr: referral.ErrUnavailable, want: referral.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, r, _, authority, identity, _ := managerFixture(t)
			token, _, err := m.deps.Evidence.Issue(r.link)
			require.NoError(t, err)
			identity.signup = SignupFact{ID: "signup", CustomerID: "payer", CreatedAt: m.deps.Clock.Now(), NewAccount: true, Individual: true, AttributionEvidence: token}
			m.deps.Controls.Attribution = !tc.paused
			authority.deny = tc.revoked
			r.lookupSignupErr = tc.lookupErr
			if tc.accepted {
				r.acceptedSignup = referral.Referral{ID: "immutable-original", SignupID: "signup", ReferredCustomer: "payer", TermsSnapshot: referral.TermsSnapshot{TermsVersion: "original-terms"}}
				p.lookupErr = partnerprogram.ErrUnavailable
			}
			out, err := m.ConsumeSignup(context.Background(), "worker", "signup")
			require.ErrorIs(t, err, tc.want)
			if err == nil {
				require.Equal(t, "immutable-original", out.ID)
				require.Equal(t, "original-terms", out.TermsSnapshot.TermsVersion)
			}
			require.Nil(t, r.elig.Terms.PolicyVersionIDs)
			require.Contains(t, authority.calls, CapabilitySignupWorker)
		})
	}
}
