package partnerearnings

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (f *fakeRepo) GetMaturitySource(_ context.Context, program, id string) (MaturitySource, error) {
	v, ok := f.maturitySources[id]
	if !ok || v.ProgramID != program {
		return MaturitySource{}, ErrNotFound
	}
	return v, nil
}
func (f *fakeRepo) InsertMaturitySource(_ context.Context, v MaturitySource) error {
	if _, exists := f.maturitySources[v.ID]; exists {
		return ErrAlreadyExists
	}
	f.maturitySources[v.ID] = v
	return nil
}
func (f *fakeRepo) ReplaceMaturitySource(_ context.Context, v MaturitySource, expected int64) error {
	old, exists := f.maturitySources[v.ID]
	if !exists {
		return ErrNotFound
	}
	if old.Revision != expected || old.Fingerprint != v.Fingerprint {
		return ErrConflict
	}
	f.maturitySources[v.ID] = v
	return nil
}
func (f *fakeRepo) PendingMaturitySourcesAfter(_ context.Context, program, currency, after string, limit int) ([]MaturitySource, error) {
	var out []MaturitySource
	for _, source := range f.maturitySources {
		if source.ProgramID == program && source.Currency == currency && source.State == MaturityPending && source.ID > after {
			out = append(out, source)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Audit disposition: named owning accrual/refund/dispute/maturity journeys use
// fresh clocks/repos and assert durable source state versus actual journal proof.
func TestMaturitySourcesRetainAcceptedFinancialProof(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
	}{
		{name: "scheduled_accrual_survives_until_original_deadline"},
		{name: "hold_free_accrual_is_born_completed", mode: "immediate"},
		{name: "zero_commission_still_has_maturity_work", mode: "zero"},
		{name: "fully_refunded_source_keeps_original_proof", mode: "refund"},
		{name: "dispute_hold_does_not_release_on_maturity", mode: "held"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			req := accrualReq("partner", "payment", 5000, 2000)
			req.OccurredAt, req.HoldDuration = clock.Now(), time.Hour
			if tc.mode == "immediate" {
				req.HoldDuration = 0
			}
			if tc.mode == "zero" {
				req.RateBasisPoints = 0
			}
			accrued, err := s.Accrue(ctx, req)
			require.NoError(t, err)
			id := maturityID(testProgram, testCurrency, "partner", "payment")
			source, err := s.GetMaturitySource(ctx, id)
			require.NoError(t, err)
			require.NoError(t, source.Validate())
			require.Equal(t, accrued.ID, source.AccruedEntryID)
			require.Equal(t, accrued.Fingerprint, source.AccruedFingerprint)
			require.Equal(t, accrued.AmountMinor, source.AccruedAmountMinor)
			require.Equal(t, *accrued.AvailableAt, source.AvailableAt)
			encoded, err := json.Marshal(source)
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(encoded))
			page, err := s.PendingMaturitySourcesAfter(ctx, "", 1)
			require.NoError(t, err)
			if tc.mode == "immediate" {
				require.Empty(t, page)
				require.Equal(t, MaturityCompleted, source.State)
				require.EqualValues(t, 2, source.Revision)
			} else {
				require.Equal(t, []MaturitySource{source}, page)
				require.Equal(t, MaturityPending, source.State)
				before, err := s.Mature(ctx, "partner")
				require.NoError(t, err)
				require.Empty(t, before)
			}
			switch tc.mode {
			case "refund":
				_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: "payment", RefundID: "refund", CumulativeRefundedMinor: 5000, Currency: testCurrency, OccurredAt: clock.Now()})
				require.NoError(t, err)
			case "held":
				_, err = s.Dispute(ctx, DisputeRequest{PartnerID: "partner", PaymentID: "payment", DisputeID: "dispute", OperationID: "hold", Action: "hold", ActorID: "worker", Currency: testCurrency, OccurredAt: clock.Now(), Reason: "verified"})
				require.NoError(t, err)
			}
			clock.t = source.AvailableAt
			matured, err := s.Mature(ctx, "partner")
			require.NoError(t, err)
			if tc.mode == "immediate" {
				require.Empty(t, matured)
			} else {
				require.Len(t, matured, 1)
				require.Equal(t, accrued.AmountMinor, matured[0].AmountMinor)
			}
			completed, err := s.GetMaturitySource(ctx, id)
			require.NoError(t, err)
			require.Equal(t, MaturityCompleted, completed.State)
			require.EqualValues(t, 2, completed.Revision)
			require.Equal(t, source.Fingerprint, completed.Fingerprint)
			require.Equal(t, source.AvailableAt, completed.AvailableAt)
			proof, err := repo.EntryBySource(ctx, testProgram, "partner", EntryMatured, "payment")
			require.NoError(t, err)
			require.Equal(t, proof.ID, completed.MaturedEntryID)
			require.Equal(t, proof.CreatedAt, completed.MaturedAt)
			page, err = s.PendingMaturitySourcesAfter(ctx, "", 200)
			require.NoError(t, err)
			require.Empty(t, page)
			before := append([]Entry(nil), repo.entries...)
			replay, err := s.Accrue(ctx, req)
			require.NoError(t, err)
			require.Equal(t, accrued, replay)
			again, err := s.Mature(ctx, "partner")
			require.NoError(t, err)
			require.Empty(t, again)
			require.Equal(t, before, repo.entries)
			balances, err := s.Balances(ctx, "partner")
			require.NoError(t, err)
			if tc.mode == "held" {
				require.EqualValues(t, 1000, balances.DisputeHoldMinor)
				require.Zero(t, balances.AvailableMinor)
			}
			if tc.mode == "refund" || tc.mode == "zero" {
				require.Zero(t, balances.AvailableMinor)
			}
		})
	}
}

type maturityFaultRepo struct {
	Repository
	insertErr, replaceErr error
}

func (r *maturityFaultRepo) WithTransaction(ctx context.Context, program, partner, currency string, fn func(Repository) error) error {
	return r.Repository.WithTransaction(ctx, program, partner, currency, func(tx Repository) error {
		return fn(&maturityFaultRepo{Repository: tx, insertErr: r.insertErr, replaceErr: r.replaceErr})
	})
}
func (r *maturityFaultRepo) InsertMaturitySource(ctx context.Context, source MaturitySource) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	return r.Repository.InsertMaturitySource(ctx, source)
}
func (r *maturityFaultRepo) ReplaceMaturitySource(ctx context.Context, source MaturitySource, expected int64) error {
	if r.replaceErr != nil {
		return r.replaceErr
	}
	return r.Repository.ReplaceMaturitySource(ctx, source, expected)
}

func TestMaturitySourceFailuresRollBackFinancialWrites(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
	}{
		{name: "source_insert_rolls_back_accrual", mode: "insert"},
		{name: "immediate_completion_rolls_back_both_journal_entries", mode: "immediate"},
		{name: "scheduled_completion_rolls_back_maturity", mode: "mature"},
		{name: "later_missing_source_rolls_back_earlier_completion", mode: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			req := accrualReq("partner", "payment", 5000, 2000)
			req.OccurredAt, req.HoldDuration = clock.Now(), time.Hour
			fault := &maturityFaultRepo{Repository: repo}
			if tc.mode == "insert" {
				fault.insertErr = ErrUnavailable
			} else {
				fault.replaceErr = ErrUnavailable
			}
			broken, err := NewService(fault, clock, s.ids, s.Config())
			require.NoError(t, err)
			if tc.mode == "insert" || tc.mode == "immediate" {
				if tc.mode == "immediate" {
					req.HoldDuration = 0
				}
				out, err := broken.Accrue(ctx, req)
				require.ErrorIs(t, err, ErrUnavailable)
				require.Empty(t, out)
				require.Empty(t, repo.entries)
				require.Empty(t, repo.maturitySources)
				require.Empty(t, repo.seq)
				return
			}
			_, err = s.Accrue(ctx, req)
			require.NoError(t, err)
			if tc.mode == "missing" {
				req.PaymentID = "second"
				_, err = s.Accrue(ctx, req)
				require.NoError(t, err)
				delete(repo.maturitySources, maturityID(testProgram, testCurrency, "partner", "second"))
				broken = s
			}
			clock.t = clock.Now().Add(time.Hour)
			entries := append([]Entry(nil), repo.entries...)
			sources := repo.clone().maturitySources
			out, err := broken.Mature(ctx, "partner")
			require.ErrorIs(t, err, ErrUnavailable)
			require.Empty(t, out)
			require.Equal(t, entries, repo.entries)
			require.Equal(t, sources, repo.maturitySources)
		})
	}
}

func TestMaturitySourceCorruptionCannotBecomeAcceptedProof(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
	}{
		{name: "frozen_deadline_differs_from_journal", mode: "deadline"},
		{name: "completed_source_has_no_maturity_receipt", mode: "invented_completion"},
		{name: "wrong_original_entry_identity", mode: "entry"},
		{name: "missing_source_prevents_accrual_replay", mode: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			req := accrualReq("partner", "payment", 5000, 2000)
			req.OccurredAt, req.HoldDuration = clock.Now(), time.Hour
			_, err := s.Accrue(ctx, req)
			require.NoError(t, err)
			id := maturityID(testProgram, testCurrency, "partner", "payment")
			source := repo.maturitySources[id]
			switch tc.mode {
			case "deadline":
				source.AvailableAt = source.AvailableAt.Add(time.Second)
			case "invented_completion":
				source.State, source.Revision, source.MaturedEntryID, source.MaturedAt = MaturityCompleted, 2, "invented", clock.Now()
			case "entry":
				source.AccruedEntryID = "another-entry"
			}
			source.Fingerprint = maturityFingerprint(source)
			repo.maturitySources[id] = source
			want := ErrConflict
			if tc.mode == "missing" {
				delete(repo.maturitySources, id)
				want = ErrNotFound
			}
			out, err := s.GetMaturitySource(ctx, id)
			require.ErrorIs(t, err, want)
			require.Empty(t, out)
			before := append([]Entry(nil), repo.entries...)
			replay, err := s.Accrue(ctx, req)
			if tc.mode == "missing" {
				want = ErrUnavailable
			}
			require.ErrorIs(t, err, want)
			require.Empty(t, replay)
			require.Equal(t, before, repo.entries)
		})
	}
}

func TestMaturitySourceConcurrentCommandsRetainOneReceipt(t *testing.T) {
	for _, mode := range []string{"accrual", "maturity"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			req := accrualReq("partner", "payment", 5000, 2000)
			req.OccurredAt, req.HoldDuration = clock.Now(), time.Hour
			if mode == "maturity" {
				_, err := s.Accrue(ctx, req)
				require.NoError(t, err)
				clock.t = clock.Now().Add(time.Hour)
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if mode == "accrual" {
						_, err := s.Accrue(ctx, req)
						errs <- err
					} else {
						_, err := s.Mature(ctx, "partner")
						errs <- err
					}
				}()
			}
			close(start)
			wg.Wait()
			require.NoError(t, <-errs)
			require.NoError(t, <-errs)
			require.Len(t, repo.maturitySources, 1)
			if mode == "accrual" {
				require.Len(t, repo.entries, 1)
			} else {
				require.Len(t, repo.entries, 2)
				source, err := s.GetMaturitySource(ctx, maturityID(testProgram, testCurrency, "partner", "payment"))
				require.NoError(t, err)
				require.Equal(t, repo.entries[1].ID, source.MaturedEntryID)
			}
		})
	}
}

type maturitySelectionRepo struct {
	Repository
	completeAfterSelection *Service
}

func (r *maturitySelectionRepo) PendingMaturitySourcesAfter(ctx context.Context, program, currency, after string, limit int) ([]MaturitySource, error) {
	page, err := r.Repository.PendingMaturitySourcesAfter(ctx, program, currency, after, limit)
	if err != nil || r.completeAfterSelection == nil {
		return page, err
	}
	_, err = r.completeAfterSelection.Mature(ctx, "partner")
	return page, err
}

func TestMaturityDiscoveryRetainsInputAcrossCompletion(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "pending_snapshot"
		if completed {
			name = "committed_completion_after_pending_selection"
		}
		t.Run(name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			req := accrualReq("partner", "payment", 5000, 2000)
			req.OccurredAt, req.HoldDuration = clock.Now(), time.Hour
			_, err := s.Accrue(ctx, req)
			require.NoError(t, err)
			original, err := s.PendingMaturitySourcesAfter(ctx, "", 1)
			require.NoError(t, err)
			require.Len(t, original, 1)
			clock.t = clock.Now().Add(time.Hour)
			selection := &maturitySelectionRepo{Repository: repo}
			if completed {
				selection.completeAfterSelection = s
			}
			reader, err := NewService(selection, clock, s.ids, s.Config())
			require.NoError(t, err)
			page, err := reader.PendingMaturitySourcesAfter(ctx, "", 1)
			require.NoError(t, err)
			require.Len(t, page, 1)
			require.Equal(t, original[0].ID, page[0].ID)
			require.Equal(t, original[0].Fingerprint, page[0].Fingerprint)
			require.Equal(t, original[0].AvailableAt, page[0].AvailableAt)
			if completed {
				require.Equal(t, MaturityCompleted, page[0].State)
				require.NotEmpty(t, page[0].MaturedEntryID)
			} else {
				require.Equal(t, MaturityPending, page[0].State)
			}
		})
	}
}

type maturityPageRepo struct {
	Repository
	page []MaturitySource
	fail error
}

func (r *maturityPageRepo) PendingMaturitySourcesAfter(context.Context, string, string, string, int) ([]MaturitySource, error) {
	return r.page, r.fail
}

func TestMaturityDiscoveryRejectsInvalidPagesAndRequests(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{name: "zero_page_limit", mode: "zero_limit", want: ErrInvalid},
		{name: "page_limit_above_200", mode: "large_limit", want: ErrInvalid},
		{name: "foreign_cursor_shape", mode: "cursor", want: ErrInvalid},
		{name: "cancelled_discovery", mode: "cancelled", want: context.Canceled},
		{name: "later_source_invalidates_whole_page", mode: "later_deadline", want: ErrConflict},
		{name: "duplicate_source_order", mode: "duplicate", want: ErrConflict},
		{name: "reversed_source_order", mode: "reverse", want: ErrConflict},
		{name: "page_above_bound", mode: "oversize", want: ErrUnavailable},
		{name: "source_currency_outside_configured_scope", mode: "currency", want: ErrConflict},
		{name: "joined_absence_and_outage_is_not_empty_success", mode: "joined", want: ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			for _, payment := range []string{"one", "two"} {
				req := accrualReq("partner", payment, 5000, 2000)
				req.OccurredAt, req.HoldDuration = clock.Now(), time.Hour
				_, err := s.Accrue(ctx, req)
				require.NoError(t, err)
			}
			page, err := s.PendingMaturitySourcesAfter(ctx, "", 200)
			require.NoError(t, err)
			require.Len(t, page, 2)
			input := &maturityPageRepo{Repository: repo, page: append([]MaturitySource(nil), page...)}
			limit, after := 200, ""
			switch tc.mode {
			case "zero_limit":
				limit = 0
			case "large_limit":
				limit = 201
			case "cursor":
				after = "payment-cursor"
			case "cancelled":
				cancel()
			case "later_deadline":
				input.page[1].AvailableAt = input.page[1].AvailableAt.Add(time.Second)
				input.page[1].Fingerprint = maturityFingerprint(input.page[1])
			case "duplicate":
				input.page[1] = input.page[0]
			case "reverse":
				input.page[0], input.page[1] = input.page[1], input.page[0]
			case "oversize":
				input.page = make([]MaturitySource, 201)
				for i := range input.page {
					input.page[i] = page[0]
				}
			case "currency":
				input.page[0].Currency = "USD"
				input.page[0].ID = maturityID(testProgram, "USD", input.page[0].PartnerID, input.page[0].PaymentID)
				input.page[0].Fingerprint = maturityFingerprint(input.page[0])
			case "joined":
				input.fail = errors.Join(ErrNotFound, ErrUnavailable)
			}
			reader, err := NewService(input, clock, s.ids, s.Config())
			require.NoError(t, err)
			out, err := reader.PendingMaturitySourcesAfter(ctx, after, limit)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
		})
	}
}
