package blueprint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/ritwickdey/querydecoder"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// blueprintActorContext publishes fixture authentication, not credential validation.
func blueprintActorContext(ctx context.Context, actor string) context.Context {
	return accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), true)
}

// invokeBlueprintActor exercises each actual service boundary with a distinct
// target and caller. Nil requests retain their concrete pointer type.
func invokeBlueprintActor(s *Service, ctx context.Context, op, actor string, absent bool) error {
	var err error
	switch op {
	case "create":
		r := &CreateBlueprintRequest{Name: "Example", Kind: "demo", ActorID: actor}
		if absent {
			r = nil
		}
		_, err = s.CreateBlueprint(ctx, r)
	case "get":
		r := &GetBlueprintByIDRequest{ID: "bp-1", ActorID: actor}
		if absent {
			r = nil
		}
		_, err = s.GetBlueprintByID(ctx, r)
	case "update":
		r := &UpdateBlueprintRequest{ID: "bp-1", Name: "Changed", ActorID: actor}
		if absent {
			r = nil
		}
		_, err = s.UpdateBlueprint(ctx, r)
	case "delete":
		r := &DeleteBlueprintRequest{ID: "bp-1", ActorID: actor}
		if absent {
			r = nil
		}
		_, err = s.DeleteBlueprint(ctx, r)
	default:
		panic("unknown blueprint actor operation")
	}
	return err
}

func TestBlueprintActorBoundaries(t *testing.T) {
	for _, op := range []string{"create", "get", "update", "delete"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"trusted internal", nil}, {"verified", nil}, {"cached user", nil},
			{"empty", ErrBlueprintUserIDIsRequired}, {"padded", ErrBlueprintUserIDIsRequired},
			{"conflicting caller", ErrBlueprintUserIDIsRequired}, {"ID only", ErrBlueprintUserIDIsRequired},
			{"anonymous", ErrBlueprintUserIDIsRequired}, {"cached user only", ErrBlueprintUserIDIsRequired},
			{"conflicting user", ErrBlueprintUserIDIsRequired}, {"nil cached user", ErrBlueprintUserIDIsRequired},
			{"nil context", ErrBlueprintInvalidPayload}, {"nil request", ErrBlueprintInvalidPayload},
			{"nil service", ErrBlueprintUnavailable}, {"nil repository", ErrBlueprintUnavailable},
			{"typed nil repository", ErrBlueprintUnavailable}, {"cancelled", context.Canceled},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				calls := 0
				port := &mockBlueprintRepository{
					createFunc: func(_ context.Context, b *Blueprint) (*Blueprint, error) {
						calls++
						require.Equal(t, "caller", b.CreatedByUserID)
						return b, nil
					},
					getFunc: func(_ context.Context, id string) (*Blueprint, error) {
						calls++
						require.Equal(t, "bp-1", id)
						return &Blueprint{ID: id}, nil
					},
					updateFunc: func(_ context.Context, b *Blueprint) (*Blueprint, error) {
						calls++
						require.Equal(t, "bp-1", b.ID)
						require.Equal(t, "caller", b.UpdatedByUserID)
						return b, nil
					},
					deleteFunc: func(_ context.Context, id string) error { calls++; require.Equal(t, "bp-1", id); return nil },
				}
				s, ctx, actor := NewService(port), context.Background(), "caller"
				switch tc.name {
				case "verified":
					ctx = blueprintActorContext(ctx, actor)
				case "cached user":
					ctx = accesshelpers.TransitUserWith(blueprintActorContext(ctx, actor), &userv2.UniversalUser{ID: actor})
				case "empty":
					actor = ""
				case "padded":
					actor = " caller "
				case "conflicting caller":
					ctx = blueprintActorContext(ctx, "other")
				case "ID only":
					ctx = accesshelpers.TransitWith(ctx, actor)
				case "anonymous":
					ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), false)
				case "cached user only":
					ctx = accesshelpers.TransitUserWith(ctx, &userv2.UniversalUser{ID: actor})
				case "conflicting user":
					ctx = accesshelpers.TransitUserWith(blueprintActorContext(ctx, actor), &userv2.UniversalUser{ID: "other"})
				case "nil cached user":
					ctx = accesshelpers.TransitUserWith(blueprintActorContext(ctx, actor), nil)
				case "nil context":
					ctx = nil
				case "nil service":
					s = nil
				case "nil repository":
					s.BlueprintRepository = nil
				case "typed nil repository":
					s.BlueprintRepository = (*mockBlueprintRepository)(nil)
				case "cancelled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				err := invokeBlueprintActor(s, ctx, op, actor, tc.name == "nil request")
				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
					require.Zero(t, calls)
					return
				}
				require.NoError(t, err)
				wantCalls := 1
				if op == "update" {
					wantCalls = 2
				}
				require.Equal(t, wantCalls, calls)
			})
		}
	}
}

func TestBlueprintSelectedResults(t *testing.T) {
	for _, op := range []string{"get", "update"} {
		for _, tc := range []struct {
			name          string
			failure, want error
		}{
			{"nil result", nil, ErrBlueprintUnavailable}, {"foreign ID", nil, ErrBlueprintUnavailable},
			{"cancel after read", nil, context.Canceled},
			{"wrapped absence", fmt.Errorf("private: %w", ErrBlueprintResourceNotFound), nil},
			{"mixed failure", errors.Join(ErrBlueprintResourceNotFound, errors.New("private")), nil},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				writes := 0
				port := &mockBlueprintRepository{getFunc: func(context.Context, string) (*Blueprint, error) {
					if tc.name == "cancel after read" {
						cancel()
					}
					if tc.name == "nil result" {
						return nil, nil
					}
					id := "bp-1"
					if tc.name == "foreign ID" {
						id = "other"
					}
					return &Blueprint{ID: id}, tc.failure
				}, updateFunc: func(_ context.Context, b *Blueprint) (*Blueprint, error) { writes++; return b, nil }}
				err := invokeBlueprintActor(NewService(port), ctx, op, "caller", false)
				if tc.failure != nil {
					require.Same(t, tc.failure, err)
				} else {
					require.ErrorIs(t, err, tc.want)
				}
				require.Zero(t, writes)
			})
		}
	}
}

func TestBlueprintWriteResultsAndSnapshots(t *testing.T) {
	for _, op := range []string{"create", "update", "delete"} {
		for _, scenario := range []string{"success", "native failure", "cancel after write", "nil result", "changed ID", "changed actor"} {
			if op == "delete" && (scenario == "nil result" || scenario == "changed ID" || scenario == "changed actor") {
				continue
			}
			t.Run(op+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				original := &Blueprint{ID: "bp-1", Name: "Before", Kind: "demo", CreatedByUserID: "owner", CreatedAt: "original"}
				before := *original
				failure := fmt.Errorf("private: %w", ErrBlueprintDatabaseError)
				writes := 0
				write := func(_ context.Context, b *Blueprint) (*Blueprint, error) {
					writes++
					if op == "update" {
						require.NotSame(t, original, b)
						require.Equal(t, "owner", b.CreatedByUserID)
						require.Equal(t, "Changed", b.Name)
						require.NotEmpty(t, b.UpdatedAt)
					}
					switch scenario {
					case "native failure":
						return nil, failure
					case "cancel after write":
						cancel()
					case "nil result":
						return nil, nil
					case "changed ID":
						b.ID = "other"
					case "changed actor":
						b.CreatedByUserID = "other"
						b.UpdatedByUserID = "other"
					}
					return b, nil
				}
				port := &mockBlueprintRepository{getFunc: func(context.Context, string) (*Blueprint, error) { return original, nil }, createFunc: write, updateFunc: write,
					deleteFunc: func(context.Context, string) error { _, err := write(ctx, &Blueprint{}); return err }}
				err := invokeBlueprintActor(NewService(port), ctx, op, "caller", false)
				switch scenario {
				case "success":
					require.NoError(t, err)
				case "native failure":
					require.Same(t, failure, err)
				case "cancel after write":
					require.ErrorIs(t, err, context.Canceled)
				default:
					require.ErrorIs(t, err, ErrBlueprintUnavailable)
				}
				require.Equal(t, 1, writes, "writes must not be retried")
				require.Equal(t, before, *original)
			})
		}
	}
}

func TestBlueprintActorTransportIsolation(t *testing.T) {
	for _, kind := range []string{"create", "get", "update", "delete"} {
		for _, value := range []string{`"spoofed"`, `null`} {
			t.Run(kind+"/"+value, func(t *testing.T) {
				var request any
				switch kind {
				case "create":
					request = &CreateBlueprintRequest{ActorID: "verified"}
				case "get":
					request = &GetBlueprintByIDRequest{ActorID: "verified"}
				case "update":
					request = &UpdateBlueprintRequest{ActorID: "verified"}
				case "delete":
					request = &DeleteBlueprintRequest{ActorID: "verified"}
				}
				body := `{"ActorID":VALUE,"actor_id":VALUE,"actorid":VALUE,"created_by_user_id":VALUE,"updated_by_user_id":VALUE,"UserID":VALUE}`
				require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(body, "VALUE", value)), request))
				r := httptest.NewRequest("GET", "/?ActorID=spoofed&actor_id=spoofed&-=spoofed", nil)
				require.NoError(t, querydecoder.New(r.URL.Query()).Decode(request))
				switch r := request.(type) {
				case *CreateBlueprintRequest:
					require.Equal(t, "verified", r.ActorID)
				case *GetBlueprintByIDRequest:
					require.Equal(t, "verified", r.ActorID)
				case *UpdateBlueprintRequest:
					require.Equal(t, "verified", r.ActorID)
				case *DeleteBlueprintRequest:
					require.Equal(t, "verified", r.ActorID)
				}
				encoded, err := json.Marshal(request)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "verified")
			})
		}
	}
}

func TestBlueprintMappersEntry(t *testing.T) {
	for _, op := range []string{"create", "get", "list"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"nil request", ErrBlueprintInvalidPayload}, {"nil URL", ErrBlueprintInvalidPayload}, {"cancelled", context.Canceled},
			{"nil body", ErrBlueprintInvalidPayload}, {"typed nil body", ErrBlueprintInvalidPayload},
			{"ID only", ErrBlueprintUserIDIsRequired}, {"padded verified ID", ErrBlueprintUserIDIsRequired}, {"conflicting user", ErrBlueprintUserIDIsRequired},
		} {
			if op == "list" && tc.want == ErrBlueprintUserIDIsRequired {
				continue
			}
			if op != "create" && (tc.name == "nil body" || tc.name == "typed nil body") {
				continue
			}
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"Example","kind":"demo"}`))
				r = mux.SetURLVars(r, map[string]string{BlueprintURIVariableID: "bp-1"})
				ctx := context.Background()
				switch tc.name {
				case "nil body":
					ctx = blueprintActorContext(ctx, "caller")
					r.Body = nil
				case "typed nil body":
					ctx = blueprintActorContext(ctx, "caller")
					r.Body = (*io.PipeReader)(nil)
				case "ID only":
					ctx = accesshelpers.TransitWith(ctx, "caller")
				case "padded verified ID":
					ctx = blueprintActorContext(ctx, " caller ")
				case "conflicting user":
					ctx = accesshelpers.TransitUserWith(blueprintActorContext(ctx, "caller"), &userv2.UniversalUser{ID: "other"})
				case "cancelled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				r = r.WithContext(ctx)
				if tc.name == "nil request" {
					r = nil
				}
				if tc.name == "nil URL" {
					r.URL = nil
				}
				var err error
				switch op {
				case "create":
					_, err = MapRequestToCreateBlueprintRequest(r, validator.NewValidator())
				case "get":
					_, err = MapRequestToGetBlueprintByIDRequest(r, validator.NewValidator())
				case "list":
					_, err = MapRequestToGetBlueprintsRequest(r, validator.NewValidator())
				}
				require.ErrorIs(t, err, tc.want)
			})
		}
	}
}

func TestBlueprintComposedHTTPErrorMaps(t *testing.T) {
	for _, op := range []string{"create", "get", "list"} {
		for _, tc := range []struct {
			name    string
			failure error
			status  int
			code    string
		}{
			{"wrapped", fmt.Errorf("private diagnostic: %w", ErrBlueprintResourceNotFound), 404, "BLP0-005"},
			{"joined validation", errors.Join(ErrBlueprintNameIsRequired, ErrBlueprintKindIsRequired), 400, "BLP0-002"},
			{"mixed", errors.Join(ErrBlueprintResourceNotFound, errors.New("private diagnostic")), 500, ""},
			{"unknown", errors.New("private diagnostic"), 500, ""},
			{"host override", fmt.Errorf("private diagnostic: %w", ErrBlueprintUnavailable), 502, "HOST-BLUEPRINT"},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				calls := 0
				port := &mockBlueprintRepository{
					createFunc: func(context.Context, *Blueprint) (*Blueprint, error) { calls++; return nil, tc.failure },
					getFunc:    func(context.Context, string) (*Blueprint, error) { calls++; return nil, tc.failure },
					listFunc:   func(context.Context, *GetBlueprintsRequest) ([]Blueprint, error) { calls++; return nil, tc.failure },
				}
				core, logs := observer.New(zap.DebugLevel)
				ctx := logger.TransitWith(blueprintActorContext(context.Background(), "private caller"), zap.New(core))
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"private name","kind":"private kind"}`)).WithContext(ctx)
				r = mux.SetURLVars(r, map[string]string{BlueprintURIVariableID: "private target"})
				h := NewHandler(NewService(port), validator.NewValidator(), reply.ErrorManifest{ErrBlueprintUnavailable: {StatusCode: 502, Code: "HOST-BLUEPRINT"}})
				w := httptest.NewRecorder()
				switch op {
				case "create":
					h.CreateBlueprint(w, r)
				case "get":
					h.GetBlueprintByID(w, r)
				case "list":
					h.GetBlueprints(w, r)
				}
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.Equal(t, 1, calls)
				if tc.code != "" {
					require.Contains(t, w.Body.String(), tc.code)
				}
				require.NotContains(t, w.Body.String(), "private")
				for _, entry := range logs.All() {
					require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private")
				}
			})
		}
	}
}

func TestBlueprintNaturalKeyResults(t *testing.T) {
	failure := fmt.Errorf("private: %w", ErrBlueprintDatabaseError)
	for _, tc := range []struct {
		name string
		want error
	}{
		{"normalised", nil}, {"nil request", ErrBlueprintInvalidPayload},
		{"missing name", ErrBlueprintNameIsRequired}, {"missing kind", ErrBlueprintKindIsRequired},
		{"nil result", ErrBlueprintUnavailable}, {"wrong name", ErrBlueprintUnavailable},
		{"wrong kind", ErrBlueprintUnavailable}, {"empty ID", ErrBlueprintUnavailable},
		{"native failure", failure}, {"cancel after read", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			r := &GetBlueprintByNameRequest{Name: " Example ", Kind: " Demo "}
			if tc.name == "nil request" {
				r = nil
			}
			if tc.name == "missing name" {
				r.Name = " "
			}
			if tc.name == "missing kind" {
				r.Kind = " "
			}
			port := &mockBlueprintRepository{nameFunc: func(_ context.Context, name, kind string) (*Blueprint, error) {
				calls++
				require.Equal(t, "Example", name)
				require.Equal(t, "demo", kind)
				b := &Blueprint{ID: "bp-1", Name: name, Kind: kind}
				switch tc.name {
				case "nil result":
					return nil, nil
				case "wrong name":
					b.Name = "other"
				case "wrong kind":
					b.Kind = "other"
				case "empty ID":
					b.ID = ""
				case "native failure":
					return nil, failure
				case "cancel after read":
					cancel()
				}
				return b, nil
			}}
			got, err := NewService(port).GetBlueprintByName(ctx, r)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, "bp-1", got.Blueprint.ID)
			}
			if tc.name == "nil request" || tc.name == "missing name" || tc.name == "missing kind" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestBlueprintUpdateInputSnapshot(t *testing.T) {
	for _, scenario := range []string{"empty fields retain values", "nonempty fields replace", "request scalar mutation during read"} {
		t.Run(scenario, func(t *testing.T) {
			original := &Blueprint{ID: "bp-1", Name: "Before", Kind: "demo", Description: "Keep", Status: "active", Metadata: map[string]interface{}{"source": "original"}, CreatedByUserID: "owner", CreatedAt: "created"}
			req := &UpdateBlueprintRequest{ID: "bp-1", ActorID: "caller"}
			if scenario == "nonempty fields replace" {
				req.Name = " After "
				req.Kind = " New "
				req.Description = " Changed "
				req.Status = " Archived "
				req.Metadata = map[string]interface{}{"source": "replacement"}
			}
			port := &mockBlueprintRepository{getFunc: func(context.Context, string) (*Blueprint, error) {
				if scenario == "request scalar mutation during read" {
					req.ActorID = "other"
					req.ID = "other"
					req.Name = "Wrong"
				}
				return original, nil
			}}
			got, err := NewService(port).UpdateBlueprint(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, "bp-1", got.Blueprint.ID)
			require.Equal(t, "caller", got.Blueprint.UpdatedByUserID)
			require.Equal(t, "owner", got.Blueprint.CreatedByUserID)
			require.Equal(t, "created", got.Blueprint.CreatedAt)
			if scenario == "nonempty fields replace" {
				require.Equal(t, "After", got.Blueprint.Name)
				require.Equal(t, "new", got.Blueprint.Kind)
				require.Equal(t, "Changed", got.Blueprint.Description)
				require.Equal(t, "archived", got.Blueprint.Status)
				require.Equal(t, "replacement", got.Blueprint.Metadata["source"])
			} else {
				require.Equal(t, "Before", got.Blueprint.Name)
				require.Equal(t, "demo", got.Blueprint.Kind)
				require.Equal(t, "Keep", got.Blueprint.Description)
				require.Equal(t, "active", got.Blueprint.Status)
				require.Equal(t, "original", got.Blueprint.Metadata["source"])
			}
			require.Equal(t, "Before", original.Name)
			require.Empty(t, original.UpdatedAt)
			require.Empty(t, original.UpdatedByUserID)
		})
	}
}
