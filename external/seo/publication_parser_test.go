package seo

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseSitemapPublishedTimestamp(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr error
		wantUTC time.Time
	}{
		{
			name:    "empty value clears schedule",
			value:   "",
			wantUTC: time.Time{},
		},
		{
			name:    "whitespace only clears schedule",
			value:   "   ",
			wantUTC: time.Time{},
		},
		{
			name:    "RFC3339Nano UTC Z with nanoseconds",
			value:   "2027-01-05T09:00:00.123456789Z",
			wantUTC: time.Date(2027, 1, 5, 9, 0, 0, 123456789, time.UTC),
		},
		{
			name:    "RFC3339 with positive offset normalised to UTC",
			value:   "2027-01-05T10:30:15.5+01:30",
			wantUTC: time.Date(2027, 1, 5, 9, 0, 15, 500000000, time.UTC),
		},
		{
			name:    "RFC3339 with negative offset normalised to UTC",
			value:   "2027-01-05T04:00:00-05:00",
			wantUTC: time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC),
		},
		{
			name:    "zone-less platform UTC timestamp with fraction",
			value:   "2027-01-05T09:00:00.25",
			wantUTC: time.Date(2027, 1, 5, 9, 0, 0, 250000000, time.UTC),
		},
		{
			name:    "zone-less platform UTC timestamp without fraction",
			value:   "2027-01-05T09:00:00",
			wantUTC: time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC),
		},
		{
			name:    "maximal boundary offset accepted",
			value:   "2027-01-05T08:14:59+00:45",
			wantUTC: time.Date(2027, 1, 5, 7, 29, 59, 0, time.UTC),
		},
		{
			name:    "negative boundary offset accepted",
			value:   "2027-01-05T09:59:59-23:59",
			wantUTC: time.Date(2027, 1, 6, 9, 58, 59, 0, time.UTC),
		},
		{
			name:    "date-only rejected",
			value:   "2027-01-05",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{
			name:    "offset hours out of range",
			value:   "2027-01-05T09:00:00+24:00",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{
			name:    "offset minutes out of range",
			value:   "2027-01-05T09:00:00+00:60",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{
			name:    "zero timestamp rejected",
			value:   "0000-01-01T00:00:00Z",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{
			name:    "year zero rejected",
			value:   "0000-12-31T23:59:59Z",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{
			name:    "year out of representable range rejected",
			value:   "10000-01-01T00:00:00Z",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{
			name:    "malformed month rejected",
			value:   "2027-13-05T09:00:00Z",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{
			name:    "missing offset on fractional value rejected",
			value:   "2027-01-05T09:00:00.5+",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
		{name: "single digit hour", value: "2027-01-05T9:00:00Z", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "comma fraction", value: "2027-01-05T09:00:00,1Z", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "excess precision", value: "2027-01-05T09:00:00.1234567891Z", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "raw year zero offset crosses into year one", value: "0000-12-31T23:59:59-01:00", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "Go zero instant", value: "0001-01-01T00:00:00Z", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "UTC underflow", value: "0001-01-01T00:00:00+01:00", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "UTC overflow", value: "9999-12-31T23:59:59-01:00", wantErr: ErrSitemapItemInvalidPublishedAt},
		{
			name:    "not a timestamp",
			value:   "tomorrow",
			wantErr: ErrSitemapItemInvalidPublishedAt,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSitemapPublishedTimestamp(tc.value)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.True(t, got.Equal(tc.wantUTC), "got %v want %v", got, tc.wantUTC)
		})
	}
}

func TestNormaliseSitemapItemPublishedAt(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr error
		want    string
	}{
		{name: "empty stays empty", value: "", want: ""},
		{name: "whitespace clears to empty", value: " \t ", want: ""},
		{
			name:  "offset value normalised to precise UTC",
			value: "2027-01-05T10:00:00.123456789+01:00",
			want:  "2027-01-05T09:00:00.123456789Z",
		},
		{
			name:  "already UTC keeps precise nanoseconds",
			value: "2027-01-05T09:00:00.000000001Z",
			want:  "2027-01-05T09:00:00.000000001Z",
		},
		{
			name:  "zone-less platform value normalised",
			value: "2027-01-05T09:00:00.5",
			want:  "2027-01-05T09:00:00.5Z",
		},
		{name: "invalid rejected", value: "2027-01-05T09:00:00+24:00", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "zero instant rejected", value: "0001-01-01T00:00:00Z", wantErr: ErrSitemapItemInvalidPublishedAt},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normaliseSitemapItemPublishedAt(tc.value)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNormaliseSitemapItemPublishedAtInput(t *testing.T) {
	t.Run("nil pointer omits schedule", func(t *testing.T) {
		got, err := normaliseSitemapItemPublishedAtInput(nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("empty pointer clears gate", func(t *testing.T) {
		value := ""
		got, err := normaliseSitemapItemPublishedAtInput(&value)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("whitespace pointer clears gate", func(t *testing.T) {
		value := "  "
		got, err := normaliseSitemapItemPublishedAtInput(&value)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("valid pointer normalised", func(t *testing.T) {
		value := "2027-01-05T10:00:00+01:00"
		got, err := normaliseSitemapItemPublishedAtInput(&value)
		require.NoError(t, err)
		require.Equal(t, "2027-01-05T09:00:00Z", got)
	})

	t.Run("invalid pointer rejected before writes", func(t *testing.T) {
		value := "not-a-time"
		_, err := normaliseSitemapItemPublishedAtInput(&value)
		require.ErrorIs(t, err, ErrSitemapItemInvalidPublishedAt)
	})
}

func TestValidateSitemapItemNormalisesPublishedAt(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr error
		want    string
	}{
		{name: "empty keeps legacy inclusion", value: "", want: ""},
		{
			name:  "valid offset normalised in place",
			value: "2027-01-05T10:00:00.25+01:00",
			want:  "2027-01-05T09:00:00.25Z",
		},
		{
			name:  "zone-less normalised with Z",
			value: "2027-01-05T09:00:00",
			want:  "2027-01-05T09:00:00Z",
		},
		{name: "invalid stored value rejected", value: "2027-01-05T09:00:00+00:60", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "zero stored value rejected", value: "0000-01-01T00:00:00Z", wantErr: ErrSitemapItemInvalidPublishedAt},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &SitemapItem{URI: "/scheduled", PublishedAt: tc.value}
			err := validateSitemapItem(item)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, item.PublishedAt)
		})
	}
}

func TestNewSitemapItemPreservesRawPublishedAtForValidation(t *testing.T) {
	invalid := "definitely-not-a-timestamp"
	item := NewSitemapItem(&CreateSitemapItemRequest{
		URI:         "/raw-invalid",
		PublishedAt: &invalid,
	})
	require.Equal(t, invalid, item.PublishedAt, "constructor must preserve raw value for validation")
	require.ErrorIs(t, validateSitemapItem(item), ErrSitemapItemInvalidPublishedAt)
}

func TestSitemapItemIsPublishedForEligibility(t *testing.T) {
	evaluation := time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		publishedAt string
		wantErr     error
		eligible    bool
	}{
		{name: "missing value keeps legacy inclusion", publishedAt: "", eligible: true},
		{name: "empty value keeps legacy inclusion", publishedAt: "", eligible: true},
		{name: "past instant eligible", publishedAt: "2027-01-05T08:59:59.999999999Z", eligible: true},
		{name: "exact instant eligible", publishedAt: "2027-01-05T09:00:00Z", eligible: true},
		{name: "future instant excluded", publishedAt: "2027-01-05T09:00:00.000000001Z", eligible: false},
		{
			name:        "future offset instant excluded after UTC conversion",
			publishedAt: "2027-01-05T10:00:00+01:00",
			eligible:    true,
		},
		{name: "invalid stored value errors", publishedAt: "nonsense", wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "zero stored value errors", publishedAt: "0000-01-01T00:00:00Z", wantErr: ErrSitemapItemInvalidPublishedAt},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := SitemapItem{URI: "/eligibility", PublishedAt: tc.publishedAt}
			eligible, err := item.isPublishedForEligibility(evaluation)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.eligible, eligible)
		})
	}
}

func TestSitemapItemPublishedAtIndependentFromLastMod(t *testing.T) {
	item := &SitemapItem{
		URI:         "/independent",
		LastMod:     "2026-01-01T00:00:00Z",
		PublishedAt: "2027-06-01T12:00:00Z",
	}
	require.NoError(t, validateSitemapItem(item))
	require.Equal(t, "2026-01-01T00:00:00Z", item.LastMod, "validation must not touch last_mod")
	require.Equal(t, "2027-06-01T12:00:00Z", item.PublishedAt)
	require.NotEqual(t, item.LastMod, item.PublishedAt)
}

func TestSitemapItemErrorsAreDistinct(t *testing.T) {
	require.False(t, errors.Is(ErrSitemapItemInvalidPublishedAt, ErrSitemapItemInvalidLastMod))
	require.False(t, errors.Is(ErrSitemapItemInvalidPublishedAt, ErrSitemapItemPublicationScheduleUnsupported))
}
