package internationalisationmanager

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type httpFixture struct {
	calls       int
	code, actor string
	revision    int64
	err         error
}

func (f *httpFixture) List(context.Context, Kind, bool, ListQuery) (ListResult, error) {
	f.calls++
	return ListResult{}, f.err
}
func (f *httpFixture) Get(_ context.Context, _ Kind, code string, _ bool) (RecordView, error) {
	f.calls++
	f.code = code
	return RecordView{Code: code, Revision: 3, SVG: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`}, f.err
}
func (f *httpFixture) Create(_ context.Context, _ Kind, code string, _ Mutation, actor string) (RecordView, error) {
	f.calls++
	f.code = code
	f.actor = actor
	return RecordView{Code: code, Revision: 1}, f.err
}
func (f *httpFixture) Update(_ context.Context, _ Kind, code string, _ Mutation, revision int64, actor string) (RecordView, error) {
	f.calls++
	f.code = code
	f.actor = actor
	f.revision = revision
	return RecordView{Code: code, Revision: revision + 1}, f.err
}
func (f *httpFixture) Remove(ctx context.Context, k Kind, c string, rev int64, actor string) (RecordView, error) {
	return f.Update(ctx, k, c, Mutation{}, rev, actor)
}
func (f *httpFixture) Restore(ctx context.Context, k Kind, c string, rev int64, actor string) (RecordView, error) {
	return f.Update(ctx, k, c, Mutation{}, rev, actor)
}
func (f *httpFixture) CheckPhone(context.Context, string, string) (PhoneCheck, error) {
	f.calls++
	return PhoneCheck{NormalisedNumber: "+442079460018", RegionCode: "GB", State: "unavailable", Channel: "sms"}, f.err
}

type securityFixture struct{ admin, csrf bool }

func (s securityFixture) Resolve(_ context.Context, _ *http.Request, admin bool) (Identity, error) {
	if admin && !s.admin {
		return Identity{}, &Error{Code: "I18N_FORBIDDEN", Status: 403}
	}
	return Identity{ActorID: "verified-admin"}, nil
}
func (s securityFixture) Issue(w http.ResponseWriter, _ *http.Request, _ Identity) error {
	w.Header().Set("X-CSRF-Token", "test-proof")
	return nil
}
func (s securityFixture) Verify(*http.Request, Identity) error {
	if !s.csrf {
		return &Error{Code: "I18N_FORBIDDEN", Status: 403}
	}
	return nil
}
func (s securityFixture) Limit(context.Context, *http.Request, Identity, string) error { return nil }
func callHTTP(t *testing.T, h *Handler, method, path, body, revision string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, BasePath+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if revision != "" {
		req.Header.Set("If-Match", revision)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestHTTPAdminAuthorityCSRFAndOptimisticRevision(t *testing.T) {
	for _, tc := range []struct {
		name, body, revision string
		security             securityFixture
		status               int
	}{
		{"no admin", `{"enabled":true}`, `"3"`, securityFixture{false, true}, 403},
		{"no csrf", `{"enabled":true}`, `"3"`, securityFixture{true, false}, 403},
		{"missing revision", `{"enabled":true}`, ``, securityFixture{true, true}, 428},
		{"weak revision", `{"enabled":true}`, `W/"3"`, securityFixture{true, true}, 400},
		{"spoof actor", `{"enabled":true,"updated_by":"someone-else"}`, `"3"`, securityFixture{true, true}, 400},
		{"extra document", `{"enabled":true}{}`, `"3"`, securityFixture{true, true}, 400},
		{"valid", `{"enabled":true}`, `"3"`, securityFixture{true, true}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &httpFixture{}
			h, err := NewHandler(service, tc.security)
			require.NoError(t, err)
			w := callHTTP(t, h, "PATCH", "/admin/timezones/Europe%2FLondon", tc.body, tc.revision)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if tc.status == 200 {
				require.Equal(t, "Europe/London", service.code)
				require.Equal(t, "verified-admin", service.actor)
				require.Equal(t, int64(3), service.revision)
				require.Equal(t, `"4"`, w.Header().Get("ETag"))
			} else {
				require.Zero(t, service.calls)
			}
		})
	}
}
func TestHTTPPublicChoicesAndSVG(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		fails      bool
		status     int
	}{
		{"choices", "/currencies?limit=200", false, 200}, {"svg", "/flags/GB/svg?v=3", false, 200},
		{"public_json", "/flags/GB", false, 200}, {"private_failure", "/currencies", true, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &httpFixture{}
			if tc.fails {
				service.err = errors.New("database password must never appear")
			}
			h, err := NewHandler(service, securityFixture{})
			require.NoError(t, err)
			w := callHTTP(t, h, "GET", tc.path, "", "")
			require.Equal(t, tc.status, w.Code)
			switch tc.name {
			case "choices":
				require.Contains(t, w.Body.String(), `"data":[]`)
			case "svg":
				require.Contains(t, w.Header().Get("Content-Type"), "image/svg+xml")
				require.Contains(t, w.Header().Get("Content-Security-Policy"), "sandbox")
				require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
			case "public_json":
				require.NotContains(t, w.Body.String(), "<svg")
			case "private_failure":
				require.NotContains(t, w.Body.String(), "password")
			}
		})
	}
}
func TestHTTPPhoneLookupDoesNotRunWithoutCSRF(t *testing.T) {
	for _, tc := range []struct {
		name          string
		csrf          bool
		status, calls int
	}{{"missing_csrf", false, 403, 0}, {"valid_csrf", true, 200, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			service := &httpFixture{}
			h, err := NewHandler(service, securityFixture{csrf: tc.csrf})
			require.NoError(t, err)
			w := callHTTP(t, h, "POST", "/phonecodes/check", `{"phone":"02079460018","region_code":"GB"}`, "")
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.calls, service.calls)
			if tc.csrf {
				require.Contains(t, w.Body.String(), `"state":"unavailable"`)
			}
		})
	}
}
