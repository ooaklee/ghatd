package internationalisationmanager

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPListLimitBoundaries(t *testing.T) {
	type limitCase struct {
		name, suffix  string
		limit, status int
	}
	for _, prefix := range []string{"", "/admin"} {
		for _, kind := range []Kind{Currencies, PhoneCodes, Timezones, Flags} {
			for _, tc := range []limitCase{
				{"omitted_default", "", 200, http.StatusOK},
				{"minimum", "?limit=1", 1, http.StatusOK},
				{"previous_maximum", "?limit=200", 200, http.StatusOK},
				{"above_previous_maximum", "?limit=201", 201, http.StatusOK},
				{"below_maximum", "?limit=299", 299, http.StatusOK},
				{"maximum", "?limit=300", 300, http.StatusOK},
				{"above_maximum", "?limit=301", 0, http.StatusBadRequest},
				{"zero", "?limit=0", 0, http.StatusBadRequest},
				{"negative", "?limit=-1", 0, http.StatusBadRequest},
				{"non_integer", "?limit=300.0", 0, http.StatusBadRequest},
				{"duplicate", "?limit=300&limit=200", 0, http.StatusBadRequest},
			} {
				t.Run(prefix+"/"+string(kind)+"/"+tc.name, func(t *testing.T) {
					service := &httpFixture{}
					handler, err := NewHandler(service, securityFixture{admin: true})
					require.NoError(t, err)
					response := callHTTP(t, handler, http.MethodGet, prefix+"/"+string(kind)+tc.suffix, "", "")
					require.Equal(t, tc.status, response.Code, response.Body.String())
					if tc.status != http.StatusOK {
						require.Zero(t, service.calls)
						return
					}
					require.Equal(t, 1, service.calls)
					require.Equal(t, tc.limit, service.query.Limit)
				})
			}
		}
	}
}

func TestServiceListLimitBoundaries(t *testing.T) {
	type limitCase struct {
		name  string
		limit int
		valid bool
	}
	for _, view := range []struct {
		name  string
		admin bool
	}{{"public", false}, {"admin", true}} {
		for _, kind := range []Kind{Currencies, PhoneCodes, Timezones, Flags} {
			for _, tc := range []limitCase{
				{"minimum", 1, true},
				{"previous_maximum", 200, true},
				{"above_previous_maximum", 201, true},
				{"below_maximum", 299, true},
				{"maximum", 300, true},
				{"above_maximum", 301, false},
				{"zero", 0, false},
				{"negative", -1, false},
			} {
				t.Run(view.name+"/"+string(kind)+"/"+tc.name, func(t *testing.T) {
					service := managerFixture(t)
					page, err := service.List(t.Context(), kind, view.admin, ListQuery{Limit: tc.limit})
					if !tc.valid {
						var boundaryError *Error
						require.ErrorAs(t, err, &boundaryError)
						require.Equal(t, http.StatusBadRequest, boundaryError.Status)
						return
					}
					require.NoError(t, err)
					require.NotEmpty(t, page.Records)
					require.LessOrEqual(t, len(page.Records), tc.limit)
				})
			}
		}
	}
}

func TestMaximumListLimitPagination(t *testing.T) {
	type paginationCase struct {
		name                string
		admin, omitFlagURLs bool
	}
	for _, tc := range []paginationCase{
		{"public_enriched", false, false},
		{"public_without_flags", false, true},
		{"admin_enriched", true, false},
		{"admin_without_flags", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := managerFixture(t)
			query := ListQuery{Limit: 300, OmitFlagURLs: tc.omitFlagURLs}
			first, err := service.List(t.Context(), Timezones, tc.admin, query)
			require.NoError(t, err)
			require.Len(t, first.Records, 300, "the backing store must not clamp the requested limit")
			require.NotEmpty(t, first.Cursor)
			query.Cursor = first.Cursor
			second, err := service.List(t.Context(), Timezones, tc.admin, query)
			require.NoError(t, err)
			require.Len(t, second.Records, 219)
			require.Empty(t, second.Cursor)
			seen := make(map[string]bool)
			for _, page := range []ListResult{first, second} {
				for _, row := range page.Records {
					require.False(t, seen[row.Code], "duplicate timezone %s", row.Code)
					seen[row.Code] = true
				}
			}
			require.Len(t, seen, 519)
			query.Limit = 200
			_, err = service.List(t.Context(), Timezones, tc.admin, query)
			var boundaryError *Error
			require.ErrorAs(t, err, &boundaryError)
			require.Equal(t, http.StatusBadRequest, boundaryError.Status, "cursors remain bound to their original limit")
		})
	}
}
