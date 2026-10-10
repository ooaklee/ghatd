package billing

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type revenueTestClock struct{ at time.Time }

func (c revenueTestClock) Now() time.Time { return c.at }

type revenueTestRepo struct {
	mu               sync.Mutex
	facts            map[string]RevenueFact
	observations     map[string]RevenueObservation
	acknowledgements map[string]RevenueAcknowledgement
	sequence         int64
	failAt           int
	uncertain        bool
	readsFail        error
}

func newRevenueTestRepo() *revenueTestRepo {
	return &revenueTestRepo{facts: map[string]RevenueFact{}, observations: map[string]RevenueObservation{}, acknowledgements: map[string]RevenueAcknowledgement{}}
}

type revenueTestTx struct {
	facts          map[string]RevenueFact
	observations   map[string]RevenueObservation
	sequence       int64
	writes, failAt int
}

func (tx *revenueTestTx) write() error {
	tx.writes++
	if tx.failAt > 0 && tx.writes == tx.failAt {
		return ErrRevenueUnavailable
	}
	return nil
}
func (tx *revenueTestTx) GetObservation(ctx context.Context, id string) (RevenueObservation, error) {
	o, ok := tx.observations[id]
	if !ok {
		return RevenueObservation{}, ErrRevenueNotFound
	}
	return o, nil
}
func (tx *revenueTestTx) InsertObservation(ctx context.Context, o RevenueObservation) error {
	if err := tx.write(); err != nil {
		return err
	}
	if _, ok := tx.observations[o.ID]; ok {
		return ErrRevenueConflict
	}
	tx.observations[o.ID] = o
	return nil
}
func (tx *revenueTestTx) GetFact(ctx context.Context, id string) (RevenueFact, error) {
	f, ok := tx.facts[id]
	if !ok {
		return RevenueFact{}, ErrRevenueNotFound
	}
	return f, nil
}
func (tx *revenueTestTx) AppendFact(ctx context.Context, f RevenueFact) (RevenueFact, error) {
	if err := tx.write(); err != nil {
		return RevenueFact{}, err
	}
	if _, ok := tx.facts[f.ID]; ok {
		return RevenueFact{}, ErrRevenueConflict
	}
	tx.sequence++
	f.Sequence = tx.sequence
	tx.facts[f.ID] = f
	return f, nil
}
func (r *revenueTestRepo) WithRevenueTransaction(ctx context.Context, fn func(RevenueTx) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.readsFail != nil {
		return r.readsFail
	}
	tx := &revenueTestTx{facts: map[string]RevenueFact{}, observations: map[string]RevenueObservation{}, sequence: r.sequence, failAt: r.failAt}
	for k, v := range r.facts {
		tx.facts[k] = v
	}
	for k, v := range r.observations {
		tx.observations[k] = v
	}
	if err := fn(tx); err != nil {
		return err
	}
	r.facts = tx.facts
	r.observations = tx.observations
	r.sequence = tx.sequence
	if r.uncertain {
		return ErrRevenueUncertain
	}
	return nil
}
func (r *revenueTestRepo) GetRevenueFact(ctx context.Context, id string) (RevenueFact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readsFail != nil {
		return RevenueFact{}, r.readsFail
	}
	f, ok := r.facts[id]
	if !ok {
		return RevenueFact{}, ErrRevenueNotFound
	}
	return f, nil
}
func (r *revenueTestRepo) PendingRevenueFacts(ctx context.Context, consumer string, limit int) ([]RevenueFact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readsFail != nil {
		return nil, r.readsFail
	}
	out := []RevenueFact{}
	for id, f := range r.facts {
		if _, ok := r.acknowledgements[consumer+":"+id]; !ok {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *revenueTestRepo) AcknowledgeRevenueFact(ctx context.Context, a RevenueAcknowledgement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := a.ConsumerID + ":" + a.FactID
	if old, ok := r.acknowledgements[key]; ok {
		if old.AcceptanceID == a.AcceptanceID && old.Outcome == a.Outcome {
			return nil
		}
		return ErrRevenueConflict
	}
	r.acknowledgements[key] = a
	return nil
}
func revenueFixture(t *testing.T) (*RevenueService, *revenueTestRepo, VerifiedRevenueRequest) {
	t.Helper()
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	repo := newRevenueTestRepo()
	svc, err := NewRevenueService(repo, revenueTestClock{at.Add(time.Minute)})
	require.NoError(t, err)
	scope := RevenueScope{Provider: "fixture", AccountID: "account_fixture", LiveMode: false}
	fact := RevenueFact{Scope: scope, Kind: RevenuePayment, PaymentID: "payment_fixture", InvoiceID: "invoice_fixture", AllocationID: "line_fixture", PrincipalID: "payer_fixture", SubscriptionID: "subscription_fixture", PlanID: "plan_fixture", CostID: "cost_fixture", Currency: "EUR", CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: at}
	return svc, repo, VerifiedRevenueRequest{Scope: scope, EnvelopeID: "envelope_fixture", Facts: []RevenueFact{fact}}
}

func TestRevenueEconomicDeduplication(t *testing.T) {
	cases := []struct {
		name                string
		alter               func(*VerifiedRevenueRequest)
		want                error
		facts, observations int
	}{
		{"identical delivery replay", func(r *VerifiedRevenueRequest) {}, nil, 1, 1},
		{"distinct webhook same payment", func(r *VerifiedRevenueRequest) { r.EnvelopeID = "envelope_other" }, nil, 1, 2},
		{"distinct renewal", func(r *VerifiedRevenueRequest) {
			r.EnvelopeID = "renewal"
			r.Facts[0].PaymentID = "payment_renewal"
			r.Facts[0].InvoiceID = "invoice_renewal"
		}, nil, 2, 2},
		{"provider account isolation", func(r *VerifiedRevenueRequest) { r.Scope.AccountID = "other_account"; r.Facts[0].Scope = r.Scope }, nil, 2, 2},
		{"live test isolation", func(r *VerifiedRevenueRequest) { r.Scope.LiveMode = true; r.Facts[0].Scope = r.Scope }, nil, 2, 2},
		{"changed economics same envelope", func(r *VerifiedRevenueRequest) { r.Facts[0].PaidMinor = 9999 }, ErrRevenueConflict, 1, 1},
		{"changed economics another envelope", func(r *VerifiedRevenueRequest) { r.EnvelopeID = "another"; r.Facts[0].PaidMinor = 9999 }, ErrRevenueConflict, 1, 1},
		{"refund before payment fact", func(r *VerifiedRevenueRequest) {
			r.EnvelopeID = "refund"
			r.Facts[0].Kind = RevenueRefund
			r.Facts[0].AdjustmentID = "refund_fixture"
			r.Facts[0].CumulativeRefundedMinor = 2000
		}, nil, 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, req := revenueFixture(t)
			first, err := svc.AcceptVerified(context.Background(), req)
			require.NoError(t, err)
			req.Facts = append([]RevenueFact(nil), req.Facts...)
			tc.alter(&req)
			second, err := svc.AcceptVerified(context.Background(), req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
				if tc.observations == 1 {
					require.Equal(t, first, second)
				}
			}
			require.Len(t, repo.facts, tc.facts)
			require.Len(t, repo.observations, tc.observations)
		})
	}
}
func TestRevenueValidation(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*VerifiedRevenueRequest)
		want  error
	}{
		{"missing provider scope", func(r *VerifiedRevenueRequest) { r.Scope.AccountID = "" }, ErrRevenueInvalid},
		{"mismatched fact scope", func(r *VerifiedRevenueRequest) { r.Facts[0].Scope.LiveMode = true }, ErrRevenueInvalid},
		{"missing payer", func(r *VerifiedRevenueRequest) { r.Facts[0].PrincipalID = "" }, ErrRevenueInvalid},
		{"missing subscription", func(r *VerifiedRevenueRequest) { r.Facts[0].SubscriptionID = "" }, ErrRevenueInvalid},
		{"negative paid revenue", func(r *VerifiedRevenueRequest) { r.Facts[0].PaidMinor = -1 }, ErrRevenueInvalid},
		{"missing authoritative time", func(r *VerifiedRevenueRequest) { r.Facts[0].EffectiveAt = time.Time{} }, ErrRevenueInvalid},
		{"unsupported exponent", func(r *VerifiedRevenueRequest) { r.Facts[0].CurrencyExponent = 4 }, ErrRevenueInvalid},
		{"refunded beyond original", func(r *VerifiedRevenueRequest) {
			r.Facts[0].Kind = RevenueRefund
			r.Facts[0].AdjustmentID = "refund"
			r.Facts[0].CumulativeRefundedMinor = 10001
		}, ErrRevenueInvalid},
		{"duplicate allocations", func(r *VerifiedRevenueRequest) { r.Facts = append(r.Facts, r.Facts[0]) }, ErrRevenueInvalid},
		{"zero payment remains no entitlement input", func(r *VerifiedRevenueRequest) { r.Facts[0].PaidMinor = 0 }, nil},
		{"unassessable is durable quarantine", func(r *VerifiedRevenueRequest) { r.Facts = nil; r.QuarantineReason = "allocation_incomplete" }, nil},
		{"empty input not silent success", func(r *VerifiedRevenueRequest) { r.Facts = nil }, ErrRevenueInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, req := revenueFixture(t)
			tc.alter(&req)
			o, err := svc.AcceptVerified(context.Background(), req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, repo.facts)
				require.Empty(t, repo.observations)
				return
			}
			require.NoError(t, err)
			if req.QuarantineReason != "" {
				require.Equal(t, req.QuarantineReason, o.QuarantineReason)
				require.Empty(t, repo.facts)
				require.Len(t, repo.observations, 1)
			}
		})
	}
}
func TestRevenueAcceptanceAtomicity(t *testing.T) {
	cases := []struct {
		name      string
		failAt    int
		uncertain bool
		want      error
		committed bool
	}{
		{"first fact failure", 1, false, ErrRevenueUnavailable, false},
		{"observation after fact failure", 2, false, ErrRevenueUnavailable, false},
		{"lost commit result", 0, true, ErrRevenueUncertain, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, req := revenueFixture(t)
			repo.failAt = tc.failAt
			repo.uncertain = tc.uncertain
			_, err := svc.AcceptVerified(context.Background(), req)
			require.ErrorIs(t, err, tc.want)
			if tc.committed {
				require.Len(t, repo.facts, 1)
				require.Len(t, repo.observations, 1)
			} else {
				require.Empty(t, repo.facts)
				require.Empty(t, repo.observations)
			}
			repo.failAt = 0
			repo.uncertain = false
			_, err = svc.AcceptVerified(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, repo.facts, 1)
			require.Len(t, repo.observations, 1)
			require.EqualValues(t, 1, repo.sequence)
		})
	}
}
func TestRevenueFeedAcceptanceOrdering(t *testing.T) {
	// A single lifecycle proves independent acknowledgements cannot skip earlier
	// pending facts, and one consumer's progress cannot advance another's feed.
	svc, repo, req := revenueFixture(t)
	ctx := context.Background()
	o, err := svc.AcceptVerified(ctx, req)
	require.NoError(t, err)
	firstID := o.FactIDs[0]
	req.EnvelopeID = "later"
	req.Facts[0].PaymentID = "payment_later"
	o, err = svc.AcceptVerified(ctx, req)
	require.NoError(t, err)
	laterID := o.FactIDs[0]
	ack := RevenueAcknowledgement{ConsumerID: "partners", FactID: laterID, AcceptanceID: "durable-journal-entry", Outcome: "accepted", ActorID: "worker"}
	require.NoError(t, svc.AcknowledgeRevenueFact(ctx, ack))
	require.NoError(t, svc.AcknowledgeRevenueFact(ctx, ack))
	pending, err := svc.PendingRevenueFacts(ctx, "partners", 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, firstID, pending[0].ID)
	other, err := svc.PendingRevenueFacts(ctx, "other", 10)
	require.NoError(t, err)
	require.Len(t, other, 2)
	ack.AcceptanceID = "changed"
	require.ErrorIs(t, svc.AcknowledgeRevenueFact(ctx, ack), ErrRevenueConflict)
	require.Len(t, repo.acknowledgements, 1)
}
func TestRevenueCancellationAndUnavailable(t *testing.T) {
	cases := []struct {
		name     string
		canceled bool
		failure  error
		want     error
	}{
		{"cancellation before acceptance", true, nil, context.Canceled},
		{"outage is failure", false, ErrRevenueUnavailable, ErrRevenueUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, req := revenueFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.canceled {
				cancel()
			}
			repo.readsFail = tc.failure
			_, err := svc.AcceptVerified(ctx, req)
			require.True(t, errors.Is(err, tc.want))
			require.Empty(t, repo.observations)
		})
	}
}
