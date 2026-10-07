package partnerearnings

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testProgram  = "prg_1"
	testCurrency = "EUR"
)

// ---- fakes ----

// fakeRepo is a transactional in-memory Repository. WithTransaction serializes
// on a mutex (the host write guard) and commits a snapshot only when the
// callback succeeds, modelling rollback on error and re-check on retry.
type fakeRepo struct {
	mu              sync.Mutex
	entries         []Entry
	seq             map[string]int64
	claims          map[string]Claim
	receipts        map[string]Receipt
	maturitySources map[string]MaturitySource
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		seq:             map[string]int64{},
		claims:          map[string]Claim{},
		receipts:        map[string]Receipt{},
		maturitySources: map[string]MaturitySource{},
	}
}

func (f *fakeRepo) clone() *fakeRepo {
	c := &fakeRepo{
		entries:         append([]Entry(nil), f.entries...),
		seq:             map[string]int64{},
		claims:          map[string]Claim{},
		receipts:        map[string]Receipt{},
		maturitySources: map[string]MaturitySource{},
	}
	for k, v := range f.seq {
		c.seq[k] = v
	}
	for k, v := range f.claims {
		c.claims[k] = v
	}
	for k, v := range f.receipts {
		c.receipts[k] = v
	}
	for k, v := range f.maturitySources {
		c.maturitySources[k] = v
	}
	return c
}

func (f *fakeRepo) WithTransaction(ctx context.Context, programID, partnerID, currency string, fn func(Repository) error) error {
	f.mu.Lock()
	tx := f.clone()
	if err := fn(tx); err != nil {
		f.mu.Unlock()
		return err
	}
	f.entries = tx.entries
	f.seq = tx.seq
	f.claims = tx.claims
	f.receipts = tx.receipts
	f.maturitySources = tx.maturitySources
	f.mu.Unlock()
	return nil
}

func (f *fakeRepo) ListEntries(ctx context.Context, programID, partnerID string) ([]Entry, error) {
	out := make([]Entry, 0)
	for _, e := range f.entries {
		if e.PartnerID == partnerID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}

func (f *fakeRepo) AppendEntry(ctx context.Context, e Entry) error {
	for _, x := range f.entries {
		if x.PartnerID == e.PartnerID && x.Sequence == e.Sequence {
			return ErrConflict
		}
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeRepo) EntryBySource(ctx context.Context, programID, partnerID, kind, sourceEventID string) (Entry, error) {
	for _, e := range f.entries {
		if e.PartnerID == partnerID && e.Kind == kind && e.SourceEventID == sourceEventID {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

func (f *fakeRepo) NextSequence(ctx context.Context, programID, partnerID string) (int64, error) {
	f.seq[partnerID]++
	return f.seq[partnerID], nil
}

func (f *fakeRepo) GetClaim(ctx context.Context, programID, id string) (Claim, error) {
	if c, ok := f.claims[id]; ok {
		return c, nil
	}
	return Claim{}, ErrNotFound
}

func (f *fakeRepo) InsertClaim(ctx context.Context, c Claim) error {
	if _, exists := f.claims[c.ID]; exists {
		return ErrAlreadyExists
	}
	f.claims[c.ID] = c
	return nil
}

func (f *fakeRepo) ReplaceClaim(ctx context.Context, c Claim, expectedRevision int64) (Claim, error) {
	existing, ok := f.claims[c.ID]
	if !ok {
		return Claim{}, ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return Claim{}, ErrStaleWrite
	}
	f.claims[c.ID] = c
	return c, nil
}

func (f *fakeRepo) ListClaims(ctx context.Context, programID, partnerID string, states []string, limit int, afterID string) ([]Claim, error) {
	out := make([]Claim, 0)
	for _, c := range f.claims {
		if partnerID != "" && c.PartnerID != partnerID {
			continue
		}
		if len(states) > 0 && !containsString(states, c.State) {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt.Before(out[j].RequestedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRepo) GetReceipt(ctx context.Context, key ReceiptKey) (Receipt, error) {
	if r, ok := f.receipts[receiptKeyOf(key)]; ok {
		return r, nil
	}
	return Receipt{}, ErrNotFound
}

func (f *fakeRepo) PutReceipt(ctx context.Context, r Receipt) error {
	k := receiptKeyOf(ReceiptKey{
		ProgramID: r.ProgramID, PartnerID: r.PartnerID, ActorID: r.ActorID,
		UseCase: r.UseCase, Currency: r.Currency, Key: r.Key,
	})
	if _, exists := f.receipts[k]; exists {
		return ErrAlreadyExists
	}
	f.receipts[k] = r
	return nil
}

func receiptKeyOf(k ReceiptKey) string {
	return k.ProgramID + "|" + k.PartnerID + "|" + k.ActorID + "|" + k.UseCase + "|" + k.Currency + "|" + k.Key
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

type fixedClock struct{ t time.Time }

func (c *fixedClock) Now() time.Time { return c.t }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) NewID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return fmt.Sprintf("id-%04d", s.n)
}

func newTestService(t *testing.T) (*Service, *fakeRepo, *fixedClock) {
	t.Helper()
	repo := newFakeRepo()
	clock := &fixedClock{t: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(repo, clock, &seqIDs{}, Config{ProgramID: testProgram, Currency: testCurrency})
	require.NoError(t, err)
	return svc, repo, clock
}

func accrualReq(partner, payment string, paymentMinor int64, rate int) AccrualRequest {
	return AccrualRequest{
		PartnerID: partner, PaymentID: payment, PaymentMinor: paymentMinor,
		RateBasisPoints: rate, HoldDuration: 0, Currency: testCurrency,
		OccurredAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
	}
}

// ---- CommissionMinor ----

func TestCommissionMinor(t *testing.T) {
	tests := []struct {
		name    string
		amount  int64
		rate    int
		want    int64
		wantErr bool
	}{
		{name: "zero rate", amount: 10000, rate: 0, want: 0},
		{name: "zero amount", amount: 0, rate: 2000, want: 0},
		{name: "twenty percent", amount: 10000, rate: 2000, want: 2000},
		{name: "exact half up", amount: 15, rate: 1000, want: 2},
		{name: "half up boundary", amount: 5, rate: 1000, want: 1},
		{name: "below half rounds down", amount: 4, rate: 1000, want: 0},
		{name: "full rate", amount: 10000, rate: 10000, want: 10000},
		{name: "max int no overflow", amount: math.MaxInt64, rate: 10000, want: math.MaxInt64},
		{name: "rate too high", amount: 10000, rate: 10001, wantErr: true},
		{name: "rate negative", amount: 10000, rate: -1, wantErr: true},
		{name: "amount negative", amount: -1, rate: 2000, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CommissionMinor(tt.amount, tt.rate)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalid)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tt.want, got)
		})
	}
}

// ---- Accrue ----

func TestAccrueIdempotentAndConflicting(t *testing.T) {
	cases := []struct {
		name, payment, currency string
		amount                  int64
		want                    error
	}{
		{name: "exact_replay", payment: "pay_1", amount: 10000, currency: testCurrency},
		{name: "changed_amount_conflicts", payment: "pay_1", amount: 9999, currency: testCurrency, want: ErrConflict},
		{name: "wrong_currency_fails_closed", payment: "pay_2", amount: 10000, currency: "USD", want: ErrCurrencyMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			ctx := context.Background()
			first, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)
			require.EqualValues(t, 2000, first.AmountMinor)
			entries, err := svc.ListJournal(ctx, "prt_1")
			require.NoError(t, err)
			require.Len(t, entries, 2)
			require.Equal(t, EntryAccrued, entries[0].Kind)
			require.Equal(t, EntryMatured, entries[1].Kind)
			req := accrualReq("prt_1", tc.payment, tc.amount, 2000)
			req.Currency = tc.currency
			again, err := svc.Accrue(ctx, req)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, first.ID, again.ID)
			}
			entries, err = svc.ListJournal(ctx, "prt_1")
			require.NoError(t, err)
			require.Len(t, entries, 2, "replay or refusal must not append another financial operation")
		})
	}
}

func TestAccrueValidation(t *testing.T) {

	tests := []struct {
		name    string
		mutate  func(*AccrualRequest)
		wantErr error
	}{
		{name: "missing partner", mutate: func(r *AccrualRequest) { r.PartnerID = "" }, wantErr: ErrInvalid},
		{name: "missing payment", mutate: func(r *AccrualRequest) { r.PaymentID = "" }, wantErr: ErrInvalid},
		{name: "negative amount", mutate: func(r *AccrualRequest) { r.PaymentMinor = -5 }, wantErr: ErrInvalid},
		{name: "negative hold", mutate: func(r *AccrualRequest) { r.HoldDuration = -time.Hour }, wantErr: ErrInvalid},
		{name: "rate out of bounds", mutate: func(r *AccrualRequest) { r.RateBasisPoints = 10001 }, wantErr: ErrInvalid},
		{name: "missing occurred at", mutate: func(r *AccrualRequest) { r.OccurredAt = time.Time{} }, wantErr: ErrInvalid},
		{name: "currency mismatch", mutate: func(r *AccrualRequest) { r.Currency = "USD" }, wantErr: ErrCurrencyMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			ctx := context.Background()
			req := accrualReq("prt_1", "pay_"+tt.name, 10000, 2000)
			tt.mutate(&req)
			_, err := svc.Accrue(ctx, req)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestMaturity(t *testing.T) {
	cases := []struct {
		name string
		days int
	}{{"seven_day_hold", 7}, {"fourteen_day_hold", 14}, {"twenty_eight_day_hold", 28}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()
			req := accrualReq("prt_1", "pay_1", 10000, 2000)
			req.HoldDuration = time.Duration(tc.days) * 24 * time.Hour
			req.OccurredAt = clock.Now()
			_, err := svc.Accrue(ctx, req)
			require.NoError(t, err)
			b, err := svc.Balances(ctx, "prt_1")
			require.NoError(t, err)
			require.EqualValues(t, 2000, b.PendingMinor)
			require.Zero(t, b.MatchedMinor)
			require.Zero(t, b.AvailableMinor)
			matured, err := svc.Mature(ctx, "prt_1")
			require.NoError(t, err)
			require.Empty(t, matured)
			clock.t = req.OccurredAt.Add(req.HoldDuration - time.Nanosecond)
			matured, err = svc.Mature(ctx, "prt_1")
			require.NoError(t, err)
			require.Empty(t, matured, "one nanosecond before the boundary remains pending")
			clock.t = clock.t.Add(time.Nanosecond)
			matured, err = svc.Mature(ctx, "prt_1")
			require.NoError(t, err)
			require.Len(t, matured, 1)
			require.Equal(t, EntryMatured, matured[0].Kind)
			require.EqualValues(t, 2000, matured[0].AmountMinor)
			b, err = svc.Balances(ctx, "prt_1")
			require.NoError(t, err)
			require.Zero(t, b.PendingMinor)
			require.EqualValues(t, 2000, b.MatchedMinor)
			again, err := svc.Mature(ctx, "prt_1")
			require.NoError(t, err)
			require.Empty(t, again)
			b, err = svc.Balances(ctx, "prt_1")
			require.NoError(t, err)
			require.EqualValues(t, 2000, b.MatchedMinor)
		})
	}
}

// ---- Reverse ----

// TestReverse covers refund reversal replay, conflict, boundary and rounding
// scenarios as named cases against a fresh service each.
func TestReverse(t *testing.T) {
	minor := func(v int64) *int64 { return &v }

	type refundStep struct {
		id         string // explicit refund identity: replay reuses it, split evidence does not
		cumulative int64
		wantAmount *int64 // expected single-entry reversal amount on success
		wantErr    error
	}
	tests := []struct {
		name                string
		accrue              bool
		seedPaymentMinor    int64
		seedRate            int
		refunds             []refundStep
		wantReversedEntries int // journal-wide EntryReversed count; -1 skips
		wantMatchedMinor    *int64
		wantAvailableMinor  *int64
	}{
		{
			name:   "half refund reverses half the commission",
			accrue: true, seedRate: 2000,
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 5000, wantAmount: minor(-1000)},
			},
			wantReversedEntries: -1,
			wantMatchedMinor:    minor(1000),
			wantAvailableMinor:  minor(1000),
		},
		{
			name:   "duplicate refund replays original and adds nothing",
			accrue: true, seedRate: 2000,
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 5000, wantAmount: minor(-1000)},
				{id: "rfd_1", cumulative: 5000, wantAmount: minor(-1000)},
			},
			wantReversedEntries: 1,
			wantMatchedMinor:    minor(1000),
			wantAvailableMinor:  minor(1000),
		},
		{
			name:   "changed cumulative under same refund id conflicts",
			accrue: true, seedRate: 2000,
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 5000, wantAmount: minor(-1000)},
				{id: "rfd_1", cumulative: 6000, wantErr: ErrConflict},
			},
			wantReversedEntries: -1,
			wantMatchedMinor:    minor(1000),
			wantAvailableMinor:  minor(1000),
		},
		{
			name:   "late lower cumulative is a no-debit observation",
			accrue: true, seedRate: 2000,
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 5000, wantAmount: minor(-1000)},
				{id: "rfd_2", cumulative: 4000, wantAmount: minor(0)}, // never reverses extra money
			},
			wantReversedEntries: -1,
			wantMatchedMinor:    minor(1000),
			wantAvailableMinor:  minor(1000),
		},
		{
			name:   "cumulative refund beyond payment is invalid",
			accrue: true, seedRate: 2000,
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 10001, wantErr: ErrInvalid},
			},
			wantReversedEntries: -1,
		},
		{
			name:   "split refunds match one cumulative reversal",
			accrue: true, seedRate: 2000,
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 3000, wantAmount: minor(-600)},
				{id: "rfd_2", cumulative: 5000, wantAmount: minor(-400)}, // cumulative 1000 minus prior 600
			},
			wantReversedEntries: -1,
			wantMatchedMinor:    minor(1000),
		},
		{
			name: "refund before accrual is unresolved",
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 5000, wantErr: ErrUnresolved},
			},
			wantReversedEntries: -1,
		},
		{
			name:             "zero rounding refund anchors replay and conflicts",
			accrue:           true,
			seedPaymentMinor: 1000000,
			seedRate:         100,
			refunds: []refundStep{
				{id: "rfd_1", cumulative: 1, wantAmount: minor(0)}, // zero-rounding delta still journals an anchor
				{id: "rfd_1", cumulative: 1, wantAmount: minor(0)}, // replay returns the anchor
				{id: "rfd_1", cumulative: 2, wantErr: ErrConflict},
			},
			wantReversedEntries: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()

			if tt.accrue {
				paymentMinor := int64(10000)
				if tt.seedPaymentMinor != 0 {
					paymentMinor = tt.seedPaymentMinor
				}
				_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", paymentMinor, tt.seedRate))
				require.NoError(t, err)
			}

			for _, step := range tt.refunds {
				revs, err := svc.Reverse(ctx, ReversalRequest{
					PartnerID: "prt_1", RefundID: step.id, PaymentID: "pay_1",
					CumulativeRefundedMinor: step.cumulative, Currency: testCurrency, OccurredAt: clock.Now(),
				})
				if step.wantErr != nil {
					require.ErrorIs(t, err, step.wantErr)
					continue
				}
				require.NoError(t, err)
				require.Len(t, revs, 1)
				require.Equal(t, EntryReversed, revs[0].Kind)
				require.EqualValues(t, *step.wantAmount, revs[0].AmountMinor)
			}

			if tt.wantReversedEntries >= 0 {
				entries, err := svc.ListJournal(ctx, "prt_1")
				require.NoError(t, err)
				reversedCount := 0
				for _, e := range entries {
					if e.Kind == EntryReversed {
						reversedCount++
					}
				}
				require.Equal(t, tt.wantReversedEntries, reversedCount, "replay adds no new reversal")
			}
			if tt.wantMatchedMinor != nil || tt.wantAvailableMinor != nil {
				b, err := svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				if tt.wantMatchedMinor != nil {
					require.EqualValues(t, *tt.wantMatchedMinor, b.MatchedMinor)
				}
				if tt.wantAvailableMinor != nil {
					require.EqualValues(t, *tt.wantAvailableMinor, b.AvailableMinor)
				}
			}
		})
	}
}

// TestReverseClaimInteraction covers how a full refund interacts with an open
// claim, as named cases over the claim's state before the reversal.
func TestReverseClaimInteraction(t *testing.T) {
	tests := []struct {
		name            string
		processing      bool // move the claim to processing before the refund
		wantState       string
		wantReservation int64
	}{
		{name: "requested claim is cancelled and reservation released", wantState: ClaimCancelled, wantReservation: 0},
		{name: "processing claim keeps reservation and is flagged", processing: true, wantState: ClaimProcessing, wantReservation: 1500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()

			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)
			claim, err := svc.RequestClaim(ctx, ClaimRequest{
				ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
				DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
			})
			require.NoError(t, err)
			if tt.processing {
				_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1"})
				require.NoError(t, err)
			}

			_, err = svc.Reverse(ctx, ReversalRequest{
				PartnerID: "prt_1", RefundID: "rfd_1", PaymentID: "pay_1",
				CumulativeRefundedMinor: 10000, Currency: testCurrency, OccurredAt: clock.Now(),
			})
			require.NoError(t, err)

			got, err := svc.GetClaim(ctx, claim.ID)
			require.NoError(t, err)
			require.Equal(t, tt.wantState, got.State)
			if tt.processing {
				require.Equal(t, "reversed", got.ReviewReason)
			} else {
				require.Equal(t, "reversed", got.Reason)
			}

			b, _ := svc.Balances(ctx, "prt_1")
			require.EqualValues(t, tt.wantReservation, b.ReservedMinor)
		})
	}
}

// ---- Claims ----

// One lifecycle follows the same reservation and processing assignment through
// denied takeover, settlement and terminal denial; splitting its steps would
// lose the asserted continuity of that obligation and recorder attribution.
func TestClaimLifecycle(t *testing.T) {
	svc, _, clock := newTestService(t)
	ctx := context.Background()

	_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
	require.NoError(t, err)

	claim, err := svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
		DestinationID: "dst_1", DestinationSnapshot: map[string]string{"method": "paypal"},
		IdempotencyKey: "claim-key-1",
	})
	require.NoError(t, err)
	require.Equal(t, ClaimRequested, claim.State)

	b, _ := svc.Balances(ctx, "prt_1")
	require.EqualValues(t, 1500, b.ReservedMinor)
	require.EqualValues(t, 500, b.AvailableMinor)

	// Operator takes the claim.
	processing, err := svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1"})
	require.NoError(t, err)
	require.Equal(t, ClaimProcessing, processing.State)
	require.Equal(t, "admin_1", processing.ProcessingActor)

	// Another actor may not take it.
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_2"})
	require.ErrorIs(t, err, ErrConflict)

	// DecideClaim may never reach paid.
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimPaid, ActorID: "admin_1"})
	require.ErrorIs(t, err, ErrInvalidState)

	// Only RecordPayment pays.
	paid, err := svc.RecordPayment(ctx, RecordPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, AmountMinor: fixtureClaim(t, svc, ctx, claim.ID).AmountMinor, Currency: testCurrency, State: PaymentStateFull,
		ClaimID: claim.ID, Method: "paypal", Reference: "PP-9F2K",
		PaidAt: clock.Now(), ActorID: "admin_1", IdempotencyKey: "pay-key-1",
	})
	require.NoError(t, err)
	require.Equal(t, ClaimPaid, paid.State)
	require.Equal(t, "PP-9F2K", paid.Payment.Reference)
	require.Equal(t, "admin_1", paid.Payment.RecordedBy)
	require.Equal(t, clock.Now(), paid.Payment.RecordedAt)

	b, _ = svc.Balances(ctx, "prt_1")
	require.EqualValues(t, 0, b.ReservedMinor)
	require.EqualValues(t, 1500, b.PaidOutMinor)
	require.EqualValues(t, 500, b.AvailableMinor)

	// A terminal claim cannot transition further.
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimCancelled, ActorID: "admin_1"})
	require.ErrorIs(t, err, ErrInvalidState)
}

// TestClaimRequestValidationAndIdempotency covers request-claim denial,
// replay and conflict scenarios as named cases against a fresh service each
// (2,000 available after the standard accrual).
func TestClaimRequestValidationAndIdempotency(t *testing.T) {
	tests := []struct {
		name      string
		amount    int64
		key       string
		wantErr   error
		wantState string // expected state on success
	}{
		{name: "amount above available is insufficient", amount: 2001, key: "claim-key-1", wantErr: ErrInsufficient},
		{name: "first claim reserves funds", amount: 1500, key: "claim-key-1", wantState: ClaimRequested},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			ctx := context.Background()

			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)

			req := ClaimRequest{
				ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: tt.amount, Currency: testCurrency,
				DestinationID: "dst_1", IdempotencyKey: tt.key,
			}
			first, err := svc.RequestClaim(ctx, req)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantState, first.State)
		})
	}

	t.Run("same identity replays original claim and changed payload conflicts", func(t *testing.T) {
		svc, _, _ := newTestService(t)
		ctx := context.Background()

		_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
		require.NoError(t, err)

		req := ClaimRequest{
			ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
			DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
		}
		first, err := svc.RequestClaim(ctx, req)
		require.NoError(t, err)

		// Same identity replays the original claim.
		again, err := svc.RequestClaim(ctx, req)
		require.NoError(t, err)
		require.Equal(t, first.ID, again.ID)

		// Changed payload conflicts.
		changed := req
		changed.AmountMinor = 1600
		_, err = svc.RequestClaim(ctx, changed)
		require.ErrorIs(t, err, ErrConflict)
	})
}

func TestClaimOldestFirstAllocation(t *testing.T) {
	cases := []struct {
		name   string
		amount int64
		want   []int64
	}{
		{"within_oldest_credit", 300, []int64{300}},
		{"exact_oldest_credit", 500, []int64{500}},
		{"partial_newer_credit", 1200, []int64{500, 700}},
		{"all_available_credit", 2000, []int64{500, 1500}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			ctx := context.Background()
			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 2500, 2000))
			require.NoError(t, err)
			_, err = svc.Accrue(ctx, accrualReq("prt_1", "pay_2", 7500, 2000))
			require.NoError(t, err)
			_, err = svc.RequestClaim(ctx, ClaimRequest{ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: tc.amount, Currency: testCurrency, DestinationID: "dst_1", IdempotencyKey: "claim-key-1"})
			require.NoError(t, err)
			entries, err := svc.ListJournal(ctx, "prt_1")
			require.NoError(t, err)
			var allocs []Entry
			for _, e := range entries {
				if e.Kind == EntryAllocated {
					allocs = append(allocs, e)
				}
			}
			require.Len(t, allocs, len(tc.want))
			for i, amount := range tc.want {
				require.Equal(t, fmt.Sprintf("pay_%d", i+1), allocs[i].SourceRef, "oldest credit first")
				require.EqualValues(t, amount, allocs[i].AmountMinor)
			}
		})
	}
}

// ---- Payment ----

// One receipt recovery sequence inspects the original settlement, exact replay
// and changed-reference refusal against the same paid claim and single debit.
// Metadata variants are independently table-tested in RecordPaymentValidation.
func TestRecordPaymentIdempotency(t *testing.T) {
	svc, _, clock := newTestService(t)
	ctx := context.Background()

	_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
	require.NoError(t, err)
	claim, err := svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
		DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
	})
	require.NoError(t, err)
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1"})
	require.NoError(t, err)

	req := RecordPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, AmountMinor: fixtureClaim(t, svc, ctx, claim.ID).AmountMinor, Currency: testCurrency, State: PaymentStateFull,
		ClaimID: claim.ID, Method: "paypal", Reference: "PP-9F2K",
		PaidAt: clock.Now(), ActorID: "admin_1", IdempotencyKey: "pay-key-1",
	}
	first, err := svc.RecordPayment(ctx, req)
	require.NoError(t, err)
	require.Equal(t, ClaimPaid, first.State)

	// Same identity replays the original payment, no second debit.
	again, err := svc.RecordPayment(ctx, req)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)

	entries, _ := svc.ListJournal(ctx, "prt_1")
	paidCount := 0
	for _, e := range entries {
		if e.Kind == EntryPaid {
			paidCount++
		}
	}
	require.Equal(t, 1, paidCount)

	// Changed payload conflicts.
	changed := req
	changed.Reference = "PP-OTHER"
	_, err = svc.RecordPayment(ctx, changed)
	require.ErrorIs(t, err, ErrConflict)
}

func TestRecordPaymentValidation(t *testing.T) {
	clock := &fixedClock{t: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name    string
		mutate  func(*RecordPaymentRequest)
		wantErr error
	}{
		{name: "empty method", mutate: func(r *RecordPaymentRequest) { r.Method = "  " }, wantErr: ErrInvalid},
		{name: "empty reference", mutate: func(r *RecordPaymentRequest) { r.Reference = "" }, wantErr: ErrInvalid},
		{name: "control char in method", mutate: func(r *RecordPaymentRequest) { r.Method = "pay\npal" }, wantErr: ErrInvalid},
		{name: "future paid at", mutate: func(r *RecordPaymentRequest) { r.PaidAt = clock.Now().Add(time.Hour) }, wantErr: ErrInvalid},
		{name: "zero paid at", mutate: func(r *RecordPaymentRequest) { r.PaidAt = time.Time{} }, wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()

			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)
			claim, err := svc.RequestClaim(ctx, ClaimRequest{
				ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
				DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
			})
			require.NoError(t, err)
			_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1"})
			require.NoError(t, err)

			req := RecordPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, AmountMinor: fixtureClaim(t, svc, ctx, claim.ID).AmountMinor, Currency: testCurrency, State: PaymentStateFull,
				ClaimID: claim.ID, Method: "paypal", Reference: "PP-1",
				PaidAt: clock.Now(), ActorID: "admin_1", IdempotencyKey: "k-" + tt.name,
			}
			tt.mutate(&req)
			_, err = svc.RecordPayment(ctx, req)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRecordPaymentRequiresProcessing(t *testing.T) {
	cases := []struct {
		name, state string
		want        error
	}{
		{"requested_claim_cannot_settle", ClaimRequested, ErrInvalidState},
		{"cancelled_claim_cannot_settle", ClaimCancelled, ErrInvalidState},
		{"rejected_claim_cannot_settle", ClaimRejected, ErrInvalidState},
		{"assigned_processing_claim_can_settle", ClaimProcessing, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()
			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)
			claim, err := svc.RequestClaim(ctx, ClaimRequest{ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency, DestinationID: "dst_1", IdempotencyKey: "claim-key-1"})
			require.NoError(t, err)
			if tc.state != ClaimRequested {
				claim, err = svc.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, NewState: tc.state, ActorID: "admin_1", Reason: "fixture", ExpectedRevision: claim.Revision})
				require.NoError(t, err)
			}
			paid, err := svc.RecordPayment(ctx, RecordPaymentRequest{ExpectedRevision: claim.Revision, AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull, ClaimID: claim.ID, Method: "paypal", Reference: "PP-1", PaidAt: clock.Now(), ActorID: "admin_1", IdempotencyKey: "pay-key-1"})
			require.ErrorIs(t, err, tc.want)
			current, err := svc.GetClaim(ctx, claim.ID)
			require.NoError(t, err)
			if tc.want != nil {
				require.Equal(t, claim, current)
			} else {
				require.Equal(t, ClaimPaid, paid.State)
				require.Equal(t, ClaimPaid, current.State)
			}
		})
	}
}

// One amendment lifecycle verifies required reasoning and a valid correction
// against the same paid claim, retaining the settlement and one debit. Receipt
// replay/conflict variants are separately table-tested in financial boundaries.
func TestAmendPayment(t *testing.T) {
	svc, _, clock := newTestService(t)
	ctx := context.Background()

	_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
	require.NoError(t, err)
	claim, err := svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
		DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
	})
	require.NoError(t, err)
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1"})
	require.NoError(t, err)
	original, err := svc.RecordPayment(ctx, RecordPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, AmountMinor: fixtureClaim(t, svc, ctx, claim.ID).AmountMinor, Currency: testCurrency, State: PaymentStateFull,
		ClaimID: claim.ID, Method: "paypal", Reference: "PP-1",
		PaidAt: clock.Now(), ActorID: "admin_1", IdempotencyKey: "pay-key-1",
	})
	require.NoError(t, err)

	// No reason is rejected.
	_, err = svc.AmendPayment(ctx, AmendPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, IdempotencyKey: "amend-" + claim.ID,
		ClaimID: claim.ID, Method: "bank", Reference: "IBAN-1", PaidAt: clock.Now(), ActorID: "admin_1",
	})
	require.ErrorIs(t, err, ErrInvalid)

	amended, err := svc.AmendPayment(ctx, AmendPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, IdempotencyKey: "amend-" + claim.ID,
		ClaimID: claim.ID, Method: "bank", Reference: "IBAN-1", PaidAt: clock.Now(),
		Reason: "typo", ActorID: "admin_1",
	})
	require.NoError(t, err)
	require.Len(t, amended.PaymentAmendments, 1)
	require.Equal(t, "bank", amended.PaymentAmendments[0].Method)
	require.Equal(t, original.Payment, amended.Payment, "amendment preserves original payment evidence")

	// No second debit.
	entries, _ := svc.ListJournal(ctx, "prt_1")
	paidCount := 0
	for _, e := range entries {
		if e.Kind == EntryPaid {
			paidCount++
		}
	}
	require.Equal(t, 1, paidCount)
}

// ---- DecideClaim transitions ----

func TestDecideClaimTransitions(t *testing.T) {
	tests := []struct {
		name        string
		from        string
		to          string
		confirmed   bool
		reason      string
		wantState   string
		wantRelease bool
		wantErr     error
	}{
		{name: "requested to processing", from: ClaimRequested, to: ClaimProcessing, wantState: ClaimProcessing},
		{name: "requested to review needs reason", from: ClaimRequested, to: ClaimNeedsReview, wantErr: ErrInvalid},
		{name: "requested to review", from: ClaimRequested, to: ClaimNeedsReview, reason: "check", wantState: ClaimNeedsReview},
		{name: "requested to cancelled", from: ClaimRequested, to: ClaimCancelled, reason: "withdrawn", wantState: ClaimCancelled, wantRelease: true},
		{name: "requested to rejected", from: ClaimRequested, to: ClaimRejected, reason: "ineligible", wantState: ClaimRejected, wantRelease: true},
		{name: "requested to paid forbidden", from: ClaimRequested, to: ClaimPaid, wantErr: ErrInvalidState},
		{name: "processing to cancelled needs confirmed", from: ClaimProcessing, to: ClaimCancelled, reason: "x", wantErr: ErrDenied},
		{name: "processing to cancelled confirmed", from: ClaimProcessing, to: ClaimCancelled, confirmed: true, reason: "x", wantState: ClaimCancelled, wantRelease: true},
		{name: "processing to review", from: ClaimProcessing, to: ClaimNeedsReview, reason: "check", wantState: ClaimNeedsReview},
		{name: "needs review to processing", from: ClaimNeedsReview, to: ClaimProcessing, wantState: ClaimProcessing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			ctx := context.Background()

			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)
			claim, err := svc.RequestClaim(ctx, ClaimRequest{
				ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
				DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
			})
			require.NoError(t, err)

			if tt.from != ClaimRequested {
				_, err := svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: tt.from, ActorID: "admin_1", Reason: "seed"})
				require.NoError(t, err)
			}
			got, err := svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision,
				ClaimID: claim.ID, NewState: tt.to, ActorID: "admin_1",
				Reason: tt.reason, ConfirmedUnsent: tt.confirmed,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantState, got.State)
			if tt.wantRelease {
				b, _ := svc.Balances(ctx, "prt_1")
				require.EqualValues(t, 0, b.ReservedMinor)
			}
		})
	}
}

// ---- Balances and the documented fixture ----

// TestBalanceFixture reproduces the documented accounting fixture: 2,000
// commission on a 10,000 payment; claim and pay 1,500; a 50% refund reverses
// 1,000, leaving 500 debt; a later 800 credit recovers the debt and leaves 300
// available.
// This is the specified post-payout debt recovery fixture: the same obligation
// passes through payout, refund debt and later credit, leaving exactly 300. A
// separate case per stage would not prove recovery across that paid history.
func TestBalanceFixture(t *testing.T) {
	svc, _, clock := newTestService(t)
	ctx := context.Background()

	_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
	require.NoError(t, err)

	claim, err := svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
		DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
	})
	require.NoError(t, err)
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1"})
	require.NoError(t, err)
	_, err = svc.RecordPayment(ctx, RecordPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, AmountMinor: fixtureClaim(t, svc, ctx, claim.ID).AmountMinor, Currency: testCurrency, State: PaymentStateFull,
		ClaimID: claim.ID, Method: "paypal", Reference: "PP-9F2K",
		PaidAt: clock.Now(), ActorID: "admin_1", IdempotencyKey: "pay-key-1",
	})
	require.NoError(t, err)

	_, err = svc.Reverse(ctx, ReversalRequest{
		PartnerID: "prt_1", RefundID: "rfd_1", PaymentID: "pay_1",
		CumulativeRefundedMinor: 5000, Currency: testCurrency, OccurredAt: clock.Now(),
	})
	require.NoError(t, err)

	b, _ := svc.Balances(ctx, "prt_1")
	require.EqualValues(t, -500, b.MatchedMinor, "2000 - 1500 paid - 1000 reversal")
	require.EqualValues(t, 500, b.DebtMinor)
	require.EqualValues(t, 0, b.AvailableMinor)

	_, err = svc.Accrue(ctx, accrualReq("prt_1", "pay_2", 4000, 2000))
	require.NoError(t, err)
	b, _ = svc.Balances(ctx, "prt_1")
	require.EqualValues(t, 300, b.MatchedMinor, "800 credit less 500 debt")
	require.EqualValues(t, 0, b.DebtMinor, "debt is max(0, -M)")
	require.EqualValues(t, 300, b.AvailableMinor)

	_, err = svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 301, Currency: testCurrency,
		DestinationID: "dst_1", IdempotencyKey: "claim-key-2",
	})
	require.ErrorIs(t, err, ErrInsufficient)
}

// ---- Concurrency ----

// TestConcurrentClaimsNoDoubleSpend is a standalone concurrency scenario: two
// concurrent claims against one credit pool must not both succeed when their
// total exceeds available funds.
func TestConcurrentClaimsNoDoubleSpend(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
	require.NoError(t, err)

	type result struct {
		claim Claim
		err   error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			c, err := svc.RequestClaim(ctx, ClaimRequest{
				ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
				DestinationID: "dst_1", IdempotencyKey: fmt.Sprintf("key-%d", i),
			})
			results <- result{claim: c, err: err}
		}(i)
	}

	ok, insufficient := 0, 0
	for i := 0; i < 2; i++ {
		r := <-results
		switch {
		case r.err == nil:
			ok++
		case errors.Is(r.err, ErrInsufficient):
			insufficient++
		default:
			require.NoError(t, r.err)
		}
	}
	require.Equal(t, 1, ok, "exactly one claim succeeds")
	require.Equal(t, 1, insufficient, "the other is refused")

	b, _ := svc.Balances(ctx, "prt_1")
	require.EqualValues(t, 1500, b.ReservedMinor)
	require.EqualValues(t, 500, b.AvailableMinor)
}

// ---- Service wiring ----

func TestNewServiceValidation(t *testing.T) {
	cases := []struct {
		name          string
		noRepo, noIDs bool
		config        Config
		want          error
	}{
		{name: "missing_repo", noRepo: true, config: Config{ProgramID: "p", Currency: "USD"}, want: ErrUnavailable},
		{name: "missing_ids", noIDs: true, config: Config{ProgramID: "p", Currency: "USD"}, want: ErrUnavailable},
		{name: "missing_program", config: Config{Currency: "USD"}, want: ErrInvalid},
		{name: "missing_currency", config: Config{ProgramID: "p"}, want: ErrInvalid},
		{name: "lowercase_currency", config: Config{ProgramID: "p", Currency: "usd"}, want: ErrInvalid},
		{name: "invalid_currency_length", config: Config{ProgramID: "p", Currency: "US"}, want: ErrInvalid},
		{name: "complete_wiring", config: Config{ProgramID: "p", Currency: "USD"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var repo Repository = newFakeRepo()
			var ids IDGenerator = &seqIDs{}
			if tc.noRepo {
				repo = nil
			}
			if tc.noIDs {
				ids = nil
			}
			svc, err := NewService(repo, RealClock{}, ids, tc.config)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, svc)
			} else {
				require.Equal(t, "p", svc.ProgramID())
				require.Equal(t, "USD", svc.Currency())
			}
		})
	}
}

func TestNilContextFailsClosed(t *testing.T) {
	for _, operation := range []string{"balances", "journal", "claim", "accrual"} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancelled_%t", operation, cancelled), func(t *testing.T) {
				svc, _, _ := newTestService(t)
				var ctx context.Context
				want := ErrUnavailable
				if cancelled {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
					want = context.Canceled
				}
				var err error
				switch operation {
				case "balances":
					_, err = svc.Balances(ctx, "prt_1")
				case "journal":
					_, err = svc.ListJournal(ctx, "prt_1")
				case "claim":
					_, err = svc.GetClaim(ctx, "claim")
				case "accrual":
					_, err = svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
				}
				require.ErrorIs(t, err, want)
			})
		}
	}
}

// ---- Fingerprint and conflict coverage ----

func TestAccrueFingerprintConflicts(t *testing.T) {
	base := accrualReq("prt_1", "pay_1", 10000, 2000)
	base.HoldDuration = 24 * time.Hour
	base.ReferralID = "ref_1"
	base.TermsVersion = "v1"
	base.PolicyID = "pol_1"
	base.SourceKind = "invoice"

	tests := []struct {
		name   string
		mutate func(*AccrualRequest)
	}{
		{name: "amount", mutate: func(r *AccrualRequest) { r.PaymentMinor = 9999 }},
		{name: "rate", mutate: func(r *AccrualRequest) { r.RateBasisPoints = 1999 }},
		{name: "hold", mutate: func(r *AccrualRequest) { r.HoldDuration = 48 * time.Hour }},
		{name: "occurred at", mutate: func(r *AccrualRequest) { r.OccurredAt = r.OccurredAt.Add(time.Minute) }},
		{name: "referral", mutate: func(r *AccrualRequest) { r.ReferralID = "ref_2" }},
		{name: "terms", mutate: func(r *AccrualRequest) { r.TermsVersion = "v2" }},
		{name: "policy", mutate: func(r *AccrualRequest) { r.PolicyID = "pol_2" }},
		{name: "source kind", mutate: func(r *AccrualRequest) { r.SourceKind = "credit-note" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			ctx := context.Background()
			req := base
			req.PaymentID = "pay_" + tt.name
			_, err := svc.Accrue(ctx, req)
			require.NoError(t, err)
			tt.mutate(&req)
			_, err = svc.Accrue(ctx, req)
			require.ErrorIs(t, err, ErrConflict, "changed frozen field must conflict")
		})
	}
}

// One cross-claim receipt collision exercises two independently assigned
// claims in the same ledger. Both the conflicting replay and the second valid
// operator receipt must coexist to prove identity isolation, not just denial.
func TestRecordPaymentBindsClaimIdentity(t *testing.T) {
	svc, _, clock := newTestService(t)
	ctx := context.Background()

	_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
	require.NoError(t, err)
	claimA, err := svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
		DestinationID: "dst_1", IdempotencyKey: "claim-key-a",
	})
	require.NoError(t, err)
	claimB, err := svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 500, Currency: testCurrency,
		DestinationID: "dst_1", IdempotencyKey: "claim-key-b",
	})
	require.NoError(t, err)
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claimA.ID).Revision, ClaimID: claimA.ID, NewState: ClaimProcessing, ActorID: "admin_1"})
	require.NoError(t, err)
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claimB.ID).Revision, ClaimID: claimB.ID, NewState: ClaimProcessing, ActorID: "admin_2"})
	require.NoError(t, err)

	req := RecordPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claimA.ID).Revision, AmountMinor: fixtureClaim(t, svc, ctx, claimA.ID).AmountMinor, Currency: testCurrency, State: PaymentStateFull,
		ClaimID: claimA.ID, Method: "paypal", Reference: "PP-1",
		PaidAt: clock.Now(), ActorID: "admin_1", IdempotencyKey: "pay-key-1",
	}
	first, err := svc.RecordPayment(ctx, req)
	require.NoError(t, err)
	require.Equal(t, claimA.ID, first.ID)

	// The same identity replayed against a different claim must conflict, not
	// silently repay claim A.
	req.ClaimID = claimB.ID
	_, err = svc.RecordPayment(ctx, req)
	require.ErrorIs(t, err, ErrConflict)

	// A different actor with the same key is a different receipt identity.
	// That actor must also own the selected claim processing assignment.
	req2 := RecordPaymentRequest{ExpectedRevision: fixtureClaim(t, svc, ctx, claimB.ID).Revision, AmountMinor: fixtureClaim(t, svc, ctx, claimB.ID).AmountMinor, Currency: testCurrency, State: PaymentStateFull,
		ClaimID: claimB.ID, Method: "paypal", Reference: "PP-1",
		PaidAt: clock.Now(), ActorID: "admin_2", IdempotencyKey: "pay-key-1",
	}
	paid, err := svc.RecordPayment(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, claimB.ID, paid.ID)
}

func TestExpectedRevisionPreconditions(t *testing.T) {
	cases := []struct {
		name             string
		processing, paid bool
	}{
		{name: "decision"},
		{name: "payment", processing: true},
		{name: "amendment", processing: true, paid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()
			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)
			claim, err := svc.RequestClaim(ctx, ClaimRequest{ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency, DestinationID: "dst_1", IdempotencyKey: "claim-key-1"})
			require.NoError(t, err)
			if tc.processing {
				claim, err = svc.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1", ExpectedRevision: claim.Revision})
				require.NoError(t, err)
			}
			if tc.paid {
				claim, err = svc.RecordPayment(ctx, RecordPaymentRequest{ClaimID: claim.ID, ActorID: "admin_1", AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull, Method: "paypal", Reference: "PP-1", PaidAt: clock.Now(), IdempotencyKey: "pay-key-1", ExpectedRevision: claim.Revision})
				require.NoError(t, err)
			}
			switch tc.name {
			case "decision":
				_, err = svc.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1", ExpectedRevision: 99})
			case "payment":
				_, err = svc.RecordPayment(ctx, RecordPaymentRequest{ClaimID: claim.ID, ActorID: "admin_1", AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull, Method: "paypal", Reference: "PP-1", PaidAt: clock.Now(), IdempotencyKey: "pay-key-1", ExpectedRevision: 99})
			case "amendment":
				_, err = svc.AmendPayment(ctx, AmendPaymentRequest{ClaimID: claim.ID, ActorID: "admin_1", Method: "bank", Reference: "IBAN-1", PaidAt: clock.Now(), Reason: "typo", IdempotencyKey: "amend-" + claim.ID, ExpectedRevision: 99})
			}
			require.ErrorIs(t, err, ErrStaleWrite)
			current, err := svc.GetClaim(ctx, claim.ID)
			require.NoError(t, err)
			require.Equal(t, claim, current, "stale write retains the complete original claim")
			if tc.name == "decision" {
				_, err = svc.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "admin_1", ExpectedRevision: claim.Revision})
				require.NoError(t, err)
			}
		})
	}
}

func TestActorRequired(t *testing.T) {
	cases := []struct {
		name    string
		payment bool
	}{{name: "claim_decision"}, {name: "manual_payment", payment: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()
			_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
			require.NoError(t, err)
			claim, err := svc.RequestClaim(ctx, ClaimRequest{ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency, DestinationID: "dst_1", IdempotencyKey: "claim-key-1"})
			require.NoError(t, err)
			if tc.payment {
				_, err = svc.RecordPayment(ctx, RecordPaymentRequest{ExpectedRevision: claim.Revision, AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull, ClaimID: claim.ID, Method: "paypal", Reference: "PP-1", PaidAt: clock.Now(), ActorID: "", IdempotencyKey: "payment"})
			} else {
				_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: claim.Revision, ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: ""})
			}
			require.ErrorIs(t, err, ErrInvalid)
			current, err := svc.GetClaim(ctx, claim.ID)
			require.NoError(t, err)
			require.Equal(t, claim, current)
		})
	}
}

// One allocation/release lifecycle compares the requesting and releasing
// actors on the same reserved funds; isolated actor cases would lose the
// provenance relationship between those immutable journal rows.
func TestProvenanceRecorded(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Accrue(ctx, accrualReq("prt_1", "pay_1", 10000, 2000))
	require.NoError(t, err)
	claim, err := svc.RequestClaim(ctx, ClaimRequest{
		ActorID: "actor_1", PartnerID: "prt_1", AmountMinor: 1500, Currency: testCurrency,
		DestinationID: "dst_1", IdempotencyKey: "claim-key-1",
	})
	require.NoError(t, err)
	require.Equal(t, "actor_1", claim.RequestedBy)

	entries, err := svc.ListJournal(ctx, "prt_1")
	require.NoError(t, err)
	var allocated []Entry
	for _, e := range entries {
		if e.Kind == EntryAllocated {
			allocated = append(allocated, e)
		}
	}
	require.NotEmpty(t, allocated)
	for _, a := range allocated {
		require.Equal(t, "actor_1", a.ActorID, "allocation carries the requesting actor")
	}

	// Reject releases the allocation with the operator actor recorded.
	_, err = svc.DecideClaim(ctx, ClaimDecision{ExpectedRevision: fixtureClaim(t, svc, ctx, claim.ID).Revision, ClaimID: claim.ID, NewState: ClaimRejected, Reason: "bad", ActorID: "admin_1"})
	require.NoError(t, err)
	entries, _ = svc.ListJournal(ctx, "prt_1")
	var released []Entry
	for _, e := range entries {
		if e.Kind == EntryAllocationReleased {
			released = append(released, e)
		}
	}
	require.NotEmpty(t, released)
	for _, r := range released {
		require.Equal(t, "admin_1", r.ActorID, "release carries the releasing actor")
	}
}

func TestDeriveOverflow(t *testing.T) {
	cases := []struct {
		name          string
		first, second int64
		want          error
		matched       int64
	}{
		{"representable_boundary", math.MaxInt64 - 1, 1, nil, math.MaxInt64},
		{"overflow_by_one", math.MaxInt64, 1, ErrInvalid, 0},
		{"two_maximum_credits", math.MaxInt64, math.MaxInt64, ErrInvalid, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := []Entry{{Kind: EntryMatured, SourceEventID: "pay_1", AmountMinor: tc.first, Sequence: 1}, {Kind: EntryMatured, SourceEventID: "pay_2", AmountMinor: tc.second, Sequence: 2}}
			b, _, err := derive(entries, nil)
			require.ErrorIs(t, err, tc.want, "unrepresentable aggregate must fail closed, never wrap")
			if tc.want == nil {
				require.Equal(t, tc.matched, b.MatchedMinor)
			}
		})
	}
}

// retryOnceRepo runs the transaction callback twice, modelling an aborted write
// followed by a retry that commits, to prove driver callbacks build their
// result fresh rather than accumulating across attempts.
type retryOnceRepo struct{ *fakeRepo }

func (r *retryOnceRepo) WithTransaction(ctx context.Context, programID, partnerID, currency string, fn func(Repository) error) error {
	r.mu.Lock()
	discard := r.clone()
	if err := fn(discard); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	return r.fakeRepo.WithTransaction(ctx, programID, partnerID, currency, fn)
}

// One aborted transaction callback followed by its successful retry verifies
// both returned slice isolation and committed journal uniqueness together.
// The repeated callback, rather than an input variant, is the behavior tested.
func TestMatureRetryDoesNotDuplicate(t *testing.T) {
	base := newFakeRepo()
	repo := &retryOnceRepo{fakeRepo: base}
	clock := &fixedClock{t: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(repo, clock, &seqIDs{}, Config{ProgramID: testProgram, Currency: testCurrency})
	require.NoError(t, err)
	ctx := context.Background()

	req := accrualReq("prt_1", "pay_1", 10000, 2000)
	req.HoldDuration = 24 * time.Hour
	req.OccurredAt = clock.Now()
	_, err = svc.Accrue(ctx, req)
	require.NoError(t, err)

	clock.t = clock.t.Add(25 * time.Hour)
	matured, err := svc.Mature(ctx, "prt_1")
	require.NoError(t, err)
	require.Len(t, matured, 1, "aborted attempt must not leak into the retry result")

	entries, _ := svc.ListJournal(ctx, "prt_1")
	maturedCount := 0
	for _, e := range entries {
		if e.Kind == EntryMatured {
			maturedCount++
		}
	}
	require.Equal(t, 1, maturedCount, "retry must not duplicate the matured entry")
}

// fixtureClaim captures the revision and frozen obligation a test operator saw.
// Requests retain that captured precondition on retry; validation tests supply
// explicit zero/stale revisions directly rather than relying on this fixture.
func fixtureClaim(t *testing.T, svc *Service, ctx context.Context, id string) Claim {
	t.Helper()
	c, err := svc.GetClaim(ctx, id)
	require.NoError(t, err)
	return c
}
