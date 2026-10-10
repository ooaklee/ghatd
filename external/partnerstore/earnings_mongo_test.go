package partnerstore

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoTestClock struct{ now time.Time }

func (c *mongoTestClock) Now() time.Time { return c.now }

type randomIDs struct{}

func (randomIDs) NewID() string { return rand.Text() }
func mongoEarnings(t *testing.T) (*partnerearnings.Service, *EarningsRepository, *recordstore.MongoStore, *mongo.Database, *mongoTestClock, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("ghatd_partner_test_" + strings.ToLower(rand.Text()))
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
	repo, err := NewEarningsRepository(store, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
	require.NoError(t, err)
	clock := &mongoTestClock{time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	svc, err := partnerearnings.NewService(repo, clock, randomIDs{}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
	require.NoError(t, err)
	return svc, repo, store, db, clock, ctx
}
func accrue(t *testing.T, svc *partnerearnings.Service, ctx context.Context, clock *mongoTestClock, payment string, amount int64) {
	t.Helper()
	_, err := svc.Accrue(ctx, partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: payment, PaymentMinor: amount, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: clock.Now().Add(-time.Hour), ReferralID: "referral", TermsVersion: "fixture-terms", PolicyID: "fixture-policy"})
	require.NoError(t, err)
}
func claimRequest(amount int64, key string) partnerearnings.ClaimRequest {
	return partnerearnings.ClaimRequest{ActorID: "owner", PartnerID: "partner", AmountMinor: amount, Currency: "EUR", DestinationID: "destination", DestinationSnapshot: map[string]string{"method": "paypal", "email": "fixture@example.test"}, IdempotencyKey: key}
}
func process(t *testing.T, svc *partnerearnings.Service, ctx context.Context, c partnerearnings.Claim) {
	t.Helper()
	_, err := svc.DecideClaim(ctx, partnerearnings.ClaimDecision{ClaimID: c.ID, NewState: partnerearnings.ClaimProcessing, ActorID: "operator", Reason: "manual processing", ExpectedRevision: c.Revision})
	require.NoError(t, err)
}
func paymentRequest(c partnerearnings.Claim, clock *mongoTestClock, key string) partnerearnings.RecordPaymentRequest {
	return partnerearnings.RecordPaymentRequest{ClaimID: c.ID, ActorID: "operator", Method: "paypal", Reference: "fixture-manual-reference", PaidAt: clock.Now().Add(-time.Minute), IdempotencyKey: key, ExpectedRevision: c.Revision + 1, AmountMinor: c.AmountMinor, Currency: c.Currency, State: partnerearnings.PaymentStateFull}
}
func TestMongoConcurrentClaimsCannotOverspend(t *testing.T) {
	// One overlapping stateful race is the behavior under test; each actor has a
	// distinct receipt identity, so uniqueness alone cannot enforce the balance.
	svc, repo, _, _, clock, ctx := mongoEarnings(t)
	accrue(t, svc, ctx, clock, "payment", 10000)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, key := range []string{"claim-one", "claim-two"} {
		go func(key string) { <-start; _, err := svc.RequestClaim(ctx, claimRequest(1500, key)); errs <- err }(key)
	}
	close(start)
	a, b := <-errs, <-errs
	require.True(t, (a == nil && errors.Is(b, partnerearnings.ErrInsufficient)) || (b == nil && errors.Is(a, partnerearnings.ErrInsufficient)), "outcomes: %v / %v", a, b)
	claims, err := repo.ListClaims(ctx, "fixture-program", "partner", nil, 0, "")
	require.NoError(t, err)
	require.Len(t, claims, 1)
	balance, err := svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.EqualValues(t, 1500, balance.ReservedMinor)
	require.EqualValues(t, 500, balance.AvailableMinor)
}
func TestMongoDebtFixtureAndCustomerPaymentEvidence(t *testing.T) {
	// A stateful payout/refund/renewal lifecycle proves the net debt invariant and
	// durable customer-visible manual evidence against the actual datastore.
	svc, repo, _, db, clock, ctx := mongoEarnings(t)
	accrue(t, svc, ctx, clock, "first-payment", 10000)
	claim, err := svc.RequestClaim(ctx, claimRequest(1500, "claim"))
	require.NoError(t, err)
	process(t, svc, ctx, claim)
	paid, err := svc.RecordPayment(ctx, paymentRequest(claim, clock, "record"))
	require.NoError(t, err)
	require.Equal(t, partnerearnings.ClaimPaid, paid.State)
	require.Equal(t, "fixture-manual-reference", paid.Payment.Reference)
	fresh, err := repo.GetClaim(ctx, "fixture-program", claim.ID)
	require.NoError(t, err)
	require.Equal(t, paid.Payment, fresh.Payment)
	_, err = svc.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: "partner", PaymentID: "first-payment", RefundID: "refund", CumulativeRefundedMinor: 5000, Currency: "EUR", OccurredAt: clock.Now()})
	require.NoError(t, err)
	balance, err := svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.EqualValues(t, 500, balance.DebtMinor)
	accrue(t, svc, ctx, clock, "renewal", 4000)
	balance, err = svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.Zero(t, balance.DebtMinor)
	require.EqualValues(t, 300, balance.AvailableMinor)
	cursor, err := db.Collection("ghatd_owned_records").Find(ctx, bson.M{"kind": kindClaim})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cursor.Close(context.Background()) })
	var raw []bson.M
	require.NoError(t, cursor.All(ctx, &raw))
	require.NotContains(t, fmt.Sprint(raw), "fixture@example.test")
	require.NotContains(t, fmt.Sprint(raw), "fixture-manual-reference")
	auditCount, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindClaimRevision})
	require.NoError(t, err)
	require.EqualValues(t, 3, auditCount)
}

type injectedStore struct {
	recordstore.Store
	failAt    int
	uncertain bool
}
type injectedTx struct {
	recordstore.Tx
	writes, failAt int
}

var injectedFailure = errors.New("injected-financial-write-failure")

func (t *injectedTx) Insert(ctx context.Context, r recordstore.Record) error {
	t.writes++
	if t.writes == t.failAt {
		return injectedFailure
	}
	return t.Tx.Insert(ctx, r)
}
func (s injectedStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	err := s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(&injectedTx{Tx: tx, failAt: s.failAt}) })
	if err == nil && s.uncertain {
		return recordstore.ErrUncertain
	}
	return err
}
func TestMongoClaimWritesRollBackTogether(t *testing.T) {
	cases := []struct {
		name   string
		failAt int
	}{{"claim head", 1}, {"immutable audit", 2}, {"allocation journal", 3}, {"source anchor", 4}, {"receipt", 5}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, _, clock, ctx := mongoEarnings(t)
			accrue(t, svc, ctx, clock, "payment", 10000)
			brokenRepo, err := NewEarningsRepository(injectedStore{Store: store, failAt: tc.failAt}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
			require.NoError(t, err)
			broken, err := partnerearnings.NewService(brokenRepo, clock, randomIDs{}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
			require.NoError(t, err)
			_, err = broken.RequestClaim(ctx, claimRequest(1500, "same-key"))
			require.ErrorIs(t, err, injectedFailure)
			claims, err := repo.ListClaims(ctx, "fixture-program", "partner", nil, 0, "")
			require.NoError(t, err)
			require.Empty(t, claims)
			entries, err := repo.ListEntries(ctx, "fixture-program", "partner")
			require.NoError(t, err)
			require.Len(t, entries, 2)
			balance, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.EqualValues(t, 2000, balance.AvailableMinor)
			_, err = svc.RequestClaim(ctx, claimRequest(1500, "same-key"))
			require.NoError(t, err)
		})
	}
}
func TestMongoUncertainCommitReplaysSameClaim(t *testing.T) {
	// A committed transaction with a lost acknowledgement must replay its receipt.
	svc, repo, store, _, clock, ctx := mongoEarnings(t)
	accrue(t, svc, ctx, clock, "payment", 10000)
	uncertainRepo, err := NewEarningsRepository(injectedStore{Store: store, uncertain: true}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
	require.NoError(t, err)
	uncertain, err := partnerearnings.NewService(uncertainRepo, clock, randomIDs{}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
	require.NoError(t, err)
	req := claimRequest(1500, "same-key")
	_, err = uncertain.RequestClaim(ctx, req)
	require.ErrorIs(t, err, partnerearnings.ErrUncertain)
	replayed, err := svc.RequestClaim(ctx, req)
	require.NoError(t, err)
	claims, err := repo.ListClaims(ctx, "fixture-program", "partner", nil, 0, "")
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, claims[0].ID, replayed.ID)
	req.AmountMinor = 1000
	_, err = svc.RequestClaim(ctx, req)
	require.ErrorIs(t, err, partnerearnings.ErrConflict)
}
func TestMongoManualRecordReceiptBindsClaimIdentity(t *testing.T) {
	// One operator reusing a key for another claim is a changed payload, even when
	// method/reference/date match. The second claim cannot replay the first one.
	svc, _, _, _, clock, ctx := mongoEarnings(t)
	accrue(t, svc, ctx, clock, "payment", 10000)
	one, err := svc.RequestClaim(ctx, claimRequest(500, "one"))
	require.NoError(t, err)
	two, err := svc.RequestClaim(ctx, claimRequest(500, "two"))
	require.NoError(t, err)
	process(t, svc, ctx, one)
	process(t, svc, ctx, two)
	_, err = svc.RecordPayment(ctx, paymentRequest(one, clock, "same-record-key"))
	require.NoError(t, err)
	_, err = svc.RecordPayment(ctx, paymentRequest(two, clock, "same-record-key"))
	require.ErrorIs(t, err, partnerearnings.ErrConflict)
}
