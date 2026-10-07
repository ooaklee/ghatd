package partnerstore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func scheduledAccrual(clock *mongoTestClock, payment string) partnerearnings.AccrualRequest {
	return partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: payment, PaymentMinor: 5000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: clock.Now(), HoldDuration: time.Hour}
}

// Audit disposition: named actual Mongo journeys prove global source/ledger
// atomicity, immutable discovery, retained receipt and held/refunded semantics.
// Every case owns a fresh disposable database and clock through mongoEarnings.
func TestMongoMaturitySourceFinancialJourneys(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
	}{
		{name: "scheduled_original_deadline_and_receipt"},
		{name: "immediate_completion_never_enters_pending_feed", mode: "immediate"},
		{name: "zero_commission_is_retained_journal_work", mode: "zero"},
		{name: "full_refund_preserves_original_maturity_receipt", mode: "refund"},
		{name: "dispute_hold_remains_after_maturity", mode: "held"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, db, clock, ctx := mongoEarnings(t)
			req := scheduledAccrual(clock, "private-original-payment")
			if tc.mode == "immediate" {
				req.HoldDuration = 0
			}
			if tc.mode == "zero" {
				req.RateBasisPoints = 0
			}
			accrued, err := svc.Accrue(ctx, req)
			require.NoError(t, err)
			var source partnerearnings.MaturitySource
			page, err := svc.PendingMaturitySourcesAfter(ctx, "", 200)
			require.NoError(t, err)
			if tc.mode == "immediate" {
				require.Empty(t, page)
				var raw bson.M
				require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindMaturitySource}).Decode(&raw))
				source, err = svc.GetMaturitySource(ctx, raw["id"].(string))
				require.NoError(t, err)
			} else {
				require.Len(t, page, 1)
				source = page[0]
				require.Equal(t, partnerearnings.MaturityPending, source.State)
				out, err := svc.Mature(ctx, "partner")
				require.NoError(t, err)
				require.Empty(t, out)
			}
			require.Equal(t, accrued.ID, source.AccruedEntryID)
			require.Equal(t, *accrued.AvailableAt, source.AvailableAt)
			switch tc.mode {
			case "refund":
				_, err = svc.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: "partner", PaymentID: req.PaymentID, RefundID: "refund", CumulativeRefundedMinor: 5000, Currency: "EUR", OccurredAt: clock.Now()})
				require.NoError(t, err)
			case "held":
				_, err = svc.Dispute(ctx, partnerearnings.DisputeRequest{PartnerID: "partner", PaymentID: req.PaymentID, DisputeID: "dispute", OperationID: "hold", Action: "hold", ActorID: "worker", Currency: "EUR", OccurredAt: clock.Now(), Reason: "verified"})
				require.NoError(t, err)
			}
			clock.now = source.AvailableAt
			_, err = svc.Mature(ctx, "partner")
			require.NoError(t, err)
			completed, err := svc.GetMaturitySource(ctx, source.ID)
			require.NoError(t, err)
			require.Equal(t, partnerearnings.MaturityCompleted, completed.State)
			require.EqualValues(t, 2, completed.Revision)
			require.Equal(t, source.Fingerprint, completed.Fingerprint)
			proof, err := repo.EntryBySource(ctx, svc.ProgramID(), "partner", partnerearnings.EntryMatured, req.PaymentID)
			require.NoError(t, err)
			require.Equal(t, proof.ID, completed.MaturedEntryID)
			require.Equal(t, accrued.AmountMinor, proof.AmountMinor)
			page, err = svc.PendingMaturitySourcesAfter(ctx, "", 200)
			require.NoError(t, err)
			require.Empty(t, page)
			replay, err := svc.Accrue(ctx, req)
			require.NoError(t, err)
			require.Equal(t, accrued, replay)
			again, err := svc.Mature(ctx, "partner")
			require.NoError(t, err)
			require.Empty(t, again)
			balances, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			if tc.mode == "held" {
				require.EqualValues(t, 1000, balances.DisputeHoldMinor)
				require.Zero(t, balances.AvailableMinor)
			}
			if tc.mode == "zero" || tc.mode == "refund" {
				require.Zero(t, balances.AvailableMinor)
			}
			var raw bson.M
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindMaturitySource, "id": source.ID}).Decode(&raw))
			require.NotContains(t, raw, "expires_at")
			require.NotContains(t, fmt.Sprint(raw), req.PaymentID, "original private evidence must be encrypted, never indexed plaintext")
			encoded, err := json.Marshal(completed)
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(encoded))
		})
	}
}

type maturityWriteFaultStore struct {
	recordstore.Store
	stage string
}
type maturityWriteFaultTx struct {
	recordstore.Tx
	stage string
}

func (s maturityWriteFaultStore) Transact(ctx context.Context, guard string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, guard, func(tx recordstore.Tx) error { return fn(maturityWriteFaultTx{Tx: tx, stage: s.stage}) })
}
func (tx maturityWriteFaultTx) Insert(ctx context.Context, row recordstore.Record) error {
	if row.Kind == kindMaturitySource && tx.stage == "insert_before" {
		return injectedFailure
	}
	if err := tx.Tx.Insert(ctx, row); err != nil {
		return err
	}
	if row.Kind == kindMaturitySource && tx.stage == "insert_after" {
		return injectedFailure
	}
	return nil
}
func (tx maturityWriteFaultTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	if row.Kind == kindMaturitySource && tx.stage == "complete_before" {
		return injectedFailure
	}
	if err := tx.Tx.Replace(ctx, row, expected); err != nil {
		return err
	}
	if row.Kind == kindMaturitySource && tx.stage == "complete_after" {
		return injectedFailure
	}
	return nil
}

func TestMongoMaturitySourceAndJournalRollBackTogether(t *testing.T) {
	for _, tc := range []struct {
		name, stage       string
		immediate, mature bool
	}{
		{name: "before_global_source_insert", stage: "insert_before"},
		{name: "after_global_source_insert", stage: "insert_after"},
		{name: "before_immediate_completion", stage: "complete_before", immediate: true},
		{name: "after_immediate_completion", stage: "complete_after", immediate: true},
		{name: "before_scheduled_completion", stage: "complete_before", mature: true},
		{name: "after_scheduled_completion", stage: "complete_after", mature: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, db, clock, ctx := mongoEarnings(t)
			req := scheduledAccrual(clock, "payment")
			if tc.immediate {
				req.HoldDuration = 0
			}
			var before []partnerearnings.Entry
			var source partnerearnings.MaturitySource
			if tc.mature {
				_, err := svc.Accrue(ctx, req)
				require.NoError(t, err)
				page, err := svc.PendingMaturitySourcesAfter(ctx, "", 1)
				require.NoError(t, err)
				require.Len(t, page, 1)
				source = page[0]
				before, err = repo.ListEntries(ctx, svc.ProgramID(), "partner")
				require.NoError(t, err)
				clock.now = source.AvailableAt
			}
			brokenRepo, err := NewEarningsRepository(maturityWriteFaultStore{Store: store, stage: tc.stage}, svc.Config())
			require.NoError(t, err)
			broken, err := partnerearnings.NewService(brokenRepo, clock, randomIDs{}, svc.Config())
			require.NoError(t, err)
			if tc.mature {
				out, err := broken.Mature(ctx, "partner")
				require.ErrorIs(t, err, injectedFailure)
				require.Empty(t, out)
				current, err := svc.GetMaturitySource(ctx, source.ID)
				require.NoError(t, err)
				require.Equal(t, source, current)
			} else {
				out, err := broken.Accrue(ctx, req)
				require.ErrorIs(t, err, injectedFailure)
				require.Empty(t, out)
				count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": bson.A{kindMaturitySource, kindLedgerHead, kindEntrySource}}})
				require.NoError(t, err)
				require.Zero(t, count)
			}
			after, err := repo.ListEntries(ctx, svc.ProgramID(), "partner")
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestMongoMaturityLostAcknowledgementRecoversOriginalReceipt(t *testing.T) {
	for _, mode := range []string{"accrual", "maturity"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo, store, _, clock, ctx := mongoEarnings(t)
			req := scheduledAccrual(clock, "payment")
			uncertainRepo, err := NewEarningsRepository(injectedStore{Store: store, uncertain: true}, svc.Config())
			require.NoError(t, err)
			uncertain, err := partnerearnings.NewService(uncertainRepo, clock, randomIDs{}, svc.Config())
			require.NoError(t, err)
			if mode == "accrual" {
				out, err := uncertain.Accrue(ctx, req)
				require.ErrorIs(t, err, partnerearnings.ErrUncertain)
				require.Empty(t, out)
			} else {
				_, err := svc.Accrue(ctx, req)
				require.NoError(t, err)
				clock.now = clock.Now().Add(time.Hour)
				out, err := uncertain.Mature(ctx, "partner")
				require.ErrorIs(t, err, partnerearnings.ErrUncertain)
				require.Empty(t, out)
			}
			before, err := repo.ListEntries(ctx, svc.ProgramID(), "partner")
			require.NoError(t, err)
			out, err := svc.Accrue(ctx, req)
			require.NoError(t, err)
			require.Equal(t, before[0], out)
			if mode == "maturity" {
				again, err := svc.Mature(ctx, "partner")
				require.NoError(t, err)
				require.Empty(t, again)
			}
			after, err := repo.ListEntries(ctx, svc.ProgramID(), "partner")
			require.NoError(t, err)
			require.Equal(t, before, after)
			var sourceID string
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				rows, err := tx.Find(ctx, recordstore.Query{Kind: kindMaturitySource, Partition: maturityPartition(svc.ProgramID(), svc.Currency()), Limit: 1})
				if err != nil {
					return err
				}
				require.Len(t, rows, 1)
				sourceID = rows[0].ID
				return nil
			}))
			source, err := svc.GetMaturitySource(ctx, sourceID)
			require.NoError(t, err)
			if mode == "maturity" {
				require.Equal(t, before[1].ID, source.MaturedEntryID)
			}
		})
	}
}

func TestMongoMaturityDiscoveryIsIndexedAndScoped(t *testing.T) {
	for _, currency := range []string{"EUR", "USD"} {
		t.Run("separate_currency_"+currency, func(t *testing.T) {
			svc, _, _, db, clock, ctx := mongoEarnings(t)
			port, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
			require.NoError(t, err)
			capture := &maturityQueryPort{MongoPort: port}
			cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			store, err := recordstore.NewMongoStore(db, capture, cipher)
			require.NoError(t, err)
			require.NoError(t, store.EnsureIndexes(ctx))
			require.NoError(t, store.Probe(ctx))
			ownRepo, err := NewEarningsRepository(store, svc.Config())
			require.NoError(t, err)
			svc, err = partnerearnings.NewService(ownRepo, clock, randomIDs{}, svc.Config())
			require.NoError(t, err)
			for _, payment := range []string{"one", "two", "three"} {
				_, err := svc.Accrue(ctx, scheduledAccrual(clock, payment))
				require.NoError(t, err)
			}
			config := partnerearnings.Config{ProgramID: "other-program", Currency: currency}
			if currency == "USD" {
				config.ProgramID = svc.ProgramID()
			}
			repo, err := NewEarningsRepository(store, config)
			require.NoError(t, err)
			other, err := partnerearnings.NewService(repo, clock, randomIDs{}, config)
			require.NoError(t, err)
			req := scheduledAccrual(clock, "private-other-payment")
			req.Currency = currency
			_, err = other.Accrue(ctx, req)
			require.NoError(t, err)
			var ids []string
			cursor := ""
			for {
				page, err := svc.PendingMaturitySourcesAfter(ctx, cursor, 1)
				require.NoError(t, err)
				if len(page) == 0 {
					break
				}
				require.Len(t, page, 1)
				require.Equal(t, svc.ProgramID(), page[0].ProgramID)
				require.Equal(t, svc.Currency(), page[0].Currency)
				require.Greater(t, page[0].ID, cursor)
				cursor = page[0].ID
				ids = append(ids, cursor)
			}
			require.Len(t, ids, 3)
			foreign, err := other.PendingMaturitySourcesAfter(ctx, "", 200)
			require.NoError(t, err)
			require.Len(t, foreign, 1)
			for _, after := range []string{"", ids[0]} {
				page, err := svc.PendingMaturitySourcesAfter(ctx, after, 2)
				require.NoError(t, err)
				require.Len(t, page, 2)
				require.Equal(t, bson.M{"kind": kindMaturitySource, "partition": maturityPartition(svc.ProgramID(), svc.Currency()), "state": partnerearnings.MaturityPending}, withoutMaturityCursor(capture.filter))
				if after != "" {
					require.Equal(t, bson.M{"$gt": after}, capture.filter["id"])
				}
				// Explain the filter/sort/limit captured at the actual managed
				// Mongo ExecuteFindCommand boundary, including cursor pages.
				query := bson.D{{Key: "find", Value: capture.collection}, {Key: "filter", Value: capture.filter}, {Key: "sort", Value: capture.sort}, {Key: "limit", Value: capture.limit}}
				var explain bson.M
				require.NoError(t, db.RunCommand(ctx, bson.D{{Key: "explain", Value: query}, {Key: "verbosity", Value: "executionStats"}}).Decode(&explain))
				wire, err := bson.Marshal(explain)
				require.NoError(t, err)
				winning, ok := bson.Raw(wire).Lookup("queryPlanner", "winningPlan").DocumentOK()
				require.True(t, ok)
				require.Contains(t, winning.String(), "IXSCAN", "bounded discovery must use an index")
				require.NotContains(t, winning.String(), "COLLSCAN")
			}
		})
	}
}

// maturityQueryPort observes the actual native query and delegates operations
// to the same managed port, preserving the store's transaction behaviour.
type maturityQueryPort struct {
	recordstore.MongoPort
	collection string
	filter     bson.M
	sort       any
	limit      int64
}

func (p *maturityQueryPort) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter any, builders ...options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
	if f, ok := filter.(bson.M); ok && f["kind"] == kindMaturitySource {
		var opts options.FindOptions
		for _, builder := range builders {
			for _, setter := range builder.List() {
				if err := setter(&opts); err != nil {
					return nil, err
				}
			}
		}
		p.collection, p.filter, p.sort = collection.Name(), f, opts.Sort
		if opts.Limit != nil {
			p.limit = *opts.Limit
		}
	}
	return p.MongoPort.ExecuteFindCommand(ctx, collection, filter, builders...)
}

func withoutMaturityCursor(filter bson.M) bson.M {
	copy := bson.M{}
	for key, value := range filter {
		if key != "id" {
			copy[key] = value
		}
	}
	return copy
}

type maturitySelectionMongoStore struct {
	recordstore.Store
	mature    *partnerearnings.Service
	committed bool
}
type maturitySelectionMongoTx struct {
	recordstore.Tx
	store *maturitySelectionMongoStore
}

func (s *maturitySelectionMongoStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error { return fn(maturitySelectionMongoTx{Tx: tx, store: s}) })
}
func (tx maturitySelectionMongoTx) Find(ctx context.Context, query recordstore.Query) ([]recordstore.Record, error) {
	rows, err := tx.Tx.Find(ctx, query)
	if err != nil || query.Kind != kindMaturitySource || tx.store.mature == nil || tx.store.committed {
		return rows, err
	}
	// The read's actual snapshot remains open while a separate original-context
	// financial transaction commits completion. Only then is its selected page
	// returned for the owning service's second, complete financial snapshot.
	_, err = tx.store.mature.Mature(ctx, "partner")
	if err == nil {
		tx.store.committed = true
	}
	return rows, err
}

func TestMongoMaturityDiscoverySeesCommittedCompletion(t *testing.T) {
	for _, complete := range []bool{false, true} {
		name := "pending_financial_snapshot"
		if complete {
			name = "separate_completion_commits_after_native_pending_selection"
		}
		t.Run(name, func(t *testing.T) {
			svc, repo, store, _, clock, ctx := mongoEarnings(t)
			_, err := svc.Accrue(ctx, scheduledAccrual(clock, "payment"))
			require.NoError(t, err)
			original, err := svc.PendingMaturitySourcesAfter(ctx, "", 1)
			require.NoError(t, err)
			require.Len(t, original, 1)
			clock.now = original[0].AvailableAt
			selection := &maturitySelectionMongoStore{Store: store}
			if complete {
				selection.mature = svc
			}
			readerRepo, err := NewEarningsRepository(selection, svc.Config())
			require.NoError(t, err)
			reader, err := partnerearnings.NewService(readerRepo, clock, randomIDs{}, svc.Config())
			require.NoError(t, err)
			page, err := reader.PendingMaturitySourcesAfter(ctx, "", 1)
			require.NoError(t, err)
			require.Len(t, page, 1)
			require.Equal(t, original[0].ID, page[0].ID)
			require.Equal(t, original[0].Fingerprint, page[0].Fingerprint)
			require.Equal(t, original[0].AvailableAt, page[0].AvailableAt)
			if complete {
				require.True(t, selection.committed)
				require.Equal(t, partnerearnings.MaturityCompleted, page[0].State)
				proof, err := repo.EntryBySource(ctx, svc.ProgramID(), "partner", partnerearnings.EntryMatured, "payment")
				require.NoError(t, err)
				require.Equal(t, proof.ID, page[0].MaturedEntryID)
			} else {
				require.Equal(t, partnerearnings.MaturityPending, page[0].State)
			}
		})
	}
}

func TestMongoMaturitySourcesConcurrentLedgerGuards(t *testing.T) {
	for _, tc := range []struct {
		name                string
		mature, independent bool
	}{
		{name: "same_payment_concurrent_accrual"},
		{name: "same_partner_concurrent_maturity", mature: true},
		{name: "different_partner_concurrent_accrual", independent: true},
		{name: "different_partner_concurrent_maturity", mature: true, independent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, db, clock, ctx := mongoEarnings(t)
			partners := []string{"partner", "partner"}
			if tc.independent {
				partners[1] = "other-partner"
			}
			if tc.mature {
				for i, partner := range partners {
					if i == 1 && !tc.independent {
						continue
					}
					req := scheduledAccrual(clock, "payment")
					req.PartnerID = partner
					_, err := svc.Accrue(ctx, req)
					require.NoError(t, err)
				}
				clock.now = clock.Now().Add(time.Hour)
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			for _, partner := range partners {
				go func(partner string) {
					<-start
					if tc.mature {
						_, err := svc.Mature(ctx, partner)
						errs <- err
					} else {
						req := scheduledAccrual(clock, "payment")
						req.PartnerID = partner
						_, err := svc.Accrue(ctx, req)
						errs <- err
					}
				}(partner)
			}
			close(start)
			require.NoError(t, <-errs)
			require.NoError(t, <-errs)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindMaturitySource})
			require.NoError(t, err)
			want := int64(1)
			if tc.independent {
				want = 2
			}
			require.Equal(t, want, count)
			for i, partner := range partners {
				if i == 1 && !tc.independent {
					continue
				}
				entries, err := repo.ListEntries(ctx, svc.ProgramID(), partner)
				require.NoError(t, err)
				if tc.mature {
					require.Len(t, entries, 2)
				} else {
					require.Len(t, entries, 1)
				}
				var raw bson.M
				// The source's encrypted partner identity is validated through the
				// owning service; raw queries select only opaque source identifiers.
				id := "maturity_" + identity(svc.ProgramID(), svc.Currency(), partner, "payment")
				require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindMaturitySource, "id": id}).Decode(&raw))
				source, err := svc.GetMaturitySource(ctx, id)
				require.NoError(t, err)
				require.Equal(t, partner, source.PartnerID)
				if tc.mature {
					require.Equal(t, entries[1].ID, source.MaturedEntryID)
				}
			}
		})
	}
}

func TestMongoMaturitySourceRejectsCorruptPersistence(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{name: "financial_source_cannot_have_expiry", mode: "expiry", want: partnerearnings.ErrUnavailable},
		{name: "global_partition_must_match_financial_scope", mode: "partition", want: partnerearnings.ErrUnavailable},
		{name: "indexed_state_must_match_encrypted_state", mode: "state", want: partnerearnings.ErrUnavailable},
		{name: "completion_requires_actual_maturity_entry", mode: "receipt", want: partnerearnings.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, db, clock, ctx := mongoEarnings(t)
			req := scheduledAccrual(clock, "payment")
			_, err := svc.Accrue(ctx, req)
			require.NoError(t, err)
			page, err := svc.PendingMaturitySourcesAfter(ctx, "", 1)
			require.NoError(t, err)
			require.Len(t, page, 1)
			if tc.mode == "receipt" {
				clock.now = page[0].AvailableAt
				_, err = svc.Mature(ctx, "partner")
				require.NoError(t, err)
			}
			before, err := repo.ListEntries(ctx, svc.ProgramID(), "partner")
			require.NoError(t, err)
			var row recordstore.Record
			err = store.Read(ctx, func(tx recordstore.Tx) error {
				var err error
				row, err = tx.Get(ctx, kindMaturitySource, page[0].ID)
				return err
			})
			require.NoError(t, err)
			switch tc.mode {
			case "expiry":
				row, err = row.WithExpiration(time.Now().UTC().Add(24 * time.Hour))
				require.NoError(t, err)
			case "partition":
				row.Partition = maturityPartition("foreign-program", svc.Currency())
			case "state":
				row.State = partnerearnings.MaturityCompleted
			case "receipt":
				var stored persistedMaturity
				require.NoError(t, row.Decode(&stored))
				stored.MaturedEntryID = "invented-receipt"
				row.Data, err = json.Marshal(stored)
				require.NoError(t, err)
			}
			// Deliberately corrupt this test's own disposable source. Reinsert
			// with the same storage/domain revision and valid encryption so the
			// selected invariant, rather than a revision/ciphertext mismatch,
			// is what rejects it. No production adapter has a deletion port.
			deleted, err := db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindMaturitySource, "id": row.ID})
			require.NoError(t, err)
			require.EqualValues(t, 1, deleted.DeletedCount)
			err = store.Transact(ctx, ledgerPartition(svc.ProgramID(), "partner", svc.Currency()), func(tx recordstore.Tx) error {
				return tx.Insert(ctx, row)
			})
			require.NoError(t, err)
			out, err := svc.GetMaturitySource(ctx, page[0].ID)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
			replay, err := svc.Accrue(ctx, req)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, replay)
			after, err := repo.ListEntries(ctx, svc.ProgramID(), "partner")
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
