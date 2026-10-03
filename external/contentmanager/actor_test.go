package contentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/ritwickdey/querydecoder"
	"github.com/stretchr/testify/require"
)

const contentActorID = "00000000-0000-4000-8000-000000000001"
const contentTargetID = "00000000-0000-4000-8000-000000000002"

// contentMapper adapts production fenders without replacing their real validator.
func contentMapper[T any](f func(*http.Request, contentManagerValidator) (*T, error)) func(*http.Request) (any, error) {
	return func(r *http.Request) (any, error) { return f(r, validator.NewValidator()) }
}

func contentActorRequest(ctx context.Context, body string) *http.Request {
	if body == "" {
		body = `{"title":"Example","text":"Body","type":"faq","ActorID":"forged","actorid":null,"UserId":"forged","PostId":"forged"}`
	}
	r := httptest.NewRequest(http.MethodPost, "/?ActorID=forged&UserId=forged&actor_id=forged&-=forged&hard_delete=true", strings.NewReader(body)).WithContext(ctx)
	return mux.SetURLVars(r, map[string]string{"postId": contentTargetID, "urlFriendlyId": "article-example"})
}

func TestContentActorMappers(t *testing.T) {
	for _, op := range []struct {
		name       string
		private    bool
		actorField string
		mapRequest func(*http.Request) (any, error)
	}{
		{"create", true, "ActorID", contentMapper(mapRequestToCreatePostRequest)},
		{"update", true, "ActorID", contentMapper(mapRequestToUpdatePostByIdRequest)},
		{"delete", true, "ActorID", contentMapper(mapRequestToDeletePostByIdRequest)},
		{"restore", true, "ActorID", contentMapper(mapRequestToRestorePostByIdRequest)},
		{"articles", false, "ActorID", contentMapper(mapRequestToGetArticlesRequest)},
		{"article", false, "ActorID", contentMapper(mapRequestToGetArticleItemByUrlFriendlyIdRequest)},
		{"changelog", false, "ActorID", contentMapper(mapRequestToGetChangelogItemsRequest)},
		{"changelog item", false, "ActorID", contentMapper(mapRequestToGetChangelogItemByUrlFriendlyIdRequest)},
		{"faq", false, "ActorID", contentMapper(mapRequestToGetFaqItemsRequest)},
		{"glossary", false, "ActorID", contentMapper(mapRequestToGetGlossaryItemsRequest)},
		{"sitemap", false, "ActorID", contentMapper(mapRequestToGetArticleSitemapItemsRequest)},
		{"latest", false, "ActorID", contentMapper(mapRequestToGetLatestPostsByTypeRequest)},
		{"notifications", false, "UserID", contentMapper(mapRequestToGetLatestNotificationOverviewsRequest)},
	} {
		t.Run(op.name, func(t *testing.T) {
			for _, state := range []struct {
				name, id       string
				flag, verified bool
			}{
				{"verified", contentActorID, true, true}, {"placeholder", contentActorID, true, false}, {"ID only", contentActorID, false, false}, {"flag only", "", true, true}, {"absent", "", false, false},
			} {
				t.Run(state.name, func(t *testing.T) {
					ctx := accesshelpers.TransitWith(context.Background(), state.id)
					if state.flag {
						ctx = accesshelpers.TransitAuthenticatedWith(ctx, state.verified)
					}
					got, err := op.mapRequest(contentActorRequest(ctx, ""))
					want := ""
					if state.verified {
						want = state.id
					}
					if op.private && want == "" {
						require.ErrorIs(t, err, ErrUnauthorisedCMUser)
						return
					}
					require.NoError(t, err)
					value := reflect.ValueOf(got).Elem()
					require.Equal(t, want, value.FieldByName(op.actorField).String())
					if op.name == "update" {
						require.Equal(t, contentTargetID, value.FieldByName("PostId").String())
					}
					if op.name == "delete" || op.name == "restore" {
						require.Equal(t, contentTargetID, value.FieldByName("Id").String())
					}
					if op.name == "delete" {
						require.True(t, value.FieldByName("HardDelete").Bool())
					}
				})
			}
			for _, r := range []*http.Request{nil, {}} {
				_, err := op.mapRequest(r)
				require.ErrorIs(t, err, post.ErrPostBadRequest)
			}
		})
	}
}

func TestContentActorCodecs(t *testing.T) {
	for _, request := range []any{&post.CreatePostRequest{}, &post.UpdatePostRequest{}, &post.DeletePostByIdRequest{}, &post.RestorePostByIdRequest{}, &GetArticlesRequest{}, &GetArticleItemByUrlFriendlyIdRequest{}, &GetChangelogItemsRequest{}, &GetChangelogItemByUrlFriendlyIdRequest{}, &GetFaqItemsRequest{}, &GetGlossaryItemsRequest{}, &GetArticleSitemapItemsRequest{}, &GetLatestPostsByTypeRequest{}} {
		t.Run(reflect.TypeOf(request).String(), func(t *testing.T) {
			value := reflect.ValueOf(request).Elem()
			field, ok := value.Type().FieldByName("ActorID")
			require.True(t, ok)
			require.Equal(t, "-", field.Tag.Get("json"))
			require.Empty(t, field.Tag.Get("query"))
			require.Empty(t, field.Tag.Get("path"))
			for _, body := range []string{`{"ActorID":"forged","actorid":"forged","UserId":"forged"}`, `{"ActorID":null}`} {
				value.FieldByName("ActorID").SetString(contentActorID)
				require.NoError(t, json.Unmarshal([]byte(body), request))
				require.NoError(t, querydecoder.New(url.Values{"ActorID": {"forged"}, "actorid": {"forged"}, "-": {"forged"}}).Decode(request))
				require.Equal(t, contentActorID, value.FieldByName("ActorID").String())
				data, err := json.Marshal(request)
				require.NoError(t, err)
				require.NotContains(t, string(data), contentActorID)
			}
		})
	}
}

func TestContentUpdateTransportFields(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"editable fields", `{"title":"New","PostId":"forged"}`, nil},
		{"same target replacement", `{"post":{"id":"` + contentTargetID + `","created_by_id":"forged"}}`, post.ErrInvalidPostPayload},
		{"different target replacement", `{"post":{"id":"other"}}`, post.ErrInvalidPostPayload},
		{"malformed payload", `{`, post.ErrInvalidPostPayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mapRequestToUpdatePostByIdRequest(contentActorRequest(contentManagerRequestContext(contentActorID, true), tc.body), validator.NewValidator())
			require.ErrorIs(t, err, tc.want)
			if err == nil {
				require.Equal(t, contentTargetID, got.PostId)
				require.Equal(t, contentActorID, got.ActorID)
				require.Nil(t, got.Post)
			}
		})
	}
}

// contentMutationPort records the actor/target crossing the manager boundary.
type contentMutationPort struct {
	postService
	calls                  int
	actor, target          string
	failure                error
	nilResult, wrongTarget bool
}

func (s *contentMutationPort) record(actor, target string) {
	s.calls++
	s.actor = actor
	s.target = target
}
func (s *contentMutationPort) result(id string) *post.Post {
	if s.wrongTarget {
		id = "other"
	}
	return &post.Post{Id: id}
}
func (s *contentMutationPort) CreatePost(_ context.Context, r *post.CreatePostRequest) (*post.CreatePostResponse, error) {
	s.record(r.ActorID, "")
	if s.nilResult {
		return nil, s.failure
	}
	return &post.CreatePostResponse{Post: s.result(contentTargetID)}, s.failure
}
func (s *contentMutationPort) UpdatePost(_ context.Context, r *post.UpdatePostRequest) (*post.UpdatePostResponse, error) {
	s.record(r.ActorID, r.PostId)
	if s.nilResult {
		return nil, s.failure
	}
	return &post.UpdatePostResponse{Post: s.result(r.PostId)}, s.failure
}
func (s *contentMutationPort) DeletePostById(_ context.Context, r *post.DeletePostByIdRequest) (*post.DeletePostByIdResponse, error) {
	s.record(r.ActorID, r.Id)
	if s.nilResult {
		return nil, s.failure
	}
	return &post.DeletePostByIdResponse{}, s.failure
}
func (s *contentMutationPort) RestorePostById(_ context.Context, r *post.RestorePostByIdRequest) (*post.RestorePostByIdResponse, error) {
	s.record(r.ActorID, r.Id)
	if s.nilResult {
		return nil, s.failure
	}
	return &post.RestorePostByIdResponse{Post: s.result(r.Id)}, s.failure
}

func contentMutation(s *Service, ctx context.Context, op, actor string) error {
	switch op {
	case "create":
		_, err := s.CreatePost(ctx, &CreatePostRequest{CreatePostRequest: &post.CreatePostRequest{ActorID: actor}})
		return err
	case "update":
		_, err := s.UpdatePostById(ctx, &UpdatePostByIdRequest{UpdatePostRequest: &post.UpdatePostRequest{ActorID: actor, PostId: contentTargetID}})
		return err
	case "delete":
		_, err := s.DeletePostById(ctx, &DeletePostByIdRequest{DeletePostByIdRequest: &post.DeletePostByIdRequest{ActorID: actor, Id: contentTargetID}})
		return err
	default:
		_, err := s.RestorePostById(ctx, &RestorePostByIdRequest{RestorePostByIdRequest: &post.RestorePostByIdRequest{ActorID: actor, Id: contentTargetID}})
		return err
	}
}

func TestContentMutationAuthority(t *testing.T) {
	outage := errors.New("private diagnostic")
	wrapped := fmt.Errorf("private diagnostic: %w", user.ErrUserNotFound)
	for _, op := range []string{"create", "update", "delete", "restore"} {
		for _, tc := range []struct {
			name                                                                                                                                 string
			actor                                                                                                                                string
			member, wrongUser, nilUser, nilResponse, noUsers, typedNilUsers, noPosts, cancelBefore, cancelLookup, anonymous, mismatch, nilResult bool
			lookupErr, postErr, want                                                                                                             error
		}{
			{name: "trusted administrator", actor: contentActorID},
			{name: "member", actor: contentActorID, member: true, want: ErrUnauthorisedCMUser},
			{name: "empty actor", want: ErrUnauthorisedCMUser},
			{name: "blank actor", actor: " ", want: ErrUnauthorisedCMUser},
			{name: "anonymous placeholder", actor: contentActorID, anonymous: true, want: ErrUnauthorisedCMUser},
			{name: "context disagreement", actor: contentActorID, mismatch: true, want: ErrUnauthorisedCMUser},
			{name: "wrong authority identity", actor: contentActorID, wrongUser: true, want: ErrContentManagerUnavailable},
			{name: "nil authority user", actor: contentActorID, nilUser: true, want: ErrContentManagerUnavailable},
			{name: "nil authority response", actor: contentActorID, nilResponse: true, want: ErrContentManagerUnavailable},
			{name: "missing user port", actor: contentActorID, noUsers: true, want: ErrContentManagerUnavailable},
			{name: "typed nil user port", actor: contentActorID, typedNilUsers: true, want: ErrContentManagerUnavailable},
			{name: "missing post port", actor: contentActorID, noPosts: true, want: ErrContentManagerUnavailable},
			{name: "native lookup outage", actor: contentActorID, lookupErr: outage, want: outage},
			{name: "wrapped lookup failure", actor: contentActorID, lookupErr: wrapped, want: wrapped},
			{name: "joined lookup failure", actor: contentActorID, lookupErr: errors.Join(user.ErrUserNotFound, outage), want: outage},
			{name: "native domain failure", actor: contentActorID, postErr: wrapped, want: wrapped},
			{name: "nil domain result", actor: contentActorID, nilResult: true, want: ErrContentManagerUnavailable},
			{name: "canceled before lookup", actor: contentActorID, cancelBefore: true, want: context.Canceled},
			{name: "canceled during lookup", actor: contentActorID, cancelLookup: true, want: context.Canceled},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var requestCtx context.Context = ctx
				if tc.anonymous {
					requestCtx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, tc.actor), false)
				}
				if tc.mismatch {
					requestCtx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "other"), true)
				}
				u := &user.UniversalUser{ID: tc.actor, Roles: []string{user.UserRoleAdmin}}
				if tc.member {
					u.Roles = nil
				}
				if tc.wrongUser {
					u.ID = "other"
				}
				if tc.nilUser {
					u = nil
				}
				users := &countingContentManagerUserService{user: u, lookupError: tc.lookupErr, nilResponse: tc.nilResponse}
				if tc.cancelLookup {
					users.cancel = cancel
				}
				port := &contentMutationPort{failure: tc.postErr, nilResult: tc.nilResult}
				svc := NewService(port, users)
				if tc.noUsers {
					svc.userService = nil
				}
				if tc.typedNilUsers {
					svc.userService = (*countingContentManagerUserService)(nil)
				}
				if tc.noPosts {
					svc.postService = nil
				}
				if tc.cancelBefore {
					cancel()
				}
				err := contentMutation(svc, requestCtx, op, tc.actor)
				require.ErrorIs(t, err, tc.want)
				wantDispatch := tc.want == nil || tc.postErr != nil || tc.nilResult
				if wantDispatch {
					require.Equal(t, 1, port.calls)
					require.Equal(t, tc.actor, port.actor)
					if op != "create" {
						require.Equal(t, contentTargetID, port.target)
					}
				} else {
					require.Zero(t, port.calls)
				}
				for _, id := range users.calls {
					require.Equal(t, tc.actor, id, "never authorize a target")
				}
				if tc.lookupErr != nil {
					require.Same(t, tc.lookupErr, err, "preserve original error graph")
				}
			})
		}
	}
}

func TestContentMutationErrorResponses(t *testing.T) {
	for _, op := range []string{"create", "update", "delete", "restore"} {
		for _, tc := range []struct {
			name     string
			failure  error
			status   int
			override bool
		}{
			{"native", user.ErrUserNotFound, 404, false},
			{"wrapped", fmt.Errorf("private diagnostic: %w", user.ErrUserNotFound), 404, false},
			{"mixed", errors.Join(user.ErrUserNotFound, errors.New("private diagnostic")), 500, false},
			{"unknown", errors.New("private diagnostic"), 500, false},
			{"override", fmt.Errorf("private diagnostic: %w", user.ErrUserNotFound), 409, true},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				users := &countingContentManagerUserService{lookupError: tc.failure}
				port := &contentMutationPort{}
				var maps []reply.ErrorManifest
				if tc.override {
					maps = []reply.ErrorManifest{{user.ErrUserNotFound: {Title: "Conflict", Detail: "Mapped by host", Code: "HOST-CONTENT", StatusCode: 409}}}
				}
				h := NewHandler(NewService(port, users), validator.NewValidator(), maps...)
				r := contentActorRequest(contentManagerRequestContext(contentActorID, true), "")
				w := httptest.NewRecorder()
				switch op {
				case "create":
					h.CreatePost(w, r)
				case "update":
					h.UpdatePostById(w, r)
				case "delete":
					h.DeletePostById(w, r)
				case "restore":
					h.RestorePostById(w, r)
				}
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "private diagnostic")
				require.Zero(t, port.calls)
				if tc.override {
					require.Contains(t, w.Body.String(), "HOST-CONTENT")
				}
			})
		}
	}
}
