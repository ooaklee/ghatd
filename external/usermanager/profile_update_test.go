package usermanager_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
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

// profileUsers records only live reads and the narrow command; broad writes panic.
type profileUsers struct {
	usermanager.UserService
	owner             *user.UniversalUser
	readErr, writeErr error
	fault             string
	reads, writes     int
	input             user.UpdateProfileNamesRequest
	cancel            context.CancelFunc
}

func (p *profileUsers) GetUserByID(_ context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	p.reads++
	if p.fault == "cancel read" {
		p.cancel()
	}
	return &user.GetUserByIDResponse{User: p.owner}, p.readErr
}
func (p *profileUsers) UpdateProfileNames(_ context.Context, r *user.UpdateProfileNamesRequest) (*user.UniversalUser, error) {
	p.writes++
	p.input = *r
	if p.writeErr != nil {
		return nil, p.writeErr
	}
	if p.fault == "nil receipt" {
		return nil, nil
	}
	v := *p.owner
	v.PersonalInfo = &user.PersonalInfo{FirstName: r.FirstName, LastName: r.LastName}
	if p.fault == "wrong receipt" {
		v.ID = "other"
	}
	if p.fault == "type receipt" {
		v.Type = "other"
	}
	return &v, nil
}

// profileContext publishes fixture identities; it does not verify credentials.
func profileContext(kind string) context.Context {
	ctx := accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(context.Background(), "owner"), true)
	if kind == "api" || kind == "mixed" {
		ctx = accesshelpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: "owner", TokenID: "api"})
	}
	if kind != "api" {
		ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "owner", AccessUUID: "session", UserType: "default", EmailRevision: 2})
	}
	return ctx
}

func TestProfileUpdateManager(t *testing.T) {
	for _, tc := range []struct {
		name   string
		want   error
		writes int
	}{
		{"session", nil, 1}, {"api", nil, 1}, {"payload target ignored", nil, 1},
		{"mixed", router.ErrRouteUnauthenticated, 0}, {"bare identity", router.ErrRouteUnauthenticated, 0}, {"anonymous", usermanager.ErrUnableToIdentifyUser, 0}, {"wrong actor", usermanager.ErrUnableToIdentifyUser, 0},
		{"nil request", usermanager.ErrUnableToIdentifyUser, 0}, {"nil payload", usermanager.ErrInvalidUserBody, 0}, {"nil context", user.ErrProfileUpdateUnavailable, 0}, {"nil manager", user.ErrProfileUpdateUnavailable, 0}, {"typed nil users", user.ErrProfileUpdateUnavailable, 0}, {"missing capability", user.ErrProfileUpdateUnavailable, 0},
		{"suspended", router.ErrRouteDenied, 0}, {"stale revision", router.ErrRouteUnauthenticated, 0}, {"wrong type", router.ErrRouteUnauthenticated, 0}, {"wrong owner", user.ErrProfileUpdateUnavailable, 0}, {"nil owner", user.ErrProfileUpdateUnavailable, 0}, {"nil receipt", user.ErrProfileUpdateUnavailable, 1}, {"wrong receipt", user.ErrProfileUpdateUnavailable, 1}, {"type receipt", user.ErrProfileUpdateUnavailable, 1}, {"cancel read", context.Canceled, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(profileContext(tc.name))
			defer cancel()
			p := &profileUsers{owner: &user.UniversalUser{ID: "owner", Email: "owner@example.test", EmailRevision: 2, Type: "default", Status: "ACTIVE"}, fault: tc.name, cancel: cancel}
			s := &usermanager.Service{UserService: p}
			r := &usermanager.UpdateUserProfileRequest{ActorID: "owner", UpdateUserRequest: &user.UpdateUserRequest{FirstName: "New", LastName: "Name", ID: "forged", Email: "forged@example.test", Status: "ADMIN", User: &user.UniversalUser{ID: "forged"}}}
			switch tc.name {
			case "bare identity":
				ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(context.Background(), "owner"), true)
			case "anonymous":
				ctx = context.Background()
			case "wrong actor":
				r.ActorID = "other"
			case "nil request":
				r = nil
			case "nil payload":
				r.UpdateUserRequest = nil
			case "nil context":
				ctx = nil
			case "nil manager":
				s = nil
			case "typed nil users":
				s.UserService = (*profileUsers)(nil)
			case "missing capability":
				s.UserService = struct{ usermanager.UserService }{p}
			case "suspended":
				p.owner.Status = "SUSPENDED"
			case "stale revision":
				p.owner.EmailRevision++
			case "wrong type":
				p.owner.Type = "other"
			case "wrong owner":
				p.owner.ID = "other"
			case "nil owner":
				p.owner = nil
			}
			got, err := s.UpdateUserProfile(ctx, r)
			require.Equal(t, tc.want, err)
			require.Equal(t, tc.writes, p.writes)
			if err != nil {
				require.Nil(t, got)
			} else {
				require.Equal(t, "owner", got.User.ID)
				require.Equal(t, "owner", p.input.UserID)
				require.Equal(t, "owner@example.test", p.input.ExpectedEmail)
				require.Equal(t, "ACTIVE", p.input.ExpectedStatus)
			}
		})
	}
}

func TestProfileUpdateHTTP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"success", nil, 200, ""}, {"conflict", user.ErrProfileUpdateConflict, 409, "USV2-038"}, {"wrapped", fmt.Errorf("private-diagnostic: %w", user.ErrProfileUpdateConflict), 409, "USV2-038"}, {"unavailable", user.ErrProfileUpdateUnavailable, 503, "USV2-037"}, {"native validation", user.ErrUserRequiredFieldMissingFirstName, 400, "USV2-005"}, {"unknown", errors.New("private-diagnostic"), 500, ""}, {"mixed", errors.Join(user.ErrProfileUpdateConflict, errors.New("private-diagnostic")), 500, ""}, {"override", user.ErrProfileUpdateConflict, 422, "HOST-PROFILE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &profileUsers{owner: &user.UniversalUser{ID: "owner", Email: "owner@example.test", EmailRevision: 2, Type: "default", Status: "ACTIVE"}, writeErr: tc.err}
			core, logs := observer.New(zap.DebugLevel)
			var maps []reply.ErrorManifest
			if tc.name == "override" {
				maps = []reply.ErrorManifest{{user.ErrProfileUpdateConflict: {StatusCode: 422, Code: "HOST-PROFILE"}}}
			}
			h := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: &usermanager.Service{UserService: p}, Validator: validator.NewValidator(), ErrorMaps: maps})
			r := httptest.NewRequest(http.MethodPatch, "/api/v1/ums/me?id=other&ActorID=other", strings.NewReader(`{"first_name":"New","last_name":"Name","id":"other","ActorID":"other"}`)).WithContext(logger.TransitWith(profileContext("session"), zap.New(core)))
			w := httptest.NewRecorder()
			h.UpdateUserProfile(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.code)
			require.NotContains(t, w.Body.String(), "private-diagnostic")
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Equal(t, "owner", p.input.UserID)
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private-diagnostic")
			}
		})
	}
}

// Malformed transport and missing command adapters must fail before mutation.
func TestProfileUpdateTransportBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"malformed", "{", 400}, {"null", "null", 400}, {"empty", "", 400}, {"nil service", `{"first_name":"New"}`, 503}, {"canceled", `{"first_name":"New"}`, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &profileUsers{owner: &user.UniversalUser{ID: "owner", Email: "owner@example.test", EmailRevision: 2, Type: "default", Status: "ACTIVE"}}
			ctx, cancel := context.WithCancel(profileContext("session"))
			defer cancel()
			if tc.name == "canceled" {
				cancel()
			}
			h := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: &usermanager.Service{UserService: p}, Validator: validator.NewValidator()})
			if tc.name == "nil service" {
				h.Service = nil
			}
			w := httptest.NewRecorder()
			h.UpdateUserProfile(w, httptest.NewRequest("PATCH", "/me", strings.NewReader(tc.body)).WithContext(ctx))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Zero(t, p.writes)
		})
	}
}
