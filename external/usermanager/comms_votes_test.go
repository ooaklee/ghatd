package usermanager_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/router"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// voteDomain records only the lower-domain port; it cannot authorize requests.
type voteDomain struct {
	calls                          int
	err                            error
	fault                          string
	cancel                         context.CancelFunc
	actor, comms, entry, operation string
}

// legacyVoteManager has the original manager surface without optional voting.
type legacyVoteManager struct{ usermanager.UsermanagerService }

// legacyVoteHandler forwards the original routes but offers no vote capability.
type legacyVoteHandler struct{ usermanager.UsermanagerHandler }

// voteUsers records decoration separately from the live authority test double.
type voteUsers struct {
	profileUsers
	lookups int
	err     error
	cancel  context.CancelFunc
}

func (p *voteUsers) GetUsersByIDs(context.Context, *userv2.GetUsersByIDsRequest) (*userv2.GetUsersByIDsResponse, error) {
	p.lookups++
	if p.cancel != nil {
		p.cancel()
	}
	return &userv2.GetUsersByIDsResponse{}, p.err
}

func (p *voteDomain) page(actor, comms string, entries []string) (*contacter.CommsVoteResult, error) {
	p.calls++
	p.actor, p.comms = actor, comms
	if p.err != nil {
		return nil, p.err
	}
	if p.cancel != nil {
		p.cancel()
	}
	if p.fault == "nil receipt" {
		return nil, nil
	}
	page := &contacter.CommsVoteResult{CommsID: comms, ViewerActorID: actor, ByEntry: map[string]contacter.CommsVoteSummary{}, EntryAuthors: map[string]string{}}
	for _, entry := range entries {
		page.ByEntry[entry] = contacter.CommsVoteSummary{CommsID: comms, EntryID: entry}
		if p.operation == "read" && entry != "" {
			page.EntryAuthors[entry] = ""
		}
	}
	switch p.fault {
	case "missing authors":
		page.EntryAuthors = nil
	case "unexpected authors":
		page.EntryAuthors["other-entry"] = "unrelated-actor"
	case "original sender":
		page.EntryAuthors[""] = "sender"
	case "wrong author entry":
		delete(page.EntryAuthors, "entry")
		page.EntryAuthors["other-entry"] = "unrelated-actor"
	case "wrong viewer":
		page.ViewerActorID = "other"
	case "wrong parent":
		page.CommsID = "other"
	case "wrong entry":
		page.ByEntry = map[string]contacter.CommsVoteSummary{"other": {CommsID: comms, EntryID: "other"}}
	case "missing summary":
		delete(page.ByEntry, entries[0])
	case "negative count", "invalid direction", "impossible own vote", "wrong row parent", "wrong row entry":
		row := page.ByEntry[entries[0]]
		switch p.fault {
		case "negative count":
			row.Up = -1
		case "invalid direction":
			v := 2
			row.ViewerVote = &v
		case "impossible own vote":
			v := 1
			row.ViewerVote = &v
		case "wrong row parent":
			row.CommsID = "other"
		case "wrong row entry":
			row.EntryID = "other"
		}
		page.ByEntry[entries[0]] = row
	}
	return page, nil
}

func (p *voteDomain) GetCommsVotes(_ context.Context, r *contacter.GetCommsVotesRequest) (*contacter.CommsVoteResult, error) {
	p.operation = "read"
	entries := append([]string{""}, r.EntryIDs...)
	actor, comms := r.ActorID, r.CommsID
	if p.fault == "mutate request" {
		r.ActorID = "forged"
		r.EntryIDs[0] = "other"
	}
	return p.page(actor, comms, entries)
}

func (p *voteDomain) SetCommsVote(_ context.Context, r *contacter.ChangeCommsVoteRequest) (*contacter.CommsVoteResult, error) {
	p.operation, p.entry = "set", r.EntryID
	return p.page(r.ActorID, r.CommsID, []string{r.EntryID})
}

func (p *voteDomain) RemoveCommsVote(_ context.Context, r *contacter.ChangeCommsVoteRequest) (*contacter.CommsVoteResult, error) {
	p.operation, p.entry = "remove", r.EntryID
	return p.page(r.ActorID, r.CommsID, []string{r.EntryID})
}

func TestCommsVoteManagerAdmissionAndReceipts(t *testing.T) {
	native := fmt.Errorf("private-diagnostic: %w", router.ErrRouteDenied)
	for _, op := range []string{"read", "set", "remove"} {
		t.Run(op, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				want  error
				calls int
			}{
				{"success", nil, 1}, {"nil manager", contacter.ErrCommsVoteUnavailable, 0},
				{"nil context", contacter.ErrCommsVoteUnavailable, 0}, {"nil users", contacter.ErrCommsVoteUnavailable, 0},
				{"typed nil users", contacter.ErrCommsVoteUnavailable, 0},
				{"nil authority", contacter.ErrCommsVoteUnavailable, 0}, {"nil port", contacter.ErrCommsVoteUnavailable, 0},
				{"typed nil port", contacter.ErrCommsVoteUnavailable, 0}, {"nil request", contacter.ErrCommsVoteInvalid, 0},
				{"anonymous", usermanager.ErrUnableToIdentifyUser, 0}, {"forged actor", usermanager.ErrUnableToIdentifyUser, 0},
				{"wrong authority actor", usermanager.ErrUnableToIdentifyUser, 0}, {"denied", router.ErrRouteDenied, 0},
				{"native authority", native, 0}, {"native domain", native, 1},
				{"canceled", context.Canceled, 0}, {"cancel authority", context.Canceled, 0}, {"cancel domain", context.Canceled, 1},
				{"nil receipt", contacter.ErrCommsVoteUnavailable, 1}, {"wrong viewer", contacter.ErrCommsVoteUnavailable, 1},
				{"wrong parent", contacter.ErrCommsVoteUnavailable, 1}, {"wrong entry", contacter.ErrCommsVoteUnavailable, 1},
				{"wrong row parent", contacter.ErrCommsVoteUnavailable, 1}, {"wrong row entry", contacter.ErrCommsVoteUnavailable, 1},
				{"missing summary", contacter.ErrCommsVoteUnavailable, 1}, {"negative count", contacter.ErrCommsVoteUnavailable, 1},
				{"invalid direction", contacter.ErrCommsVoteUnavailable, 1}, {"impossible own vote", contacter.ErrCommsVoteUnavailable, 1},
				{"mutate request", nil, 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					p := &voteDomain{fault: tc.name}
					a := &statusAuthority{actor: "owner"}
					users := &voteUsers{}
					s := (&usermanager.Service{UserService: users}).WithAdministratorAuthorizer(a).WithCommsVotingService(p)
					ctx, cancel := context.WithCancel(profileContext("session"))
					defer cancel()
					actor := "owner"
					switch tc.name {
					case "nil manager":
						s = nil
					case "nil context":
						ctx = nil
					case "nil users":
						s.UserService = nil
					case "typed nil users":
						s.UserService = (*voteUsers)(nil)
					case "nil authority":
						s.WithAdministratorAuthorizer(nil)
					case "nil port":
						s.WithCommsVotingService(nil)
					case "typed nil port":
						s.WithCommsVotingService((*voteDomain)(nil))
					case "anonymous":
						ctx = context.Background()
					case "forged actor":
						actor = "other"
					case "wrong authority actor":
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
					case "cancel domain":
						p.cancel = cancel
					}
					query := &contacter.GetCommsVotesRequest{ActorID: actor, CommsID: "contact", EntryIDs: []string{"entry"}}
					command := &contacter.ChangeCommsVoteRequest{ActorID: actor, CommsID: "contact", EntryID: "entry"}
					if tc.name == "nil request" {
						query, command = nil, nil
					}
					var got *usermanager.CommsVotePage
					var err error
					switch op {
					case "read":
						got, err = s.GetCommsVotes(ctx, query)
					case "set":
						got, err = s.SetCommsVote(ctx, command)
					case "remove":
						got, err = s.RemoveCommsVote(ctx, command)
					}
					require.Equal(t, tc.want, err)
					require.Equal(t, tc.calls, p.calls)
					wantLookups := 0
					if op == "read" && tc.want == nil {
						wantLookups = 1
					}
					require.Equal(t, wantLookups, users.lookups, "enrichment must follow valid admission and receipts")
					if err != nil {
						require.Nil(t, got)
					} else {
						require.Equal(t, op, p.operation)
					}
					if query != nil {
						require.Equal(t, []string{"entry"}, query.EntryIDs)
						require.Equal(t, actor, query.ActorID)
					}
				})
			}
		})
	}
}

func TestCommsVoteParticipantBoundary(t *testing.T) {
	for _, op := range []string{"read", "set", "remove"} {
		for _, tc := range []struct {
			name     string
			want     error
			readOnly bool
		}{
			{"success", nil, false},
			{"lookup fails", nil, false},
			{"cancel lookup", context.Canceled, true},
			{"missing authors", contacter.ErrCommsVoteUnavailable, true},
			{"wrong author entry", contacter.ErrCommsVoteUnavailable, true},
			{"unexpected authors", contacter.ErrCommsVoteUnavailable, false},
			{"original sender", contacter.ErrCommsVoteUnavailable, false},
		} {
			if tc.readOnly && op != "read" {
				continue
			}
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(profileContext("session"))
				defer cancel()
				users := &voteUsers{}
				if tc.name == "lookup fails" {
					users.err = fmt.Errorf("private profile lookup failure")
				}
				if tc.name == "cancel lookup" {
					users.cancel = cancel
				}
				p := &voteDomain{fault: tc.name}
				s := (&usermanager.Service{UserService: users}).WithAdministratorAuthorizer(&statusAuthority{actor: "owner"}).WithCommsVotingService(p)
				var page *usermanager.CommsVotePage
				var err error
				switch op {
				case "read":
					page, err = s.GetCommsVotes(ctx, &contacter.GetCommsVotesRequest{ActorID: "owner", CommsID: "contact", EntryIDs: []string{"entry"}})
				case "set":
					page, err = s.SetCommsVote(ctx, &contacter.ChangeCommsVoteRequest{ActorID: "owner", CommsID: "contact", EntryID: "entry", Vote: 1})
				case "remove":
					page, err = s.RemoveCommsVote(ctx, &contacter.ChangeCommsVoteRequest{ActorID: "owner", CommsID: "contact", EntryID: "entry"})
				}
				require.ErrorIs(t, err, tc.want)
				if err != nil {
					require.Nil(t, page)
				} else {
					raw, err := json.Marshal(page)
					require.NoError(t, err)
					var body map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(raw, &body))
					require.Len(t, body, 4)
					require.Equal(t, "[]", string(body["participants"]))
					require.NotContains(t, string(raw), "EntryAuthors")
					require.NotContains(t, string(raw), "private profile")
				}
				wantLookups := 0
				if op == "read" && (tc.want == nil || tc.name == "cancel lookup") {
					wantLookups = 1
				}
				require.Equal(t, wantLookups, users.lookups)
			})
		}
	}
}

func TestCommsVoteHandlerTransport(t *testing.T) {
	for _, op := range []string{"read", "set", "remove"} {
		t.Run(op, func(t *testing.T) {
			for _, tc := range []struct {
				name          string
				status, calls int
			}{
				{"success", 200, 1}, {"anonymous", 401, 0}, {"denied", 403, 0},
				{"native error", 400, 1}, {"host override", 409, 1}, {"nil port", 503, 0},
				{"no manager capability", 503, 0}, {"nil handler", 503, 0}, {"nil receipt", 503, 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					p := &voteDomain{fault: tc.name}
					a := &statusAuthority{actor: "owner"}
					s := (&usermanager.Service{UserService: &voteUsers{}}).WithAdministratorAuthorizer(a).WithCommsVotingService(p)
					h := &usermanager.Handler{Service: s}
					ctx := profileContext("session")
					switch tc.name {
					case "anonymous":
						ctx = context.Background()
					case "denied":
						a.err = router.ErrRouteDenied
					case "native error", "host override":
						p.err = fmt.Errorf("private-diagnostic: %w", contacter.ErrCommsVoteInvalid)
					case "nil port":
						s.WithCommsVotingService(nil)
					case "no manager capability":
						h.Service = &legacyVoteManager{}
					case "nil handler":
						h = nil
					}
					if tc.name == "host override" {
						h.ErrorMaps = []reply.ErrorManifest{{contacter.ErrCommsVoteInvalid: {StatusCode: 409, Code: "CUSTOM_VOTE", Title: "Review selection."}}}
					}
					r := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/?actor_id=forged&entry_id=entry", strings.NewReader(`{"vote":1}`)).WithContext(ctx), map[string]string{"id": "contact", "entryId": "entry"})
					w := httptest.NewRecorder()
					switch op {
					case "read":
						h.GetCommsVotes(w, r)
					case "set":
						h.SetCommsVote(w, r)
					case "remove":
						h.RemoveCommsVote(w, r)
					}
					require.Equal(t, tc.status, w.Code, w.Body.String())
					require.Equal(t, tc.calls, p.calls)
					require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
					require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
					require.NotContains(t, w.Body.String(), "private-diagnostic")
					if tc.name == "success" {
						require.Equal(t, "owner", p.actor)
						require.Equal(t, "contact", p.comms)
					}
					if tc.name == "native error" {
						require.Contains(t, w.Body.String(), "HOST_COMMS_VOTE_INVALID")
					}
					if tc.name == "host override" {
						require.Contains(t, w.Body.String(), "CUSTOM_VOTE")
					}
				})
			}
		})
	}
	for _, body := range []string{"", `{}`, `null`, `{"vote":null}`, `{"vote":1,"actor_id":"forged"}`, `{"vote":1}{}`, strings.Repeat(" ", 1024) + `{"vote":1}`} {
		t.Run("invalid body "+fmt.Sprint(len(body))+body[:min(len(body), 30)], func(t *testing.T) {
			p := &voteDomain{}
			s := (&usermanager.Service{UserService: &voteUsers{}}).WithAdministratorAuthorizer(&statusAuthority{actor: "owner"}).WithCommsVotingService(p)
			h := &usermanager.Handler{Service: s}
			w := httptest.NewRecorder()
			h.SetCommsVote(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).WithContext(profileContext("session")))
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Zero(t, p.calls)
		})
	}
}

func TestCommsVoteRouteOwnership(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			p := &voteDomain{}
			s := (&usermanager.Service{UserService: &voteUsers{}}).WithAdministratorAuthorizer(&statusAuthority{actor: "owner"}).WithCommsVotingService(p)
			routes := router.NewRouter(nil, nil)
			publish := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					next.ServeHTTP(w, r.WithContext(profileContext("session")))
				})
			}
			usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{Router: routes, Handler: &usermanager.Handler{Service: s}, EnableCommsVoting: enabled, AdminOnlyMiddleware: publish, ActiveValidApiTokenOrJWTMiddleware: publish, ValidApiTokenOrJWTMiddleware: publish, RateLimitOrActiveMiddleware: publish})
			require.NoError(t, routes.ValidateRoutePolicies())
			seen := map[string]string{}
			for _, def := range routes.RouteInventory() {
				if !strings.HasPrefix(def.Operation, "commsconversation.") {
					continue
				}
				require.Equal(t, router.AdminSession, def.Access)
				require.Len(t, def.Methods, 2)
				require.Contains(t, def.Methods, http.MethodOptions)
				for _, method := range def.Methods {
					if method != http.MethodOptions {
						key := method + " " + def.Path
						require.NotContains(t, seen, key)
						seen[key] = def.Operation
					}
				}
			}
			if !enabled {
				require.Empty(t, seen)
				return
			}
			require.Equal(t, map[string]string{
				"GET /api/v1/ums/comms/{id}/conversation/votes":             "commsconversation.ReadVotes",
				"POST /api/v1/ums/comms/{id}/vote":                          "commsconversation.SetVote",
				"DELETE /api/v1/ums/comms/{id}/vote":                        "commsconversation.RemoveVote",
				"POST /api/v1/ums/comms/{id}/conversation/{entryId}/vote":   "commsconversation.SetVote",
				"DELETE /api/v1/ums/comms/{id}/conversation/{entryId}/vote": "commsconversation.RemoveVote",
			}, seen)
			for _, method := range []string{http.MethodPost, http.MethodDelete} {
				for _, path := range []string{"/api/v1/ums/comms/contact/vote", "/api/v1/ums/comms/contact/conversation/entry/vote"} {
					for _, tc := range []struct {
						owner  string
						status int
					}{{"", 428}, {"other", 412}, {"owner", 200}} {
						t.Run(method+path+tc.owner, func(t *testing.T) {
							r := httptest.NewRequest(method, path, strings.NewReader(`{"vote":1}`))
							if tc.owner != "" {
								r.Header.Set(usermanager.CommsOwnerHeader, tc.owner)
							}
							before := p.calls
							w := httptest.NewRecorder()
							routes.GetRouter().ServeHTTP(w, r)
							require.Equal(t, tc.status, w.Code, w.Body.String())
							if tc.status != 200 {
								require.Equal(t, before, p.calls)
							}
						})
					}
				}
			}
			require.NoError(t, usermanager.RequireCommsConversationRoutes(routes, append(usermanager.DependencyErrorMaps(), usermanager.UsermanagerErrorMap)...))
			err := usermanager.RequireCommsConversationRoutes(routes, reply.ErrorManifest{contacter.ErrCommsVoteInvalid: {Code: usermanager.CommsOwnerChangedCode}})
			require.ErrorContains(t, err, "collides")
		})
	}
}

func TestCommsVoteRoutesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name              string
		missingMiddleware bool
		owner             string
		want              int
	}{
		{"legacy handler", false, "owner", 503},
		{"missing owner before legacy handler", false, "", 428},
		{"missing administrator middleware", true, "owner", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := router.NewRouter(nil, nil)
			publish := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					next.ServeHTTP(w, r.WithContext(profileContext("session")))
				})
			}
			req := &usermanager.AttachRoutesRequest{Router: routes, Handler: &legacyVoteHandler{UsermanagerHandler: &usermanager.Handler{}}, EnableCommsVoting: true, AdminOnlyMiddleware: publish, ActiveValidApiTokenOrJWTMiddleware: publish, ValidApiTokenOrJWTMiddleware: publish, RateLimitOrActiveMiddleware: publish}
			if tc.missingMiddleware {
				req.AdminOnlyMiddleware = nil
			}
			usermanager.AttachRoutes(req)
			if tc.missingMiddleware {
				require.ErrorIs(t, routes.ValidateRoutePolicies(), router.ErrRouteConfiguration)
				return
			}
			require.NoError(t, routes.ValidateRoutePolicies())
			r := httptest.NewRequest(http.MethodPost, "/api/v1/ums/comms/contact/vote", strings.NewReader(`{"vote":1}`))
			if tc.owner != "" {
				r.Header.Set(usermanager.CommsOwnerHeader, tc.owner)
			}
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
}
