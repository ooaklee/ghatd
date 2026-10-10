package partnermanager

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

// Related read boundaries use named cases over concrete operation/result data.
// Fixtures are fresh per case; no callbacks combine unrelated complete tests.
type readEarningsStub struct {
	EarningsService
	claim     partnerearnings.Claim
	entry     partnerearnings.Entry
	fail      error
	afterRead func()
}

func (s *readEarningsStub) read() {
	if s.afterRead != nil {
		s.afterRead()
	}
}
func (s *readEarningsStub) Balances(context.Context, string) (partnerearnings.Balances, error) {
	s.read()
	return partnerearnings.Balances{AvailableMinor: 100}, s.fail
}
func (s *readEarningsStub) ListJournal(context.Context, string) ([]partnerearnings.Entry, error) {
	s.read()
	return []partnerearnings.Entry{s.entry}, s.fail
}
func (s *readEarningsStub) ListClaims(context.Context, string, []string, int, string) ([]partnerearnings.Claim, error) {
	s.read()
	return []partnerearnings.Claim{s.claim}, s.fail
}
func (s *readEarningsStub) GetClaim(context.Context, string) (partnerearnings.Claim, error) {
	s.read()
	return s.claim, s.fail
}

type readProgramStub struct {
	*programStub
	destination partnerprogram.Destination
	fail        error
	afterRead   func()
}

func (s *readProgramStub) GetPayoutDestination(context.Context, string) (partnerprogram.Destination, error) {
	if s.afterRead != nil {
		s.afterRead()
	}
	return s.destination, s.fail
}
func (s *readProgramStub) ListPolicyVersions(context.Context) ([]partnerprogram.PolicyVersion, error) {
	if s.afterRead != nil {
		s.afterRead()
	}
	return []partnerprogram.PolicyVersion{{ID: "policy"}}, s.fail
}

type readReferralStub struct {
	ReferralService
	row       referral.Referral
	fail      error
	afterRead func()
}

func (s *readReferralStub) ListByPartner(context.Context, string, int, string) ([]referral.Referral, error) {
	if s.afterRead != nil {
		s.afterRead()
	}
	return []referral.Referral{s.row}, s.fail
}
func TestManagerReadBoundaries(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "authorized_owning_result"},
		{name: "revoked_during_read", mode: "revoked", want: ErrDenied},
		{name: "joined_absence_and_outage", mode: "outage", want: ErrUnavailable},
		{name: "wrong_result_scope", mode: "scope", want: ErrUnavailable},
	}
	for _, operation := range []string{"overview", "referrals", "ledger", "claims", "claim", "destination", "admin_queue", "admin_policy_history"} {
		for _, tc := range cases {
			if tc.mode == "scope" && operation == "admin_policy_history" {
				continue
			} // Global policy history has no selected owner.
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				m, base, _, _, a, _, _ := managerFixture(t)
				p := &readProgramStub{programStub: base, destination: partnerprogram.Destination{ID: "destination", CustomerID: "owner", Version: 1, Method: "paypal", Email: "owner@example.test"}}
				e := &readEarningsStub{claim: partnerearnings.Claim{ID: "claim", PartnerID: "partner", Currency: "EUR"}, entry: partnerearnings.Entry{ID: "entry", PartnerID: "partner", Currency: "EUR"}}
				r := &readReferralStub{row: referral.Referral{ID: "referral", PartnerID: "partner", ProgramID: referral.ProgramID}}
				m.deps.Program, m.deps.Earnings, m.deps.Referral = p, e, r
				if tc.mode == "revoked" {
					p.afterRead = func() { a.deny = true }
					e.afterRead = p.afterRead
					r.afterRead = p.afterRead
				}
				if tc.mode == "outage" {
					p.fail = errors.Join(partnerprogram.ErrNotFound, ErrUnavailable)
					e.fail = errors.Join(partnerearnings.ErrNotFound, ErrUnavailable)
					r.fail = errors.Join(referral.ErrNotFound, ErrUnavailable)
				}
				if tc.mode == "scope" {
					p.destination.CustomerID = "other"
					r.row.PartnerID = "other"
					e.claim.Currency = "USD"
					e.entry.PartnerID = "other"
				}
				var out any
				var err error
				switch operation {
				case "overview":
					out, err = m.Overview(context.Background(), "owner")
				case "referrals":
					out, err = m.ListReferrals(context.Background(), "owner", 100, "")
				case "ledger":
					out, err = m.Ledger(context.Background(), "owner")
				case "claims":
					out, err = m.ListClaims(context.Background(), "owner", nil, 100, "")
				case "claim":
					out, err = m.GetClaim(context.Background(), "owner", "claim")
				case "destination":
					out, err = m.Destination(context.Background(), "owner")
				case "admin_queue":
					out, err = m.AdminQueue(context.Background(), "operator", nil, 100, "")
				case "admin_policy_history":
					out, err = m.AdminPolicyVersions(context.Background(), "operator")
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, out, "failed reads cannot return private partial data")
				}
			})
		}
	}
}
