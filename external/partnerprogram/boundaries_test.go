package partnerprogram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named admission cases preserve strict absence and reject
// invalid/cancelled use cases before any partner enrollment write.
type enrollmentBoundaryRepo struct {
	Repository
	readErr error
	writes  int
}

func (r *enrollmentBoundaryRepo) GetPartnerByCustomer(context.Context, string, string) (Partner, error) {
	return Partner{}, r.readErr
}
func (r *enrollmentBoundaryRepo) InsertPartner(context.Context, Partner) error {
	r.writes++
	return nil
}
func TestProgramAdmissionBoundary(t *testing.T) {
	cases := []struct {
		name                  string
		readErr               error
		nilContext, cancelled bool
		want                  error
	}{
		{name: "joined_absence_outage", readErr: errors.Join(ErrNotFound, ErrUnavailable), want: ErrUnavailable},
		{name: "nil_context", readErr: ErrNotFound, nilContext: true, want: ErrInvalid},
		{name: "cancelled_context", readErr: ErrNotFound, cancelled: true, want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &enrollmentBoundaryRepo{readErr: tc.readErr}
			s, err := NewService(repo, fixedClock{time.Now()}, &seqIDs{}, testConfig())
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
			_, err = s.Enroll(ctx, EnrollRequest{CustomerID: "owner", AcceptedTermsVersion: testConfig().TermsVersion})
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, repo.writes)
		})
	}
}
func TestProgramRejectsTypedNilDependencies(t *testing.T) {
	cases := []struct {
		name             string
		repo, clock, ids bool
	}{{name: "repository", repo: true}, {name: "clock", clock: true}, {name: "ids", ids: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var repo Repository = newFakeRepo()
			var clock Clock = fixedClock{time.Now()}
			var ids IDGenerator = &seqIDs{}
			if tc.repo {
				var x *fakeRepo
				repo = x
			}
			if tc.clock {
				var x *fixedClock
				clock = x
			}
			if tc.ids {
				var x *seqIDs
				ids = x
			}
			_, err := NewService(repo, clock, ids, testConfig())
			require.ErrorIs(t, err, ErrUnavailable)
		})
	}
}
