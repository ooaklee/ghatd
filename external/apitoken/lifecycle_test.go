package apitoken

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// creationRepository records only the new credential; unexpected calls panic
// through its nil embedded port rather than hiding an accidental dependency.
type creationRepository struct {
	ApitokenRespository
	created *UserAPIToken
}

func (r *creationRepository) CreateUserAPIToken(_ context.Context, token *UserAPIToken) (*UserAPIToken, error) {
	copy := *token
	copy.Generate().GenerateNewUUID()
	r.created = &copy
	return &copy, nil
}

func TestCreationLifetimeAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, prefix                   string
		ttl                            int64
		nilRequest, noOwner, cancelled bool
		want                           error
	}{
		{name: "permanent", prefix: "prefix"},
		{name: "one second", prefix: "prefix", ttl: 1},
		{name: "duration boundary", prefix: "prefix", ttl: math.MaxInt64 / int64(time.Second)},
		{name: "negative", prefix: "prefix", ttl: -1, want: ErrInvalidTokenTTL},
		{name: "duration overflow", prefix: "prefix", ttl: math.MaxInt64/int64(time.Second) + 1, want: ErrInvalidTokenTTL},
		{name: "integer overflow", prefix: "prefix", ttl: math.MaxInt64, want: ErrInvalidTokenTTL},
		{name: "missing prefix", want: ErrInvalidAPIFormatDetected},
		{name: "dotted prefix", prefix: "pre.fix", want: ErrInvalidAPIFormatDetected},
		{name: "whitespace prefix", prefix: "pre fix", want: ErrInvalidAPIFormatDetected},
		{name: "comma prefix", prefix: "pre,fix", want: ErrInvalidAPIFormatDetected},
		{name: "invisible prefix", prefix: "pre\u200bfix", want: ErrInvalidAPIFormatDetected},
		{name: "invalid UTF8 prefix", prefix: string([]byte{0xff}), want: ErrInvalidAPIFormatDetected},
		{name: "oversized prefix", prefix: strings.Repeat("p", 257), want: ErrInvalidAPIFormatDetected},
		{name: "missing owner", prefix: "prefix", noOwner: true, want: ErrRequiredUserIDMissing},
		{name: "nil request", nilRequest: true, want: ErrRequiredUserIDMissing},
		{name: "cancelled", prefix: "prefix", cancelled: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &creationRepository{}
			request := &CreateAPITokenRequest{UserID: "owner", UserNanoId: tc.prefix, TokenTtl: tc.ttl, Description: "  device  "}
			if tc.noOwner {
				request.UserID = ""
			}
			if tc.nilRequest {
				request = nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.cancelled {
				cancel()
			}
			got, err := NewService(repo).CreateAPIToken(ctx, request)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, got)
				require.Nil(t, repo.created)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, got.APIToken.ID)
			require.True(t, strings.HasPrefix(got.APIToken.Value, tc.prefix+"."))
			require.Equal(t, "device", repo.created.Description)
			require.Equal(t, "  device  ", request.Description, "service does not mutate caller request")
			created, err := time.Parse(time.RFC3339Nano, repo.created.CreatedAt)
			require.NoError(t, err)
			if tc.ttl == 0 {
				require.Empty(t, repo.created.TtlExpiresAt)
			} else {
				expiry, err := time.Parse(time.RFC3339Nano, repo.created.TtlExpiresAt)
				require.NoError(t, err)
				require.Equal(t, time.Duration(tc.ttl)*time.Second, expiry.Sub(created))
			}
		})
	}
}

// inventoryRepository rejects list-based admission and exposes query intent.
type inventoryRepository struct {
	ApitokenRespository
	permanent, ephemeral int64
	owners               []string
	failAt               int
	cancelAt             int
	cancel               context.CancelFunc
}

func (r *inventoryRepository) GetTotalApiTokens(_ context.Context, owner, prefix, description, status, to, from string, ephemeral, permanent bool) (int64, error) {
	r.owners = append(r.owners, owner)
	if len(r.owners) == r.cancelAt && r.cancel != nil {
		r.cancel()
	}
	if len(r.owners) == r.failAt {
		return 0, errors.New("count unavailable")
	}
	if prefix != "" || description != "" || status != "" || to != "" || from != "" || ephemeral == permanent {
		panic("inventory uses only owner and one type")
	}
	if permanent {
		return r.permanent, nil
	}
	return r.ephemeral, nil
}

func TestInventoryRejectsInvalidOrCanceledResults(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		permanent, ephemeral int64
		cancelAt, calls      int
		want                 error
	}{
		{"negative permanent", -1, 2, 0, 1, ErrInventoryUnavailable},
		{"negative ephemeral", 2, -1, 0, 2, ErrInventoryUnavailable},
		{"cancel first read", 2, 3, 1, 1, context.Canceled},
		{"cancel second read", 2, 3, 2, 2, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			repo := &inventoryRepository{permanent: tc.permanent, ephemeral: tc.ephemeral, cancelAt: tc.cancelAt, cancel: cancel}
			got, err := NewService(repo).CountTokenInventory(ctx, "owner")
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, got)
			require.Len(t, repo.owners, tc.calls)
		})
	}
}

func TestInventoryUsesExactCounts(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		permanent, ephemeral int64
		failAt               int
	}{
		{"empty", 0, 0, 0}, {"beyond presentation pages", 301, 145, 0}, {"first read failure", 2, 1, 1}, {"second read failure", 2, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &inventoryRepository{permanent: tc.permanent, ephemeral: tc.ephemeral, failAt: tc.failAt}
			got, err := NewService(repo).CountTokenInventory(context.Background(), "owner")
			if tc.failAt != 0 {
				require.Error(t, err)
				require.Zero(t, got)
				require.Len(t, repo.owners, tc.failAt)
			} else {
				require.NoError(t, err)
				require.Equal(t, Inventory{tc.permanent, tc.ephemeral}, got)
				require.Equal(t, []string{"owner", "owner"}, repo.owners)
			}
		})
	}
}

func TestReadableTokenTimestamps(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"fractional timestamp", "2026-01-01T10:00:00.123456789Z", true}, {"offset timestamp", "2026-01-01T12:00:00+02:00", true}, {"whole second timestamp", "2026-01-01T10:00:00Z", true}, {"malformed", "not-a-date", false}, {"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range []string{"last-used", "updated", "expires"} {
				t.Run(field, func(t *testing.T) {
					token := UserAPIToken{HumanReadableLastUsedAt: "stale", HumanReadableUpdatedAt: "stale", HumanReadableTtlExpiresAt: "stale"}
					switch field {
					case "last-used":
						token.LastUsedAt = tc.raw
					case "updated":
						token.UpdatedAt = tc.raw
					case "expires":
						token.TtlExpiresAt = tc.raw
					}
					before := token
					token.GenerateHumanReadable()
					require.Equal(t, before.LastUsedAt, token.LastUsedAt)
					require.Equal(t, before.UpdatedAt, token.UpdatedAt)
					require.Equal(t, before.TtlExpiresAt, token.TtlExpiresAt)
					for actual, value := range map[string]string{"last-used": token.HumanReadableLastUsedAt, "updated": token.HumanReadableUpdatedAt, "expires": token.HumanReadableTtlExpiresAt} {
						if actual == field && tc.valid {
							require.NotEmpty(t, value)
							require.NotEqual(t, "stale", value)
						} else {
							require.Empty(t, value)
						}
					}
				})
			}
		})
	}
}

func TestContradictoryListFiltersFailBeforeStorage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count bool
	}{{"count", true}, {"list", false}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewRepository(nil)
			if tc.count {
				got, err := repo.GetTotalApiTokens(context.Background(), "owner", "", "", "", "", "", true, true)
				require.ErrorIs(t, err, ErrInvalidTokenQuery)
				require.Zero(t, got)
			} else {
				got, err := repo.GetAPITokens(context.Background(), &GetAPITokensRequest{OnlyPermanent: true, OnlyEphemeral: true})
				require.ErrorIs(t, err, ErrInvalidTokenQuery)
				require.Nil(t, got)
			}
		})
	}
}
