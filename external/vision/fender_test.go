package vision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ritwickdey/querydecoder"
	"github.com/stretchr/testify/require"
)

type visionTestValidator struct{ err error }

func (v visionTestValidator) Validate(interface{}) error { return v.err }

// visionMapper exercises production codecs and validation, not a mock decoder.
func visionMapper[T any](fn func(*http.Request, visionValidator) (*T, error)) func(*http.Request) (any, error) {
	return func(r *http.Request) (any, error) { return fn(r, validator.NewValidator()) }
}

// authenticatedActor is test identity publication, not credential verification.
func authenticatedActor(ctx context.Context, actor string) context.Context {
	return accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), true)
}

const visionActorPayload = `{"title":"Better search","type":"feedback","status":"UNDER_REVIEW","vote":1,"message":"Ask <@nano-user>","parent_comment_id":"comment-0","nano_id":"forged","NanoID":"forged","comment_id":"forged","CommentID":"forged","ActorID":"forged","actor_id":null,"UserID":"forged","created_by_user_id":"forged","updated_by_user_id":"forged"}`

func TestVisionActorMappers(t *testing.T) {
	for _, route := range []struct {
		name       string
		mapRequest func(*http.Request) (any, error)
	}{
		{"create", visionMapper(MapRequestToCreateVisionRequest)},
		{"update", visionMapper(MapRequestToUpdateVisionRequest)},
		{"status", visionMapper(MapRequestToUpdateVisionStatusRequest)},
		{"vote", visionMapper(MapRequestToSetVisionVoteRequest)},
		{"remove vote", visionMapper(MapRequestToRemoveVisionVoteRequest)},
		{"comment", visionMapper(MapRequestToAddVisionCommentRequest)},
		{"comment vote", visionMapper(MapRequestToSetVisionCommentVoteRequest)},
		{"remove comment vote", visionMapper(MapRequestToRemoveVisionCommentVoteRequest)},
		{"delete", visionMapper(MapRequestToDeleteVisionRequest)},
	} {
		for _, tc := range []struct {
			name, actor         string
			flag, authenticated bool
		}{
			{"authenticated", "caller", true, true}, {"anonymous placeholder", "caller", true, false},
			{"ID only", "caller", false, false}, {"flag only", "", true, true}, {"missing", "", false, false},
		} {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				ctx := accesshelpers.TransitWith(context.Background(), tc.actor)
				if tc.flag {
					ctx = accesshelpers.TransitAuthenticatedWith(ctx, tc.authenticated)
				}
				r := httptest.NewRequest("POST", "/?ActorID=forged&-=forged&nano_id=forged", strings.NewReader(visionActorPayload)).WithContext(ctx)
				r = mux.SetURLVars(r, map[string]string{VisionURIVariableNanoID: "vision-1", VisionURIVariableCommentID: "comment-1"})
				got, err := route.mapRequest(r)
				if !tc.authenticated || tc.actor == "" {
					require.ErrorIs(t, err, ErrVisionUserIDIsRequired)
					return
				}
				require.NoError(t, err)
				v := reflect.ValueOf(got).Elem()
				require.Equal(t, "caller", v.FieldByName("ActorID").String())
				for field, want := range map[string]string{"NanoID": "vision-1", "CommentID": "comment-1", "Title": "Better search", "Type": "feedback", "Status": "UNDER_REVIEW", "Message": "Ask <@nano-user>", "ParentCommentID": "comment-0"} {
					if f := v.FieldByName(field); f.IsValid() && f.Kind() == reflect.String {
						require.Equal(t, want, f.String())
					}
				}
				if vote := v.FieldByName("Vote"); vote.IsValid() {
					require.Equal(t, int64(VisionVoteUpvote), vote.Int())
				}
				field, _ := v.Type().FieldByName("ActorID")
				require.Len(t, field.Index, 1)
				require.Equal(t, "-", field.Tag.Get("json"))
				require.Empty(t, field.Tag.Get("query"))
				require.Empty(t, field.Tag.Get("path"))
				require.NoError(t, json.Unmarshal([]byte(visionActorPayload), got))
				require.NoError(t, querydecoder.New(url.Values{"ActorID": {"forged"}, "actor_id": {"forged"}, "-": {"forged"}}).Decode(got))
				require.Equal(t, "caller", v.FieldByName("ActorID").String())
				encoded, err := json.Marshal(got)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "caller")
			})
		}
		if route.name == "create" || route.name == "update" || route.name == "status" || route.name == "vote" || route.name == "comment" || route.name == "comment vote" {
			t.Run(route.name+"/nil body", func(t *testing.T) {
				r := httptest.NewRequest("POST", "/", nil).WithContext(authenticatedActor(context.Background(), "caller"))
				r.Body = nil
				_, err := route.mapRequest(r)
				require.ErrorIs(t, err, ErrVisionInvalidPayload)
			})
		}
		for _, tc := range []struct {
			name string
			r    *http.Request
			want error
		}{
			{"nil request", nil, ErrVisionInvalidPayload}, {"nil URL", &http.Request{}, ErrVisionInvalidPayload},
		} {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) { _, err := route.mapRequest(tc.r); require.ErrorIs(t, err, tc.want) })
		}
		t.Run(route.name+"/cancelled", func(t *testing.T) {
			ctx, cancel := context.WithCancel(authenticatedActor(context.Background(), "caller"))
			cancel()
			r := httptest.NewRequest("POST", "/", nil).WithContext(ctx)
			_, err := route.mapRequest(r)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestVisionFenderErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*http.Request) (any, error)
		body string
		want error
	}{
		{"missing NanoID", visionMapper(MapRequestToGetVisionByNanoIDRequest), "", ErrVisionNanoIDIsRequired},
		{"invalid body", visionMapper(MapRequestToCreateVisionRequest), "{", ErrVisionInvalidPayload},
		{"invalid query", func(r *http.Request) (any, error) {
			return MapRequestToGetVisionsRequest(r, visionTestValidator{errors.New("invalid")})
		}, "", ErrVisionInvalidQueryParam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body)).WithContext(authenticatedActor(context.Background(), "caller"))
			_, err := tc.run(r)
			require.ErrorIs(t, err, tc.want)
		})
	}
}
