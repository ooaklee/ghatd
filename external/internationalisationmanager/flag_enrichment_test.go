package internationalisationmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/globalflagger"
	"github.com/stretchr/testify/require"
)

func TestOmittedFlagURLsDoNotBypassSVGVisibility(t *testing.T) {
	type visibilityCase struct {
		name    string
		hidden  bool
		enabled bool
		deleted bool
		status  int
	}
	for _, tc := range []visibilityCase{
		{"visible", false, true, false, http.StatusOK},
		{"hidden", true, true, false, http.StatusNotFound},
		{"disabled", false, false, false, http.StatusNotFound},
		{"deleted", false, true, true, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := managerFixture(t)
			_, err := service.Update(t.Context(), Flags, "GB", Mutation{Hidden: ptr(tc.hidden), Enabled: ptr(tc.enabled)}, 1, "admin")
			require.NoError(t, err)
			if tc.deleted {
				_, err = service.Remove(t.Context(), Flags, "GB", 2, "admin")
				require.NoError(t, err)
			}
			handler, err := NewHandler(service, securityFixture{})
			require.NoError(t, err)
			response := callHTTP(t, handler, "GET", "/currencies?include_flags=false", "", "")
			require.Equal(t, http.StatusOK, response.Code)
			var page struct {
				Data []RecordView `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
			var flagID string
			for _, row := range page.Data {
				if row.Code == "GBP" {
					flagID = row.FlagID
					require.Empty(t, row.FlagURL)
				}
			}
			require.Equal(t, "GB", flagID)
			image := callHTTP(t, handler, "GET", "/flags/"+flagID+"/svg", "", "")
			require.Equal(t, tc.status, image.Code, image.Body.String())
			if tc.status == http.StatusOK {
				require.Equal(t, "image/svg+xml; charset=utf-8", image.Header().Get("Content-Type"))
			} else {
				require.NotContains(t, image.Body.String(), "<svg")
			}
		})
	}
}

// Unimplemented repository operations panic if the supposedly independent list
// unexpectedly crosses another flag-storage boundary.
type unavailableFlagRepository struct {
	globalflagger.Repository
	listCalls int
}

func (r *unavailableFlagRepository) List(context.Context, catalogue.ListQuery, bool) ([]globalflagger.Record, int64, error) {
	r.listCalls++
	return nil, 0, catalogue.ErrUnavailable
}

func TestListWithoutFlagURLsSurvivesFlagStorageFailure(t *testing.T) {
	type listCase struct {
		kind  Kind
		count int
	}
	for _, tc := range []listCase{{Currencies, 165}, {PhoneCodes, 245}, {Timezones, 519}} {
		for _, view := range []struct {
			name  string
			admin bool
		}{{"public", false}, {"admin", true}} {
			t.Run(string(tc.kind)+"/"+view.name, func(t *testing.T) {
				service := managerFixture(t)
				flags := &unavailableFlagRepository{}
				service.flags = globalflagger.NewService(flags, service.clock)
				query := ListQuery{Limit: 80, OmitFlagURLs: true}
				seen := make(map[string]bool)
				withFlagID := 0
				for {
					page, err := service.List(t.Context(), tc.kind, view.admin, query)
					require.NoError(t, err)
					for _, row := range page.Records {
						require.False(t, seen[row.Code], "duplicate code %s", row.Code)
						seen[row.Code] = true
						require.Empty(t, row.FlagURL)
						require.NotEmpty(t, row.Name)
						if row.FlagID != "" {
							withFlagID++
						}
						switch tc.kind {
						case Currencies:
							require.NotNil(t, row.MinorUnit)
						case PhoneCodes:
							require.NotEmpty(t, row.CallingCode)
						case Timezones:
							require.NotNil(t, row.OffsetSeconds)
							require.NotEmpty(t, row.LocalTime)
						}
					}
					if page.Cursor == "" {
						break
					}
					query.Cursor = page.Cursor
				}
				require.Len(t, seen, tc.count)
				require.Positive(t, withFlagID)
				require.Zero(t, flags.listCalls, "flag storage must not be consulted on any page")
				// Default enrichment still depends on flags; the successful opt-out
				// cannot be caused by a fixture that silently ignores failures.
				_, err := service.List(t.Context(), tc.kind, view.admin, ListQuery{Limit: 80})
				require.ErrorIs(t, err, catalogue.ErrUnavailable)
				require.Equal(t, 1, flags.listCalls)
			})
		}
	}
}

func TestFlagEnrichmentPreservesViewsAndCursors(t *testing.T) {
	type enrichmentCase struct {
		name          string
		admin, hidden bool
	}
	for _, kind := range []Kind{Currencies, PhoneCodes, Timezones} {
		for _, tc := range []enrichmentCase{
			{"public_visible", false, false}, {"public_hidden", false, true},
			{"admin_visible", true, false}, {"admin_hidden", true, true},
		} {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				service := managerFixture(t)
				if kind == Timezones {
					// London spans multiple seed regions and therefore has no seed
					// flag. Give this fixture an explicit reference before testing it.
					_, err := service.Update(t.Context(), Timezones, "Europe/London", Mutation{FlagID: ptr("GB")}, 1, "admin")
					require.NoError(t, err)
				}
				_, err := service.Update(t.Context(), Flags, "GB", Mutation{Hidden: ptr(tc.hidden)}, 1, "admin")
				require.NoError(t, err)
				query := ListQuery{Limit: 80}
				matched := 0
				for {
					enriched, err := service.List(t.Context(), kind, tc.admin, query)
					require.NoError(t, err)
					withoutQuery := query
					withoutQuery.OmitFlagURLs = true
					without, err := service.List(t.Context(), kind, tc.admin, withoutQuery)
					require.NoError(t, err)
					require.Equal(t, enriched.Cursor, without.Cursor, "enrichment must not change page positions")
					for i := range enriched.Records {
						row := &enriched.Records[i]
						if row.FlagID == "GB" {
							matched++
							if tc.hidden && !tc.admin {
								require.Empty(t, row.FlagURL, "hidden flags stay unavailable to public callers")
							} else {
								require.Equal(t, flagURL("GB", 2, tc.admin), row.FlagURL)
							}
						}
						row.FlagURL = ""
					}
					require.Equal(t, enriched.Records, without.Records, "only referenced flag URLs may change")
					if enriched.Cursor == "" {
						break
					}
					query.Cursor = enriched.Cursor
				}
				require.Positive(t, matched)
			})
		}
	}
}

func TestHTTPIncludeFlagsValidation(t *testing.T) {
	type queryCase struct {
		name, suffix string
		status       int
		omit         bool
	}
	cases := []queryCase{
		{"default", "", http.StatusOK, false},
		{"explicit_true", "?include_flags=true", http.StatusOK, false},
		{"explicit_false", "?include_flags=false", http.StatusOK, true},
		{"page", "?include_flags=false&limit=80&cursor=opaque", http.StatusOK, true},
		{"empty", "?include_flags=", http.StatusBadRequest, false},
		{"bare", "?include_flags", http.StatusBadRequest, false},
		{"numeric", "?include_flags=0", http.StatusBadRequest, false},
		{"uppercase", "?include_flags=TRUE", http.StatusBadRequest, false},
		{"space", "?include_flags=%20false", http.StatusBadRequest, false},
		{"unknown", "?include_flags=no", http.StatusBadRequest, false},
		{"duplicate_same", "?include_flags=false&include_flags=false", http.StatusBadRequest, false},
		{"duplicate_different", "?include_flags=false&include_flags=true", http.StatusBadRequest, false},
		{"malformed_escape", "?include_flags=%zz", http.StatusBadRequest, false},
		{"malformed_separator", "?include_flags=false;ignored=true", http.StatusBadRequest, false},
	}
	for _, prefix := range []string{"", "/admin"} {
		for _, kind := range []Kind{Currencies, PhoneCodes, Timezones, Flags} {
			for _, tc := range cases {
				t.Run(prefix+"/"+string(kind)+"/"+tc.name, func(t *testing.T) {
					service := &httpFixture{}
					h, err := NewHandler(service, securityFixture{admin: true})
					require.NoError(t, err)
					response := callHTTP(t, h, "GET", prefix+"/"+string(kind)+tc.suffix, "", "")
					want := tc.status
					if kind == Flags && tc.suffix != "" {
						want = http.StatusBadRequest
					}
					require.Equal(t, want, response.Code, response.Body.String())
					if want != http.StatusOK {
						require.Zero(t, service.calls)
						return
					}
					require.Equal(t, 1, service.calls)
					require.Equal(t, tc.omit, service.query.OmitFlagURLs)
					if tc.name == "page" {
						require.Equal(t, 80, service.query.Limit)
						require.Equal(t, "opaque", service.query.Cursor)
					}
				})
			}
		}
	}
}

func TestFlagListRejectsOmittedFlagURLs(t *testing.T) {
	for _, admin := range []bool{false, true} {
		name := "public"
		if admin {
			name = "admin"
		}
		t.Run(name, func(t *testing.T) {
			service := managerFixture(t)
			flags := &unavailableFlagRepository{}
			service.flags = globalflagger.NewService(flags, service.clock)
			_, err := service.List(t.Context(), Flags, admin, ListQuery{Limit: 200, OmitFlagURLs: true})
			var boundaryError *Error
			require.ErrorAs(t, err, &boundaryError)
			require.Equal(t, http.StatusBadRequest, boundaryError.Status)
			require.Zero(t, flags.listCalls)
		})
	}
}
