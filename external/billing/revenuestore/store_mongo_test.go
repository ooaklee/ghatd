package revenuestore

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type fixtureClock struct{ at time.Time }

func (c fixtureClock) Now() time.Time { return c.at }
func revenueFixture(t *testing.T) (*Repository, *recordstore.MongoStore, *mongo.Database, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("revenue_test_" + strings.ToLower(rand.Text()))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, client.Disconnect(cleanup))
	})
	cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
	require.NoError(t, err)
	require.NoError(t, store.EnsureIndexes(ctx))
	require.NoError(t, store.Probe(ctx))
	r, err := NewRepository(store)
	require.NoError(t, err)
	return r, store, db, ctx
}
func revenueFact() billing.RevenueFact {
	return billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture-provider", AccountID: "fixture-account", LiveMode: false}, Kind: billing.RevenuePayment, PaymentID: "economic-payment", InvoiceID: "private-invoice-reference", AllocationID: "line-one", PrincipalID: "owning-principal", SubscriptionID: "subscription", PlanID: "eligible-plan", CostID: "cost", Currency: "EUR", CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
}
func revenueService(t *testing.T, r billing.RevenueRepository) *billing.RevenueService {
	t.Helper()
	s, err := billing.NewRevenueService(r, fixtureClock{time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	return s
}
func accept(t *testing.T, s *billing.RevenueService, ctx context.Context, envelope string, f billing.RevenueFact) billing.RevenueObservation {
	t.Helper()
	o, err := s.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: envelope, Facts: []billing.RevenueFact{f}})
	require.NoError(t, err)
	return o
}
func TestMongoRevenueEconomicIdentity(t *testing.T) {
	cases := []struct {
		name          string
		account       string
		live          bool
		changedAmount bool
		wantNew       bool
	}{
		{"same economics new delivery", "fixture-account", false, false, false},
		{"changed economics conflict", "fixture-account", false, true, false},
		{"separate provider account", "other-account", false, false, true},
		{"separate live mode", "fixture-account", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, db, ctx := revenueFixture(t)
			s := revenueService(t, r)
			f := revenueFact()
			first := accept(t, s, ctx, "delivery-one", f)
			f.Scope.AccountID = tc.account
			f.Scope.LiveMode = tc.live
			if tc.changedAmount {
				f.PaidMinor++
			}
			second, err := s.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "delivery-two", Facts: []billing.RevenueFact{f}})
			if tc.changedAmount {
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
			} else {
				require.NoError(t, err)
				if tc.wantNew {
					require.NotEqual(t, first.FactIDs, second.FactIDs)
				} else {
					require.Equal(t, first.FactIDs, second.FactIDs)
				}
			}
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindFact})
			require.NoError(t, err)
			want := int64(1)
			if tc.wantNew {
				want = 2
			}
			require.Equal(t, want, count)
			var raw bson.M
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindFact}).Decode(&raw))
			require.NotContains(t, fmt.Sprint(raw), "private-invoice-reference")
		})
	}
}

var failWrite = errors.New("injected-revenue-write")

type failingStore struct {
	recordstore.Store
	at        int
	uncertain bool
}
type failingTx struct {
	recordstore.Tx
	count, at int
}

func (t *failingTx) Insert(ctx context.Context, r recordstore.Record) error {
	t.count++
	if t.count == t.at {
		return failWrite
	}
	return t.Tx.Insert(ctx, r)
}
func (s failingStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	err := s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(&failingTx{Tx: tx, at: s.at}) })
	if err == nil && s.uncertain {
		return recordstore.ErrUncertain
	}
	return err
}
func TestMongoRevenueAtomicAcceptance(t *testing.T) {
	cases := []struct {
		name string
		at   int
	}{{"sequence head", 1}, {"verified fact", 2}, {"delivery observation", 3}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, store, db, ctx := revenueFixture(t)
			r, err := NewRepository(failingStore{Store: store, at: tc.at})
			require.NoError(t, err)
			s := revenueService(t, r)
			f := revenueFact()
			_, err = s.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "delivery", Facts: []billing.RevenueFact{f}})
			require.ErrorIs(t, err, failWrite)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count)
			pending, err := s.PendingRevenueFacts(ctx, "consumer", 200)
			require.NoError(t, err)
			require.Empty(t, pending)
		})
	}
}
func TestMongoRevenueLostAcknowledgementAndConcurrentDeliveries(t *testing.T) {
	// A real commit with a lost receipt followed by overlapping provider deliveries
	// must retain exactly one economic fact and a stable committed sequence.
	r, store, db, ctx := revenueFixture(t)
	uncertain, err := NewRepository(failingStore{Store: store, uncertain: true})
	require.NoError(t, err)
	u := revenueService(t, uncertain)
	f := revenueFact()
	req := billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "first-delivery", Facts: []billing.RevenueFact{f}}
	_, err = u.AcceptVerified(ctx, req)
	require.ErrorIs(t, err, billing.ErrRevenueUncertain)
	s := revenueService(t, r)
	first, err := s.AcceptVerified(ctx, req)
	require.NoError(t, err)
	start := make(chan struct{})
	out := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			<-start
			obs, err := s.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: fmt.Sprintf("replay-%d", i), Facts: []billing.RevenueFact{f}})
			if err == nil && obs.FactIDs[0] != first.FactIDs[0] {
				err = errors.New("changed economic fact")
			}
			out <- err
		}(i)
	}
	close(start)
	for i := 0; i < 8; i++ {
		require.NoError(t, <-out)
	}
	count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindFact})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	pending, err := s.PendingRevenueFacts(ctx, "consumer", 200)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.EqualValues(t, 1, pending[0].Sequence)
}
func TestMongoRevenueIndependentFeedRecovery(t *testing.T) {
	// This multi-consumer sequence exercises a late earlier-paid fact and a poison
	// quarantine independently of a later acknowledgement; no global cursor exists.
	r, _, _, ctx := revenueFixture(t)
	s := revenueService(t, r)
	f := revenueFact()
	first := accept(t, s, ctx, "first", f)
	f.PaymentID = "renewal"
	f.EffectiveAt = f.EffectiveAt.Add(time.Hour)
	second := accept(t, s, ctx, "second", f)
	ack := billing.RevenueAcknowledgement{ConsumerID: "partners", FactID: second.FactIDs[0], AcceptanceID: "durable-owning-journal-decision", Outcome: "accepted", ActorID: "worker"}
	require.NoError(t, s.AcknowledgeRevenueFact(ctx, ack))
	require.NoError(t, s.AcknowledgeRevenueFact(ctx, ack))
	ack.AcceptanceID = "changed"
	require.ErrorIs(t, s.AcknowledgeRevenueFact(ctx, ack), billing.ErrRevenueConflict)
	f.PaymentID = "late-first-period"
	f.EffectiveAt = f.EffectiveAt.Add(-24 * time.Hour)
	late := accept(t, s, ctx, "late", f)
	pending, err := s.PendingRevenueFacts(ctx, "partners", 200)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.Equal(t, first.FactIDs[0], pending[0].ID)
	require.Equal(t, late.FactIDs[0], pending[1].ID)
	other, err := s.PendingRevenueFacts(ctx, "independent-audit", 200)
	require.NoError(t, err)
	require.Len(t, other, 3)
	require.NoError(t, s.AcknowledgeRevenueFact(ctx, billing.RevenueAcknowledgement{ConsumerID: "partners", FactID: first.FactIDs[0], AcceptanceID: "durable-quarantine-decision", Outcome: "quarantined", ActorID: "worker"}))
	pending, err = s.PendingRevenueFacts(ctx, "partners", 1)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, late.FactIDs[0], pending[0].ID)
	// A restarted repository must derive the same pending state from durable data.
	restarted, err := NewRepository(r.store)
	require.NoError(t, err)
	again, err := restarted.PendingRevenueFacts(ctx, "partners", 200)
	require.NoError(t, err)
	require.Equal(t, pending, again)
}
