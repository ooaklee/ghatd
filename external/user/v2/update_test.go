package user

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
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// genericUpdateProbe returns adapter-owned records and native failures. It is
// deliberately not evidence of atomic storage or credential authorization.
type genericUpdateProbe struct {
	UserRepository
	stored            *UniversalUser
	readErr, writeErr error
	fault             string
	reads, writes     int
	key               string
	cancel            context.CancelFunc
}

func (p *genericUpdateProbe) GetUserByID(_ context.Context, id string) (*UniversalUser, error) {
	p.reads++
	p.key = id
	if p.fault == "cancel read" {
		p.cancel()
	}
	return p.stored, p.readErr
}
func (p *genericUpdateProbe) UpdateUser(_ context.Context, v *UniversalUser) (*UniversalUser, error) {
	p.writes++
	if p.writeErr != nil {
		return nil, p.writeErr
	}
	v.Handle = "persisted-handle"
	switch p.fault {
	case "nil receipt":
		return nil, nil
	case "wrong receipt", "mutated command":
		v.ID = "other"
	case "email receipt":
		v.Email = "other@example.test"
	case "revision receipt":
		v.EmailRevision++
	case "status receipt":
		v.Status = "SUSPENDED"
	case "type receipt":
		v.Type = "other"
	case "cancel write":
		p.cancel()
	}
	return v, nil
}

func genericAccount() *UniversalUser {
	return &UniversalUser{ID: "selected", Email: "owner@example.test", Type: "default", Status: "ACTIVE", EmailRevision: 2,
		PersonalInfo: &PersonalInfo{FirstName: "Old", LastName: "Name", FullName: "Old Name"}, Roles: []string{},
		Metadata:   &UserMetadata{CreatedAt: "original", UpdatedAt: "old", CustomTimestamps: map[string]string{"keep": "value"}},
		Extensions: map[string]any{"old": true}, Verification: &VerificationStatus{EmailVerified: true}}
}

func TestGenericUserUpdateDomain(t *testing.T) {
	native := fmt.Errorf("private-diagnostic: %w", ErrOAuthConnectionConflict)
	for _, tc := range []struct {
		name   string
		want   error
		writes int
	}{
		{"scalar", nil, 1}, {"replacement", nil, 1}, {"replacement only ID", nil, 1}, {"empty no-op", nil, 0}, {"equal no-op", nil, 0}, {"nil optional fields", nil, 1},
		{"nil request", ErrInvalidUserBody, 0}, {"nil service", ErrUserUpdateUnavailable, 0}, {"nil context", ErrUserUpdateUnavailable, 0}, {"nil repository", ErrUserUpdateUnavailable, 0}, {"typed nil repository", ErrUserUpdateUnavailable, 0},
		{"nil strings", ErrUserUpdateUnavailable, 0}, {"typed nil strings", ErrUserUpdateUnavailable, 0}, {"nil clock", ErrUserUpdateUnavailable, 0}, {"typed nil clock", ErrUserUpdateUnavailable, 0},
		{"empty ID", ErrInvalidUserID, 0}, {"conflicting IDs", ErrInvalidUserID, 0}, {"replacement missing ID", ErrInvalidUserID, 0},
		{"read error", native, 0}, {"write error", native, 1}, {"nil stored", ErrUserUpdateUnavailable, 0}, {"wrong stored", ErrUserUpdateUnavailable, 0},
		{"email change", ErrEmailChangeRequired, 0}, {"replacement email change", ErrEmailChangeRequired, 0}, {"invalid type", ErrInvalidUserConfigType, 0}, {"invalid status", ErrUserInvalidTargetStatus, 0}, {"invalid role", ErrUserInvalidRole, 0},
		{"canceled", context.Canceled, 0}, {"cancel read", context.Canceled, 0}, {"cancel write", nil, 1},
		{"nil receipt", ErrUserUpdateUnavailable, 1}, {"wrong receipt", ErrUserUpdateUnavailable, 1}, {"mutated command", ErrUserUpdateUnavailable, 1}, {"email receipt", ErrUserUpdateUnavailable, 1}, {"revision receipt", ErrUserUpdateUnavailable, 1}, {"status receipt", ErrUserUpdateUnavailable, 1}, {"type receipt", ErrUserUpdateUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &genericUpdateProbe{stored: genericAccount(), fault: tc.name, cancel: cancel}
			s := NewService(p, nil, nil, &DefaultIDGenerator{}, &DefaultTimeProvider{}, &DefaultStringUtils{}, "")
			r := &UpdateUserRequest{ID: "selected", FirstName: "new", Extensions: map[string]any{"new": true}}
			switch tc.name {
			case "replacement", "replacement only ID":
				r.User = genericAccount()
				r.User.PersonalInfo.FirstName = "new"
				if tc.name == "replacement only ID" {
					r.ID = ""
				}
			case "empty no-op":
				r.FirstName = ""
				r.Extensions = nil
			case "equal no-op":
				r.FirstName = "Old"
				r.Extensions = nil
			case "nil optional fields":
				p.stored.Metadata = nil
				p.stored.Extensions = nil
				p.stored.Verification = nil
			case "nil request":
				r = nil
			case "nil service":
				s = nil
			case "nil context":
				ctx = nil
			case "nil repository":
				s.UserRepository = nil
			case "typed nil repository":
				s.UserRepository = (*genericUpdateProbe)(nil)
			case "nil strings":
				s.StringUtils = nil
			case "typed nil strings":
				s.StringUtils = (*DefaultStringUtils)(nil)
			case "nil clock":
				s.TimeProvider = nil
			case "typed nil clock":
				s.TimeProvider = (*DefaultTimeProvider)(nil)
			case "empty ID":
				r.ID = " "
			case "conflicting IDs":
				r.User = genericAccount()
				r.User.ID = "other"
			case "replacement missing ID":
				r.User = genericAccount()
				r.User.ID = ""
			case "read error":
				p.readErr = native
			case "write error":
				p.writeErr = native
			case "nil stored":
				p.stored = nil
			case "wrong stored":
				p.stored.ID = "other"
			case "email change":
				r.Email = "other@example.test"
			case "replacement email change":
				r.User = genericAccount()
				r.User.Email = "other@example.test"
			case "invalid type":
				r.Type = "private-diagnostic"
			case "invalid status":
				r.Status = "private-diagnostic"
			case "invalid role":
				p.stored.Roles = []string{"private-diagnostic"}
			case "canceled":
				cancel()
			}
			before := copyUserForUpdate(p.stored)
			var requestBefore UpdateUserRequest
			if r != nil {
				requestBefore = *r
				requestBefore.User = copyUserForUpdate(r.User)
			}
			got, err := s.UpdateUser(ctx, r)
			require.Equal(t, tc.want, err)
			require.Equal(t, tc.writes, p.writes)
			require.Equal(t, before, p.stored, "adapter-owned source mutated")
			if r != nil {
				require.Equal(t, requestBefore, *r, "caller-owned input mutated")
			}
			if err != nil {
				require.Nil(t, got)
			} else {
				require.Equal(t, "selected", got.User.ID)
				if tc.writes == 1 {
					require.Equal(t, "persisted-handle", got.User.Handle)
				}
			}
		})
	}
}

func TestGenericUserUpdateHTTP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		status  int
		code    string
	}{
		{"success", nil, 200, ""}, {"conflict", ErrOAuthConnectionConflict, 409, "OAuthConnectionConflict"},
		{"wrapped", fmt.Errorf("private-diagnostic: %w", ErrUserInvalidRole), 400, "USV2-008"},
		{"unknown", errors.New("private-diagnostic"), 500, ""}, {"mixed", errors.Join(ErrUserInvalidRole, errors.New("private-diagnostic")), 500, ""},
		{"override", ErrUserInvalidRole, 422, "HOST-UPDATE"}, {"nil receipt", nil, 503, "USV2-039"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &genericUpdateProbe{stored: genericAccount(), writeErr: tc.failure, fault: tc.name}
			s := NewService(p, nil, nil, &DefaultIDGenerator{}, &DefaultTimeProvider{}, &DefaultStringUtils{}, "")
			var overrides []reply.ErrorManifest
			if tc.name == "override" {
				overrides = []reply.ErrorManifest{{ErrUserInvalidRole: {StatusCode: 422, Code: "HOST-UPDATE"}}}
			}
			h := NewHandler(s, validator.NewValidator(), overrides...)
			core, logs := observer.New(zap.DebugLevel)
			r := httptest.NewRequest(http.MethodPatch, "/selected", strings.NewReader(`{"id":"forged","first_name":"New"}`)).WithContext(logger.TransitWith(context.Background(), zap.New(core)))
			r = mux.SetURLVars(r, map[string]string{UserURIVariableID: "selected"})
			w := httptest.NewRecorder()
			h.UpdateUser(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.code)
			require.Equal(t, "selected", p.key)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.NotContains(t, w.Body.String(), "private-diagnostic")
			for _, e := range logs.All() {
				require.NotContains(t, fmt.Sprint(e.ContextMap()), "private-diagnostic")
			}
		})
	}
}

func TestGenericUserUpdateTransport(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		writes     int
	}{
		{"path bound", `{"id":"other","first_name":"New"}`, 200, 1}, {"null", "null", 400, 0}, {"malformed", "{", 400, 0}, {"empty", "", 400, 0}, {"missing route", `{}`, 400, 0}, {"nil service", `{}`, 503, 0}, {"typed nil service", `{}`, 503, 0}, {"nil validator", `{}`, 400, 0},
		{"replacement body ignored", `{"User":{"id":"selected","email":"owner@example.test","status":"SUSPENDED"},"first_name":"New"}`, 200, 1},
		{"capitalized body ID", `{"ID":"other","first_name":"New"}`, 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &genericUpdateProbe{stored: genericAccount()}
			s := NewService(p, nil, nil, &DefaultIDGenerator{}, &DefaultTimeProvider{}, &DefaultStringUtils{}, "")
			h := NewHandler(s, validator.NewValidator())
			r := httptest.NewRequest(http.MethodPatch, "/selected", strings.NewReader(tc.body))
			r = mux.SetURLVars(r, map[string]string{UserURIVariableID: "selected"})
			switch tc.name {
			case "missing route":
				r = mux.SetURLVars(r, nil)
			case "nil service":
				h.Service = nil
			case "typed nil service":
				h.Service = (*Service)(nil)
			case "nil validator":
				h.Validator = nil
			}
			w := httptest.NewRecorder()
			h.UpdateUser(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.writes, p.writes)
			require.NotContains(t, w.Body.String(), "SUSPENDED")
		})
	}
}

// Invalid repository entries must not initialize stores or dispatch writes.
func TestGenericUserUpdateRepositoryEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil repository", ErrUserUpdateUnavailable}, {"nil context", ErrUserUpdateUnavailable}, {"nil store", ErrUserUpdateUnavailable}, {"typed nil store", ErrUserUpdateUnavailable}, {"missing capability", ErrUserUpdateUnavailable},
		{"nil account", ErrInvalidUserBody}, {"empty ID", ErrInvalidUserBody}, {"negative revision", ErrInvalidUserBody}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &emailStorageSetupProbe{}
			r := NewRepository(p)
			v := genericAccount()
			switch tc.name {
			case "nil repository":
				r = nil
			case "nil context":
				ctx = nil
			case "nil store":
				r.Store = nil
			case "typed nil store":
				r.Store = (*emailStorageSetupProbe)(nil)
			case "nil account":
				v = nil
			case "empty ID":
				v.ID = " "
			case "negative revision":
				v.EmailRevision = -1
			case "canceled":
				cancel()
			}
			got, err := r.UpdateUser(ctx, v)
			require.Equal(t, tc.want, err)
			require.Nil(t, got)
			require.Zero(t, p.initCalls)
		})
	}
}
