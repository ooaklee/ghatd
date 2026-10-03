package usermanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// conversationDomain permits only the new narrow port. Legacy calls panic.
type conversationDomain struct {
	usermanager.ContacterService
	input  contacter.AppendCommsEntryRequest
	query  contacter.ListCommsConversationRequest
	calls  int
	err    error
	fault  string
	cancel context.CancelFunc
}

func (p *conversationDomain) AppendCommsEntry(_ context.Context, r *contacter.AppendCommsEntryRequest) (*contacter.AppendCommsEntryResponse, error) {
	p.calls++
	p.input = *r
	if p.err != nil {
		return nil, p.err
	}
	if p.fault == "nil receipt" {
		return nil, nil
	}
	v := &contacter.CommsEntry{ID: strings.Repeat("a", 64), CommsID: r.CommsID, ActorID: r.ActorID, Kind: r.Kind, Body: r.Body, ParentEntryID: r.ParentEntryID, RecordedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if p.fault == "wrong target" {
		v.CommsID = "other"
	}
	if p.fault == "wrong actor receipt" {
		v.ActorID = "other"
	}
	if p.fault == "mutated request" {
		r.CommsID = "other"
		v.CommsID = "other"
	}
	if p.cancel != nil {
		p.cancel()
	}
	return &contacter.AppendCommsEntryResponse{Entry: v, Replayed: p.fault == "replayed"}, nil
}
func (p *conversationDomain) ListCommsConversation(_ context.Context, r *contacter.ListCommsConversationRequest) (*contacter.CommsConversationPage, error) {
	p.calls++
	p.query = *r
	if p.err != nil {
		return nil, p.err
	}
	if p.fault == "nil receipt" {
		return nil, nil
	}
	root := &contacter.Comms{Id: r.CommsID, Message: "Original", AdminNotes: "Private legacy"}
	if p.fault == "wrong target" {
		root.Id = "other"
	}
	return &contacter.CommsConversationPage{Legacy: root, Entries: []contacter.CommsEntry{}}, nil
}

func TestCommsConversationManager(t *testing.T) {
	native := fmt.Errorf("private-domain-detail: %w", contacter.ErrCommsEntryConflict)
	for _, op := range []string{"append", "read"} {
		t.Run(op, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				want  error
				calls int
			}{
				{"success", nil, 1}, {"nil manager", contacter.ErrCommsConversationUnavailable, 0}, {"nil users", contacter.ErrCommsConversationUnavailable, 0}, {"nil authority", contacter.ErrCommsConversationUnavailable, 0}, {"missing capability", contacter.ErrCommsConversationUnavailable, 0}, {"typed nil domain", contacter.ErrCommsConversationUnavailable, 0},
				{"wrong actor", usermanager.ErrUnableToIdentifyUser, 0}, {"wrong verified actor", usermanager.ErrUnableToIdentifyUser, 0}, {"denied", router.ErrRouteDenied, 0}, {"native authority", native, 0}, {"native domain", native, 1}, {"nil receipt", contacter.ErrCommsConversationUnavailable, 1}, {"wrong target", contacter.ErrCommsConversationUnavailable, 1}, {"canceled", context.Canceled, 0}, {"cancel authority", context.Canceled, 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					p := &conversationDomain{fault: tc.name}
					a := &statusAuthority{actor: "owner"}
					s := (&usermanager.Service{UserService: &profileUsers{}, ContacterService: p}).WithAdministratorAuthorizer(a)
					ctx, cancel := context.WithCancel(profileContext("session"))
					defer cancel()
					actor := "owner"
					switch tc.name {
					case "nil manager":
						s = nil
					case "nil users":
						s.UserService = nil
					case "nil authority":
						s.WithAdministratorAuthorizer(nil)
					case "missing capability":
						s.ContacterService = &commsTypesServiceStub{}
					case "typed nil domain":
						s.ContacterService = (*conversationDomain)(nil)
					case "wrong actor":
						actor = "forged"
					case "wrong verified actor":
						a.actor = "other"
					case "denied":
						a.err = router.ErrRouteDenied
					case "native authority":
						a.err = native
					case "native domain":
						p.err = native
					case "canceled":
						cancel()
					case "cancel authority":
						a.cancel = cancel
					}
					var err error
					if op == "append" {
						var got *contacter.AppendCommsEntryResponse
						got, err = s.AppendCommsEntry(ctx, &contacter.AppendCommsEntryRequest{ActorID: actor, CommsID: "contact", Kind: contacter.CommsEntryInternalNote, Body: "Private note"})
						if err != nil {
							require.Nil(t, got)
						}
					} else {
						var got *contacter.CommsConversationPage
						got, err = s.ListCommsConversation(ctx, &contacter.ListCommsConversationRequest{ActorID: actor, CommsID: "contact"})
						if err != nil {
							require.Nil(t, got)
						}
					}
					require.Equal(t, tc.want, err)
					require.Equal(t, tc.calls, p.calls)
				})
			}
		})
	}
}

func TestCommsConversationAudit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events int
		want   error
	}{{"new note", 1, nil}, {"replayed", 0, nil}, {"audit outage", 1, nil}, {"late cancellation", 1, nil}, {"wrong actor receipt", 0, contacter.ErrCommsConversationUnavailable}, {"mutated request", 0, contacter.ErrCommsConversationUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			ctx, cancel := context.WithCancel(logger.TransitWith(profileContext("session"), zap.New(core)))
			defer cancel()
			p := &conversationDomain{fault: tc.name}
			a := &statusAudit{}
			if tc.name == "audit outage" {
				a.err = errors.New("private-audit-detail")
			}
			if tc.name == "late cancellation" {
				p.cancel = cancel
			}
			s := (&usermanager.Service{UserService: &profileUsers{}, ContacterService: p, AuditService: a}).WithAdministratorAuthorizer(&statusAuthority{actor: "owner"})
			_, err := s.AppendCommsEntry(ctx, &contacter.AppendCommsEntryRequest{ActorID: "owner", CommsID: "contact", Kind: contacter.CommsEntryReply, Body: "Private reply"})
			require.Equal(t, tc.want, err)
			require.Len(t, a.events, tc.events)
			if tc.events > 0 {
				require.Equal(t, "owner", a.events[0].ActorId)
				require.Equal(t, "contact", a.events[0].TargetId)
				require.NotContains(t, fmt.Sprint(a.events[0].Details), "Private reply")
			}
			for _, l := range logs.All() {
				require.NotContains(t, fmt.Sprint(l), "private-")
			}
		})
	}
}

func TestCommsConversationHTTP(t *testing.T) {
	for _, op := range []string{"append", "read"} {
		t.Run(op, func(t *testing.T) {
			for _, tc := range []struct {
				name          string
				status, calls int
			}{{"success", 200, 1}, {"anonymous", 401, 0}, {"denied", 403, 0}, {"missing manager", 503, 0}, {"native conflict", 409, 1}, {"native missing", 404, 1}, {"unknown failure", 500, 1}, {"host override", 418, 1}, {"nil receipt", 503, 1}, {"query cannot forge actor", 200, 1}} {
				t.Run(tc.name, func(t *testing.T) {
					p := &conversationDomain{fault: tc.name}
					a := &statusAuthority{actor: "owner"}
					s := (&usermanager.Service{UserService: &profileUsers{}, ContacterService: p}).WithAdministratorAuthorizer(a)
					h := &usermanager.Handler{Service: s}
					ctx := profileContext("session")
					switch tc.name {
					case "anonymous":
						ctx = context.Background()
					case "denied":
						a.err = router.ErrRouteDenied
					case "missing manager":
						h.Service = nil
					case "native conflict", "host override":
						p.err = fmt.Errorf("private-diagnostic: %w", contacter.ErrCommsEntryConflict)
					case "native missing":
						p.err = contacter.ErrCommsNotFound
					case "unknown failure":
						p.err = errors.New("private-diagnostic")
					}
					if tc.name == "host override" {
						h.ErrorMaps = []reply.ErrorManifest{{contacter.ErrCommsEntryConflict: {StatusCode: 418, Code: "HOST", Title: "Host conflict"}}}
					}
					core, logs := observer.New(zap.DebugLevel)
					ctx = logger.TransitWith(ctx, zap.New(core))
					r := httptest.NewRequest(http.MethodPost, "/api/v1/ums/comms/contact/conversation?actor_id=forged", strings.NewReader(`{"request_id":"`+uuid.NewString()+`","kind":"internal_note","body":"Private note"}`)).WithContext(ctx)
					r = mux.SetURLVars(r, map[string]string{"id": "contact"})
					w := httptest.NewRecorder()
					if op == "append" {
						h.AppendCommsConversationEntry(w, r)
					} else {
						h.GetCommsConversation(w, r)
					}
					require.Equal(t, tc.status, w.Code, w.Body.String())
					require.Equal(t, tc.calls, p.calls)
					require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
					require.NotContains(t, w.Body.String(), "private-diagnostic")
					if tc.status == 200 {
						if op == "append" {
							require.Equal(t, "owner", p.input.ActorID)
							require.Equal(t, "contact", p.input.CommsID)
						} else {
							require.Equal(t, "owner", p.query.ActorID)
						}
					}
					for _, l := range logs.All() {
						require.NotContains(t, fmt.Sprint(l), "private-diagnostic")
					}
				})
			}
		})
	}
}

func TestCommsConversationHTTPMalformed(t *testing.T) {
	for _, tc := range []struct {
		name, body, query string
		read              bool
	}{
		{"null", "null", "", false}, {"broken", "{", "", false}, {"extra value", "{} {}", "", false}, {"body actor", `{"actor_id":"forged"}`, "", false}, {"body target", `{"comms_id":"forged"}`, "", false}, {"import via HTTP", `{"kind":"email_inbound","body":"Email"}`, "", false}, {"oversized body", `{"body":"` + strings.Repeat("x", 512*1024) + `"}`, "", false}, {"bad limit", "", "?limit=abc", true}, {"zero limit", "", "?limit=0", true}, {"large limit", "", "?limit=101", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &conversationDomain{}
			s := (&usermanager.Service{UserService: &profileUsers{}, ContacterService: p}).WithAdministratorAuthorizer(&statusAuthority{actor: "owner"})
			h := &usermanager.Handler{Service: s}
			r := httptest.NewRequest(http.MethodPost, "/conversation"+tc.query, strings.NewReader(tc.body)).WithContext(profileContext("session"))
			r = mux.SetURLVars(r, map[string]string{"id": "contact"})
			w := httptest.NewRecorder()
			if tc.read {
				h.GetCommsConversation(w, r)
			} else {
				h.AppendCommsConversationEntry(w, r)
			}
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Zero(t, p.calls)
		})
	}
}

// legacyConversationHandler intentionally has no optional conversation methods.
type legacyConversationHandler struct{ usermanager.UsermanagerHandler }

// creationConversationRepository models an adapter returning private fields;
// public receipts must not serialize those fields even from trusted storage.
type creationConversationRepository struct{ *contacter.Repository }

func (*creationConversationRepository) CreateComms(_ context.Context, c *contacter.Comms) (*contacter.Comms, error) {
	r := *c
	r.Id, r.NanoId, r.CreatedAt = "contact", "short-contact", "2026-01-01T00:00:00Z"
	r.AdminNotes, r.AdminReply, r.ReachedOutAt = "private-adapter-note", "private-adapter-reply", "private-adapter-time"
	r.LinkedCommsIds = []string{"private-adapter-link"}
	return &r, nil
}

func TestCommsConversationCreationContract(t *testing.T) {
	for _, name := range []string{"anonymous", "authenticated"} {
		t.Run(name, func(t *testing.T) {
			repo := &creationConversationRepository{}
			s := usermanager.NewService(&usermanager.NewServiceRequest{ContacterService: contacter.NewService(repo)})
			h := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: s, Validator: responseCommsValidator{}})
			r := httptest.NewRequest(http.MethodPost, "/api/v1/ums/comms", strings.NewReader(`{"full_name":"Example Person","email":"person@example.test","message":"Original message","type":"feedback-companion","meta":{"rating":5},"user_id":"forged","admin_notes":"private-injected-note","entries":[{"body":"private-injected-history"}]}`))
			if name == "authenticated" {
				r = r.WithContext(profileContext("session"))
			}
			w := httptest.NewRecorder()
			h.CreateComms(w, r)
			require.Equal(t, 201, w.Code, w.Body.String())
			var body struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, "Original message", body.Data["message"])
			require.Equal(t, "contact", body.Data["id"])
			require.Equal(t, "short-contact", body.Data["nano_id"])
			require.Equal(t, "person@example.test", body.Data["email"])
			require.Equal(t, "Example Person", body.Data["full_name"])
			require.Equal(t, "feedback-companion", body.Data["type"])
			require.Equal(t, float64(5), body.Data["meta"].(map[string]any)["rating"])
			require.Equal(t, "2026-01-01T00:00:00Z", body.Data["created_at"])
			require.Equal(t, name == "authenticated", body.Data["user_logged_in"])
			if name == "authenticated" {
				require.Equal(t, "owner", body.Data["user_id"])
			} else {
				require.NotContains(t, body.Data, "user_id")
			}
			require.NotContains(t, body.Data, "entries")
			require.NotContains(t, body.Data, "legacy")
			for _, key := range []string{"admin_notes", "admin_reply", "linked_comms_ids", "reached_out_at"} {
				require.NotContains(t, body.Data, key)
			}
			require.NotContains(t, w.Body.String(), "private-injected")
			require.NotContains(t, w.Body.String(), "private-adapter")
		})
	}
}

func TestCommsConversationRouteAdmission(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			for _, tc := range []struct {
				name          string
				status, calls int
			}{{"admin", 200, 1}, {"middleware denied", 403, 0}, {"live authority denied", 403, 0}, {"old handler", 503, 0}, {"missing middleware", 503, 0}} {
				t.Run(tc.name, func(t *testing.T) {
					p := &conversationDomain{}
					a := &statusAuthority{actor: "owner"}
					s := (&usermanager.Service{UserService: &profileUsers{}, ContacterService: p}).WithAdministratorAuthorizer(a)
					var h usermanager.UsermanagerHandler = &usermanager.Handler{Service: s}
					if tc.name == "old handler" {
						h = &legacyConversationHandler{}
					}
					routerInstance := router.NewRouter(nil, nil)
					pass := func(next http.Handler) http.Handler { return next }
					admitted := 0
					admin := func(next http.Handler) http.Handler {
						return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							admitted++
							if tc.name == "middleware denied" {
								w.WriteHeader(403)
								return
							}
							next.ServeHTTP(w, r.WithContext(profileContext("session")))
						})
					}
					if tc.name == "live authority denied" {
						a.err = router.ErrRouteDenied
					}
					if tc.name == "missing middleware" {
						admin = nil
					}
					usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{Router: routerInstance, Handler: h, RateLimitOrActiveMiddleware: pass, ValidApiTokenOrJWTMiddleware: pass, ActiveValidApiTokenOrJWTMiddleware: pass, AdminOnlyMiddleware: admin})
					if tc.name == "missing middleware" {
						require.Error(t, routerInstance.ValidateRoutePolicies())
					} else {
						require.NoError(t, routerInstance.ValidateRoutePolicies())
					}
					r := httptest.NewRequest(method, "/api/v1/ums/comms/contact/conversation", strings.NewReader(`{"request_id":"`+uuid.NewString()+`","kind":"reply","body":"Recorded reply"}`))
					w := httptest.NewRecorder()
					routerInstance.GetRouter().ServeHTTP(w, r)
					require.Equal(t, tc.status, w.Code, w.Body.String())
					require.Equal(t, tc.calls, p.calls)
					if tc.name != "missing middleware" {
						require.Equal(t, 1, admitted)
					}
				})
			}
		})
	}
}
