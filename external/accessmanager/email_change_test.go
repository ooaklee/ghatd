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

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/audit"
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

// Ports record only the operations needed by the new command. Embedded legacy
// interfaces make an accidental fallback/extra logout fail rather than succeed.
type emailUsersProbe struct {
	UserService
	accounts       map[string]*user.UniversalUser
	calls          []string
	write          *user.ChangeUserEmailRequest
	err            error
	cancel         context.CancelFunc
	invalidReceipt bool
}

func (p *emailUsersProbe) GetUserByID(_ context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	p.calls = append(p.calls, r.ID)
	return &user.GetUserByIDResponse{User: p.accounts[r.ID]}, nil
}
func (p *emailUsersProbe) ChangeUserEmail(_ context.Context, r *user.ChangeUserEmailRequest) (*user.UniversalUser, error) {
	copy := *r
	p.write = &copy
	if p.err != nil {
		return nil, p.err
	}
	account := *p.accounts[r.UserID]
	account.Email = strings.ToLower(strings.TrimSpace(r.Email))
	account.EmailRevision++
	account.Status = "PROVISIONED"
	account.Verification = &user.VerificationStatus{}
	if p.cancel != nil {
		p.cancel()
	}
	if p.invalidReceipt {
		account.ID = "foreign"
	}
	return &account, nil
}

type emailCacheProbe struct {
	EphemeralStore
	owner                  string
	exemptions             []string
	cleanupErr, errorStore error
	operations             []string
}

func (p *emailCacheProbe) DeleteAllTokenExceptedSpecified(_ context.Context, id string, exemptions []string) error {
	p.owner = id
	p.exemptions = append([]string{}, exemptions...)
	p.operations = append(p.operations, "cleanup")
	return p.cleanupErr
}
func (p *emailCacheProbe) StoreToken(context.Context, string, string, time.Duration) error {
	p.operations = append(p.operations, "store")
	return p.errorStore
}
func (p *emailCacheProbe) CodeExists(context.Context, string) (bool, error)       { return false, nil }
func (p *emailCacheProbe) StoreCode(context.Context, string, time.Duration) error { return nil }
func (p *emailCacheProbe) StoreCodeMapping(context.Context, string, string, time.Duration) error {
	p.operations = append(p.operations, "code")
	return nil
}

type emailAuthProbe struct {
	AuthService
	revision  int64
	err       error
	nilResult bool
}

func (p *emailAuthProbe) CreateEmailVerificationToken(_ context.Context, model auth.UserModel) (*auth.TokenDetails, error) {
	p.revision = model.(*user.UniversalUser).EmailRevision
	if p.err != nil || p.nilResult {
		return nil, p.err
	}
	return &auth.TokenDetails{EmailVerificationToken: "private-proof", EmailVerificationUUID: "proof-id", EvTTL: time.Minute}, nil
}

type emailMailProbe struct {
	EmailManager
	notice       *emailmanager.SendCustomEmailRequest
	verification *emailmanager.SendVerificationEmailRequest
	err          error
}

func (p *emailMailProbe) SendCustomEmail(_ context.Context, r *emailmanager.SendCustomEmailRequest) error {
	p.notice = r
	return p.err
}
func (p *emailMailProbe) SendVerificationEmail(_ context.Context, r *emailmanager.SendVerificationEmailRequest) error {
	p.verification = r
	return p.err
}

type emailAuditProbe struct {
	event *audit.LogAuditEventRequest
	err   error
}

func (p *emailAuditProbe) LogAuditEvent(_ context.Context, r *audit.LogAuditEventRequest) error {
	p.event = r
	return p.err
}

func TestEmailChangeManagerAuthorityAndCompletion(t *testing.T) {
	for _, kind := range []string{"self", "admin", "nonadmin", "inactive admin", "inactive self", "missing target", "missing actor", "bare context", "contradictory context", "anonymous context", "missing capability", "typed nil service", "native write failure", "invalid receipt", "cleanup failure", "mail failure", "audit failure", "proof failure", "nil proof", "late cancellation", "canceled", "nil request", "nil context"} {
		t.Run(kind, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
			defer cancel()
			base := ctx
			ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "actor"), true)
			actor := &user.UniversalUser{ID: "actor", Email: "actor@example.test", Type: "default", Status: "ACTIVE", Roles: []string{"ADMIN"}, EmailRevision: 4, Verification: &user.VerificationStatus{EmailVerified: true}}
			target := &user.UniversalUser{ID: "target", Email: "old@example.test", Type: "default", Status: "ACTIVE", EmailRevision: 2, Verification: &user.VerificationStatus{EmailVerified: true}}
			users := &emailUsersProbe{accounts: map[string]*user.UniversalUser{"actor": actor, "target": target}}
			cache, mail, signer, auditor := &emailCacheProbe{}, &emailMailProbe{}, &emailAuthProbe{}, &emailAuditProbe{}
			s := &Service{UserService: users, EphemeralStore: cache, EmailManager: mail, AuthService: signer, AuditService: auditor}
			req := &UpdateUserEmailRequest{ActorID: "actor", TargetUserID: "target", Email: " NEW@example.test "}
			failure := errors.New("private-driver-diagnostic")
			var want error
			switch kind {
			case "bare context":
				ctx = base
				want = ErrUnauthorizedUnableToAttainRequestorID
			case "self":
				req.TargetUserID = "actor"
			case "nonadmin":
				actor.Roles = nil
				want = ErrForbiddenUnableToAction
			case "inactive admin":
				actor.Status = "SUSPENDED"
				want = ErrForbiddenUnableToAction
			case "inactive self":
				actor.Status = "SUSPENDED"
				req.TargetUserID = "actor"
				want = ErrForbiddenUnableToAction
			case "missing target":
				delete(users.accounts, "target")
				want = user.ErrEmailChangeUnavailable
			case "missing actor":
				delete(users.accounts, "actor")
				want = user.ErrEmailChangeUnavailable
			case "contradictory context":
				ctx = context.WithValue(ctx, accesshelpers.RequestorKey, "foreign")
				want = ErrUnauthorizedUnableToAttainRequestorID
			case "anonymous context":
				ctx = context.WithValue(ctx, accesshelpers.RequestorAuthenticatedKey, false)
				want = ErrUnauthorizedUnableToAttainRequestorID
			case "missing capability":
				s.UserService = struct{ UserService }{users}
				want = user.ErrEmailChangeUnavailable
			case "typed nil service":
				s.UserService = (*emailUsersProbe)(nil)
				want = user.ErrEmailChangeUnavailable
			case "native write failure":
				users.err = failure
				want = failure
			case "invalid receipt":
				users.invalidReceipt = true
				want = user.ErrEmailChangeUnavailable
			case "cleanup failure":
				cache.cleanupErr = failure
			case "mail failure":
				mail.err = failure
			case "audit failure":
				auditor.err = failure
			case "proof failure":
				signer.err = failure
			case "nil proof":
				signer.nilResult = true
			case "late cancellation":
				users.cancel = cancel
			case "canceled":
				cancel()
				want = context.Canceled
			case "nil request":
				req = nil
				want = ErrBadRequest
			case "nil context":
				ctx = nil
				want = ErrBadRequest
			}
			response, err := s.UpdateUserEmail(ctx, req)
			require.Equal(t, want, err)
			if want != nil {
				require.Nil(t, response)
				require.Empty(t, cache.operations)
				require.Nil(t, mail.notice)
				require.Nil(t, auditor.event)
			} else {
				require.True(t, response.Changed)
				require.Equal(t, kind == "self", response.SignOutRequired)
				require.Equal(t, kind != "cleanup failure" && kind != "late cancellation", response.SessionCleanupComplete)
				require.Equal(t, kind != "mail failure" && kind != "proof failure" && kind != "nil proof" && kind != "late cancellation", response.VerificationEmailSent)
				require.Equal(t, kind != "mail failure" && kind != "late cancellation", response.PreviousAddressNotified)
				require.Equal(t, kind != "audit failure" && kind != "late cancellation", response.AuditRecorded)
				if kind != "late cancellation" {
					require.Equal(t, req.TargetUserID, cache.owner)
					require.Empty(t, cache.exemptions)
					require.Equal(t, req.ActorID, auditor.event.ActorId)
					require.Equal(t, req.TargetUserID, auditor.event.TargetId)
					require.NotContains(t, fmt.Sprint(auditor.event.Details), "@")
					require.Equal(t, users.write.ExpectedRevision+1, signer.revision)
				}
			}
			require.Equal(t, "actor@example.test", actor.Email)
			require.Equal(t, "old@example.test", target.Email)
			require.NotContains(t, fmt.Sprint(logs.All()), "private-driver-diagnostic")
			require.NotContains(t, fmt.Sprint(logs.All()), "private-proof")
		})
	}
}

type emailHandlerProbe struct {
	AccessmanagerService
	request *UpdateUserEmailRequest
	err     error
	result  *UpdateUserEmailResponse
}

func (p *emailHandlerProbe) UpdateUserEmail(_ context.Context, r *UpdateUserEmailRequest) (*UpdateUserEmailResponse, error) {
	copy := *r
	p.request = &copy
	return p.result, p.err
}

func TestEmailChangeHTTPMappingAndCookies(t *testing.T) {
	for _, kind := range []string{"self", "admin", "anonymous", "id only", "missing id", "forged body", "invalid json", "missing target", "native wrapped", "mixed failure", "host override", "nil result", "wrong signout receipt"} {
		t.Run(kind, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(context.Background(), zap.New(core))
			ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "actor"), true)
			target := "actor"
			body := `{"email":"new@example.test"}`
			status := 200
			probe := &emailHandlerProbe{result: &UpdateUserEmailResponse{Changed: true, SignOutRequired: true}}
			switch kind {
			case "admin":
				target = "target"
				probe.result.SignOutRequired = false
			case "anonymous":
				ctx = accesshelpers.TransitAuthenticatedWith(ctx, false)
				status = 401
			case "id only":
				ctx = accesshelpers.TransitWith(context.Background(), "actor")
				status = 401
			case "missing id":
				ctx = accesshelpers.TransitAuthenticatedWith(context.Background(), true)
				status = 401
			case "forged body":
				body = `{"email":"new@example.test","ActorID":"forged","TargetUserID":"forged","UserId":"forged","AuthToken":"secret"}`
			case "invalid json":
				body = "{"
				status = 400
			case "missing target":
				target = ""
				status = 400
			case "native wrapped", "host override":
				probe.err = fmt.Errorf("private-diagnostic: %w", user.ErrEmailChangeConflict)
				status = 409
				if kind == "host override" {
					status = 412
				}
			case "mixed failure":
				probe.err = errors.Join(user.ErrEmailChangeConflict, errors.New("private-diagnostic"))
				status = 500
			case "nil result":
				probe.result = nil
				status = 503
			case "wrong signout receipt":
				probe.result.SignOutRequired = false
				status = 503
			}
			h := NewHandler(&NewHandlerRequest{Service: probe, Validator: validator.NewValidator(), CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh"})
			if kind == "host override" {
				h.errorMaps = []reply.ErrorManifest{{user.ErrEmailChangeConflict: {Title: "Reload", StatusCode: 412, Code: "HOST_EMAIL"}}}
			}
			r := httptest.NewRequest(http.MethodPatch, "/users/"+target+"/email?ActorID=forged", strings.NewReader(body)).WithContext(ctx)
			r = mux.SetURLVars(r, map[string]string{UserURIVariableID: target})
			w := httptest.NewRecorder()
			h.UpdateUserEmail(w, r)
			require.Equal(t, status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.NotContains(t, w.Body.String()+fmt.Sprint(logs.All()), "private-diagnostic")
			if status == 200 && target == "actor" {
				require.Len(t, w.Result().Cookies(), 4)
			} else {
				require.Empty(t, w.Result().Cookies())
			}
			if probe.request != nil {
				require.Equal(t, "actor", probe.request.ActorID)
				require.Equal(t, target, probe.request.TargetUserID)
			}
		})
	}
}
