package contentmanager

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ooaklee/ghatd/external/indexnow"
	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestIndexNowMongoMutationPersistsBeforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		status          int
		expected        []string
	}{
		{name: "renamed article old and new", operation: "rename", status: 200, expected: []string{"https://example.com/blog/old-title", "https://example.com/blog/new-title"}},
		{name: "rescheduled article removal", operation: "reschedule", status: 200, expected: []string{"https://example.com/blog/old-title"}},
		{name: "deleted article removal", operation: "delete", status: 200, expected: []string{"https://example.com/blog/old-title"}},
		{name: "network rejection preserves content", operation: "rename", status: 503, expected: []string{"https://example.com/blog/old-title", "https://example.com/blog/new-title"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, core := changelogSitemapMongoFixture(t)
			posts := post.NewRepository(core)
			collection, err := posts.GetPostCollection(ctx)
			require.NoError(t, err)
			item := cmsIndexNowPost("article-old-title")
			item.Title = "Old title"
			item.Text = "Original content"
			item.HeaderImage = "/cover.png"
			item.PublishedAt = "2020-01-01T00:00:00.000000000"
			_, err = collection.InsertOne(ctx, item)
			require.NoError(t, err)
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				stored, lookupErr := posts.GetPostById(ctx, item.Id)
				require.NoError(t, lookupErr)
				switch tc.operation {
				case "rename":
					require.Equal(t, "New title", stored.Title)
				case "reschedule":
					require.True(t, strings.HasPrefix(stored.PublishedAt, "2050-"))
				case "delete":
					require.NotEmpty(t, stored.DeletedAt)
				}
				var payload struct {
					URLs []string `json:"urlList"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				require.Equal(t, tc.expected, payload.URLs)
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(endpoint.Close)
			notifier, err := indexnow.NewService("cms-test-key", indexnow.Config{SiteURL: "https://example.com", Endpoint: endpoint.URL})
			require.NoError(t, err)
			config := cmsIndexNowConfig(&cmsIndexNowPostPort{})
			config.LookupPost = posts.GetPostById
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
			service := NewService(post.NewService(posts, nil), users, WithIndexNow(notifier, config))
			var receipt *IndexNowNotificationReceipt
			if tc.operation == "delete" {
				response, writeErr := service.DeletePostById(ctx, &DeletePostByIdRequest{DeletePostByIdRequest: &post.DeletePostByIdRequest{Id: item.Id, ActorID: contentActorID}})
				require.NoError(t, writeErr)
				receipt = response.IndexNow
			} else {
				request := &post.UpdatePostRequest{PostId: item.Id, ActorID: contentActorID}
				if tc.operation == "rename" {
					title := "New title"
					request.Title = &title
				} else {
					future := "2050-01-01T00:00:00"
					request.PublishAtUtc = &future
				}
				response, writeErr := service.UpdatePostById(ctx, &UpdatePostByIdRequest{UpdatePostRequest: request})
				require.NoError(t, writeErr)
				receipt = response.IndexNow
			}
			require.EqualValues(t, 1, calls.Load())
			require.Equal(t, len(tc.expected), receipt.URLCount)
			expectedStatus := "acknowledged"
			if tc.status == 503 {
				expectedStatus = "failed"
			}
			require.Equal(t, expectedStatus, receipt.Status)
		})
	}
}
