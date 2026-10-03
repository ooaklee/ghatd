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

// roleUsers exposes only narrow capabilities; the embedded broad methods panic
// if management accidentally invokes a fallback. It does not prove persistence.
type roleUsers struct {
	usermanager.UserService
	calls  int
	ids    []string
	fault  string
	err    error
	cancel context.CancelFunc
}

func (p *roleUsers) role(id, role string, remove bool) (*user.UniversalUser, bool, error) {
	p.calls++
	p.ids = append(p.ids, id)
	if p.cancel != nil {
		p.cancel()
	}
	if p.err != nil {
		return nil, false, p.err
	}
	v := &user.UniversalUser{ID: id, Roles: []string{"USER"}}
	if !remove {
		v.Roles = append(v.Roles, role)
	}
	switch p.fault {
	case "nil user":
		v = nil
	case "wrong target":
		v.ID = "other"
	case "wrong roles":
		if remove {
			v.Roles = append(v.Roles, role)
		} else {
			v.Roles = nil
		}
	}
	return v, p.fault != "no-op", nil
}
func (p *roleUsers) AddUserRole(_ context.Context, r *user.AddUserRoleRequest) (*user.AddUserRoleResponse, error) {
	v, changed, err := p.role(r.ID, r.Role, false)
	if err != nil {
		return nil, err
	}
	if p.fault == "nil response" {
		return nil, nil
	}
	return &user.AddUserRoleResponse{User: v, Changed: changed}, nil
}
func (p *roleUsers) RemoveUserRole(_ context.Context, r *user.RemoveUserRoleRequest) (*user.RemoveUserRoleResponse, error) {
	v, changed, err := p.role(r.ID, r.Role, true)
	if err != nil {
		return nil, err
	}
	if p.fault == "nil response" {
		return nil, nil
	}
	return &user.RemoveUserRoleResponse{User: v, Changed: changed}, nil
}

func TestAccountRoleManager(t *testing.T) {
	native := fmt.Errorf("private-native-diagnostic: %w", user.ErrRoleUpdateConflict)
	for _, remove := range []bool{false, true} {
		for _, tc := range []struct {
			name           string
			want           error
			writes, audits int
		}{
			{"success", nil, 1, 1}, {"self target", nil, 1, 1}, {"no-op", nil, 1, 0}, {"no audit", nil, 1, 0}, {"audit outage", nil, 1, 1}, {"late cancellation", nil, 1, 1},
			{"nil manager", user.ErrRoleUpdateUnavailable, 0, 0}, {"nil context", user.ErrRoleUpdateUnavailable, 0, 0}, {"nil authority", user.ErrRoleUpdateUnavailable, 0, 0}, {"typed nil authority", user.ErrRoleUpdateUnavailable, 0, 0}, {"typed nil users", user.ErrRoleUpdateUnavailable, 0, 0}, {"missing capability", user.ErrRoleUpdateUnavailable, 0, 0},
			{"wrong actor", usermanager.ErrUnableToIdentifyUser, 0, 0}, {"wrong verified actor", usermanager.ErrUnableToIdentifyUser, 0, 0}, {"denied", router.ErrRouteDenied, 0, 0}, {"native authority", native, 0, 0}, {"native domain", native, 1, 0}, {"cancel authority", context.Canceled, 0, 0}, {"canceled", context.Canceled, 0, 0},
			{"nil request", usermanager.ErrInvalidUserBody, 0, 0}, {"empty target", user.ErrInvalidUserID, 0, 0}, {"blank role", user.ErrUserInvalidRole, 0, 0}, {"nil response", user.ErrRoleUpdateUnavailable, 1, 0}, {"nil user", user.ErrRoleUpdateUnavailable, 1, 0}, {"wrong target", user.ErrRoleUpdateUnavailable, 1, 0}, {"wrong roles", user.ErrRoleUpdateUnavailable, 1, 0},
		} {
			t.Run(fmt.Sprintf("remove=%v/%s", remove, tc.name), func(t *testing.T) {
				core, logs := observer.New(zap.WarnLevel)
				ctx, cancel := context.WithCancel(logger.TransitWith(profileContext("session"), zap.New(core)))
				defer cancel()
				p, a, verify := &roleUsers{fault: tc.name}, &statusAudit{}, &statusAuthority{actor: "owner"}
				s := (&usermanager.Service{UserService: p, AuditService: a}).WithAdministratorAuthorizer(verify)
				r := &usermanager.ChangeAccountRoleRequest{ActorID: "owner", TargetUserID: "target", Role: "ADMIN", Remove: remove}
				switch tc.name {
				case "self target":
					r.TargetUserID = "owner"
				case "no audit":
					s.AuditService = (*statusAudit)(nil)
				case "audit outage":
					a.err = errors.New("private-audit-diagnostic")
				case "late cancellation":
					p.cancel = cancel
				case "nil manager":
					s = nil
				case "nil context":
					ctx = nil
				case "nil authority":
					s.WithAdministratorAuthorizer(nil)
				case "typed nil authority":
					s.WithAdministratorAuthorizer((*statusAuthority)(nil))
				case "typed nil users":
					s.UserService = (*roleUsers)(nil)
				case "missing capability":
					s.UserService = &profileUsers{}
				case "wrong actor":
					r.ActorID = "forged"
				case "wrong verified actor":
					verify.actor = "forged"
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
				case "nil request":
					r = nil
				case "empty target":
					r.TargetUserID = " "
				case "blank role":
					r.Role = " "
				}
				v, changed, err := s.ChangeAccountRole(ctx, r)
				require.Equal(t, tc.want, err)
				require.Equal(t, tc.writes, p.calls)
				require.Len(t, a.events, tc.audits)
				if err != nil {
					require.Nil(t, v)
					require.False(t, changed)
					return
				}
				require.Equal(t, r.TargetUserID, v.ID)
				require.Equal(t, tc.name != "no-op", changed)
				if tc.audits > 0 {
					require.Equal(t, "owner", a.events[0].ActorId)
					require.Equal(t, r.TargetUserID, a.events[0].TargetId)
					require.Equal(t, "ADMIN", a.events[0].Details.(map[string]interface{})["role"])
					action := "user.role_added"
					if remove {
						action = "user.role_removed"
					}
					require.Equal(t, action, string(a.events[0].Action))
				}
				if tc.name == "audit outage" {
					require.Equal(t, 1, logs.Len())
					require.Equal(t, "role-audit-delivery-failed", logs.All()[0].Message)
					require.Equal(t, map[string]interface{}{"source": "ghatd", "ghatd-package": "external/usermanager", "operation": "change-account-role"}, logs.All()[0].ContextMap())
				}
			})
		}
	}
}

func TestAccountRoleHTTP(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, tc := range []struct {
			name         string
			code, writes int
		}{
			{"success", 200, 1}, {"null", 400, 0}, {"malformed", 400, 0}, {"nil validator", 400, 0}, {"nil manager", 503, 0}, {"typed nil manager", 503, 0}, {"conflict", 409, 1}, {"unavailable", 503, 1}, {"unknown", 500, 1}, {"override", 418, 1}, {"denied", 403, 0}, {"nil response", 503, 1}, {"wrong target", 503, 1}, {"wrong roles", 503, 1}, {"blank role", 400, 0},
		} {
			t.Run(fmt.Sprintf("remove=%v/%s", remove, tc.name), func(t *testing.T) {
				p, verify := &roleUsers{fault: tc.name}, &statusAuthority{actor: "owner"}
				s := (&usermanager.Service{UserService: p}).WithAdministratorAuthorizer(verify)
				h := user.NewHandler(nil, validator.NewValidator()).WithRoleManager(s)
				body := `{"id":"forged","role":"ADMIN","actor_id":"forged"}`
				switch tc.name {
				case "null":
					body = "null"
				case "malformed":
					body = "{"
				case "nil validator":
					h.Validator = nil
				case "nil manager":
					h.RoleManager = nil
				case "typed nil manager":
					h.RoleManager = (*usermanager.Service)(nil)
				case "conflict", "override":
					p.err = fmt.Errorf("private-diagnostic: %w", user.ErrRoleUpdateConflict)
				case "unavailable":
					p.err = user.ErrRoleUpdateUnavailable
				case "unknown":
					p.err = errors.New("private-diagnostic")
				case "denied":
					verify.err = router.ErrRouteDenied
				case "blank role":
					body = `{"role":" "}`
				}
				if tc.name == "override" {
					h.ErrorMaps = []reply.ErrorManifest{{user.ErrRoleUpdateConflict: {StatusCode: 418, Code: "HOST", Title: "Host conflict"}}}
				}
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).WithContext(profileContext("session"))
				r = mux.SetURLVars(r, map[string]string{"userID": "target"})
				w := httptest.NewRecorder()
				if remove {
					h.RemoveUserRole(w, r)
				} else {
					h.AddUserRole(w, r)
				}
				require.Equal(t, tc.code, w.Code, w.Body.String())
				require.Equal(t, tc.writes, p.calls)
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.NotContains(t, w.Body.String(), "private-diagnostic")
				if tc.code == 200 {
					require.Equal(t, []string{"target"}, p.ids)
					require.Contains(t, w.Body.String(), `"data":`)
					require.NotContains(t, w.Body.String(), `"Changed"`)
				}
			})
		}
	}
}

func TestAccountRoleMapperGuards(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, name := range []string{"nil request", "nil URL", "nil body", "canceled", "missing target"} {
			t.Run(fmt.Sprintf("remove=%v/%s", remove, name), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"role":"ADMIN"}`))
				r = mux.SetURLVars(r, map[string]string{"userID": "target"})
				want := user.ErrInvalidUserBody
				switch name {
				case "nil request":
					r = nil
				case "nil URL":
					r.URL = nil
				case "nil body":
					r.Body = nil
				case "missing target":
					r = mux.SetURLVars(r, map[string]string{})
					want = user.ErrInvalidUserID
				case "canceled":
					ctx, cancel := context.WithCancel(r.Context())
					cancel()
					r = r.WithContext(ctx)
					want = context.Canceled
				}
				if remove {
					v, err := user.MapRequestToRemoveUserRoleRequest(r, validator.NewValidator())
					require.Nil(t, v)
					require.Equal(t, want, err)
				} else {
					v, err := user.MapRequestToAddUserRoleRequest(r, validator.NewValidator())
					require.Nil(t, v)
					require.Equal(t, want, err)
				}
			})
		}
	}
}
