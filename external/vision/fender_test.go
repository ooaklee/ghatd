package vision

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

type visionTestValidator struct{ err error }

func (v visionTestValidator) Validate(interface{}) error { return v.err }

func TestMapRequestToCreateVisionRequest(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/visions",
		bytes.NewBufferString(`{"title":"Better search","type":"feedback"}`),
	)
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))

	got, err := MapRequestToCreateVisionRequest(req, nil)
	if err != nil {
		t.Fatalf("MapRequestToCreateVisionRequest() error = %v", err)
	}
	if got.Title != "Better search" || got.Type != VisionTypeFeedback || got.CreatedByUserID != "user-1" {
		t.Fatalf("mapped request = %+v", got)
	}
}

func TestMapRequestToSetVisionVoteUsesContextUser(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/visions/vision-1/votes",
		bytes.NewBufferString(`{"vote":1,"user_id":"spoofed"}`),
	)
	req = mux.SetURLVars(req, map[string]string{VisionURIVariableNanoID: "vision-1"})
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))

	got, err := MapRequestToSetVisionVoteRequest(req, nil)
	if err != nil {
		t.Fatalf("MapRequestToSetVisionVoteRequest() error = %v", err)
	}
	if got.UserID != "user-1" || got.Vote != VisionVoteUpvote || got.NanoID != "vision-1" {
		t.Fatalf("mapped request = %+v", got)
	}
}

func TestMapRequestToAddVisionCommentPreservesMention(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/visions/vision-1/comments",
		bytes.NewBufferString(`{"message":"Please ask <@nano-user> about this","parent_comment_id":"comment-0"}`),
	)
	req = mux.SetURLVars(req, map[string]string{VisionURIVariableNanoID: "vision-1"})
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))

	got, err := MapRequestToAddVisionCommentRequest(req, nil)
	if err != nil {
		t.Fatalf("MapRequestToAddVisionCommentRequest() error = %v", err)
	}
	if got.Message != "Please ask <@nano-user> about this" || got.ParentCommentID != "comment-0" || got.UserID != "user-1" {
		t.Fatalf("mapped request = %+v", got)
	}
}

func TestMapRequestToSetVisionCommentVoteUsesContextUser(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/visions/vision-1/comments/comment-1/votes",
		bytes.NewBufferString(`{"vote":1,"user_id":"spoofed"}`),
	)
	req = mux.SetURLVars(req, map[string]string{
		VisionURIVariableNanoID:    "vision-1",
		VisionURIVariableCommentID: "comment-1",
	})
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))

	got, err := MapRequestToSetVisionCommentVoteRequest(req, nil)
	if err != nil {
		t.Fatalf("MapRequestToSetVisionCommentVoteRequest() error = %v", err)
	}
	if got.NanoID != "vision-1" || got.CommentID != "comment-1" || got.UserID != "user-1" || got.Vote != VisionVoteUpvote {
		t.Fatalf("mapped request = %+v", got)
	}
}

func TestVisionFenderErrors(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/visions/missing", nil)
	if _, err := MapRequestToGetVisionByNanoIDRequest(req, nil); !errors.Is(err, ErrVisionNanoIDIsRequired) {
		t.Fatalf("missing NanoID error = %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/visions", bytes.NewBufferString(`{`))
	if _, err := MapRequestToCreateVisionRequest(req, nil); !errors.Is(err, ErrVisionInvalidPayload) {
		t.Fatalf("invalid payload error = %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/visions", nil)
	if _, err := MapRequestToGetVisionsRequest(req, visionTestValidator{err: errors.New("invalid")}); !errors.Is(err, ErrVisionInvalidQueryParam) {
		t.Fatalf("invalid query error = %v", err)
	}
}
