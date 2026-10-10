package internationalisationmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"github.com/ooaklee/ghatd/external/currencycoder"
	"github.com/ooaklee/ghatd/external/globalflagger"
	"github.com/ooaklee/ghatd/external/telenumcoder"
	"github.com/ooaklee/ghatd/external/timezonecoder"
	"github.com/stretchr/testify/require"
)

func managerFixture(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	clock := catalogue.ClockFunc(func() time.Time { return time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC) })
	cr, err := currencycoder.NewRepository(cataloguestore.NewMemoryStore(nil))
	require.NoError(t, err)
	currencies, err := currencycoder.NewService(cr, clock)
	require.NoError(t, err)
	require.NoError(t, currencies.Migrate(ctx))
	pr, err := telenumcoder.NewRepository(cataloguestore.NewMemoryStore(nil))
	require.NoError(t, err)
	phones, err := telenumcoder.NewService(pr, clock)
	require.NoError(t, err)
	require.NoError(t, phones.Migrate(ctx))
	tr, err := timezonecoder.NewRepository(cataloguestore.NewMemoryStore(nil))
	require.NoError(t, err)
	zones, err := timezonecoder.NewService(tr, clock)
	require.NoError(t, err)
	require.NoError(t, zones.Migrate(ctx))
	flags := globalflagger.NewService(globalflagger.NewMemoryRepository(nil), clock)
	for _, code := range []string{"GB", "US", "EU", "JM"} {
		_, err = flags.Create(ctx, &globalflagger.CreateFlagRequest{Code: code, Name: code, ActorID: "system:fixture", SVG: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M0 0h10v10H0z" fill="#123"/></svg>`})
		require.NoError(t, err)
	}
	s, err := NewService(currencies, phones, flags, zones, clock)
	require.NoError(t, err)
	return s
}
func ptr[T any](value T) *T { return &value }

// A single cross-domain lifecycle sequence checks referenced flag edits against
// previously stored currency revisions. Keeping the dependent history together
// preserves the reference/audit contract; independent request inputs use tables.
func TestManagerAvailabilityAuditAndFlagReferences(t *testing.T) {
	s := managerFixture(t)
	ctx := context.Background()
	public, err := s.List(ctx, Currencies, false, ListQuery{Limit: 200})
	require.NoError(t, err)
	require.Len(t, public.Records, 165)
	gbp, err := s.Get(ctx, Currencies, "GBP", false)
	require.NoError(t, err)
	require.Equal(t, "GBP", gbp.Code)
	require.Equal(t, "GB", gbp.FlagID)
	require.Equal(t, BasePath+"/flags/GB/svg?v=1", gbp.FlagURL)
	require.Empty(t, gbp.CreatedBy)
	require.Empty(t, gbp.SVG)
	changed, err := s.Update(ctx, Currencies, "EUR", Mutation{Enabled: ptr(true), Name: ptr("Euro"), FlagID: ptr("EU")}, 1, "admin-one")
	require.NoError(t, err)
	require.EqualValues(t, 2, changed.Revision)
	require.Equal(t, "admin-one", changed.UpdatedBy)
	require.Equal(t, catalogue.SystemSeedActor, changed.CreatedBy)
	_, err = s.Update(ctx, Currencies, "EUR", Mutation{Hidden: ptr(true)}, 1, "admin-two")
	var publicError *Error
	require.ErrorAs(t, err, &publicError)
	require.Equal(t, 412, publicError.Status)
	_, err = s.Update(ctx, Currencies, "EUR", Mutation{FlagID: ptr("EUR")}, 2, "admin-one")
	require.ErrorAs(t, err, &publicError)
	require.Equal(t, "flag_id", publicError.Field)
	_, err = s.Update(ctx, Currencies, "EUR", Mutation{MinorUnit: ptr(0)}, 2, "admin-one")
	require.ErrorAs(t, err, &publicError)
	require.Equal(t, 422, publicError.Status)
	// Multiple definitions deliberately share a canonical flag via explicit IDs.
	_, err = s.Update(ctx, Currencies, "GBP", Mutation{FlagID: ptr("EU")}, 1, "admin-one")
	require.NoError(t, err)
	public, err = s.List(ctx, Currencies, false, ListQuery{Limit: 200})
	require.NoError(t, err)
	require.Len(t, public.Records, 165)
	euro, err := s.Get(ctx, Currencies, "EUR", false)
	require.NoError(t, err)
	gbp, err = s.Get(ctx, Currencies, "GBP", false)
	require.NoError(t, err)
	require.Equal(t, euro.FlagURL, gbp.FlagURL)
	_, err = s.Update(ctx, Flags, "EU", Mutation{Hidden: ptr(true)}, 1, "admin-one")
	require.NoError(t, err)
	public, err = s.List(ctx, Currencies, false, ListQuery{Limit: 200})
	require.NoError(t, err)
	require.Len(t, public.Records, 165)
	for _, r := range public.Records {
		if r.FlagID == "EU" {
			require.Empty(t, r.FlagURL)
		}
	}
	_, err = s.Get(ctx, Flags, "EU", false)
	require.ErrorAs(t, err, &publicError)
	require.Equal(t, 404, publicError.Status)
	_, err = s.Remove(ctx, Currencies, "EUR", 2, "admin-two")
	require.NoError(t, err)
	require.ErrorIs(t, s.ValidateCurrency(ctx, "EUR"), catalogue.ErrNotSelectable)
	restored, err := s.Restore(ctx, Currencies, "EUR", 3, "admin-three")
	require.NoError(t, err)
	require.False(t, restored.Enabled)
	require.Nil(t, restored.DeletedAt)
	admin, err := s.Get(ctx, Flags, "EU", true)
	require.NoError(t, err)
	require.Contains(t, admin.SVG, "<svg")
	require.Contains(t, admin.SVGURL, "/admin/flags/EU/svg")
}
func TestManagerPaginationAndDerivedViews(t *testing.T) {
	for _, tc := range []struct {
		kind  Kind
		count int
	}{{Timezones, 519}, {PhoneCodes, 245}} {
		t.Run(string(tc.kind), func(t *testing.T) {
			s := managerFixture(t)
			ctx := t.Context()
			seen := map[string]bool{}
			query := ListQuery{Limit: 137}
			for {
				result, err := s.List(ctx, tc.kind, false, query)
				require.NoError(t, err)
				for _, row := range result.Records {
					require.False(t, seen[row.Code])
					seen[row.Code] = true
					if tc.kind == Timezones {
						require.NotNil(t, row.OffsetSeconds)
						require.NotEmpty(t, row.LocalTime)
					}
				}
				if result.Cursor == "" {
					break
				}
				query.Cursor = result.Cursor
			}
			require.Len(t, seen, tc.count)
			other := PhoneCodes
			if tc.kind == PhoneCodes {
				other = Timezones
			}
			_, err := s.List(ctx, other, false, query)
			require.Error(t, err, "cursor cannot be reused for another catalogue")
			if tc.kind == Timezones {
				london, err := s.Get(ctx, Timezones, "Europe/London", false)
				require.NoError(t, err)
				require.Equal(t, 3600, *london.OffsetSeconds)
				require.Equal(t, "2026-07-15T13:00:00+01:00", london.LocalTime)
			} else {
				result, err := s.CheckPhone(ctx, "07400123456", "GB")
				require.NoError(t, err)
				require.Equal(t, "+447400123456", result.NormalisedNumber)
				require.Equal(t, "unavailable", result.State)
			}
		})
	}
}

// This is one ordered administration-to-public-read journey: later reads depend
// on the exact earlier mutation receipts, including stale-write and visibility
// checks. It remains a stateful integration scenario rather than a one-row table.
func TestManagerHTTPFlowWithRealServices(t *testing.T) {
	service := managerFixture(t)
	handler, err := NewHandler(service, securityFixture{admin: true, csrf: true})
	require.NoError(t, err)
	before := callHTTP(t, handler, "GET", "/currencies?limit=200", "", "")
	require.Equal(t, 200, before.Code)
	require.Contains(t, before.Body.String(), `"code":"GBP"`)
	require.Contains(t, before.Body.String(), `"code":"JPY"`)
	require.Contains(t, before.Body.String(), `"minor_unit":0`)
	enabled := callHTTP(t, handler, "PATCH", "/admin/currencies/JPY", `{"enabled":false}`, `"1"`)
	require.Equal(t, 200, enabled.Code, enabled.Body.String())
	require.Equal(t, `"2"`, enabled.Header().Get("ETag"))
	after := callHTTP(t, handler, "GET", "/currencies?limit=200", "", "")
	require.Equal(t, 200, after.Code)
	require.NotContains(t, after.Body.String(), `"code":"JPY"`)
	require.NotContains(t, after.Body.String(), "verified-admin")
	stale := callHTTP(t, handler, "PATCH", "/admin/currencies/JPY", `{"enabled":false}`, `"1"`)
	require.Equal(t, 412, stale.Code)
	malicious := callHTTP(t, handler, "POST", "/admin/flags/TEST", `{"name":"Unsafe","svg":"<svg xmlns=\"http://www.w3.org/2000/svg\"><script/></svg>"}`, "")
	require.Equal(t, 422, malicious.Code, malicious.Body.String())
	created := callHTTP(t, handler, "POST", "/admin/flags/TEST", `{"name":"Safe","enabled":false,"svg":"<svg xmlns=\"http://www.w3.org/2000/svg\"><path d=\"M0 0\"/></svg>"}`, "")
	require.Equal(t, 201, created.Code, created.Body.String())
	hidden := callHTTP(t, handler, "GET", "/flags/TEST/svg?v=1", "", "")
	require.Equal(t, 404, hidden.Code)
	preview := callHTTP(t, handler, "GET", "/admin/flags/TEST/svg?v=1", "", "")
	require.Equal(t, 200, preview.Code)
	require.Equal(t, "image/svg+xml; charset=utf-8", preview.Header().Get("Content-Type"))
	require.NotEmpty(t, preview.Header().Get("Content-Security-Policy"))
	phone := callHTTP(t, handler, "POST", "/phonecodes/check", `{"phone":"+12025550123","region_code":"GB"}`, "")
	require.Equal(t, 200, phone.Code, phone.Body.String())
	var body struct {
		Data PhoneCheck `json:"data"`
	}
	require.NoError(t, json.Unmarshal(phone.Body.Bytes(), &body))
	require.Equal(t, "US", body.Data.RegionCode)
	require.Equal(t, "unavailable", body.Data.State)
	timezone := callHTTP(t, handler, "GET", "/timezones/Europe%2FLondon", "", "")
	require.Equal(t, 200, timezone.Code, timezone.Body.String())
}

// forbiddenReachability detects accidental SMS lookups during number normalisation.
type forbiddenReachability struct{ t *testing.T }

// Check fails when catalogue normalisation crosses into the optional SMS provider.
func (f forbiddenReachability) Check(context.Context, string) (telenumcoder.State, error) {
	f.t.Fatal("normalisation must not invoke SMS reachability")
	return telenumcoder.Unavailable, nil
}

// The provider fails any accidental reachability crossing during normalisation.
func TestNormalisePhoneComposesCatalogueWithoutReachability(t *testing.T) {
	for _, tc := range []struct {
		name, input, region, number, actual string
		hidden, cancelled                   bool
		want                                error
	}{
		{"national_GB", "020 7946 0018", "GB", "+442079460018", "GB", false, false, nil},
		{"international_US", "+1 202 555 0125", "GB", "+12025550125", "US", false, false, nil},
		{"national_JM", "8765550123", "JM", "+18765550123", "JM", false, false, nil},
		{"international_JM", "+1 876 555 0123", "GB", "+18765550123", "JM", false, false, nil},
		{"missing_region", "020 7946 0018", "", "", "", false, false, catalogue.ErrNotFound},
		{"hidden_actual_region", "+18765550123", "GB", "", "", true, false, catalogue.ErrNotSelectable},
		{"cancelled", "+12025550125", "GB", "", "", false, true, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := managerFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			repo, err := telenumcoder.NewRepository(cataloguestore.NewMemoryStore(nil))
			require.NoError(t, err)
			phones, err := telenumcoder.NewService(repo, nil, telenumcoder.WithReachability(forbiddenReachability{t}))
			require.NoError(t, err)
			require.NoError(t, phones.Migrate(ctx))
			s.phones = phones
			if tc.hidden {
				_, err := s.Update(ctx, PhoneCodes, "JM", Mutation{Hidden: ptr(true)}, 1, "admin")
				require.NoError(t, err)
			}
			if tc.cancelled {
				cancel()
			}
			result, err := s.NormalisePhone(ctx, tc.input, tc.region)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, result.NormalisedNumber)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.number, result.NormalisedNumber)
				require.Equal(t, tc.actual, result.RegionCode)
			}
		})
	}
}
