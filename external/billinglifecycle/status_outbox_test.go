package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Actual billing preparation/capture and native codecs over isolated atomic
// records. This fixture does not certify encryption, Mongo, manager authority
// or platform runtime. Each case has its own records, clock and authority stub.
type statusOriginalStore struct{ records map[string]recordstore.Record }
type statusOriginalTx struct{ records map[string]recordstore.Record }

func (s *statusOriginalStore) Read(_ context.Context, fn func(recordstore.Tx) error) error {
	return fn(&statusOriginalTx{s.records})
}
func (s *statusOriginalStore) Transact(_ context.Context, _ string, fn func(recordstore.Tx) error) error {
	tx := &statusOriginalTx{maps.Clone(s.records)}
	if err := fn(tx); err != nil {
		return err
	}
	s.records = tx.records
	return nil
}
func (s *statusOriginalTx) Get(_ context.Context, kind, id string) (recordstore.Record, error) {
	r, ok := s.records[kind+":"+id]
	if !ok {
		return recordstore.Record{}, recordstore.ErrNotFound
	}
	r.Data = append([]byte(nil), r.Data...)
	return r, nil
}
func (s *statusOriginalTx) Find(_ context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	var out []recordstore.Record
	for _, r := range s.records {
		if r.Kind == q.Kind && r.Partition == q.Partition {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *statusOriginalTx) Insert(_ context.Context, r recordstore.Record) error {
	key := r.Kind + ":" + r.ID
	if _, exists := s.records[key]; exists {
		return recordstore.ErrConflict
	}
	s.records[key] = r
	return nil
}
func (s *statusOriginalTx) Replace(_ context.Context, r recordstore.Record, expected int64) error {
	key := r.Kind + ":" + r.ID
	old, exists := s.records[key]
	if !exists || old.Revision != expected {
		return recordstore.ErrConflict
	}
	s.records[key] = r
	return nil
}

type statusInputClock struct{ at time.Time }

func (c *statusInputClock) Now() time.Time { return c.at }

type statusInputValidator struct {
	t             *testing.T
	store         *inputStore
	owner         *billing.RevenueService
	calls, denyAt int
	err           error
}

func (v *statusInputValidator) ValidateSubscriptionStatusPreparation(ctx context.Context, actor string, p billing.SubscriptionStatusPreparation) error {
	v.calls++
	require.False(v.t, v.store.inside, "owning validation must remain outside retryable callbacks")
	if v.calls == v.denyAt {
		return v.err
	}
	if actor != "current-worker" && actor != "replacement-worker" {
		return partnermanager.ErrDenied
	}
	return v.owner.ValidateSubscriptionStatusPreparation(ctx, p)
}

type statusInputFixture struct {
	outbox    *StatusOutbox
	store     *inputStore
	validator *statusInputValidator
	owner     *billing.RevenueService
	clock     *statusInputClock
	p         billing.SubscriptionStatusPreparation
	e         billing.VerifiedSubscriptionStatusEvidence
}

func originalStatusFixture(t *testing.T, source string) *statusInputFixture {
	t.Helper()
	records := &statusOriginalStore{records: map[string]recordstore.Record{}}
	repo, err := revenuestore.NewRepository(records)
	require.NoError(t, err)
	clock := &statusInputClock{time.Unix(1700000000, 123456789).UTC()}
	owner, err := billing.NewRevenueService(repo, clock)
	require.NoError(t, err)
	scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
	var p billing.SubscriptionStatusPreparation
	if source == "payment" {
		r, err := owner.AcceptVerified(t.Context(), billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "evt_original", Facts: []billing.RevenueFact{{Scope: scope, Kind: billing.RevenuePayment, PaymentID: "pi_original", InvoiceID: "in_original", AllocationID: "il_original", PrincipalID: "payer", ProviderCustomerID: "cus_original", SubscriptionID: "sub_original", PlanID: "plan", CostID: "cost", Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: clock.at.Add(-time.Minute)}}})
		require.NoError(t, err)
		p, err = owner.PrepareSubscriptionStatus(t.Context(), "preparing-author", r.FactIDs[0])
		require.NoError(t, err)
	} else {
		provider := &inputNativeProvider{}
		checkout, err := billing.NewCheckoutService(repo, clock, provider)
		require.NoError(t, err)
		q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "original", PriceID: "price_frozen", PlanID: "plan", CostID: "cost", UserID: "payer", UserReference: "payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14}
		i, err := checkout.PrepareCheckout(t.Context(), scope, q)
		require.NoError(t, err)
		require.NoError(t, checkout.AcknowledgeCheckout(t.Context(), i, "cs_original"))
		i, err = checkout.FindCheckoutIntent(t.Context(), scope, q.IdempotencyKey)
		require.NoError(t, err)
		e := paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: q.UserID, CustomerID: "cus_original", SubscriptionID: "sub_original", PriceID: q.PriceID, Currency: "GBP", Mode: q.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
		_, err = checkout.CaptureCheckoutLifecycleEvidence(t.Context(), i, e)
		require.NoError(t, err)
		p, err = owner.PrepareSubscriptionStatusForCheckout(t.Context(), "preparing-author", scope, "sub_original")
		require.NoError(t, err)
	}
	s := &inputStore{}
	v := &statusInputValidator{t: t, store: s, owner: owner}
	o, err := NewStatusOutbox(s, v)
	require.NoError(t, err)
	e := billing.VerifiedSubscriptionStatusEvidence{Scope: p.Scope, SubscriptionID: p.SubscriptionID, ProviderCustomerID: p.ProviderCustomerID, Status: "trialing", CancellationScheduled: true}
	require.NoError(t, p.Validate())
	require.NoError(t, e.Validate(p))
	return &statusInputFixture{o, s, v, owner, clock, p, e}
}

func TestStatusOutboxRecovery(t *testing.T) {
	outage := errors.New("storage outage")
	for _, tc := range []struct {
		name string
		want error
	}{
		{"prepared", nil}, {"evidence", nil}, {"exact_evidence_replay", nil}, {"preparation_retry_keeps_evidence", nil},
		{"different_evidence_conflicts", recordstore.ErrConflict}, {"evidence_requires_preparation", recordstore.ErrUnavailable},
		{"fresh_absence", recordstore.ErrNotFound}, {"outage", outage}, {"joined_absence_outage", outage},
		{"initial_denial", partnermanager.ErrDenied}, {"post_commit_denial", partnermanager.ErrDenied},
		{"absence_post_denial", partnermanager.ErrDenied}, {"error_post_denial", partnermanager.ErrDenied},
		{"uncertain_preparation_recovery", nil}, {"uncertain_evidence_recovery", nil},
		{"uncertain_plus_denial", recordstore.ErrUncertain}, {"uncertain_plus_cancellation", recordstore.ErrUncertain},
		{"outage_plus_cancellation", context.Canceled}, {"revision_conflict", recordstore.ErrConflict},
		{"bad_evidence", billing.ErrRevenueConflict}, {"canceled", context.Canceled}, {"nil_context", recordstore.ErrInvalid},
		{"replacement_worker", nil}, {"malformed_preparation", recordstore.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := originalStatusFixture(t, "checkout")
			ctx := t.Context()
			actor := "current-worker"
			var out StatusInput
			var err error
			switch tc.name {
			case "prepared", "replacement_worker":
				if tc.name == "replacement_worker" {
					actor = "replacement-worker"
				}
				out, err = f.outbox.RetainPreparation(ctx, actor, f.p)
			case "fresh_absence", "absence_post_denial":
				if tc.name == "absence_post_denial" {
					f.validator.denyAt = 2
					f.validator.err = partnermanager.ErrDenied
				}
				out, err = f.outbox.Find(ctx, actor, f.p)
			case "outage", "joined_absence_outage", "error_post_denial":
				f.store.err = outage
				if tc.name == "joined_absence_outage" {
					f.store.err = errors.Join(recordstore.ErrNotFound, outage)
				}
				if tc.name == "error_post_denial" {
					f.validator.denyAt = 2
					f.validator.err = partnermanager.ErrDenied
				}
				out, err = f.outbox.RetainPreparation(ctx, actor, f.p)
				require.Nil(t, f.store.record)
			case "initial_denial", "post_commit_denial", "uncertain_plus_denial":
				f.validator.denyAt = 2
				f.validator.err = partnermanager.ErrDenied
				if tc.name == "initial_denial" {
					f.validator.denyAt = 1
				}
				if tc.name == "uncertain_plus_denial" {
					f.store.afterCommit = recordstore.ErrUncertain
				}
				out, err = f.outbox.RetainPreparation(ctx, actor, f.p)
				if tc.name == "initial_denial" {
					require.Zero(t, f.store.writes)
				} else {
					require.NotNil(t, f.store.record)
				}
				if tc.name == "uncertain_plus_denial" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
				}
			case "uncertain_plus_cancellation", "outage_plus_cancellation":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
				f.store.after = cancel
				if tc.name == "uncertain_plus_cancellation" {
					f.store.afterCommit = recordstore.ErrUncertain
				} else {
					f.store.err = outage
				}
				out, err = f.outbox.RetainPreparation(ctx, actor, f.p)
				require.ErrorIs(t, err, context.Canceled)
			case "evidence_requires_preparation":
				out, err = f.outbox.RetainEvidence(ctx, actor, f.p, f.e)
				require.Nil(t, f.store.record)
			case "malformed_preparation":
				f.p.CaptureID = "wrong"
				out, err = f.outbox.RetainPreparation(ctx, actor, f.p)
				require.Zero(t, f.store.writes)
				require.Zero(t, f.validator.calls)
			case "canceled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				out, err = f.outbox.RetainPreparation(cancelled, actor, f.p)
				require.Zero(t, f.store.writes)
			case "nil_context":
				var nilCtx context.Context
				out, err = f.outbox.RetainPreparation(nilCtx, actor, f.p)
			case "uncertain_preparation_recovery":
				f.store.afterCommit = recordstore.ErrUncertain
				first, unknown := f.outbox.RetainPreparation(ctx, actor, f.p)
				require.ErrorIs(t, unknown, recordstore.ErrUncertain)
				require.Equal(t, StatusInput{}, first)
				f.store.afterCommit = nil
				out, err = f.outbox.Find(ctx, actor, f.p)
			default:
				_, err = f.outbox.RetainPreparation(ctx, actor, f.p)
				require.NoError(t, err)
				if tc.name == "bad_evidence" {
					f.e.ProviderCustomerID = "wrong"
				}
				if tc.name == "revision_conflict" {
					f.store.replaceErr = recordstore.ErrConflict
				}
				if tc.name == "uncertain_evidence_recovery" {
					f.store.afterCommit = recordstore.ErrUncertain
				}
				out, err = f.outbox.RetainEvidence(ctx, actor, f.p, f.e)
				if tc.name == "uncertain_evidence_recovery" {
					require.ErrorIs(t, err, recordstore.ErrUncertain)
					require.Equal(t, StatusInput{}, out)
					f.store.afterCommit = nil
					out, err = f.outbox.Find(ctx, actor, f.p)
				}
				if tc.name == "bad_evidence" || tc.name == "revision_conflict" {
					require.EqualValues(t, 1, f.store.record.Revision)
					break
				}
				require.NoError(t, err)
				saved := append([]byte(nil), f.store.record.Data...)
				switch tc.name {
				case "exact_evidence_replay":
					out, err = f.outbox.RetainEvidence(ctx, actor, f.p, f.e)
				case "preparation_retry_keeps_evidence":
					out, err = f.outbox.RetainPreparation(ctx, actor, f.p)
				case "different_evidence_conflicts":
					f.e.Status = "paused"
					out, err = f.outbox.RetainEvidence(ctx, actor, f.p, f.e)
				}
				require.Equal(t, saved, []byte(f.store.record.Data))
				require.EqualValues(t, 2, f.store.record.Revision)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, StatusInput{}, out)
				return
			}
			require.NoError(t, err)
			require.Equal(t, f.p, out.Preparation)
			require.Equal(t, statusKind+":"+f.store.record.ID, f.store.guard)
			require.Nil(t, f.store.record.ExpiresAt)
			require.Zero(t, f.store.record.Sequence)
			if out.Evidence != nil {
				require.Equal(t, f.e, *out.Evidence)
				out.Evidence.Status = "caller mutation"
				read, err := f.outbox.Find(ctx, actor, f.p)
				require.NoError(t, err)
				require.Equal(t, f.e, *read.Evidence)
			}
			raw, err := json.Marshal(out)
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(raw))
		})
	}
}

func TestStatusOutboxStoredIntegrity(t *testing.T) {
	for _, mutation := range []string{"kind", "id", "partition", "expiry", "sequence", "revision", "state", "schema", "unknown_field", "trailing_json", "capture", "author", "customer", "source", "requested_at", "evidence"} {
		t.Run(mutation, func(t *testing.T) {
			f := originalStatusFixture(t, "checkout")
			_, err := f.outbox.RetainPreparation(t.Context(), "current-worker", f.p)
			require.NoError(t, err)
			switch mutation {
			case "kind":
				f.store.record.Kind = "wrong"
			case "id":
				f.store.record.ID = "wrong"
			case "partition":
				f.store.record.Partition = "wrong"
			case "expiry":
				at := time.Now()
				f.store.record.ExpiresAt = &at
			case "sequence":
				f.store.record.Sequence = 1
			case "revision":
				f.store.record.Revision = 3
			case "state":
				f.store.record.State = "evidence"
			case "trailing_json":
				f.store.record.Data = append(f.store.record.Data, []byte(" {}")...)
			default:
				var p statusPayload
				require.NoError(t, f.store.record.Decode(&p))
				switch mutation {
				case "schema":
					p.Schema = 2
				case "capture":
					p.Preparation.CaptureID = "wrong"
				case "author":
					p.Preparation.ActorID = "wrong"
				case "customer":
					p.Preparation.ProviderCustomerID = "wrong"
				case "source":
					p.Preparation.Source = ""
				case "requested_at":
					p.Preparation.RequestedAt = p.Preparation.RequestedAt.Add(time.Nanosecond)
				case "evidence":
					p.Evidence = &statusEvidencePayload{Scope: f.p.Scope, SubscriptionID: f.p.SubscriptionID, ProviderCustomerID: f.p.ProviderCustomerID, Status: "unknown"}
				}
				r, err := recordstore.NewRecord(statusKind, f.store.record.ID, f.store.record.Partition, 1, p)
				require.NoError(t, err)
				f.store.record.Data = r.Data
				if mutation == "unknown_field" {
					f.store.record.Data = append(f.store.record.Data[:len(f.store.record.Data)-1], []byte(",\"Unexpected\":true}")...)
				}
			}
			before := *f.store.record
			out, err := f.outbox.Find(t.Context(), "current-worker", f.p)
			require.Error(t, err)
			require.Equal(t, StatusInput{}, out)
			require.Equal(t, before, *f.store.record)
		})
	}
}

func TestStatusOutboxSourcesAndLaterHeads(t *testing.T) {
	for _, source := range []string{"payment", "checkout"} {
		for _, captured := range []bool{false, true} {
			name := source + "_uncaptured_conflicts_after_later_head"
			if captured {
				name = source + "_captured_original_recovers_after_later_head"
			}
			t.Run(name, func(t *testing.T) {
				f := originalStatusFixture(t, source)
				_, err := f.outbox.RetainPreparation(t.Context(), "current-worker", f.p)
				require.NoError(t, err)
				_, err = f.outbox.RetainEvidence(t.Context(), "current-worker", f.p, f.e)
				require.NoError(t, err)
				var first billing.SubscriptionStatus
				if captured {
					first, err = f.owner.CaptureVerifiedSubscriptionStatus(t.Context(), f.p, f.e)
					require.NoError(t, err)
				}
				f.clock.at = f.clock.at.Add(time.Minute)
				var next billing.SubscriptionStatusPreparation
				if source == "payment" {
					next, err = f.owner.PrepareSubscriptionStatus(t.Context(), "later-author", f.p.FactID)
				} else {
					next, err = f.owner.PrepareSubscriptionStatusForCheckout(t.Context(), "later-author", f.p.Scope, f.p.SubscriptionID)
				}
				require.NoError(t, err)
				require.NotEqual(t, f.p.CaptureID, next.CaptureID)
				laterEvidence := f.e
				laterEvidence.Status = "paused"
				_, err = f.owner.CaptureVerifiedSubscriptionStatus(t.Context(), next, laterEvidence)
				require.NoError(t, err)
				read, err := f.outbox.Find(t.Context(), "replacement-worker", f.p)
				require.NoError(t, err)
				require.Equal(t, f.p, read.Preparation)
				require.Equal(t, f.e, *read.Evidence)
				got, err := f.owner.CaptureVerifiedSubscriptionStatus(t.Context(), read.Preparation, *read.Evidence)
				if captured {
					require.NoError(t, err)
					require.Equal(t, first, got)
				} else {
					require.ErrorIs(t, err, billing.ErrRevenueConflict)
					require.Equal(t, billing.SubscriptionStatus{}, got)
				}
				// Validation is provenance, not an assertion that the old CAS is current.
				require.NoError(t, f.owner.ValidateSubscriptionStatusPreparation(t.Context(), f.p))
			})
		}
	}
}

func TestStatusOutboxCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name      string
		store     recordstore.Store
		validator StatusValidator
	}{
		{"nil_store", nil, &statusInputValidator{}}, {"typed_nil_store", (*inputStore)(nil), &statusInputValidator{}},
		{"nil_validator", &inputStore{}, nil}, {"typed_nil_validator", &inputStore{}, (*statusInputValidator)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := NewStatusOutbox(tc.store, tc.validator)
			require.ErrorIs(t, err, recordstore.ErrUnavailable)
			require.Nil(t, o)
		})
	}
}

func TestStatusOutboxRetainsLaterRevisionInputs(t *testing.T) {
	for _, source := range []string{"payment", "checkout"} {
		t.Run(source, func(t *testing.T) {
			f := originalStatusFixture(t, source)
			first, err := f.owner.CaptureVerifiedSubscriptionStatus(t.Context(), f.p, f.e)
			require.NoError(t, err)
			f.clock.at = f.clock.at.Add(time.Minute + time.Nanosecond)
			if source == "payment" {
				f.p, err = f.owner.PrepareSubscriptionStatus(t.Context(), "second-author", f.p.FactID)
			} else {
				f.p, err = f.owner.PrepareSubscriptionStatusForCheckout(t.Context(), "second-author", f.p.Scope, f.p.SubscriptionID)
			}
			require.NoError(t, err)
			require.Equal(t, first.Revision, f.p.ExpectedRevision)
			require.Equal(t, first.Fingerprint, f.p.ExpectedFingerprint)
			_, err = f.outbox.RetainPreparation(t.Context(), "current-worker", f.p)
			require.NoError(t, err)
			_, err = f.outbox.RetainEvidence(t.Context(), "current-worker", f.p, f.e)
			require.NoError(t, err)
			read, err := f.outbox.Find(t.Context(), "replacement-worker", f.p)
			require.NoError(t, err)
			require.Equal(t, f.p, read.Preparation)
			require.Equal(t, f.e, *read.Evidence)
			require.Equal(t, f.clock.at, read.Preparation.RequestedAt)
			id, partition := statusIdentity(f.p)
			require.Equal(t, id, f.store.record.ID)
			require.Equal(t, partition, f.store.record.Partition)
			captured, err := f.owner.CaptureVerifiedSubscriptionStatus(t.Context(), read.Preparation, *read.Evidence)
			require.NoError(t, err)
			require.Equal(t, first.Revision+1, captured.Revision)
		})
	}
}
