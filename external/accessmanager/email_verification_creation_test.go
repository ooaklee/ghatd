package accessmanager

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// verificationCreationProbe owns each case's adapters and records phase order.
// Unexpected legacy operations on embedded interfaces panic rather than pass.
type verificationCreationProbe struct {
	AuthService
	EphemeralStore
	EmailManager
	steps             []string
	fail, cancelAt    string
	failure           error
	cancel            context.CancelFunc
	proof             *auth.TokenDetails
	mutate            bool
	owner, id, mapped string
	mail              *emailmanager.SendVerificationEmailRequest
}

func (p *verificationCreationProbe) step(name string) error {
	p.steps = append(p.steps, name)
	if p.cancelAt == name {
		p.cancel()
	}
	if p.fail == name {
		return p.failure
	}
	return nil
}
func (p *verificationCreationProbe) CreateEmailVerificationToken(_ context.Context, m auth.UserModel) (*auth.TokenDetails, error) {
	if p.mutate {
		u := m.(*user.UniversalUser)
		u.ID, u.Email = "foreign", "foreign@example.test"
		if u.PersonalInfo != nil {
			u.PersonalInfo.FirstName = "foreign"
		}
	}
	return p.proof, p.step("sign")
}
func (p *verificationCreationProbe) StoreToken(_ context.Context, id, owner string, ttl time.Duration) error {
	p.id, p.owner = id, owner
	if p.mutate {
		p.proof.EmailVerificationToken = "adapter-mutation"
	}
	return p.step("token")
}
func (p *verificationCreationProbe) CodeExists(context.Context, string) (bool, error) {
	return false, p.step("exists")
}
func (p *verificationCreationProbe) StoreCode(context.Context, string, time.Duration) error {
	return p.step("reserve")
}
func (p *verificationCreationProbe) StoreCodeMapping(_ context.Context, code, proof string, ttl time.Duration) error {
	p.mapped = proof
	return p.step("mapping")
}
func (p *verificationCreationProbe) SendVerificationEmail(_ context.Context, r *emailmanager.SendVerificationEmailRequest) error {
	copy := *r
	p.mail = &copy
	return p.step("mail")
}

func TestEmailVerificationCreationBoundaries(t *testing.T) {
	for _, kind := range []string{"success", "nil personal info", "adapter mutation", "nil context", "nil request", "nil user", "empty owner", "empty email", "negative revision", "nil service", "typed nil signer", "typed nil store", "typed nil mail", "nil proof", "empty token", "empty uuid", "zero ttl", "negative ttl", "already canceled"} {
		t.Run(kind, func(t *testing.T) {
			p, s, r := verificationCreationFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := error(nil)
			switch kind {
			case "nil personal info":
				r.User.PersonalInfo = nil
			case "adapter mutation":
				p.mutate = true
			case "nil context":
				ctx = nil
				want = ErrBadRequest
			case "nil request":
				r = nil
				want = ErrBadRequest
			case "nil user":
				r.User = nil
				want = ErrBadRequest
			case "empty owner":
				r.User.ID = ""
				want = ErrBadRequest
			case "empty email":
				r.User.Email = ""
				want = ErrBadRequest
			case "negative revision":
				r.User.EmailRevision = -1
				want = ErrBadRequest
			case "nil service":
				s = nil
				want = user.ErrEmailChangeUnavailable
			case "typed nil signer":
				s.AuthService = (*verificationCreationProbe)(nil)
				want = user.ErrEmailChangeUnavailable
			case "typed nil store":
				s.EphemeralStore = (*verificationCreationProbe)(nil)
				want = user.ErrEmailChangeUnavailable
			case "typed nil mail":
				s.EmailManager = (*verificationCreationProbe)(nil)
				want = user.ErrEmailChangeUnavailable
			case "nil proof":
				p.proof = nil
				want = user.ErrEmailChangeUnavailable
			case "empty token":
				p.proof.EmailVerificationToken = ""
				want = user.ErrEmailChangeUnavailable
			case "empty uuid":
				p.proof.EmailVerificationUUID = ""
				want = user.ErrEmailChangeUnavailable
			case "zero ttl":
				p.proof.EvTTL = 0
				want = user.ErrEmailChangeUnavailable
			case "negative ttl":
				p.proof.EvTTL = -time.Second
				want = user.ErrEmailChangeUnavailable
			case "already canceled":
				cancel()
				want = context.Canceled
			}
			proof, err := s.CreateEmailVerificationToken(ctx, r)
			require.Equal(t, want, err)
			if want != nil {
				require.Empty(t, proof)
				require.LessOrEqual(t, len(p.steps), 1)
				require.Nil(t, p.mail)
				return
			}
			require.Equal(t, "private-proof", proof)
			require.Equal(t, []string{"sign", "token", "exists", "reserve", "mapping", "mail"}, p.steps)
			require.Equal(t, "owner", p.owner)
			require.Equal(t, "proof-id", p.id)
			require.Equal(t, "private-proof", p.mapped)
			require.Equal(t, "owner@example.test", p.mail.Email)
			require.Equal(t, "owner", p.mail.UserId)
			require.Equal(t, "private-proof", p.mail.Token)
			require.Len(t, p.mail.Code, 8)
			require.Equal(t, "https://example.test/verify", p.mail.RequestUrl)
			require.True(t, p.mail.IsDashboardRequest)
			require.Equal(t, "owner", r.User.ID)
			if kind == "nil personal info" {
				require.Empty(t, p.mail.FirstName)
			} else {
				require.Equal(t, "Test", p.mail.FirstName)
				require.Equal(t, "Test", r.User.PersonalInfo.FirstName)
			}
		})
	}
}

func verificationCreationFixture() (*verificationCreationProbe, *Service, *CreateEmailVerificationTokenRequest) {
	p := &verificationCreationProbe{proof: &auth.TokenDetails{EmailVerificationToken: "private-proof", EmailVerificationUUID: "proof-id", EvTTL: time.Minute}}
	s := &Service{AuthService: p, EphemeralStore: p, EmailManager: p}
	r := &CreateEmailVerificationTokenRequest{User: &user.UniversalUser{ID: "owner", Email: "owner@example.test", EmailRevision: 2, PersonalInfo: &user.PersonalInfo{FirstName: "Test"}}, RequestUrl: "https://example.test/verify", IsDashboardRequest: true}
	return p, s, r
}

func TestEmailVerificationCreationPhaseFailures(t *testing.T) {
	phases := []string{"sign", "token", "exists", "reserve", "mapping", "mail"}
	for i, phase := range phases {
		for _, cancelPhase := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%t", phase, cancelPhase), func(t *testing.T) {
				p, s, r := verificationCreationFixture()
				core, logs := observer.New(zap.DebugLevel)
				ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
				defer cancel()
				failure := errors.New("private-driver-diagnostic")
				want := error(failure)
				if cancelPhase {
					p.cancelAt, p.cancel = phase, cancel
					want = context.Canceled
				} else {
					p.fail, p.failure = phase, failure
				}
				proof, err := s.CreateEmailVerificationToken(ctx, r)
				if cancelPhase && phase == "mail" {
					require.NoError(t, err)
					require.Equal(t, "private-proof", proof)
				} else {
					require.Equal(t, want, err)
					require.Empty(t, proof)
				}
				require.Equal(t, phases[:i+1], p.steps, "nothing after the failing phase may run")
				for _, entry := range logs.All() {
					message := fmt.Sprint(entry.Message, entry.ContextMap())
					require.NotContains(t, message, "private-driver-diagnostic")
					require.NotContains(t, message, "private-proof")
				}
			})
		}
	}
}
