package usermanager_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// statusAuthority is a point-in-time verifier probe; real credential and live
// revocation checks are exercised in the routed accessmanager integration table.
type statusAuthority struct {
	actor            string
	err              error
	calls, denyAfter int
	cancel           context.CancelFunc
}

func (a *statusAuthority) AuthorizeAdministrator(context.Context) (string, error) {
	a.calls++
	if a.cancel != nil {
		a.cancel()
	}
	if a.denyAfter > 0 && a.calls > a.denyAfter {
		return "", router.ErrRouteDenied
	}
	return a.actor, a.err
}
func (*statusAuthority) AdministratorErrorMaps() []reply.ErrorManifest {
	return []reply.ErrorManifest{router.PolicyErrorManifest()}
}

// statusUsers implements only the narrow domain method; broad fallback panics.
type statusUsers struct {
	usermanager.UserService
	calls  int
	ids    []string
	fault  string
	err    error
	cancel context.CancelFunc
}

func (p *statusUsers) UpdateUserStatus(_ context.Context, r *user.UpdateUserStatusRequest) (*user.UpdateUserStatusResponse, error) {
	p.calls++
	p.ids = append(p.ids, r.ID)
	if p.cancel != nil {
		p.cancel()
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.fault == "second fails" && p.calls == 2 {
		return nil, user.ErrStatusUpdateConflict
	}
	if p.fault == "nil response" {
		return nil, nil
	}
	v := &user.UniversalUser{ID: r.ID, Status: r.DesiredStatus}
	if r.DesiredStatus == "EMAIL_CHANGE" {
		v.Status = "PROVISIONED"
	}
	switch p.fault {
	case "nil user":
		v = nil
	case "wrong target":
		v.ID = "other"
	case "wrong status":
		v.Status = "wrong"
	}
	return &user.UpdateUserStatusResponse{User: v}, nil
}

// statusAudit records attribution and models best-effort audit delivery failure.
type statusAudit struct {
	usermanager.AuditService
	events []audit.LogAuditEventRequest
	err    error
}

func (a *statusAudit) LogAuditEvent(_ context.Context, r *audit.LogAuditEventRequest) error {
	a.events = append(a.events, *r)
	return a.err
}

func TestAccountStatusManager(t *testing.T) {
	native := fmt.Errorf("private-store-diagnostic: %w", user.ErrStatusUpdateConflict)
	for _, tc := range []struct {
		name           string
		want           error
		writes, audits int
	}{
		{"success", nil, 1, 1}, {"self target", nil, 1, 1}, {"email change", nil, 1, 1}, {"audit outage", nil, 1, 1}, {"no audit", nil, 1, 0}, {"late cancellation", nil, 1, 1},
		{"nil manager", user.ErrStatusUpdateUnavailable, 0, 0}, {"nil context", user.ErrStatusUpdateUnavailable, 0, 0}, {"typed nil users", user.ErrStatusUpdateUnavailable, 0, 0}, {"missing capability", user.ErrStatusUpdateUnavailable, 0, 0}, {"nil authority", user.ErrStatusUpdateUnavailable, 0, 0}, {"typed nil authority", user.ErrStatusUpdateUnavailable, 0, 0}, {"wrong actor", usermanager.ErrUnableToIdentifyUser, 0, 0}, {"wrong verified actor", usermanager.ErrUnableToIdentifyUser, 0, 0}, {"denied", router.ErrRouteDenied, 0, 0}, {"native authority", native, 0, 0}, {"native domain", native, 1, 0}, {"cancel authority", context.Canceled, 0, 0}, {"canceled", context.Canceled, 0, 0},
		{"nil response", user.ErrStatusUpdateUnavailable, 1, 0}, {"nil user", user.ErrStatusUpdateUnavailable, 1, 0}, {"wrong target", user.ErrStatusUpdateUnavailable, 1, 0}, {"wrong status", user.ErrStatusUpdateUnavailable, 1, 0}, {"empty target", usermanager.ErrInvalidUserBody, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			ctx, cancel := context.WithCancel(logger.TransitWith(profileContext("session"), zap.New(core)))
			defer cancel()
			p := &statusUsers{fault: tc.name}
			a := &statusAudit{}
			verify := &statusAuthority{actor: "owner"}
			s := (&usermanager.Service{UserService: p, AuditService: a}).WithAdministratorAuthorizer(verify)
			r := &usermanager.ChangeAccountStatusRequest{ActorID: "owner", TargetUserID: "target", DesiredStatus: "SUSPENDED"}
			switch tc.name {
			case "self target":
				r.TargetUserID = "owner"
			case "email change":
				r.DesiredStatus = "EMAIL_CHANGE"
			case "audit outage":
				a.err = errors.New("private-audit-diagnostic")
			case "no audit":
				s.AuditService = (*statusAudit)(nil)
			case "late cancellation":
				p.cancel = cancel
			case "nil manager":
				s = nil
			case "nil context":
				ctx = nil
			case "typed nil users":
				s.UserService = (*statusUsers)(nil)
			case "missing capability":
				s.UserService = &profileUsers{}
			case "nil authority":
				s.WithAdministratorAuthorizer(nil)
			case "typed nil authority":
				s.WithAdministratorAuthorizer((*statusAuthority)(nil))
			case "wrong actor":
				r.ActorID = "target"
			case "wrong verified actor":
				verify.actor = "other"
			case "denied":
				verify.err = router.ErrRouteDenied
			case "native authority":
				verify.err = native
			case "native domain":
				p.err = native
			case "cancel authority":
				verify.cancel = cancel
			case "canceled":
				cancel()
			case "empty target":
				r.TargetUserID = " "
			}
			got, err := s.ChangeAccountStatus(ctx, r)
			require.Equal(t, tc.want, err)
			require.Equal(t, tc.writes, p.calls)
			require.Len(t, a.events, tc.audits)
			if err != nil {
				require.Nil(t, got)
			} else {
				require.Equal(t, r.TargetUserID, got.User.ID)
				if tc.audits > 0 {
					require.Equal(t, "owner", a.events[0].ActorId)
					require.Equal(t, r.TargetUserID, a.events[0].TargetId)
					require.Equal(t, got.User.Status, a.events[0].Details.(map[string]interface{})["new_status"])
				}
			}
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry), "private-")
			}
		})
	}
}

func TestAccountStatusBulkManager(t *testing.T) {
	for _, tc := range []struct {
		name   string
		count  int
		failed []string
		want   error
	}{
		{"all", 3, nil, nil}, {"partial", 2, []string{"b"}, nil}, {"revoked during batch", 1, []string{"b", "c"}, nil}, {"canceled during batch", 1, []string{"b", "c"}, nil}, {"denied initially", 0, nil, router.ErrRouteDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(profileContext("session"))
			defer cancel()
			p, a, verify := &statusUsers{}, &statusAudit{}, &statusAuthority{actor: "owner"}
			s := (&usermanager.Service{UserService: p, AuditService: a}).WithAdministratorAuthorizer(verify)
			switch tc.name {
			case "partial":
				p.fault = "second fails"
			case "revoked during batch":
				verify.denyAfter = 2
			case "canceled during batch":
				p.cancel = cancel
			case "denied initially":
				verify.err = router.ErrRouteDenied
			}
			got, err := s.BulkUpdateUsersStatus(ctx, &user.BulkUpdateUsersStatusRequest{IDs: []string{"a", "b", "c"}, DesiredStatus: "SUSPENDED"})
			require.Equal(t, tc.want, err)
			require.Len(t, a.events, tc.count)
			if err != nil {
				require.Nil(t, got)
				require.Zero(t, p.calls)
				return
			}
			require.Equal(t, tc.count, got.UpdatedCount)
			require.Equal(t, tc.failed, got.FailedIDs)
		})
	}
}

func TestAccountStatusHTTPMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		writes int
	}{
		{"success", 200, 1}, {"body cannot retarget", 200, 1}, {"null body", 400, 0}, {"bad body", 400, 0}, {"nil manager", 503, 0}, {"typed nil manager", 503, 0}, {"native conflict", 409, 1}, {"native unavailable", 503, 1}, {"unknown failure", 500, 1}, {"host override", 418, 1}, {"denied", 403, 0}, {"nil validator", 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, verify := &statusUsers{}, &statusAuthority{actor: "owner"}
			s := (&usermanager.Service{UserService: p}).WithAdministratorAuthorizer(verify)
			h := user.NewHandler(nil, validator.NewValidator()).WithStatusManager(s)
			body := `{"desired_status":"SUSPENDED","actor_id":"target"}`
			switch tc.name {
			case "body cannot retarget":
				body = `{"desired_status":"SUSPENDED","id":"forged","actor_id":"target"}`
			case "null body":
				body = `null`
			case "bad body":
				body = `{`
			case "nil manager":
				h.WithStatusManager(nil)
			case "typed nil manager":
				h.WithStatusManager((*usermanager.Service)(nil))
			case "native conflict", "host override":
				p.err = fmt.Errorf("private-diagnostic: %w", user.ErrStatusUpdateConflict)
			case "native unavailable":
				p.err = user.ErrStatusUpdateUnavailable
			case "unknown failure":
				p.err = errors.New("private-diagnostic")
			case "denied":
				verify.err = router.ErrRouteDenied
			case "nil validator":
				h.Validator = nil
			}
			if tc.name == "host override" {
				h.ErrorMaps = []reply.ErrorManifest{{user.ErrStatusUpdateConflict: {StatusCode: 418, Code: "HOST", Title: "Host conflict"}}}
			}
			r := httptest.NewRequest(http.MethodPatch, "/api/v2/users/target/status", strings.NewReader(body)).WithContext(profileContext("session"))
			r = mux.SetURLVars(r, map[string]string{"userID": "target"})
			w := httptest.NewRecorder()
			h.UpdateUserStatus(w, r)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.Equal(t, tc.writes, p.calls)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.NotContains(t, w.Body.String(), "private-diagnostic")
			if tc.code == 200 {
				require.Equal(t, []string{"target"}, p.ids)
				require.Contains(t, w.Body.String(), `"data":`)
				require.Contains(t, w.Body.String(), `"id":"target"`)
			}
		})
	}
}
