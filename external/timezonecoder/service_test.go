package timezonecoder

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"github.com/stretchr/testify/require"
)

func timezoneFixture(t *testing.T) *Service {
	t.Helper()
	repo, err := NewRepository(cataloguestore.NewMemoryStore(nil))
	require.NoError(t, err)
	s, err := NewService(repo, nil)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	return s
}
func TestTimezoneDateAwareOffsets(t *testing.T) {
	for _, tc := range []struct {
		code, date string
		offset     int
	}{
		{"Europe/London", "2026-07-01T12:00:00Z", 3600}, {"Europe/London", "2026-12-01T12:00:00Z", 0},
		{"America/New_York", "2026-07-01T12:00:00Z", -14400}, {"Asia/Kolkata", "2026-12-01T12:00:00Z", 19800},
	} {
		t.Run(tc.code+"/"+tc.date, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.date)
			require.NoError(t, err)
			value, err := timezoneFixture(t).Describe(t.Context(), tc.code, at)
			require.NoError(t, err)
			require.Equal(t, tc.offset, value.OffsetSeconds)
		})
	}
}
func TestTimezoneDefinitionsAndAliases(t *testing.T) {
	for _, tc := range []struct {
		code  string
		valid bool
	}{{"Local", false}, {"+03:00", false}, {"Etc/GMT-3", false}, {"Not/AZone", false}, {"Asia/Calcutta", true}, {"UTC", true}, {"Europe/Kyiv", true}} {
		t.Run(tc.code, func(t *testing.T) {
			s := timezoneFixture(t)
			if tc.valid {
				_, err := s.Get(t.Context(), tc.code)
				require.NoError(t, err)
			} else {
				_, err := s.Create(t.Context(), CreateRequest{ActorID: "admin", Record: Timezone{Entry: catalogue.Entry{Code: tc.code, Name: tc.code}}})
				require.ErrorIs(t, err, catalogue.ErrInvalidPayload)
			}
		})
	}
}
func TestTimezoneNamesDescribeTheIdentifierRatherThanSecondaryRegions(t *testing.T) {
	rows, err := Seeds()
	require.NoError(t, err)
	names := map[string]string{}
	for _, row := range rows {
		names[row.Code] = row.Name
	}
	for _, tc := range []struct{ code, want string }{{"Asia/Tokyo", "Tokyo"}, {"America/New_York", "New York"}, {"UTC", "Coordinated Universal Time"}} {
		t.Run(tc.code, func(t *testing.T) { require.Equal(t, tc.want, names[tc.code]) })
	}
}
