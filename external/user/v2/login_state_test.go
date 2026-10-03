package user

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// loginStateProbe models adapter-owned snapshots and uncertain native writes.
// Atomicity and field preservation are tested separately against actual Mongo.
type loginStateProbe struct {
	UserRepository
	stored *UniversalUser
	err    error
	fault  string
	writes int
	cancel context.CancelFunc
}

func (p *loginStateProbe) GetUserByID(context.Context, string) (*UniversalUser, error) {
	if p.fault == "read failure" {
		return nil, p.err
	}
	if p.fault == "cancel read" {
		p.cancel()
	}
	return p.stored, nil
}
func (p *loginStateProbe) SetFreshLogin(ctx context.Context, r *SetLoginStateRequest) (*UniversalUser, error) {
	return p.write(r, false)
}
func (p *loginStateProbe) SetVerifiedEmailActivation(ctx context.Context, r *SetLoginStateRequest) (*UniversalUser, error) {
	return p.write(r, true)
}
func (p *loginStateProbe) write(r *SetLoginStateRequest, activate bool) (*UniversalUser, error) {
	p.writes++
	if p.fault == "write failure" {
		return nil, p.err
	}
	v := copyUserForUpdate(p.stored)
	if v.Metadata == nil {
		v.Metadata = &UserMetadata{}
	}
	v.Metadata.LastLoginAt, v.Metadata.LastFreshLoginAt = r.At, r.At
	if activate {
		v.Status = AccountStatusKeyActive
		v.Verification = &VerificationStatus{EmailVerified: true, EmailVerifiedAt: r.At}
		v.Metadata.ActivatedAt, v.Metadata.StatusChangedAt, v.Metadata.UpdatedAt = r.At, r.At, r.At
	}
	switch p.fault {
	case "nil receipt":
		return nil, nil
	case "owner receipt", "mutated command":
		v.ID = "other"
		r.Account.UserID = "other"
	case "email receipt":
		v.Email = "other@example.test"
	case "revision receipt":
		v.EmailRevision++
	case "status receipt":
		v.Status = AccountStatusKeySuspended
	case "type receipt":
		v.Type = "other"
	case "stamp receipt":
		v.Metadata.LastFreshLoginAt = "bad"
	case "nil metadata":
		v.Metadata = nil
	case "cancel write":
		p.cancel()
	}
	return v, nil
}

func TestLoginStateDomain(t *testing.T) {
	native := fmt.Errorf("private-store-diagnostic: %w", ErrLoginStateConflict)
	for _, activate := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			want   error
			writes int
		}{
			{"success", nil, 1}, {"legacy type", nil, 1}, {"null metadata", nil, 1}, {"nil service", ErrLoginStateUnavailable, 0}, {"nil context", ErrLoginStateUnavailable, 0}, {"nil repository", ErrLoginStateUnavailable, 0}, {"typed nil repository", ErrLoginStateUnavailable, 0}, {"missing capability", ErrLoginStateUnavailable, 0}, {"nil clock", ErrLoginStateUnavailable, 0}, {"zero clock", ErrLoginStateUnavailable, 0}, {"nil strings", ErrLoginStateUnavailable, 0}, {"nil request", ErrInvalidUserBody, 0}, {"empty ID", ErrInvalidUserBody, 0}, {"wrong source", ErrInvalidUserBody, 0}, {"negative revision", ErrInvalidUserBody, 0}, {"read failure", native, 0}, {"write failure", native, 1}, {"nil stored", ErrLoginStateUnavailable, 0}, {"wrong stored", ErrLoginStateUnavailable, 0}, {"stale email", ErrLoginStateConflict, 0}, {"stale revision", ErrLoginStateConflict, 0}, {"stale type", ErrLoginStateConflict, 0}, {"stale status", ErrLoginStateConflict, 0}, {"invalid role", ErrUserInvalidRole, 0}, {"canceled", context.Canceled, 0}, {"cancel read", context.Canceled, 0}, {"cancel write", nil, 1}, {"nil receipt", ErrLoginStateUnavailable, 1}, {"owner receipt", ErrLoginStateUnavailable, 1}, {"email receipt", ErrLoginStateUnavailable, 1}, {"revision receipt", ErrLoginStateUnavailable, 1}, {"status receipt", ErrLoginStateUnavailable, 1}, {"type receipt", ErrLoginStateUnavailable, 1}, {"stamp receipt", ErrLoginStateUnavailable, 1}, {"nil metadata", ErrLoginStateUnavailable, 1}, {"mutated command", ErrLoginStateUnavailable, 1},
		} {
			t.Run(fmt.Sprintf("activate=%t/%s", activate, tc.name), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p := &loginStateProbe{stored: genericAccount(), err: native, fault: tc.name, cancel: cancel}
				p.stored.Status = loginSource(activate)
				s := NewService(p, nil, nil, &DefaultIDGenerator{}, profileClock{stamp: "2026-10-03T18:00:00Z"}, &DefaultStringUtils{}, "")
				r := &AccountSnapshot{UserID: "selected", Email: p.stored.Email, EmailRevision: 2, Type: "default", Status: p.stored.Status}
				switch tc.name {
				case "legacy type":
					p.stored.Type = ""
				case "null metadata":
					p.stored.Metadata = nil
				case "nil service":
					s = nil
				case "nil context":
					ctx = nil
				case "nil repository":
					s.UserRepository = nil
				case "typed nil repository":
					s.UserRepository = (*loginStateProbe)(nil)
				case "missing capability":
					s.UserRepository = &genericUpdateProbe{}
				case "nil clock":
					s.TimeProvider = nil
				case "zero clock":
					s.TimeProvider = profileClock{}
				case "nil strings":
					s.StringUtils = nil
				case "nil request":
					r = nil
				case "empty ID":
					r.UserID = " "
				case "wrong source":
					r.Status = AccountStatusKeySuspended
				case "negative revision":
					r.EmailRevision = -1
				case "nil stored":
					p.stored = nil
				case "wrong stored":
					p.stored.ID = "other"
				case "stale email":
					r.Email = "other@example.test"
				case "stale revision":
					r.EmailRevision--
				case "stale type":
					r.Type = "other"
				case "stale status":
					p.stored.Status = AccountStatusKeySuspended
				case "invalid role":
					p.stored.Roles = []string{"invalid"}
				case "canceled":
					cancel()
				}
				before := copyUserForUpdate(p.stored)
				var input AccountSnapshot
				if r != nil {
					input = *r
				}
				var got *UniversalUser
				var err error
				if activate {
					got, err = s.ActivateVerifiedEmail(ctx, r)
				} else {
					got, err = s.RecordFreshLogin(ctx, r)
				}
				require.Equal(t, tc.want, err)
				require.Equal(t, tc.writes, p.writes)
				require.Equal(t, before, p.stored)
				if r != nil {
					require.Equal(t, input, *r)
				}
				if err != nil {
					require.Nil(t, got)
				} else {
					require.Equal(t, "default", got.Type)
					require.Equal(t, "2026-10-03T18:00:00Z", got.Metadata.LastFreshLoginAt)
					if !activate && before.Metadata != nil {
						require.Equal(t, before.Metadata.UpdatedAt, got.Metadata.UpdatedAt)
					}
				}
			})
		}
	}
}

func TestLoginStateRepositoryEntry(t *testing.T) {
	for _, activate := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"nil receiver", ErrLoginStateUnavailable}, {"nil context", ErrLoginStateUnavailable}, {"nil store", ErrLoginStateUnavailable}, {"typed nil store", ErrLoginStateUnavailable}, {"missing capability", ErrLoginStateUnavailable}, {"nil command", ErrInvalidUserBody}, {"wrong source", ErrInvalidUserBody}, {"invalid stamp", ErrInvalidUserBody}, {"non UTC stamp", ErrInvalidUserBody}, {"canceled", context.Canceled},
		} {
			t.Run(fmt.Sprintf("activate=%t/%s", activate, tc.name), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p := &emailStorageSetupProbe{}
				r := NewRepository(p)
				req := &SetLoginStateRequest{Account: AccountSnapshot{UserID: "id", Email: "owner@example.test", Status: loginSource(activate)}, At: "2026-10-03T18:00:00Z"}
				switch tc.name {
				case "nil receiver":
					r = nil
				case "nil context":
					ctx = nil
				case "nil store":
					r.Store = nil
				case "typed nil store":
					r.Store = (*emailStorageSetupProbe)(nil)
				case "nil command":
					req = nil
				case "wrong source":
					req.Account.Status = "SUSPENDED"
				case "invalid stamp":
					req.At = "bad"
				case "non UTC stamp":
					req.At = "2026-10-03T19:00:00+01:00"
				case "canceled":
					cancel()
				}
				var got *UniversalUser
				var err error
				if activate {
					got, err = r.SetVerifiedEmailActivation(ctx, req)
				} else {
					got, err = r.SetFreshLogin(ctx, req)
				}
				require.Equal(t, tc.want, err)
				require.Nil(t, got)
				require.Zero(t, p.initCalls)
			})
		}
	}
}

func TestLoginStateActivationRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"valid", nil}, {"target disabled", ErrUserInvalidTargetStatus}, {"source disabled", ErrUserInvalidStatusTransition}, {"typed nil clock", ErrLoginStateUnavailable}, {"typed nil strings", ErrLoginStateUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &loginStateProbe{stored: genericAccount()}
			p.stored.Status = AccountStatusKeyProvisioned
			s := NewService(p, nil, nil, &DefaultIDGenerator{}, profileClock{stamp: "2026-10-03T18:00:00Z"}, &DefaultStringUtils{}, "")
			switch tc.name {
			case "target disabled":
				delete(s.Config.StatusTransitions, AccountStatusKeyActive)
			case "source disabled":
				s.Config.StatusTransitions[AccountStatusKeyActive] = []string{AccountStatusKeySuspended}
			case "typed nil clock":
				s.TimeProvider = (*DefaultTimeProvider)(nil)
			case "typed nil strings":
				s.StringUtils = (*DefaultStringUtils)(nil)
			}
			got, err := s.ActivateVerifiedEmail(context.Background(), &AccountSnapshot{UserID: "selected", Email: p.stored.Email, Type: p.stored.Type, EmailRevision: 2, Status: AccountStatusKeyProvisioned})
			require.Equal(t, tc.want, err)
			if err != nil {
				require.Nil(t, got)
				require.Zero(t, p.writes)
			} else {
				require.Equal(t, 1, p.writes)
			}
		})
	}
}
