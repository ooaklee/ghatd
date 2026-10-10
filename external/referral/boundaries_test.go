package referral

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named admission boundaries assert that ambiguous absence
// and cancelled work cannot create links or overwrite financial ownership.
type admissionRepo struct {
	Repository
	readErr error
	writes  int
}

func (r *admissionRepo) WithAttributionTransaction(_ context.Context, _, _ string, fn func(Repository) error) error {
	return fn(r)
}

func (r *admissionRepo) ListLinksByPartner(context.Context, string, string) ([]Link, error) {
	return nil, r.readErr
}
func (r *admissionRepo) GetPaymentAttribution(context.Context, string, string) (PaymentAttribution, error) {
	return PaymentAttribution{}, r.readErr
}
func (r *admissionRepo) ListReferralHistory(context.Context, string, string) ([]Referral, error) {
	return nil, r.readErr
}
func (r *admissionRepo) GetReferralByCustomer(context.Context, string, string) (Referral, error) {
	return Referral{}, ErrNotFound
}
func (r *admissionRepo) InsertLink(context.Context, Link) error { r.writes++; return nil }
func (r *admissionRepo) InsertReferral(context.Context, Referral, int64) error {
	r.writes++
	return nil
}
func TestReferralAdmissionRejectsAmbiguousAbsenceAndCancelledWork(t *testing.T) {
	cases := []struct {
		name, operation       string
		readErr               error
		nilContext, cancelled bool
		want                  error
	}{
		{name: "joined_link_absence_outage", operation: "link", readErr: errors.Join(ErrNotFound, ErrUnavailable), want: ErrUnavailable},
		{name: "joined_signup_absence_outage", operation: "signup", readErr: errors.Join(ErrNotFound, ErrUnavailable), want: ErrUnavailable},
		{name: "joined_payment_absence_outage", operation: "payment", readErr: errors.Join(ErrNotFound, ErrUnavailable), want: ErrUnavailable},
		{name: "nil_context_link", operation: "link", nilContext: true, want: ErrInvalid},
		{name: "cancelled_link", operation: "link", cancelled: true, want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, p, l, e := fixture(t)
			repo := &admissionRepo{readErr: tc.readErr}
			s, err := NewService(repo, fakeClock{e.At}, &seqIDs{}, 7*24*time.Hour)
			require.NoError(t, err)
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			switch tc.operation {
			case "link":
				_, err = s.IssueLink(ctx, p)
			case "signup":
				_, err = s.LockAttribution(ctx, p, l, e)
			case "payment":
				_, err = s.BindPayment(ctx, e.ReferredCustomer, "payment", e.At)
			}
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, repo.writes)
		})
	}
}

func TestReferralRejectsTypedNilDependencies(t *testing.T) {
	cases := []struct {
		name             string
		repo, clock, ids bool
	}{{name: "repository", repo: true}, {name: "clock", clock: true}, {name: "ids", ids: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var repo Repository = newFakeRepo()
			var clock Clock = fakeClock{time.Now()}
			var ids IDGenerator = &seqIDs{}
			if tc.repo {
				var x *fakeRepo
				repo = x
			}
			if tc.clock {
				var x *fakeClock
				clock = x
			}
			if tc.ids {
				var x *seqIDs
				ids = x
			}
			_, err := NewService(repo, clock, ids, time.Hour)
			require.ErrorIs(t, err, ErrUnavailable)
		})
	}
}
