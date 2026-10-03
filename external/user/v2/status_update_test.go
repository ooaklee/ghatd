package user

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// statusProbe captures narrow commands and returns case-owned receipts. Mongo
// integration tests, not this probe, establish storage atomicity/preservation.
type statusProbe struct {
	UserRepository
	stored  *UniversalUser
	fault   string
	err     error
	writes  int
	command SetAccountStatusRequest
	cancel  context.CancelFunc
}

func (p *statusProbe) GetUserByID(context.Context, string) (*UniversalUser, error) {
	if p.fault == "read failure" {
		return nil, p.err
	}
	if p.fault == "cancel read" {
		p.cancel()
	}
	return p.stored, nil
}

func (p *statusProbe) SetAccountStatus(_ context.Context, c *SetAccountStatusRequest) (*UniversalUser, error) {
	p.writes++
	p.command = *c
	if p.fault == "write failure" {
		return nil, p.err
	}
	v := copyUserForUpdate(p.stored)
	if v.Metadata == nil {
		v.Metadata = &UserMetadata{}
	}
	v.Status, v.Metadata.UpdatedAt, v.Metadata.StatusChangedAt = c.Status, c.At, c.At
	if c.Status == AccountStatusKeyActive {
		v.Metadata.ActivatedAt = c.At
	}
	if c.ClearEmailVerification {
		if v.Verification == nil {
			v.Verification = &VerificationStatus{}
		}
		v.Verification.EmailVerified, v.Verification.EmailVerifiedAt = false, ""
	}
	switch p.fault {
	case "nil receipt":
		return nil, nil
	case "mutated command":
		c.Account.UserID = "other"
		v.ID = "other"
	case "wrong ID":
		v.ID = "other"
	case "wrong email":
		v.Email = "changed@example.test"
	case "wrong revision":
		v.EmailRevision++
	case "wrong type":
		v.Type = "other"
	case "wrong status":
		v.Status = "other"
	case "nil metadata":
		v.Metadata = nil
	case "wrong updated":
		v.Metadata.UpdatedAt = "other"
	case "wrong status timestamp":
		v.Metadata.StatusChangedAt = "other"
	case "wrong activation":
		v.Metadata.ActivatedAt = "other"
	case "nil verification":
		v.Verification = nil
	case "wrong verified":
		v.Verification.EmailVerified = true
	case "wrong verification timestamp":
		v.Verification.EmailVerifiedAt = "other"
	case "cancel write":
		p.cancel()
	}
	return v, nil
}

func TestAccountStatusDomain(t *testing.T) {
	native := fmt.Errorf("private-native-diagnostic: %w", ErrStatusUpdateConflict)
	for _, mode := range []string{"suspend", "activate", "clear verification"} {
		for _, tc := range []struct {
			name   string
			want   error
			writes int
		}{
			{"success", nil, 1}, {"legacy type", nil, 1}, {"legacy objects", nil, 1},
			{"nil service", ErrStatusUpdateUnavailable, 0}, {"nil context", ErrStatusUpdateUnavailable, 0}, {"nil repository", ErrStatusUpdateUnavailable, 0}, {"typed nil repository", ErrStatusUpdateUnavailable, 0}, {"missing capability", ErrStatusUpdateUnavailable, 0}, {"nil clock", ErrStatusUpdateUnavailable, 0}, {"zero clock", ErrStatusUpdateUnavailable, 0}, {"nil strings", ErrStatusUpdateUnavailable, 0},
			{"nil request", ErrInvalidUserBody, 0}, {"empty ID", ErrInvalidUserBody, 0}, {"empty status", ErrInvalidUserBody, 0}, {"unknown transition", ErrUserInvalidTargetStatus, 0}, {"forbidden source", ErrUserInvalidStatusTransition, 0}, {"read failure", native, 0}, {"write failure", native, 1}, {"nil stored", ErrStatusUpdateUnavailable, 0}, {"wrong stored", ErrStatusUpdateUnavailable, 0}, {"negative revision", ErrStatusUpdateUnavailable, 0},
			{"canceled", context.Canceled, 0}, {"cancel read", context.Canceled, 0}, {"cancel write", nil, 1},
			{"nil receipt", ErrStatusUpdateUnavailable, 1}, {"mutated command", ErrStatusUpdateUnavailable, 1}, {"wrong ID", ErrStatusUpdateUnavailable, 1}, {"wrong email", ErrStatusUpdateUnavailable, 1}, {"wrong revision", ErrStatusUpdateUnavailable, 1}, {"wrong type", ErrStatusUpdateUnavailable, 1}, {"wrong status", ErrStatusUpdateUnavailable, 1}, {"nil metadata", ErrStatusUpdateUnavailable, 1}, {"wrong updated", ErrStatusUpdateUnavailable, 1}, {"wrong status timestamp", ErrStatusUpdateUnavailable, 1},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p := &statusProbe{stored: genericAccount(), fault: tc.name, err: native, cancel: cancel}
				r := &UpdateUserStatusRequest{ID: "selected", DesiredStatus: "SUSPENDED"}
				if mode == "activate" {
					p.stored.Status = "PROVISIONED"
					p.stored.Verification.EmailVerified = false
					r.DesiredStatus = "ACTIVE"
				}
				if mode == "clear verification" {
					r.DesiredStatus = "EMAIL_CHANGE"
				}
				s := NewService(p, nil, nil, &DefaultIDGenerator{}, profileClock{stamp: "2026-10-03T18:00:00Z"}, &DefaultStringUtils{}, "")
				switch tc.name {
				case "legacy type":
					p.stored.Type = ""
				case "legacy objects":
					p.stored.Metadata = nil
					p.stored.Verification = nil
				case "nil service":
					s = nil
				case "nil context":
					ctx = nil
				case "nil repository":
					s.UserRepository = nil
				case "typed nil repository":
					s.UserRepository = (*statusProbe)(nil)
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
					r.ID = " "
				case "empty status":
					r.DesiredStatus = " "
				case "unknown transition":
					r.DesiredStatus = "unknown"
				case "forbidden source":
					p.stored.Status = "LOCKED_OUT"
				case "nil stored":
					p.stored = nil
				case "wrong stored":
					p.stored.ID = "other"
				case "negative revision":
					p.stored.EmailRevision = -1
				case "canceled":
					cancel()
				}
				before := copyUserForUpdate(p.stored)
				got, err := s.UpdateUserStatus(ctx, r)
				require.Equal(t, tc.want, err)
				require.Equal(t, tc.writes, p.writes)
				require.Equal(t, before, p.stored, "domain mutated repository-owned snapshot")
				if err != nil {
					require.Nil(t, got)
					return
				}
				require.Equal(t, "selected", got.User.ID)
				require.Equal(t, "default", got.User.Type)
				require.Equal(t, before.Type, p.command.Account.Type)
				require.True(t, validStatusReceipt(copyUserForStatusReceipt(got.User, before.Type), p.command))
			})
		}
	}
}

// copyUserForStatusReceipt reverses only display hydration for raw receipt checks.
func copyUserForStatusReceipt(v *UniversalUser, rawType string) *UniversalUser {
	c := *v
	c.Type = rawType
	return &c
}

func TestAccountStatusConfiguredEffects(t *testing.T) {
	for _, tc := range []struct {
		name, fault    string
		config         *UserConfig
		desired, final string
		clear          bool
		want           error
	}{
		{"default clear", "", DefaultUserConfig(), "EMAIL_CHANGE", "PROVISIONED", true, nil},
		{"service preserves verification", "", APIServiceUserConfig(), "EMAIL_CHANGE", "ACTIVE", false, nil},
		{"bad activation receipt", "wrong activation", APIServiceUserConfig(), "EMAIL_CHANGE", "ACTIVE", false, ErrStatusUpdateUnavailable},
		{"nil verification receipt", "nil verification", DefaultUserConfig(), "EMAIL_CHANGE", "PROVISIONED", true, ErrStatusUpdateUnavailable},
		{"verified receipt", "wrong verified", DefaultUserConfig(), "EMAIL_CHANGE", "PROVISIONED", true, ErrStatusUpdateUnavailable},
		{"verification stamp receipt", "wrong verification timestamp", DefaultUserConfig(), "EMAIL_CHANGE", "PROVISIONED", true, ErrStatusUpdateUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &statusProbe{stored: genericAccount(), fault: tc.fault}
			p.stored.Type = tc.config.Type
			p.stored.Verification.EmailVerifiedAt = "previous-verification"
			s := NewService(p, nil, tc.config, &DefaultIDGenerator{}, profileClock{stamp: "2026-10-03T18:00:00Z"}, &DefaultStringUtils{}, "")
			got, err := s.UpdateUserStatus(context.Background(), &UpdateUserStatusRequest{ID: "selected", DesiredStatus: tc.desired})
			require.Equal(t, tc.want, err)
			require.Equal(t, tc.clear, p.command.ClearEmailVerification)
			require.Equal(t, "previous-verification", p.command.PreviousEmailVerifiedAt)
			if err == nil {
				require.Equal(t, tc.final, got.User.Status)
				require.Equal(t, !tc.clear, got.User.Verification.EmailVerified)
			}
		})
	}
}

func TestAccountStatusRepositoryEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil receiver", ErrStatusUpdateUnavailable}, {"nil context", ErrStatusUpdateUnavailable}, {"nil store", ErrStatusUpdateUnavailable}, {"typed nil store", ErrStatusUpdateUnavailable}, {"missing capability", ErrStatusUpdateUnavailable}, {"nil command", ErrInvalidUserBody}, {"empty source", ErrInvalidUserBody}, {"pseudo destination", ErrInvalidUserBody}, {"negative revision", ErrInvalidUserBody}, {"invalid time", ErrInvalidUserBody}, {"non UTC", ErrInvalidUserBody}, {"zero time", ErrInvalidUserBody}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &emailStorageSetupProbe{}
			r := NewRepository(p)
			c := &SetAccountStatusRequest{Account: AccountSnapshot{UserID: "target", Status: "ACTIVE"}, Status: "SUSPENDED", At: "2026-10-03T18:00:00Z"}
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
				c = nil
			case "empty source":
				c.Account.Status = ""
			case "pseudo destination":
				c.Status = "EMAIL_CHANGE"
			case "negative revision":
				c.Account.EmailRevision = -1
			case "invalid time":
				c.At = "bad"
			case "non UTC":
				c.At = "2026-10-03T19:00:00+01:00"
			case "zero time":
				c.At = "0001-01-01T00:00:00Z"
			case "canceled":
				cancel()
			}
			got, err := r.SetAccountStatus(ctx, c)
			require.Equal(t, tc.want, err)
			require.Nil(t, got)
			require.Zero(t, p.initCalls)
		})
	}
}
