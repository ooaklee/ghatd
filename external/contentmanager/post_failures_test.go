package contentmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// failingPostRepository allows the real lower service to classify the read.
// Any unexpected write falls through the nil embedded repository and fails.
type failingPostRepository struct {
	*post.Repository
	failure error
	reads   int
}

func (p *failingPostRepository) GetPostById(context.Context, string) (*post.Post, error) {
	p.reads++
	return nil, p.failure
}
func (p *failingPostRepository) GetPostByUrlFriendlyId(context.Context, string) (*post.Post, error) {
	p.reads++
	return nil, p.failure
}

func TestPostNativeFailuresThroughContentHTTP(t *testing.T) {
	for _, op := range []string{"update", "delete", "restore"} {
		for _, tc := range []struct {
			name     string
			failure  error
			status   int
			override bool
		}{
			{"missing", post.ErrResourceNotFound, 404, false}, {"wrapped missing", fmt.Errorf("private diagnostic: %w", post.ErrResourceNotFound), 404, false},
			{"outage", errors.New("private diagnostic"), 500, false}, {"mixed", errors.Join(post.ErrResourceNotFound, errors.New("private diagnostic")), 500, false},
			{"unavailable", post.ErrPostUnavailable, 503, false}, {"override", fmt.Errorf("private diagnostic: %w", post.ErrPostUnavailable), 502, true},
			{"cancelled", context.Canceled, 500, false}, {"deadline", context.DeadlineExceeded, 500, false},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				port := &failingPostRepository{failure: tc.failure}
				users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
				var maps []reply.ErrorManifest
				if tc.override {
					maps = []reply.ErrorManifest{{post.ErrPostUnavailable: {Title: "Unavailable", Detail: "Host mapping", Code: "HOST-POST", StatusCode: 502}}}
				}
				h := NewHandler(NewService(post.NewService(port, nil), users), validator.NewValidator(), maps...)
				r := contentActorRequest(contentManagerRequestContext(contentActorID, true), "")
				w := httptest.NewRecorder()
				switch op {
				case "update":
					h.UpdatePostById(w, r)
				case "delete":
					h.DeletePostById(w, r)
				case "restore":
					h.RestorePostById(w, r)
				}
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.Equal(t, 1, port.reads)
				require.NotContains(t, w.Body.String(), "private diagnostic")
				if tc.override {
					require.Contains(t, w.Body.String(), "HOST-POST")
				}
			})
		}
	}
}
