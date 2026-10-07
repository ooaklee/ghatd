package partnermanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named boundary cases distinguish conclusive owning proof
// from absence, outages, pauses and retained evidence; private identity fields
// participate in replay even though public transport JSON omits them.
type signupFeedStub struct {
	SignupFeed
	capture user.SignupAttribution
	err     error
	calls   int
}

func (s *signupFeedStub) GetSignupAttribution(context.Context, string) (user.SignupAttribution, error) {
	s.calls++
	return s.capture, s.err
}

type revenueFeedStub struct {
	RevenueFeed
	ack   billing.RevenueAcknowledgement
	err   error
	calls int
}

func (s *revenueFeedStub) GetRevenueAcknowledgement(context.Context, string, string) (billing.RevenueAcknowledgement, error) {
	s.calls++
	return s.ack, s.err
}

type sourceReconcilerStub struct{ RevenueSourceReconciler }

func workerCapture() user.SignupAttribution {
	at := time.Date(2026, 10, 7, 10, 0, 0, 123456789, time.UTC)
	return user.SignupAttribution{ProgramID: partnerprogram.ProgramID, CustomerID: "payer", CreatedAt: at, CreatedAtUTC: at.Format(time.RFC3339Nano), Individual: true, State: "pending"}
}

func TestWorkerNoEntitlementRequiresConclusiveOwningEvidence(t *testing.T) {
	cases := []struct {
		name, outcome, reason string
		processErr, readErr   error
		state                 string
		adjustment, before    bool
		originalProof         bool
		want                  error
	}{
		{name: "pending_capture_is_retryable", processErr: referral.ErrNotFound, want: partnerearnings.ErrUnresolved},
		{name: "consumed_attribution_without_binding_is_retryable", processErr: referral.ErrNotFound, state: "consumed", outcome: "attributed", want: partnerearnings.ErrUnresolved},
		{name: "quarantined_capture_is_retryable", processErr: referral.ErrNotFound, state: "consumed", outcome: "quarantined", want: partnerearnings.ErrUnresolved},
		{name: "owning_no_evidence_receipt_proves_refusal", processErr: referral.ErrNotFound, state: "consumed", outcome: "no_evidence", reason: "signup_no_evidence"},
		{name: "owning_ineligible_receipt_proves_refusal", processErr: referral.ErrNotFound, state: "consumed", outcome: "ineligible", reason: "signup_ineligible"},
		{name: "payment_before_owning_creation", processErr: referral.ErrNotFound, before: true, reason: "paid_before_signup"},
		{name: "historical_capture_absence_is_not_refusal", processErr: referral.ErrNotFound, readErr: user.ErrUserNotFound, want: user.ErrUserNotFound},
		{name: "joined_referral_absence_outage_is_not_refusal", processErr: errors.Join(referral.ErrNotFound, ErrUnavailable), want: ErrUnavailable},
		{name: "generic_manager_absence_is_not_refusal", processErr: ErrNotFound, want: ErrNotFound},
		{name: "frozen_plan_refusal_is_conclusive", processErr: &NoEntitlementError{ReasonCode: "ineligible_plan"}, reason: "ineligible_plan"},
		{name: "joined_refusal_outage_is_not_refusal", processErr: errors.Join(&NoEntitlementError{ReasonCode: "ineligible_plan"}, ErrUnavailable), want: ErrUnavailable},
		{name: "paused_accrual_remains_pending", processErr: ErrDenied, want: ErrDenied},
		{name: "currency_requires_review", processErr: partnerearnings.ErrCurrencyMismatch, want: partnerearnings.ErrCurrencyMismatch},
		{name: "adjustment_inherits_original_owning_refusal", processErr: partnerearnings.ErrUnresolved, adjustment: true, originalProof: true, reason: "original_no_entitlement"},
		{name: "adjustment_without_original_receipt_is_pending", processErr: partnerearnings.ErrUnresolved, adjustment: true, want: partnerearnings.ErrUnresolved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := workerCapture()
			if tc.state != "" {
				capture.State = tc.state
				capture.Consumption = &user.SignupConsumption{ReceiptID: "owning-signup-decision", Outcome: tc.outcome, ActorID: "previous-worker"}
			}
			signups := &signupFeedStub{capture: capture, err: tc.readErr}
			feed := &revenueFeedStub{err: billing.ErrRevenueNotFound}
			fact := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account"}, Kind: billing.RevenuePayment, PaymentID: "payment", InvoiceID: "invoice", AllocationID: "allocation", PrincipalID: "payer", EffectiveAt: capture.CreatedAt.Add(time.Hour)}
			if tc.before {
				fact.EffectiveAt = capture.CreatedAt.Add(-time.Nanosecond)
			}
			if tc.adjustment {
				fact.Kind = billing.RevenueRefund
			}
			if tc.originalProof {
				feed.err = nil
				feed.ack = billing.RevenueAcknowledgement{ConsumerID: "consumer", FactID: fact.PaymentFactID(), AcceptanceID: "durable-original-decision", Outcome: WorkNoEntitlement}
			}
			w := &Worker{queue: &WorkQueue{config: WorkQueueConfig{ProgramID: partnerprogram.ProgramID}}, signups: signups, revenue: feed, config: WorkerConfig{ConsumerID: "consumer"}}
			reason, err := w.refusedRevenue(context.Background(), fact, tc.processErr)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.reason, reason)
			if tc.want != nil {
				require.Empty(t, reason)
			}
		})
	}
}

func TestWorkerPrivateSignupFingerprint(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*user.SignupAttribution)
		want  error
		same  bool
	}{
		{name: "private_evidence_changes_identity", alter: func(c *user.SignupAttribution) { c.Evidence = "different-private-token" }},
		{name: "owning_customer_changes_identity", alter: func(c *user.SignupAttribution) { c.CustomerID = "different-payer" }},
		{name: "immutable_type_changes_identity", alter: func(c *user.SignupAttribution) { c.Individual = false }},
		{name: "consumption_does_not_rewrite_source", alter: func(c *user.SignupAttribution) {
			c.State = "consumed"
			c.Consumption = &user.SignupConsumption{ReceiptID: "receipt", Outcome: "attributed"}
		}, same: true},
		{name: "creation_precision_mismatch_is_denied", alter: func(c *user.SignupAttribution) { c.CreatedAt = c.CreatedAt.Truncate(time.Millisecond) }, want: ErrUnavailable},
		{name: "wrong_program_is_denied", alter: func(c *user.SignupAttribution) { c.ProgramID = "other-program" }, want: ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := workerCapture()
			original, err := signupCandidate(capture, partnerprogram.ProgramID)
			require.NoError(t, err)
			tc.alter(&capture)
			changed, err := signupCandidate(capture, partnerprogram.ProgramID)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				return
			}
			if tc.same {
				require.Equal(t, original, changed)
			} else {
				require.NotEqual(t, original.SourceFingerprint, changed.SourceFingerprint)
			}
		})
	}
}

func TestWorkerRequiredCapabilitiesAndCancellation(t *testing.T) {
	cases := []struct {
		name                  string
		nilSignup, badBounds  bool
		nilContext, cancelled bool
		wrongProgram          bool
		want                  error
	}{
		{name: "typed_nil_signup_owner", nilSignup: true, want: ErrUnavailable},
		{name: "batch_exceeds_bound", badBounds: true, want: ErrInvalid},
		{name: "incompatible_program", wrongProgram: true, want: ErrInvalid},
		{name: "nil_context_does_not_call_authority", nilContext: true, want: ErrInvalid},
		{name: "cancelled_context_does_not_call_authority", cancelled: true, want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, _, _, _, authority, _, _ := managerFixture(t)
			var signup *signupFeedStub
			if !tc.nilSignup {
				signup = &signupFeedStub{}
			}
			queue, err := NewWorkQueue(&workRepositorySpy{}, managerClock{at: time.Now().UTC()}, WorkQueueConfig{ProgramID: partnerprogram.ProgramID, LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour})
			require.NoError(t, err)
			if tc.wrongProgram {
				queue.config.ProgramID = "other"
			}
			cfg := WorkerConfig{ActorID: "worker", ConsumerID: "consumer", PageSize: 100, BatchSize: 100}
			if tc.badBounds {
				cfg.BatchSize = 201
			}
			worker, err := NewWorker(manager, queue, signup, &revenueFeedStub{}, &sourceReconcilerStub{}, cfg)
			if tc.nilContext || tc.cancelled {
				require.NoError(t, err)
				ctx := context.Background()
				if tc.nilContext {
					ctx = nil
				}
				if tc.cancelled {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				_, err = worker.RunOnce(ctx)
			}
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, authority.calls)
		})
	}
}
