package billinglifecycle

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Fresh records per case emulate atomic callbacks only. Real encrypted Mongo
// persistence, ownership-manager interoperability and platform flows require
// separate integration qualification; this fixture does not certify them.
type inputStore struct {
	record           *recordstore.Record
	err, afterCommit error
	reads, writes    int
	inside           bool
	guard            string
	after            func()
	replaceErr       error
}

func (s *inputStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	s.reads++
	if s.err != nil {
		return s.err
	}
	s.inside = true
	defer func() { s.inside = false }()
	return fn(s)
}
func (s *inputStore) Transact(ctx context.Context, guard string, fn func(recordstore.Tx) error) error {
	defer func() {
		if s.after != nil {
			s.after()
		}
	}()
	s.writes++
	s.guard = guard
	if s.err != nil {
		return s.err
	}
	before := s.record
	s.inside = true
	err := fn(s)
	s.inside = false
	if err != nil {
		s.record = before
		return err
	}
	return s.afterCommit
}
func (s *inputStore) Get(context.Context, string, string) (recordstore.Record, error) {
	if s.record == nil {
		return recordstore.Record{}, recordstore.ErrNotFound
	}
	r := *s.record
	r.Data = append([]byte(nil), r.Data...)
	return r, nil
}
func (s *inputStore) Find(context.Context, recordstore.Query) ([]recordstore.Record, error) {
	panic("outbox uses exact identity only")
}
func (s *inputStore) Insert(_ context.Context, r recordstore.Record) error {
	if s.record != nil {
		return recordstore.ErrConflict
	}
	r.Data = append([]byte(nil), r.Data...)
	s.record = &r
	return nil
}
func (s *inputStore) Replace(_ context.Context, r recordstore.Record, expected int64) error {
	if s.replaceErr != nil {
		return s.replaceErr
	}
	if s.record == nil || s.record.Revision != expected || r.Revision != expected+1 {
		return recordstore.ErrConflict
	}
	r.Data = append([]byte(nil), r.Data...)
	s.record = &r
	return nil
}

type inputValidator struct {
	t             *testing.T
	store         *inputStore
	calls, denyAt int
	err           error
	original      billing.CheckoutIntent
}

func (v *inputValidator) ValidateCheckoutLifecycle(_ context.Context, actor string, i billing.CheckoutIntent) error {
	v.calls++
	require.False(v.t, v.store.inside, "no authority or owning I/O inside DB callback")
	if v.calls == v.denyAt {
		return v.err
	}
	if actor != "current-worker" {
		return partnermanager.ErrDenied
	}
	return i.ValidateAcknowledgedInput(v.original)
}

func inputFixture(t *testing.T) (*CheckoutOutbox, *inputStore, *inputValidator, billing.CheckoutIntent, paymentprovider.RevenueCheckoutEvidence) {
	t.Helper()
	scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
	q := paymentprovider.CheckoutSessionRequest{PriceID: "price_frozen", PlanID: "plan", CostID: "cost", UserID: "payer", UserReference: "payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14, IdempotencyKey: "key", Metadata: map[string]string{}}
	id := "checkout_" + digest([]any{scope, q.IdempotencyKey})
	q.Metadata["checkout_intent_id"] = id
	i := billing.CheckoutIntent{ID: id, Scope: scope, Request: q, CreatedAt: time.Unix(1700000000, 123456789).UTC(), SessionID: "cs_original", Fingerprint: digest([]any{scope, q})}
	require.NoError(t, i.ValidateAcknowledgedSubscription())
	e := paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: "payer", CustomerID: "cus_original", SubscriptionID: "sub_original", PriceID: q.PriceID, Currency: "GBP", Mode: q.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt.Add(time.Second)}
	require.NoError(t, i.ValidateLifecycleEvidence(e))
	s := &inputStore{}
	v := &inputValidator{t: t, store: s, original: i}
	o, err := NewCheckoutOutbox(s, v)
	require.NoError(t, err)
	return o, s, v, i, e
}

func TestCheckoutOutboxRecovery(t *testing.T) {
	outage := errors.New("storage outage")
	for _, tc := range []struct {
		name, scenario string
		want           error
	}{
		{"prepared_original_round_trip", "prepare", nil},
		{"provider_evidence_retained_before_capture", "evidence", nil},
		{"exact_evidence_replay_no_revision_advance", "replay", nil},
		{"preparation_retry_recovers_existing_evidence", "prepared-retry", nil},
		{"replacement_lookup_cannot_overwrite_original", "replace", recordstore.ErrConflict},
		{"evidence_without_preparation_unavailable", "missing", recordstore.ErrUnavailable},
		{"fresh_absence_is_currently_authorized", "absence", recordstore.ErrNotFound},
		{"storage_outage_preserves_cause", "outage", outage},
		{"joined_absence_outage_does_not_insert", "joined", outage},
		{"initial_denial_before_storage", "denied", partnermanager.ErrDenied},
		{"post_commit_denial_withholds_retained_payload", "post-denied", partnermanager.ErrDenied},
		{"absence_rechecks_current_authority", "absence-denied", partnermanager.ErrDenied},
		{"error_rechecks_current_authority", "error-denied", partnermanager.ErrDenied},
		{"uncertain_preparation_replays_original", "uncertain-prepare", nil},
		{"uncertain_evidence_replays_original", "uncertain-evidence", nil},
		{"uncertain_commit_and_revocation_preserve_both_causes", "uncertain-denied", recordstore.ErrUncertain},
		{"uncertain_commit_and_cancellation_preserve_both_causes", "uncertain-canceled", recordstore.ErrUncertain},
		{"storage_outage_then_cancellation_withholds_payload", "outage-canceled", context.Canceled},
		{"evidence_revision_conflict_preserves_preparation", "revision-conflict", recordstore.ErrConflict},
		{"different_creation_time_cannot_retarget", "changed-time", billing.ErrRevenueConflict},
		{"provider_input_wrong_session_rejected", "bad-evidence", billing.ErrRevenueUnassessable},
		{"canceled_before_storage", "canceled", context.Canceled},
		{"nil_context", "nil-context", recordstore.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, s, v, i, e := inputFixture(t)
			ctx := t.Context()
			var out CheckoutInput
			var err error
			switch tc.scenario {
			case "prepare":
				out, err = o.RetainPreparation(ctx, "current-worker", i)
			case "absence", "absence-denied":
				if tc.scenario == "absence-denied" {
					v.denyAt = 2
					v.err = partnermanager.ErrDenied
				}
				out, err = o.Find(ctx, "current-worker", i)
			case "outage", "joined", "error-denied":
				s.err = outage
				if tc.scenario == "joined" {
					s.err = errors.Join(recordstore.ErrNotFound, outage)
				}
				if tc.scenario == "error-denied" {
					v.denyAt = 2
					v.err = partnermanager.ErrDenied
				}
				out, err = o.RetainPreparation(ctx, "current-worker", i)
				require.Nil(t, s.record)
			case "denied", "post-denied", "uncertain-denied":
				v.denyAt = 1
				v.err = partnermanager.ErrDenied
				if tc.scenario != "denied" {
					v.denyAt = 2
				}
				if tc.scenario == "uncertain-denied" {
					s.afterCommit = recordstore.ErrUncertain
				}
				out, err = o.RetainPreparation(ctx, "current-worker", i)
				if tc.scenario == "denied" {
					require.Zero(t, s.writes)
				} else {
					require.NotNil(t, s.record)
				}
				if tc.scenario == "uncertain-denied" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
				}
			case "missing":
				out, err = o.RetainEvidence(ctx, "current-worker", i, e)
				require.Nil(t, s.record)
			case "uncertain-canceled", "outage-canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
				s.after = cancel
				if tc.scenario == "uncertain-canceled" {
					s.afterCommit = recordstore.ErrUncertain
				} else {
					s.err = outage
				}
				out, err = o.RetainPreparation(ctx, "current-worker", i)
				require.ErrorIs(t, err, context.Canceled)
				if tc.scenario == "uncertain-canceled" {
					require.NotNil(t, s.record)
				} else {
					require.Nil(t, s.record)
				}
			case "revision-conflict":
				_, err = o.RetainPreparation(ctx, "current-worker", i)
				require.NoError(t, err)
				before := append([]byte(nil), s.record.Data...)
				s.replaceErr = recordstore.ErrConflict
				out, err = o.RetainEvidence(ctx, "current-worker", i, e)
				require.Equal(t, before, []byte(s.record.Data))
				require.EqualValues(t, 1, s.record.Revision)
			case "changed-time":
				i.CreatedAt = i.CreatedAt.Add(time.Nanosecond)
				out, err = o.RetainPreparation(ctx, "current-worker", i)
				require.Zero(t, s.writes)
			case "canceled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				out, err = o.RetainPreparation(cancelled, "current-worker", i)
				require.Zero(t, s.writes)
			case "nil-context":
				var nilCtx context.Context
				out, err = o.RetainPreparation(nilCtx, "current-worker", i)
			case "uncertain-prepare":
				s.afterCommit = recordstore.ErrUncertain
				first, unknown := o.RetainPreparation(ctx, "current-worker", i)
				require.ErrorIs(t, unknown, recordstore.ErrUncertain)
				require.Equal(t, CheckoutInput{}, first)
				s.afterCommit = nil
				out, err = o.RetainPreparation(ctx, "current-worker", i)
				require.EqualValues(t, 1, s.record.Revision)
			default:
				_, err = o.RetainPreparation(ctx, "current-worker", i)
				require.NoError(t, err)
				if tc.scenario == "bad-evidence" {
					e.SessionID = "cs_wrong"
				}
				if tc.scenario == "uncertain-evidence" {
					s.afterCommit = recordstore.ErrUncertain
				}
				out, err = o.RetainEvidence(ctx, "current-worker", i, e)
				if tc.scenario == "uncertain-evidence" {
					require.ErrorIs(t, err, recordstore.ErrUncertain)
					require.Equal(t, CheckoutInput{}, out)
					s.afterCommit = nil
					out, err = o.RetainEvidence(ctx, "current-worker", i, e)
				}
				if tc.scenario == "bad-evidence" {
					require.EqualValues(t, 1, s.record.Revision)
					break
				}
				require.NoError(t, err)
				saved := append([]byte(nil), s.record.Data...)
				switch tc.scenario {
				case "replay":
					out, err = o.RetainEvidence(ctx, "current-worker", i, e)
				case "prepared-retry":
					out, err = o.RetainPreparation(ctx, "current-worker", i)
				case "replace":
					e.CreatedAt = e.CreatedAt.Add(time.Second)
					out, err = o.RetainEvidence(ctx, "current-worker", i, e)
				}
				require.Equal(t, saved, []byte(s.record.Data))
				require.EqualValues(t, 2, s.record.Revision)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, CheckoutInput{}, out)
				return
			}
			require.NoError(t, err)
			require.NoError(t, out.Intent.ValidateAcknowledgedInput(i))
			require.Equal(t, i.Request, out.Intent.Request)
			require.Equal(t, i.CreatedAt, out.Intent.CreatedAt)
			if tc.scenario != "prepare" && tc.scenario != "uncertain-prepare" {
				require.NotNil(t, out.Evidence)
				require.Equal(t, e, *out.Evidence)
			}
			require.Nil(t, s.record.ExpiresAt)
			require.Zero(t, s.record.Sequence)
			require.Equal(t, checkoutKind+":"+s.record.ID, s.guard)
			out.Intent.Request.Metadata["checkout_intent_id"] = "caller-mutated"
			read, readErr := o.Find(ctx, "current-worker", i)
			require.NoError(t, readErr)
			require.Equal(t, i.ID, read.Intent.Request.Metadata["checkout_intent_id"])
		})
	}
}

func TestCheckoutOutboxStoredIntegrity(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"wrong_kind", "kind"}, {"wrong_identity", "id"}, {"wrong_partition", "partition"},
		{"expiry_forbidden", "expiry"}, {"sequence_forbidden", "sequence"}, {"future_revision", "revision"},
		{"state_disagrees_with_payload", "state"}, {"unknown_schema", "schema"}, {"unknown_field", "field"},
		{"missing_private_request", "request"}, {"corrupt_private_fingerprint", "fingerprint"}, {"trailing_json", "trailing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, s, _, i, _ := inputFixture(t)
			_, err := o.RetainPreparation(t.Context(), "current-worker", i)
			require.NoError(t, err)
			switch tc.mutation {
			case "kind":
				s.record.Kind = "wrong"
			case "id":
				s.record.ID = "wrong"
			case "partition":
				s.record.Partition = "wrong"
			case "expiry":
				at := time.Now()
				s.record.ExpiresAt = &at
			case "sequence":
				s.record.Sequence = 1
			case "revision":
				s.record.Revision = 3
			case "state":
				s.record.State = "evidence"
			case "trailing":
				s.record.Data = append(s.record.Data, []byte(" {}")...)
			default:
				var p checkoutPayload
				require.NoError(t, s.record.Decode(&p))
				switch tc.mutation {
				case "schema":
					p.Schema = 2
				case "request":
					p.Request = paymentprovider.CheckoutSessionRequest{}
				case "fingerprint":
					p.Fingerprint = "corrupt"
				}
				r, err := recordstore.NewRecord(s.record.Kind, s.record.ID, s.record.Partition, 1, p)
				require.NoError(t, err)
				s.record.Data = r.Data
				if tc.mutation == "field" {
					s.record.Data = append(s.record.Data[:len(s.record.Data)-1], []byte(",\"Unexpected\":true}")...)
				}
			}
			before := *s.record
			out, err := o.Find(t.Context(), "current-worker", i)
			require.Error(t, err)
			require.Equal(t, CheckoutInput{}, out)
			require.True(t, reflect.DeepEqual(before, *s.record))
		})
	}
}

func TestCheckoutOutboxAbsenceAndCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		absent bool
	}{
		{"sole", recordstore.ErrNotFound, true}, {"wrapped", fmt.Errorf("read: %w", recordstore.ErrNotFound), true},
		{"joined", errors.Join(recordstore.ErrNotFound, errors.New("outage")), false}, {"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.absent, soleNotFound(tc.err)) })
	}
	for _, tc := range []struct {
		name      string
		store     recordstore.Store
		validator CheckoutValidator
	}{
		{"nil_store", nil, &inputValidator{}}, {"typed_nil_store", (*inputStore)(nil), &inputValidator{}},
		{"nil_validator", &inputStore{}, nil}, {"typed_nil_validator", &inputStore{}, (*inputValidator)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := NewCheckoutOutbox(tc.store, tc.validator)
			require.ErrorIs(t, err, recordstore.ErrUnavailable)
			require.Nil(t, o)
		})
	}
}
