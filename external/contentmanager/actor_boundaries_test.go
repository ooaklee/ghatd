package contentmanager

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestContentServiceEntryGuards(t *testing.T) {
	for _, method := range []string{"CreatePost", "UpdatePostById", "DeletePostById", "RestorePostById", "GetChangelogItems", "GetGlossaryItems", "GetFaqItems", "GetArticles", "GetChangelogItemByUrlFriendlyId", "GetArticleItemByUrlFriendlyId", "GetLatestPostsByType", "GetLatestNotificationOverviews", "GetArticleSitemapItems"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"nil context", post.ErrPostBadRequest}, {"nil request", post.ErrPostBadRequest}, {"canceled", context.Canceled}, {"nil service", ErrContentManagerUnavailable}, {"nil port", ErrContentManagerUnavailable}, {"typed nil port", ErrContentManagerUnavailable}, {"nil embedded request", post.ErrPostBadRequest},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				if tc.name == "nil embedded request" && (method == "GetArticleSitemapItems" || method == "GetChangelogItemByUrlFriendlyId" || method == "GetArticleItemByUrlFriendlyId") {
					return
				}
				var ctx context.Context = context.Background()
				svc := NewService(&contentMutationPort{}, nil)
				switch tc.name {
				case "nil context":
					ctx = nil
				case "nil service":
					svc = nil
				case "nil port":
					svc.postService = nil
				case "typed nil port":
					svc.postService = (*contentMutationPort)(nil)
				case "canceled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				fn := reflect.ValueOf(svc).MethodByName(method)
				request := reflect.New(fn.Type().In(1).Elem())
				if tc.name == "nil request" {
					request = reflect.Zero(fn.Type().In(1))
				}
				contextArg := reflect.Zero(fn.Type().In(0))
				if ctx != nil {
					contextArg = reflect.ValueOf(ctx)
				}
				result := fn.Call([]reflect.Value{contextArg, request})
				require.Nil(t, result[0].Interface())
				require.ErrorIs(t, result[1].Interface().(error), tc.want)
			})
		}
	}
}

type contentPublicPort struct {
	postService
	item    *post.Post
	latest  *post.GetLatestPostsByTypeResponse
	failure error
	calls   int
}

func (s *contentPublicPort) GetPostByUrlFriendlyId(context.Context, string) (*post.Post, error) {
	s.calls++
	return s.item, s.failure
}
func (s *contentPublicPort) GetLatestPostsByType(context.Context, *post.GetLatestPostsByTypeRequest) (*post.GetLatestPostsByTypeResponse, error) {
	s.calls++
	return s.latest, s.failure
}

func TestContentSingleItemVisibility(t *testing.T) {
	for _, operation := range []string{"article", "changelog"} {
		for _, tc := range []struct {
			name                                                 string
			admin, anonymous, deleted, draft, nilItem, wrongSlug bool
			failure, want                                        error
		}{
			{name: "public published"},
			{name: "public draft", draft: true, want: ErrUnauthorisedCMUser},
			{name: "public deleted", deleted: true, want: ErrUnauthorisedCMUser},
			{name: "admin draft", draft: true, admin: true},
			{name: "admin deleted", deleted: true, admin: true},
			{name: "placeholder cannot view drafts", draft: true, admin: true, anonymous: true, want: ErrUnauthorisedCMUser},
			{name: "nil adapter result", nilItem: true, want: ErrContentManagerUnavailable},
			{name: "different resource", wrongSlug: true, want: ErrContentManagerUnavailable},
			{name: "native not found", failure: post.ErrResourceNotFound, want: post.ErrResourceNotFound},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				id := operation + "-example"
				port := &contentPublicPort{item: &post.Post{Id: contentTargetID, UrlFriendlyId: id, PublishedAt: "2026-01-01T00:00:00Z"}, failure: tc.failure}
				if tc.draft {
					port.item.PublishedAt = ""
				}
				if tc.deleted {
					port.item.DeletedAt = "2026-02-01T00:00:00Z"
				}
				if tc.wrongSlug {
					port.item.UrlFriendlyId = "other"
				}
				if tc.nilItem {
					port.item = nil
				}
				actor := ""
				users := &countingContentManagerUserService{}
				ctx := context.Background()
				if tc.admin {
					actor = contentActorID
					users.user = &user.UniversalUser{ID: actor, Roles: []string{user.UserRoleAdmin}}
					if tc.anonymous {
						ctx = contentManagerRequestContext(actor, false)
					}
				}
				svc := NewService(port, users)
				var result *post.Post
				var err error
				if operation == "article" {
					result, err = svc.GetArticleItemByUrlFriendlyId(ctx, &GetArticleItemByUrlFriendlyIdRequest{ActorID: actor, UrlFriendlyId: id})
				} else {
					result, err = svc.GetChangelogItemByUrlFriendlyId(ctx, &GetChangelogItemByUrlFriendlyIdRequest{ActorID: actor, UrlFriendlyId: id})
				}
				require.ErrorIs(t, err, tc.want)
				if err == nil {
					require.Equal(t, contentTargetID, result.Id)
				} else {
					require.Nil(t, result)
				}
				if tc.anonymous {
					require.Empty(t, users.calls)
				}
			})
		}
	}
}

func TestContentLatestProjectionIsolation(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		admin, placeholder, nilResult bool
		failure, want                 error
		count                         int
	}{
		{name: "public", count: 1}, {name: "administrator", admin: true, count: 2}, {name: "anonymous placeholder", admin: true, placeholder: true, count: 1}, {name: "nil result", nilResult: true, want: ErrContentManagerUnavailable}, {name: "native error", failure: post.ErrResourceNotFound, want: post.ErrResourceNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &post.GetLatestPostsByTypeResponse{Overviews: []post.PostOverview{{Id: "published", PublishedAt: "2026-01-01"}, {Id: "draft"}}}
			before := append([]post.PostOverview(nil), response.Overviews...)
			port := &contentPublicPort{latest: response, failure: tc.failure}
			if tc.nilResult {
				port.latest = nil
			}
			actor := ""
			users := &countingContentManagerUserService{}
			ctx := context.Background()
			if tc.admin {
				actor = contentActorID
				users.user = &user.UniversalUser{ID: actor, Roles: []string{user.UserRoleAdmin}}
			}
			if tc.placeholder {
				ctx = contentManagerRequestContext(actor, false)
			}
			got, err := NewService(port, users).GetLatestPostsByType(ctx, &GetLatestPostsByTypeRequest{ActorID: actor, GetLatestPostsByTypeRequest: &post.GetLatestPostsByTypeRequest{}})
			require.ErrorIs(t, err, tc.want)
			if err == nil {
				require.Len(t, got.Overviews, tc.count)
				require.Equal(t, before, response.Overviews)
			}
			if tc.placeholder {
				require.Empty(t, users.calls)
			}
		})
	}
}

func TestContentReplacementTargetAgreement(t *testing.T) {
	for _, target := range []string{contentTargetID, "other"} {
		t.Run(target, func(t *testing.T) {
			port := &contentMutationPort{}
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
			_, err := NewService(port, users).UpdatePostById(context.Background(), &UpdatePostByIdRequest{UpdatePostRequest: &post.UpdatePostRequest{ActorID: contentActorID, PostId: contentTargetID, Post: &post.Post{Id: target}}})
			if target == contentTargetID {
				require.NoError(t, err)
				require.Equal(t, 1, port.calls)
			} else {
				require.ErrorIs(t, err, post.ErrPostBadRequest)
				require.Zero(t, port.calls)
				require.Empty(t, users.calls)
			}
		})
	}
}

func TestContentCanceledViewerDoesNotDispatch(t *testing.T) {
	for _, dependencyState := range []string{"canceled lookup", "typed nil user"} {
		t.Run(dependencyState, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}, cancel: cancel}
			port := &contentPublicPort{latest: &post.GetLatestPostsByTypeResponse{}}
			svc := NewService(port, users)
			if dependencyState == "typed nil user" {
				svc.userService = (*countingContentManagerUserService)(nil)
			}
			_, err := svc.GetLatestPostsByType(ctx, &GetLatestPostsByTypeRequest{ActorID: contentActorID, GetLatestPostsByTypeRequest: &post.GetLatestPostsByTypeRequest{}})
			if dependencyState == "canceled lookup" {
				require.True(t, errors.Is(err, context.Canceled))
				require.Zero(t, port.calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, port.calls)
			}
		})
	}
}
