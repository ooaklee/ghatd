package user

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// emailLookupProbe records strict/quiet lookup and optional cancellation.
type emailLookupProbe struct {
	UserRepository
	value  *UniversalUser
	err    error
	email  string
	strict bool
	calls  int
	cancel context.CancelFunc
}

func (p *emailLookupProbe) GetUserByEmail(_ context.Context, email string, strict bool) (*UniversalUser, error) {
	p.calls++
	p.email, p.strict = email, strict
	if p.cancel != nil {
		p.cancel()
	}
	return p.value, p.err
}

func TestEmailLookupBoundaries(t *testing.T) {
	for _, strict := range []bool{true, false} {
		for _, name := range []string{"normalized", "absent", "wrapped absence", "mixed absence", "native outage", "nil result", "wrong mailbox", "empty id", "negative revision", "nil service", "nil repository", "typed nil repository", "nil context", "nil request", "empty email", "blank email", "canceled", "cancel during read", "canceled native failure"} {
			t.Run(fmt.Sprintf("strict=%t/%s", strict, name), func(t *testing.T) {
				core, logs := observer.New(zap.DebugLevel)
				ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
				defer cancel()
				p := &emailLookupProbe{value: &UniversalUser{ID: "owner", Email: "owner@example.test", Roles: []string{"USER"}, PersonalInfo: &PersonalInfo{FirstName: "Original"}}}
				s := NewService(p, nil, nil, nil, nil, nil, "")
				r := &GetUserByEmailRequest{Email: " Owner@Example.Test "}
				var want error
				calls := 1
				switch name {
				case "absent":
					p.err = ErrUserNotFound
					want = p.err
				case "wrapped absence":
					p.err = fmt.Errorf("private diagnostic: %w", ErrUserNotFound)
					want = p.err
				case "mixed absence":
					p.err = errors.Join(ErrUserNotFound, errors.New("private diagnostic"))
					want = p.err
				case "native outage":
					p.err = errors.New("private diagnostic")
					want = p.err
				case "nil result":
					p.value = nil
					want = ErrDatabaseError
				case "wrong mailbox":
					p.value.Email = "foreign@example.test"
					want = ErrDatabaseError
				case "empty id":
					p.value.ID = ""
					want = ErrDatabaseError
				case "negative revision":
					p.value.EmailRevision = -1
					want = ErrDatabaseError
				case "nil service":
					s = nil
					want = ErrDatabaseError
					calls = 0
				case "nil repository":
					s.UserRepository = nil
					want = ErrDatabaseError
					calls = 0
				case "typed nil repository":
					s.UserRepository = (*emailLookupProbe)(nil)
					want = ErrDatabaseError
					calls = 0
				case "nil context":
					ctx = nil
					want = ErrDatabaseError
					calls = 0
				case "nil request":
					r = nil
					want = ErrInvalidEmail
					calls = 0
				case "empty email":
					r.Email = ""
					want = ErrInvalidEmail
					calls = 0
				case "blank email":
					r.Email = " \t"
					want = ErrInvalidEmail
					calls = 0
				case "canceled":
					cancel()
					want = context.Canceled
					calls = 0
				case "cancel during read":
					p.cancel = cancel
					want = context.Canceled
				case "canceled native failure":
					p.cancel = cancel
					p.err = errors.New("private diagnostic")
					want = p.err
				}
				var got *GetUserByEmailResponse
				var err error
				if strict {
					got, err = s.GetUserByEmail(ctx, r)
				} else {
					got, err = s.FindUserByEmail(ctx, r)
				}
				require.Equal(t, want, err, "preserve exact native error tree")
				require.Equal(t, calls, p.calls)
				if calls > 0 {
					require.Equal(t, "owner@example.test", p.email)
					require.Equal(t, strict, p.strict)
				}
				require.Zero(t, logs.Len(), "do not duplicate repository diagnostics or expose mailbox")
				if want != nil {
					require.Nil(t, got)
					return
				}
				require.Equal(t, "owner", got.User.ID)
				require.NotSame(t, p.value, got.User)
				require.NotNil(t, got.User.config)
				require.Nil(t, p.value.config)
				got.User.Roles[0] = "changed"
				got.User.PersonalInfo.FirstName = "changed"
				require.Equal(t, "USER", p.value.Roles[0])
				require.Equal(t, "Original", p.value.PersonalInfo.FirstName)
			})
		}
	}
}
