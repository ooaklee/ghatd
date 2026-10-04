package waitlist

import (
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	grouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

type testContent struct {
	failPreview, failRender bool
	invalidVariant          string
}

func (testContent) Validate(a Announcement) error { a.Data["copy"] = "validator mutation"; return nil }
func (c testContent) Preview(a Announcement, entries []Entry, _ string) (AnnouncementPresentation, error) {
	if c.failPreview {
		return AnnouncementPresentation{}, errors.New("render unavailable")
	}
	a.Data["copy"] = "preview mutation"
	if len(entries) > 0 {
		entries[0].Email = "mutation@example.com"
	}
	variant := AnnouncementVariant{Key: "example", Label: "Example", HTML: "<p>Preview</p>", RecipientCount: len(entries)}
	switch c.invalidVariant {
	case "count":
		variant.RecipientCount++
	case "key":
		variant.Key = "bad key"
	case "blank":
		variant.HTML = ""
	}
	return AnnouncementPresentation{HTML: "<p>Preview</p>", Variants: []AnnouncementVariant{variant}}, nil
}
func (c testContent) Render(a Announcement, _ Entry, _ string) (string, error) {
	if c.failRender {
		return "", errors.New("render unavailable")
	}
	body := "<p>" + a.Data["copy"] + "</p>"
	a.Data["copy"] = "send mutation"
	return body, nil
}

func TestCustomDataValidation(t *testing.T) {
	large := map[string]string{"a": strings.Repeat("x", 4000), "b": strings.Repeat("y", 4000)}
	many := map[string]string{}
	for _, key := range strings.Split("a b c d e f g h i j k l m n o p q", " ") {
		many[key] = "x"
	}
	for _, tc := range []struct {
		name    string
		data    map[string]string
		content bool
		valid   bool
	}{
		{"default", nil, false, true}, {"default rejects extensions", map[string]string{"copy": "x"}, false, false},
		{"custom", map[string]string{"copy": "Reviewed"}, true, true},
		{"unsafe key", map[string]string{"$copy": "x"}, true, false},
		{"oversized value", map[string]string{"copy": strings.Repeat("x", 4001)}, true, false},
		{"total size", large, true, false}, {"too many fields", many, true, false},
		{"control", map[string]string{"copy": "x\rHeader"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, store, _ := announcementFixture(1)
			if tc.content {
				service.Content = testContent{}
			}
			draft := announcementDraft()
			draft.Data = tc.data
			_, err := service.Prepare(context.Background(), draft)
			if tc.valid {
				require.NoError(t, err)
				require.Len(t, store.previews, 1)
			} else {
				require.ErrorIs(t, err, ErrInvalidAnnouncement)
				require.Empty(t, store.previews)
			}
		})
	}
}

func TestContentFailureDoesNotPersistOrSpendClaim(t *testing.T) {
	for _, tc := range []struct {
		name      string
		content   testContent
		prepareOK bool
	}{
		{"preview failure", testContent{failPreview: true}, false},
		{"invalid count", testContent{invalidVariant: "count"}, false},
		{"invalid key", testContent{invalidVariant: "key"}, false},
		{"blank HTML", testContent{invalidVariant: "blank"}, false},
		{"recipient failure", testContent{failRender: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, store, provider := announcementFixture(1)
			service.Content = tc.content
			draft := announcementDraft()
			draft.Data = map[string]string{"copy": "Reviewed"}
			preview, err := service.Prepare(context.Background(), draft)
			if !tc.prepareOK {
				require.Error(t, err)
				require.Empty(t, store.previews)
			} else {
				require.NoError(t, err)
				_, err = service.Dispatch(context.Background(), preview.ID)
				require.Error(t, err)
			}
			require.Empty(t, store.states)
			require.Zero(t, provider.count())
		})
	}
}

// Mutating hooks, caller edits and response edits must all remain detached from
// the saved command across preview and multiple recipient deliveries.
func TestContentIsolationAcrossSavedCommandAndRecipients(t *testing.T) {
	service, store, provider := announcementFixture(2)
	service.Content = testContent{}
	draft := announcementDraft()
	draft.Data = map[string]string{"copy": "Reviewed"}
	preview, err := service.Prepare(context.Background(), draft)
	require.NoError(t, err)
	draft.Data["copy"] = "caller mutation"
	preview.Data["copy"] = "response mutation"
	require.Equal(t, "Reviewed", store.previews[preview.ID].Data["copy"])
	require.Equal(t, "person-0@example.com", store.entries[0].Email)
	summary, err := service.Dispatch(context.Background(), preview.ID)
	require.NoError(t, err)
	require.Equal(t, 2, summary.Accepted)
	for _, email := range provider.sent {
		require.Equal(t, "<p>Reviewed</p>", email.HTMLBody)
		require.NotEqual(t, "mutation@example.com", email.To)
	}
	current, err := service.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Reviewed", current.Data["copy"])
	current.Data["copy"] = "current response mutation"
	require.Equal(t, "Reviewed", store.active.Data["copy"])
}

func TestCSVConfigurationAndPreflight(t *testing.T) {
	value := func(e Entry) string { return e.Email }
	for _, tc := range []struct {
		name    string
		columns []CSVColumn
		valid   bool
	}{
		{"default", nil, true}, {"custom", []CSVColumn{{Header: "example", Value: value}}, true},
		{"empty", []CSVColumn{}, false}, {"unsafe", []CSVColumn{{Header: "=formula", Value: value}}, false},
		{"nil callback", []CSVColumn{{Header: "example"}}, false},
		{"duplicates", []CSVColumn{{Header: "x", Value: value}, {Header: "x", Value: value}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := grouter.NewRouter(nil, nil)
			allow := func(next http.Handler) http.Handler { return next }
			limiter := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) })
			}
			store := &fakeStore{entries: []Entry{{Email: "=unsafe"}}}
			err := AttachRoutesWithConfig(routes, store, limiter, allow, RouteConfig{Columns: tc.columns})
			if !tc.valid {
				require.Error(t, err)
				require.Empty(t, routes.RouteInventory())
				return
			}
			require.NoError(t, err)
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, httptest.NewRequest("OPTIONS", "/api/v1/waitlist", nil))
			require.Equal(t, 204, w.Code)
			w = httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/waitlist/export", nil))
			require.Equal(t, 200, w.Code)
			rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
			require.NoError(t, err)
			require.Equal(t, "'=unsafe", rows[1][0])
		})
	}
}

func TestCSVFormulaPrefixes(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"ordinary copy", "ordinary copy"}, {"  ordinary copy", "  ordinary copy"},
		{"=1+1", "'=1+1"}, {" =1+1", "' =1+1"}, {"\u2003@SUM(1)", "'\u2003@SUM(1)"},
		{"\tcopy", "'\tcopy"}, {"\rtext", "'\rtext"}, {"  -1", "'  -1"}, {" +1", "' +1"},
	} {
		t.Run(tc.input, func(t *testing.T) { require.Equal(t, tc.want, csvCell(tc.input)) })
	}
}
