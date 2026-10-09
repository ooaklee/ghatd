package currencycoder

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	repo, err := NewRepository(cataloguestore.NewMemoryStore(nil))
	require.NoError(t, err)
	service, err := NewService(repo, catalogue.ClockFunc(func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }))
	require.NoError(t, err)
	require.NoError(t, service.Migrate(context.Background()))
	return service
}

// This single revision-chain scenario verifies that reseeding preserves earlier
// edits, deletion and restoration receipts. Splitting those dependent steps
// would lose the history contract; independent definitions use tables below.
func TestCurrencyAuditPolicyAndIdempotentMigration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	rows, total, err := s.List(ctx, catalogue.ListQuery{PageSize: 500})
	require.NoError(t, err)
	require.EqualValues(t, 165, total)
	require.Len(t, rows, 165)
	for _, row := range rows {
		require.Equal(t, 1, row.Revision, row.Code)
		require.Equal(t, catalogue.SystemSeedActor, row.CreatedBy, row.Code)
	}
	gbp, err := s.Get(ctx, "GBP")
	require.NoError(t, err)
	created := gbp.CreatedAt
	require.NotNil(t, gbp.UpdatedAt)
	gbp.Hidden = true
	gbp.Name = "Sterling"
	gbp.CreatedBy = "spoof"
	changed, err := s.Update(ctx, UpdateRequest{Record: gbp, ExpectedRevision: 1, ActorID: "admin-one"})
	require.NoError(t, err)
	require.Equal(t, catalogue.SystemSeedActor, changed.CreatedBy)
	require.Equal(t, created, changed.CreatedAt)
	require.Equal(t, "admin-one", changed.UpdatedBy)
	_, err = s.Update(ctx, UpdateRequest{Record: gbp, ExpectedRevision: 1, ActorID: "admin-two"})
	require.ErrorIs(t, err, catalogue.ErrStaleWrite)
	require.NoError(t, s.Migrate(ctx))
	kept, err := s.Get(ctx, "GBP")
	require.NoError(t, err)
	require.Equal(t, changed, kept)
	_, err = s.RequireSelectable(ctx, "GBP")
	require.ErrorIs(t, err, catalogue.ErrNotSelectable)
	removed, err := s.Delete(ctx, ChangeRequest{Code: "GBP", ExpectedRevision: 2, ActorID: "admin-two"})
	require.NoError(t, err)
	require.NotNil(t, removed.DeletedAt)
	require.Equal(t, "admin-two", removed.DeletedBy)
	require.NoError(t, s.Migrate(ctx))
	kept, err = s.Get(ctx, "GBP")
	require.NoError(t, err)
	require.Equal(t, removed, kept)
	restored, err := s.Restore(ctx, ChangeRequest{Code: "GBP", ExpectedRevision: 3, ActorID: "admin-three"})
	require.NoError(t, err)
	require.Nil(t, restored.DeletedAt)
	require.False(t, restored.Enabled)
	require.Equal(t, created, restored.CreatedAt)
	require.NoError(t, s.Migrate(ctx))
	kept, err = s.Get(ctx, "GBP")
	require.NoError(t, err)
	require.Equal(t, restored, kept)
}
func TestCurrencyPrecisionCannotBeRedefined(t *testing.T) {
	for _, tc := range []struct {
		code   string
		digits int
	}{{"GBP", 0}, {"JPY", 2}, {"KWD", 2}} {
		t.Run(tc.code, func(t *testing.T) {
			s := fixture(t)
			record, err := s.Get(t.Context(), tc.code)
			require.NoError(t, err)
			record.MinorUnit = tc.digits
			_, err = s.Update(t.Context(), UpdateRequest{Record: record, ExpectedRevision: 1, ActorID: "admin"})
			require.ErrorIs(t, err, catalogue.ErrInvalidPayload)
		})
	}
}
func TestCurrencyImmutableDefinitions(t *testing.T) {
	for _, tc := range []struct {
		code      string
		digits    int
		supported bool
	}{
		{"BGN", 0, false}, {"ANG", 0, false}, {"HRK", 0, false}, {"SLL", 0, false},
		{"GGP", 0, false}, {"IMP", 0, false}, {"JEP", 0, false}, {"ZZZ", 0, false},
		{"JPY", 0, true}, {"KWD", 3, true}, {"GBP", 2, true}, {"IDR", 2, true}, {"CLF", 4, true}, {"UYW", 4, true},
	} {
		t.Run(tc.code, func(t *testing.T) {
			got, ok := MinorUnit(tc.code)
			require.Equal(t, tc.supported, ok)
			require.Equal(t, tc.digits, got)
		})
	}
}
func TestCurrencyActorsAreNotBodyFields(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"capital_field", `{"record":{"code":"GBP"},"ActorID":"spoof"}`},
		{"snake_field", `{"record":{"code":"GBP"},"actor_id":"spoof"}`},
		{"both_fields", `{"record":{"code":"GBP"},"ActorID":"spoof","actor_id":"spoof"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req CreateRequest
			require.NoError(t, json.Unmarshal([]byte(tc.body), &req))
			require.Empty(t, req.ActorID)
			_, err := fixture(t).Create(t.Context(), req)
			require.ErrorIs(t, err, catalogue.ErrInvalidPayload)
		})
	}
}
