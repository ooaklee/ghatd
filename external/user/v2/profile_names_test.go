package user

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// profileNamesProbe records calls without standing in for Mongo atomicity.
type profileNamesProbe struct {
	UserRepository
	stored            *UniversalUser
	readErr, writeErr error
	fault             string
	reads, writes     int
	command           SetProfileNamesRequest
	cancel            context.CancelFunc
}

// profileClock makes timestamp receipt checks deterministic, including bad wiring.
type profileClock struct {
	TimeProvider
	stamp string
}

func (p profileClock) NowUTC() string { return p.stamp }
func (p profileClock) Now() time.Time {
	value, _ := time.Parse(time.RFC3339Nano, p.stamp)
	return value
}

func (p *profileNamesProbe) GetUserByID(context.Context, string) (*UniversalUser, error) {
	p.reads++
	if p.fault == "cancel read" {
		p.cancel()
	}
	return p.stored, p.readErr
}

func (p *profileNamesProbe) SetProfileNames(_ context.Context, r *SetProfileNamesRequest) (*UniversalUser, error) {
	p.writes++
	p.command = *r
	if p.writeErr != nil {
		return nil, p.writeErr
	}
	v := *p.stored
	v.PersonalInfo = &PersonalInfo{FirstName: r.After.FirstName, LastName: r.After.LastName, FullName: r.After.FullName}
	v.Metadata = &UserMetadata{UpdatedAt: r.UpdatedAt}
	switch p.fault {
	case "nil receipt":
		return nil, nil
	case "owner receipt":
		v.ID = "other"
	case "email receipt":
		v.Email = "other@example.test"
	case "revision receipt":
		v.EmailRevision++
	case "status receipt":
		v.Status = "SUSPENDED"
	case "type receipt":
		v.Type = "other"
	case "names receipt":
		v.PersonalInfo.FirstName = "wrong"
	case "stamp receipt":
		v.Metadata.UpdatedAt = "wrong"
	case "nil metadata":
		v.Metadata = nil
	case "mutate command":
		r.Account.UserID = "other"
		v.ID = "other"
	case "cancel after write":
		p.cancel()
	}
	return &v, nil
}

func TestProfileNamesDomain(t *testing.T) {
	native := fmt.Errorf("private-diagnostic: %w", ErrProfileUpdateConflict)
	for _, tc := range []struct {
		name   string
		want   error
		writes int
	}{
		{"updated", nil, 1}, {"empty no-op", nil, 0}, {"equal no-op", nil, 0}, {"normalized no-op", nil, 0}, {"legacy type", nil, 1},
		{"nil strings", ErrProfileUpdateUnavailable, 0}, {"typed nil strings", ErrProfileUpdateUnavailable, 0}, {"invalid clock", ErrProfileUpdateUnavailable, 0}, {"fixed clock", nil, 1},
		{"nil request", ErrInvalidUserBody, 0}, {"empty owner", ErrInvalidUserBody, 0}, {"negative revision", ErrInvalidUserBody, 0},
		{"nil service", ErrProfileUpdateUnavailable, 0}, {"nil context", ErrProfileUpdateUnavailable, 0}, {"nil repository", ErrProfileUpdateUnavailable, 0}, {"typed nil", ErrProfileUpdateUnavailable, 0}, {"missing capability", ErrProfileUpdateUnavailable, 0},
		{"canceled", context.Canceled, 0}, {"cancel read", context.Canceled, 0}, {"cancel after write", nil, 1},
		{"read error", native, 0}, {"write error", native, 1}, {"nil stored", ErrProfileUpdateUnavailable, 0}, {"wrong stored", ErrProfileUpdateUnavailable, 0},
		{"stale email", ErrProfileUpdateConflict, 0}, {"stale revision", ErrProfileUpdateConflict, 0}, {"stale status", ErrProfileUpdateConflict, 0}, {"stale type", ErrProfileUpdateConflict, 0},
		{"invalid role", ErrUserInvalidRole, 0}, {"missing last", ErrUserRequiredFieldMissingLastName, 0},
		{"nil receipt", ErrProfileUpdateUnavailable, 1}, {"owner receipt", ErrProfileUpdateUnavailable, 1}, {"email receipt", ErrProfileUpdateUnavailable, 1}, {"revision receipt", ErrProfileUpdateUnavailable, 1}, {"status receipt", ErrProfileUpdateUnavailable, 1}, {"type receipt", ErrProfileUpdateUnavailable, 1}, {"names receipt", ErrProfileUpdateUnavailable, 1}, {"stamp receipt", ErrProfileUpdateUnavailable, 1}, {"nil metadata", ErrProfileUpdateUnavailable, 1}, {"mutate command", ErrProfileUpdateUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stored := &UniversalUser{ID: "owner", Email: "owner@example.test", Type: "default", Status: "ACTIVE", EmailRevision: 2, PersonalInfo: &PersonalInfo{FirstName: "Old", LastName: "Name", FullName: "Old Name"}, Roles: []string{}, Metadata: &UserMetadata{CreatedAt: "original", UpdatedAt: "old", CustomTimestamps: map[string]string{"keep": "value"}}}
			p := &profileNamesProbe{stored: stored, fault: tc.name, cancel: cancel}
			s := NewService(p, nil, nil, &DefaultIDGenerator{}, &DefaultTimeProvider{}, &DefaultStringUtils{}, "")
			r := &UpdateProfileNamesRequest{UserID: "owner", FirstName: "new", ExpectedEmail: stored.Email, ExpectedRevision: 2, ExpectedType: "default", ExpectedStatus: "ACTIVE"}
			switch tc.name {
			case "nil strings":
				s.StringUtils = nil
			case "typed nil strings":
				s.StringUtils = (*DefaultStringUtils)(nil)
			case "invalid clock":
				s.TimeProvider = profileClock{stamp: "private-diagnostic"}
			case "fixed clock":
				s.TimeProvider = profileClock{stamp: "2026-01-01T00:00:00Z"}
			case "empty no-op":
				r.FirstName = ""
			case "equal no-op":
				r.FirstName = "Old"
			case "normalized no-op":
				r.FirstName = "old"
			case "legacy type":
				stored.Type = ""
			case "nil request":
				r = nil
			case "empty owner":
				r.UserID = " "
			case "negative revision":
				r.ExpectedRevision = -1
			case "nil service":
				s = nil
			case "nil context":
				ctx = nil
			case "nil repository":
				s.UserRepository = nil
			case "typed nil":
				s.UserRepository = (*profileNamesProbe)(nil)
			case "missing capability":
				s.UserRepository = struct{ UserRepository }{p}
			case "canceled":
				cancel()
			case "read error":
				p.readErr = native
			case "write error":
				p.writeErr = native
			case "nil stored":
				p.stored = nil
			case "wrong stored":
				stored.ID = "other"
			case "stale email":
				r.ExpectedEmail = "stale@example.test"
			case "stale revision":
				r.ExpectedRevision = 1
			case "stale status":
				r.ExpectedStatus = "SUSPENDED"
			case "stale type":
				r.ExpectedType = "other"
			case "invalid role":
				stored.Roles = []string{"private-role"}
			case "missing last":
				stored.PersonalInfo.LastName = ""
			}
			var before UpdateProfileNamesRequest
			if r != nil {
				before = *r
			}
			personalBefore := *stored.PersonalInfo
			metadataBefore := *stored.Metadata
			result, err := s.UpdateProfileNames(ctx, r)
			require.Equal(t, tc.want, err)
			require.Equal(t, tc.writes, p.writes)
			if r != nil {
				require.Equal(t, before, *r)
			}
			require.Equal(t, personalBefore, *stored.PersonalInfo)
			require.Equal(t, metadataBefore, *stored.Metadata)
			if err != nil {
				require.Nil(t, result)
			} else {
				require.Equal(t, "owner", result.ID)
				require.Equal(t, "default", result.Type)
			}
			if p.writes > 0 {
				if tc.name == "fixed clock" {
					require.Equal(t, "2026-01-01T00:00:00Z", p.command.UpdatedAt)
				}
				require.Equal(t, "New", p.command.After.FirstName)
				require.Equal(t, "Name", p.command.After.LastName)
				require.Equal(t, "New Name", p.command.After.FullName)
			}
		})
	}
}

// Native error trees are not classified or logged inside the domain.
func TestProfileNamesNativeFailures(t *testing.T) {
	for _, native := range []error{ErrUserNotFound, fmt.Errorf("private-diagnostic: %w", ErrProfileUpdateConflict), errors.Join(ErrProfileUpdateConflict, errors.New("private-diagnostic")), context.DeadlineExceeded} {
		t.Run(fmt.Sprintf("%T/%s", native, native.Error()), func(t *testing.T) {
			p := &profileNamesProbe{readErr: native}
			s := NewService(p, nil, nil, nil, nil, nil, "")
			_, err := s.UpdateProfileNames(context.Background(), &UpdateProfileNamesRequest{UserID: "owner", ExpectedEmail: "owner@example.test", ExpectedStatus: "ACTIVE"})
			require.Equal(t, native, err)
			require.Zero(t, p.writes)
		})
	}
}

// Repository entry checks must not connect or dispatch for incomplete commands.
func TestProfileNamesMongoEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil repository", ErrProfileUpdateUnavailable}, {"nil context", ErrProfileUpdateUnavailable}, {"nil store", ErrProfileUpdateUnavailable}, {"typed nil store", ErrProfileUpdateUnavailable}, {"nil request", ErrInvalidUserBody}, {"blank owner", ErrInvalidUserBody}, {"missing mailbox", ErrInvalidUserBody}, {"missing status", ErrInvalidUserBody}, {"negative revision", ErrInvalidUserBody}, {"bad timestamp", ErrInvalidUserBody}, {"missing atomic capability", ErrProfileUpdateUnavailable}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &emailStorageSetupProbe{}
			repo := NewRepository(store)
			r := &SetProfileNamesRequest{Account: UpdateProfileNamesRequest{UserID: "owner", ExpectedEmail: "owner@example.test", ExpectedStatus: "ACTIVE"}, UpdatedAt: "2026-01-01T00:00:00Z"}
			switch tc.name {
			case "nil repository":
				repo = nil
			case "nil context":
				ctx = nil
			case "nil store":
				repo.Store = nil
			case "typed nil store":
				repo.Store = (*emailStorageSetupProbe)(nil)
			case "nil request":
				r = nil
			case "blank owner":
				r.Account.UserID = " "
			case "missing mailbox":
				r.Account.ExpectedEmail = ""
			case "missing status":
				r.Account.ExpectedStatus = ""
			case "negative revision":
				r.Account.ExpectedRevision = -1
			case "bad timestamp":
				r.UpdatedAt = "private-diagnostic"
			case "canceled":
				cancel()
			}
			got, err := repo.SetProfileNames(ctx, r)
			require.Equal(t, tc.want, err)
			require.Nil(t, got)
			require.Zero(t, store.initCalls)
		})
	}
}
