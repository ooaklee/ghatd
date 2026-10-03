package accessmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// initialEmailProbe has no permissive legacy fallbacks. Each case owns its
// mutable adapters and records phase order, selected identity and cancellation.
type initialEmailProbe struct {
	UserService
	AuthService
	EphemeralStore
	EmailManager
	steps                                        []string
	fail, cancelAt                               string
	failure                                      error
	cancel                                       context.CancelFunc
	account                                      *user.GetUserByEmailResponse
	proof                                        *auth.TokenDetails
	acquired                                     bool
	mutate                                       bool
	onLookup                                     func()
	lookup, owner, tokenID, mapped, releaseOwner string
	mail                                         *emailmanager.SendLoginEmailRequest
	verification                                 *emailmanager.SendVerificationEmailRequest
}

func (p *initialEmailProbe) step(name string) error {
	p.steps = append(p.steps, name)
	if p.cancelAt == name {
		p.cancel()
	}
	if p.fail == name {
		return p.failure
	}
	return nil
}
func (p *initialEmailProbe) GetUserByEmail(_ context.Context, r *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	p.lookup = r.Email
	if p.onLookup != nil {
		p.onLookup()
	}
	return p.account, p.step("lookup")
}
func (p *initialEmailProbe) AcquireLoginEmailCooldown(_ context.Context, owner string, _ bool, _ string, _ time.Duration) (bool, error) {
	p.owner = owner
	return p.acquired, p.step("cooldown")
}
func (p *initialEmailProbe) ReleaseLoginEmailCooldown(_ context.Context, owner string, _ bool, _ string) (int64, error) {
	p.releaseOwner = owner
	return 1, p.step("release")
}
func (p *initialEmailProbe) CreateInitalToken(_ context.Context, m auth.UserModel) (*auth.TokenDetails, error) {
	if p.mutate {
		v := m.(*user.UniversalUser)
		v.ID = "foreign"
		v.Email = "foreign@example.test"
		v.Roles[0] = "ADMIN"
		v.PersonalInfo.FirstName = "foreign"
		v.Metadata.CustomTimestamps["test"] = "foreign"
		v.Extensions["test"] = "foreign"
	}
	return p.proof, p.step("sign")
}
func (p *initialEmailProbe) CreateEmailVerificationToken(context.Context, auth.UserModel) (*auth.TokenDetails, error) {
	return p.proof, p.step("verify-sign")
}
func (p *initialEmailProbe) StoreToken(_ context.Context, id, owner string, _ time.Duration) error {
	p.tokenID, p.owner = id, owner
	if p.mutate {
		p.proof.EphemeralToken = "changed-by-store"
	}
	return p.step("token")
}
func (p *initialEmailProbe) CodeExists(context.Context, string) (bool, error) {
	return false, p.step("exists")
}
func (p *initialEmailProbe) StoreCode(context.Context, string, time.Duration) error {
	return p.step("reserve")
}
func (p *initialEmailProbe) StoreCodeMapping(_ context.Context, _ string, token string, _ time.Duration) error {
	p.mapped = token
	return p.step("mapping")
}
func (p *initialEmailProbe) SendLoginEmail(_ context.Context, r *emailmanager.SendLoginEmailRequest) error {
	x := *r
	p.mail = &x
	return p.step("mail")
}
func (p *initialEmailProbe) SendVerificationEmail(_ context.Context, r *emailmanager.SendVerificationEmailRequest) error {
	x := *r
	p.verification = &x
	return p.step("verify-mail")
}

func initialEmailFixture() (*initialEmailProbe, *Service, *user.UniversalUser) {
	u := &user.UniversalUser{ID: "owner", Email: "owner@example.test", Status: "ACTIVE", EmailRevision: 2, Roles: []string{"USER"}, PersonalInfo: &user.PersonalInfo{FirstName: "Original"}, Metadata: &user.UserMetadata{CustomTimestamps: map[string]string{"test": "original"}}, Extensions: map[string]any{"test": "original"}}
	p := &initialEmailProbe{account: &user.GetUserByEmailResponse{User: u}, acquired: true, proof: &auth.TokenDetails{EphemeralUUID: "initial-id", EphemeralToken: "private-proof", EtTTL: time.Minute, EmailVerificationUUID: "verify-id", EmailVerificationToken: "private-verify-proof", EvTTL: time.Minute}}
	return p, &Service{UserService: p, AuthService: p, EphemeralStore: p, EmailManager: p}, u
}

func TestInitialEmailDispatch(t *testing.T) {
	for _, name := range []string{"active", "provisioned", "restricted", "missing", "native outage", "nil receipt", "nil account", "wrong mailbox", "empty owner", "negative revision", "nil service", "typed nil users", "nil context", "nil request", "empty email", "already canceled", "canceled lookup", "request mutation"} {
		t.Run(name, func(t *testing.T) {
			p, s, u := initialEmailFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &CreateInitalLoginOrVerificationTokenEmailRequest{Email: " Owner@Example.Test ", Dashboard: true, Mobile: true, RequestUrl: "/original"}
			var want error
			steps := []string{"lookup", "cooldown", "sign", "token", "exists", "reserve", "mapping", "mail"}
			switch name {
			case "provisioned":
				u.Status = "PROVISIONED"
				steps = []string{"lookup", "verify-sign", "token", "exists", "reserve", "mapping", "verify-mail"}
			case "restricted":
				u.Status = "SUSPENDED"
				want = ErrUserStatusUncaught
				steps = []string{"lookup"}
			case "missing":
				p.fail = "lookup"
				p.failure = user.ErrUserNotFound
				want = p.failure
				steps = []string{"lookup"}
			case "native outage":
				p.fail = "lookup"
				p.failure = errors.New("private-driver")
				want = p.failure
				steps = []string{"lookup"}
			case "nil receipt":
				p.account = nil
				want = ErrLoginEmailUnavailable
				steps = []string{"lookup"}
			case "nil account":
				p.account.User = nil
				want = ErrLoginEmailUnavailable
				steps = []string{"lookup"}
			case "wrong mailbox":
				u.Email = "foreign@example.test"
				want = ErrLoginEmailUnavailable
				steps = []string{"lookup"}
			case "empty owner":
				u.ID = ""
				want = ErrLoginEmailUnavailable
				steps = []string{"lookup"}
			case "negative revision":
				u.EmailRevision = -1
				want = ErrLoginEmailUnavailable
				steps = []string{"lookup"}
			case "nil service":
				s = nil
				want = ErrLoginEmailUnavailable
				steps = nil
			case "typed nil users":
				s.UserService = (*initialEmailProbe)(nil)
				want = ErrLoginEmailUnavailable
				steps = nil
			case "nil context":
				ctx = nil
				want = ErrInvalidUserEmail
				steps = nil
			case "nil request":
				r = nil
				want = ErrInvalidUserEmail
				steps = nil
			case "empty email":
				r.Email = ""
				want = ErrInvalidUserEmail
				steps = nil
			case "already canceled":
				cancel()
				want = context.Canceled
				steps = nil
			case "canceled lookup":
				p.cancelAt, p.cancel = "lookup", cancel
				want = context.Canceled
				steps = []string{"lookup"}
			case "request mutation":
				p.onLookup = func() { r.Email = "foreign@example.test"; r.RequestUrl = "/foreign"; r.Dashboard = false }
			}
			err := s.CreateInitalLoginOrVerificationTokenEmail(ctx, r)
			require.Equal(t, want, err)
			require.Equal(t, steps, p.steps)
			if len(steps) > 0 {
				require.Equal(t, "owner@example.test", p.lookup)
			}
			if want != nil {
				require.Nil(t, p.mail)
				require.Nil(t, p.verification)
				return
			}
			if name == "provisioned" {
				require.Equal(t, "owner@example.test", p.verification.Email)
				require.True(t, p.verification.IsDashboardRequest)
				require.Equal(t, "/original", p.verification.RequestUrl)
			} else {
				require.Equal(t, "owner@example.test", p.mail.Email)
				require.Equal(t, "/original", p.mail.RequestUrl)
				require.True(t, p.mail.IsDashboardRequest)
			}
		})
	}
}

func TestInitialEmailProofBoundaries(t *testing.T) {
	for _, name := range []string{"success", "mutating adapters", "cooldown", "nil context", "nil user", "blank id", "blank email", "negative revision", "nil service", "typed nil signer", "typed nil store", "typed nil mail", "nil proof", "empty token", "empty uuid", "zero ttl", "negative ttl", "canceled"} {
		t.Run(name, func(t *testing.T) {
			p, s, u := initialEmailFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var want error
			switch name {
			case "mutating adapters":
				p.mutate = true
			case "cooldown":
				p.acquired = false
			case "nil context":
				ctx = nil
				want = ErrBadRequest
			case "nil user":
				u = nil
				want = ErrBadRequest
			case "blank id":
				u.ID = " "
				want = ErrBadRequest
			case "blank email":
				u.Email = " "
				want = ErrBadRequest
			case "negative revision":
				u.EmailRevision = -1
				want = ErrBadRequest
			case "nil service":
				s = nil
				want = ErrLoginEmailUnavailable
			case "typed nil signer":
				s.AuthService = (*initialEmailProbe)(nil)
				want = ErrLoginEmailUnavailable
			case "typed nil store":
				s.EphemeralStore = (*initialEmailProbe)(nil)
				want = ErrLoginEmailUnavailable
			case "typed nil mail":
				s.EmailManager = (*initialEmailProbe)(nil)
				want = ErrLoginEmailUnavailable
			case "nil proof":
				p.proof = nil
				want = ErrLoginEmailUnavailable
			case "empty token":
				p.proof.EphemeralToken = ""
				want = ErrLoginEmailUnavailable
			case "empty uuid":
				p.proof.EphemeralUUID = ""
				want = ErrLoginEmailUnavailable
			case "zero ttl":
				p.proof.EtTTL = 0
				want = ErrLoginEmailUnavailable
			case "negative ttl":
				p.proof.EtTTL = -time.Second
				want = ErrLoginEmailUnavailable
			case "canceled":
				cancel()
				want = context.Canceled
			}
			got, err := s.CreateInitalLoginToken(ctx, u, true, "/original")
			require.Equal(t, want, err)
			if want != nil {
				require.Empty(t, got)
				require.Nil(t, p.mail)
				require.LessOrEqual(t, len(p.steps), 3)
				return
			}
			if name == "cooldown" {
				require.Empty(t, got)
				require.Equal(t, []string{"cooldown"}, p.steps)
				require.Nil(t, p.mail)
				return
			}
			require.Equal(t, "private-proof", got)
			require.Equal(t, "private-proof", p.mapped)
			require.Equal(t, "private-proof", p.mail.Token)
			require.Equal(t, "owner", p.mail.UserId)
			require.Equal(t, "owner@example.test", p.mail.Email)
			require.Equal(t, "initial-id", p.tokenID)
			require.Empty(t, p.releaseOwner)
			require.Len(t, p.mail.Code, 8)
			require.Equal(t, "owner", u.ID)
			require.Equal(t, "USER", u.Roles[0])
			require.Equal(t, "Original", u.PersonalInfo.FirstName)
			require.Equal(t, "original", u.Metadata.CustomTimestamps["test"])
			require.Equal(t, "original", u.Extensions["test"])
		})
	}
}

func TestInitialEmailProofPhases(t *testing.T) {
	phases := []string{"cooldown", "sign", "token", "exists", "reserve", "mapping", "mail"}
	for i, phase := range phases {
		for _, cancelPhase := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%t", phase, cancelPhase), func(t *testing.T) {
				p, s, u := initialEmailFixture()
				core, logs := observer.New(zap.DebugLevel)
				ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
				defer cancel()
				failure := fmt.Errorf("private-driver: %w", ErrBadRequest)
				want := error(failure)
				if cancelPhase {
					p.cancelAt, p.cancel = phase, cancel
					want = context.Canceled
				} else {
					p.fail, p.failure = phase, failure
				}
				got, err := s.CreateInitalLoginToken(ctx, u, false, "/private-return")
				steps := append([]string(nil), phases[:i+1]...)
				if cancelPhase && phase == "mail" {
					require.NoError(t, err)
					require.Equal(t, "private-proof", got)
				} else {
					require.Equal(t, want, err)
					require.Empty(t, got)
					if phase != "cooldown" || cancelPhase {
						steps = append(steps, "release")
						require.Equal(t, "owner", p.releaseOwner)
					}
				}
				require.Equal(t, steps, p.steps)
				for _, e := range logs.All() {
					text := fmt.Sprint(e.Message, e.ContextMap())
					for _, secret := range []string{"private-driver", "private-proof", "owner@example.test", "/private-return"} {
						require.NotContains(t, text, secret)
					}
				}
			})
		}
	}
}

// initialEmailHandlerProbe records the transport/service context without
// dispatching to real mail providers or storage.
type initialEmailHandlerProbe struct {
	AccessmanagerService
	err   error
	calls int
	ctx   context.Context
}

func (p *initialEmailHandlerProbe) CreateInitalLoginOrVerificationTokenEmail(ctx context.Context, _ *CreateInitalLoginOrVerificationTokenEmailRequest) error {
	p.calls++
	p.ctx = ctx
	return p.err
}

func TestInitialEmailHTTPContract(t *testing.T) {
	for _, name := range []string{"accepted", "absent", "restricted", "unavailable", "private outage", "wrapped", "joined", "canceled", "nil service", "typed nil service", "invalid json", "null", "missing email", "invalid email", "nil validator", "typed nil validator", "host mapping"} {
		t.Run(name, func(t *testing.T) {
			p := &initialEmailHandlerProbe{}
			h := NewHandler(&NewHandlerRequest{Service: p, Validator: validator.NewValidator()})
			body := `{"email":"private-mailbox@example.test","request_url":"/private-return"}`
			status, calls := 202, 1
			code := ""
			switch name {
			case "absent":
				p.err = user.ErrUserNotFound
			case "restricted":
				p.err = ErrUserStatusUncaught
			case "unavailable":
				p.err = ErrLoginEmailUnavailable
			case "private outage":
				p.err = errors.New("private-driver")
			case "wrapped":
				p.err = fmt.Errorf("private-driver: %w", ErrBadRequest)
			case "joined":
				p.err = errors.Join(ErrBadRequest, errors.New("private-driver"))
			case "canceled":
				p.err = context.Canceled
			case "nil service":
				h.Service = nil
				calls = 0
			case "typed nil service":
				h.Service = (*initialEmailHandlerProbe)(nil)
				calls = 0
			case "invalid json":
				body = "{"
				status, calls, code = 400, 0, "AM00-005"
			case "null":
				body = "null"
				status, calls, code = 400, 0, "AM00-005"
			case "missing email":
				body = "{}"
				status, calls, code = 400, 0, "AM00-005"
			case "invalid email":
				body = `{"email":"not-an-email"}`
				status, calls, code = 400, 0, "AM00-005"
			case "nil validator":
				h.Validator = nil
				status, calls, code = 503, 0, "AM00-041"
			case "typed nil validator":
				h.Validator = (*validator.Validator)(nil)
				status, calls, code = 503, 0, "AM00-041"
			case "host mapping":
				body = "{}"
				h.errorMaps = []reply.ErrorManifest{{ErrInvalidUserEmail: {Title: "Invalid email", StatusCode: 422, Code: "HOST_EMAIL"}}}
				status, calls, code = 422, 0, "HOST_EMAIL"
			}
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(context.Background(), zap.New(core))
			r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body)).WithContext(ctx)
			w := httptest.NewRecorder()
			h.CreateInitalLoginOrVerificationTokenEmail(w, r)
			require.Equal(t, status, w.Code, w.Body.String())
			require.Equal(t, calls, p.calls)
			if calls > 0 {
				require.Equal(t, ctx, p.ctx)
			}
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Empty(t, w.Result().Cookies())
			if status == 202 {
				require.JSONEq(t, `{"data":"{}"}`, w.Body.String())
			} else {
				require.Contains(t, w.Body.String(), code)
			}
			for _, secret := range []string{"private-driver", "private-mailbox", "private-return"} {
				require.NotContains(t, w.Body.String()+fmt.Sprint(logs.All()), secret)
			}
		})
	}
}

func TestInitialEmailMapperEntry(t *testing.T) {
	for _, name := range []string{"nil request", "nil url", "nil body"} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"owner@example.test"}`))
			switch name {
			case "nil request":
				r = nil
			case "nil url":
				r.URL = nil
			case "nil body":
				r.Body = nil
			}
			got, err := MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(r, validator.NewValidator())
			require.Nil(t, got)
			require.Equal(t, ErrInvalidUserEmail, err)
		})
	}
}
