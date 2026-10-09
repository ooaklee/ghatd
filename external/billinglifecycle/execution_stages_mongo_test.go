package billinglifecycle

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Actual configured managers, native grants/bound contexts, owning billing and
// encrypted transactions. Identity/provider are controlled; no installed worker
// or platform is claimed. Each row has a disposable database and fresh fixtures.
func TestCheckoutExecutionEncryptedStages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		faultAt int
		lose    bool
		late    string
		want    error
	}{
		{name: "fresh_lookup_retained_before_capture"}, {name: "existing_preparation"}, {name: "retained_evidence_no_get"}, {name: "native_receipt_first_no_get"},
		{name: "missing_bound_input_is_not_repaired", want: recordstore.ErrUnavailable},
		{name: "early_revocation_no_lookup", want: partnermanager.ErrDenied},
		{name: "lost_preparation_stops_before_lookup", faultAt: 1, lose: true, want: recordstore.ErrUncertain},
		{name: "lost_evidence_stops_before_capture", faultAt: 2, lose: true, want: recordstore.ErrUncertain},
		{name: "lost_capture_recovers_native_receipt", faultAt: 3, lose: true, want: billing.ErrRevenueUncertain},
		{name: "lost_capture_then_revoked_retains_uncertainty", faultAt: 3, lose: true, late: "denied", want: billing.ErrRevenueUncertain},
		{name: "lost_capture_then_canceled_retains_uncertainty", faultAt: 3, lose: true, late: "canceled", want: billing.ErrRevenueUncertain},
		{name: "acknowledged_capture_then_revoked_is_known", faultAt: 3, late: "denied", want: partnermanager.ErrDenied},
		{name: "acknowledged_capture_then_lease_expires", faultAt: 3, late: "expired", want: recordstore.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, j := nativeCheckoutBindingFixture(t)
			b, ctx := nativeCheckoutBinder(t, f, r, clock, "worker-original")
			m := b.validator.(*billingmanager.Service)
			outbox, err := NewCheckoutOutbox(f.reply, m)
			require.NoError(t, err)
			x, err := NewCheckoutExecution(b.scheduler, r, outbox)
			require.NoError(t, err)
			if tc.name == "existing_preparation" || tc.name == "retained_evidence_no_get" || tc.name == "missing_bound_input_is_not_repaired" {
				bound, e := x.binder.Bind(ctx, JobLease(j), CheckoutInput{Intent: f.intent})
				require.NoError(t, e)
				j = bound.Job
			}
			if tc.name == "retained_evidence_no_get" {
				e, eErr := m.LookupCheckoutLifecycleEvidence(ctx, "worker-original", f.intent)
				require.NoError(t, eErr)
				bound, eErr := x.binder.Bind(ctx, JobLease(j), CheckoutInput{Intent: f.intent, Evidence: &e})
				require.NoError(t, eErr)
				j = bound.Job
			}
			if tc.name == "native_receipt_first_no_get" {
				_, err = m.CaptureCheckoutLifecycleEvidence(ctx, "worker-original", f.intent, f.provider.evidence)
				require.NoError(t, err)
			}
			if tc.name == "missing_bound_input_is_not_repaired" {
				id, _ := checkoutIdentity(f.intent)
				_, err = f.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": checkoutKind, "id": id})
				require.NoError(t, err)
			}
			if tc.name == "early_revocation_no_lookup" {
				f.revoke(t)
			}
			cancel := func() {}
			if tc.late == "canceled" {
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
			}
			count := 0
			if tc.faultAt > 0 {
				f.reply.after = func() {
					count++
					if count != tc.faultAt {
						return
					}
					f.reply.lose = tc.lose
					if tc.late == "denied" {
						f.revoke(t)
					}
					if tc.late == "canceled" {
						cancel()
					}
					if tc.late == "expired" {
						clock.set(j.LeasedUntil)
					}
				}
			}
			before := f.provider.calls
			observed, err := x.Observe(ctx, JobLease(j))
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, CheckoutObservation{}, observed)
				if tc.late == "denied" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
				}
				if tc.late == "canceled" {
					require.ErrorIs(t, err, context.Canceled)
				}
				if !tc.lose {
					require.NotErrorIs(t, err, billing.ErrRevenueUncertain)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, observed.Input.Evidence)
				require.NoError(t, observed.Receipt.ValidateCapturedEvidence(f.intent, *observed.Input.Evidence))
				require.Equal(t, ColdLane, observed.Job.Lane, "observation does not retire the job")
				require.NoError(t, observed.Job.Source.Checkout.ValidateAcknowledgedInput(f.intent))
			}
			wantGets := 1
			if tc.name == "native_receipt_first_no_get" || tc.name == "retained_evidence_no_get" || tc.name == "missing_bound_input_is_not_repaired" || tc.name == "early_revocation_no_lookup" || tc.faultAt == 1 {
				wantGets = 0
			}
			require.Equal(t, before+wantGets, f.provider.calls)
			// Every injected unknown stage stops there; restore authority, inspect the
			// durable job, then acquire a replacement lease and resume exact inputs.
			f.reply.after = nil
			f.reply.lose = false
			if tc.faultAt > 0 {
				require.Equal(t, tc.faultAt, count)
				if tc.late == "denied" {
					f.restore(t)
				}
				inspect := context.WithoutCancel(f.ctx)
				current, e := r.ReadJob(inspect, j.Source)
				require.NoError(t, e)
				clock.set(current.LeasedUntil)
				g := f.grant
				g.Subject.ID = "worker-replacement"
				g.Revision = 0
				_, e = f.policy.ReplaceGrant(inspect, g, 0)
				require.NoError(t, e)
				replacement, e := r.Acquire(inspect, LeaseRequest{Source: current.Source, ExpectedRevision: current.Revision, Actor: "worker-replacement", Token: "replacement-stage-token", Until: clock.at.Add(time.Minute)})
				require.NoError(t, e)
				rb, rctx := nativeCheckoutBinder(t, f, r, clock, "worker-replacement")
				ro, e := NewCheckoutOutbox(f.reply, rb.validator)
				require.NoError(t, e)
				rx, e := NewCheckoutExecution(rb.scheduler, r, ro)
				require.NoError(t, e)
				gets := f.provider.calls
				recovered, e := rx.Observe(rctx, JobLease(replacement))
				require.NoError(t, e)
				require.NoError(t, recovered.Receipt.ValidateCapturedEvidence(f.intent, *recovered.Input.Evidence))
				if tc.faultAt == 1 {
					require.Equal(t, gets+1, f.provider.calls)
				} else {
					require.Equal(t, gets, f.provider.calls)
				}
			}
			n, e := f.db.Collection("ghatd_owned_records").CountDocuments(context.WithoutCancel(f.ctx), bson.M{"kind": "billing_revenue_fact"})
			require.NoError(t, e)
			require.Zero(t, n)
		})
	}
}

func TestStatusExecutionEncryptedStages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		faultAt int
		lose    bool
		late    string
		want    error
	}{
		{name: "fresh_pre_payment_trial"}, {name: "retained_preparation"}, {name: "retained_evidence_no_get"}, {name: "captured_original_no_get"},
		{name: "old_receipt_after_later_head_no_get"}, {name: "missing_attached_input_is_not_repaired", want: recordstore.ErrUnavailable},
		{name: "unresolved_old_head_conflict_conserves_original", want: billing.ErrRevenueConflict},
		{name: "lost_preparation_stops_before_lookup", faultAt: 1, lose: true, want: recordstore.ErrUncertain},
		{name: "lost_evidence_stops_before_capture", faultAt: 2, lose: true, want: recordstore.ErrUncertain},
		{name: "lost_capture_exact_original_no_get", faultAt: 3, lose: true, want: billing.ErrRevenueUncertain},
		{name: "lost_capture_then_revoked", faultAt: 3, lose: true, late: "denied", want: billing.ErrRevenueUncertain},
		{name: "lost_capture_then_canceled", faultAt: 3, lose: true, late: "canceled", want: billing.ErrRevenueUncertain},
		{name: "acknowledged_capture_then_lease_expires", faultAt: 3, late: "expired", want: recordstore.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, j := nativeStatusBindingFixture(t)
			b, ctx := nativeStatusBinder(t, f, r, clock, "worker-original")
			m := b.validator.(*billingmanager.Service)
			outbox, err := NewStatusOutbox(f.base.reply, m)
			require.NoError(t, err)
			x, err := NewStatusExecution(b.scheduler, r, outbox)
			require.NoError(t, err)
			if tc.name == "retained_preparation" || tc.name == "retained_evidence_no_get" || tc.name == "captured_original_no_get" || tc.name == "old_receipt_after_later_head_no_get" || tc.name == "missing_attached_input_is_not_repaired" || tc.name == "unresolved_old_head_conflict_conserves_original" {
				bound, e := x.binder.Bind(ctx, JobLease(j), StatusInput{Preparation: f.p})
				require.NoError(t, e)
				j = bound.Job
			}
			if tc.name == "retained_evidence_no_get" || tc.name == "captured_original_no_get" || tc.name == "old_receipt_after_later_head_no_get" || tc.name == "unresolved_old_head_conflict_conserves_original" {
				evidence, e := m.LookupSubscriptionStatus(ctx, "worker-original", f.p)
				require.NoError(t, e)
				bound, e := x.binder.Bind(ctx, JobLease(j), StatusInput{Preparation: f.p, Evidence: &evidence})
				require.NoError(t, e)
				j = bound.Job
				if tc.name == "captured_original_no_get" || tc.name == "old_receipt_after_later_head_no_get" {
					_, e = m.CaptureSubscriptionStatus(ctx, "worker-original", f.p, evidence)
					require.NoError(t, e)
				}
				if tc.name == "old_receipt_after_later_head_no_get" || tc.name == "unresolved_old_head_conflict_conserves_original" {
					later, e := m.PrepareSubscriptionStatusForCheckout(ctx, "worker-original", f.p.Scope, f.p.SubscriptionID)
					require.NoError(t, e)
					// A different original must come from the owner. Advance its native clock
					// by replacing the native service, never reconstructing capture identity.
					if later.CaptureID == f.p.CaptureID {
						repo, e := newExecutionStatusOwner(t, f)
						require.NoError(t, e)
						f.owner = repo
						b, ctx = nativeStatusBinder(t, f, r, clock, "worker-original")
						m = b.validator.(*billingmanager.Service)
						outbox, e = NewStatusOutbox(f.base.reply, m)
						require.NoError(t, e)
						x, e = NewStatusExecution(b.scheduler, r, outbox)
						require.NoError(t, e)
						later, e = m.PrepareSubscriptionStatusForCheckout(ctx, "worker-original", f.p.Scope, f.p.SubscriptionID)
						require.NoError(t, e)
					}
					require.NotEqual(t, f.p.CaptureID, later.CaptureID)
					active := evidence
					active.Status = "active"
					_, e = m.CaptureSubscriptionStatus(ctx, "worker-original", later, active)
					require.NoError(t, e)
				}
			}
			if tc.name == "missing_attached_input_is_not_repaired" {
				id, _ := statusIdentity(f.p)
				_, err = f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": statusKind, "id": id})
				require.NoError(t, err)
			}
			cancel := func() {}
			if tc.late == "canceled" {
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
			}
			count := 0
			if tc.faultAt > 0 {
				f.base.reply.after = func() {
					count++
					if count != tc.faultAt {
						return
					}
					f.base.reply.lose = tc.lose
					if tc.late == "denied" {
						f.base.revoke(t)
					}
					if tc.late == "canceled" {
						cancel()
					}
					if tc.late == "expired" {
						clock.set(j.LeasedUntil)
					}
				}
			}
			gets := f.provider.calls
			out, err := x.Observe(ctx, JobLease(j))
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, StatusObservation{}, out)
			} else {
				require.NoError(t, err)
				require.NoError(t, out.Receipt.Validate())
				require.NotNil(t, out.Job.OriginalStatus)
				require.True(t, sameStatusPreparation(out.Input.Preparation, out.Receipt.Preparation))
				require.Equal(t, ColdLane, out.Job.Lane)
			}
			if tc.late == "denied" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			if tc.late == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			if !tc.lose {
				require.NotErrorIs(t, err, billing.ErrRevenueUncertain)
			}
			wantGets := 1
			switch tc.name {
			case "retained_evidence_no_get", "captured_original_no_get", "old_receipt_after_later_head_no_get", "missing_attached_input_is_not_repaired", "unresolved_old_head_conflict_conserves_original":
				wantGets = 0
			}
			if tc.faultAt == 1 {
				wantGets = 0
			}
			require.Equal(t, gets+wantGets, f.provider.calls)
			f.base.reply.after = nil
			f.base.reply.lose = false
			if tc.name == "unresolved_old_head_conflict_conserves_original" {
				stored, e := r.ReadJob(ctx, j.Source)
				require.NoError(t, e)
				require.True(t, sameStatusPreparation(f.p, *stored.OriginalStatus))
			}
			if tc.faultAt > 0 {
				require.Equal(t, tc.faultAt, count)
				if tc.late == "denied" {
					f.base.restore(t)
				}
				inspect := context.WithoutCancel(f.base.ctx)
				current, e := r.ReadJob(inspect, j.Source)
				require.NoError(t, e)
				require.NotNil(t, current.OriginalStatus)
				clock.set(current.LeasedUntil)
				g := f.base.grant
				g.Subject.ID = "worker-replacement"
				g.Revision = 0
				_, e = f.base.policy.ReplaceGrant(inspect, g, 0)
				require.NoError(t, e)
				replacement, e := r.Acquire(inspect, LeaseRequest{Source: current.Source, ExpectedRevision: current.Revision, Actor: "worker-replacement", Token: "replacement-status-token", Until: clock.at.Add(time.Minute)})
				require.NoError(t, e)
				rb, rctx := nativeStatusBinder(t, f, r, clock, "worker-replacement")
				ro, e := NewStatusOutbox(f.base.reply, rb.validator)
				require.NoError(t, e)
				rx, e := NewStatusExecution(rb.scheduler, r, ro)
				require.NoError(t, e)
				before := f.provider.calls
				recovered, e := rx.Observe(rctx, JobLease(replacement))
				require.NoError(t, e)
				require.True(t, sameStatusPreparation(*current.OriginalStatus, recovered.Receipt.Preparation))
				require.Equal(t, "worker-original", recovered.Receipt.Preparation.ActorID)
				if tc.faultAt == 1 {
					require.Equal(t, before+1, f.provider.calls)
				} else {
					require.Equal(t, before, f.provider.calls)
				}
			}
			n, e := f.base.db.Collection("ghatd_owned_records").CountDocuments(context.WithoutCancel(f.base.ctx), bson.M{"kind": "billing_revenue_fact"})
			require.NoError(t, e)
			require.Zero(t, n)
		})
	}
}

func newExecutionStatusOwner(t *testing.T, f *statusNativeFixture) (*billing.RevenueService, error) {
	t.Helper()
	repo, err := revenuestore.NewRepository(f.base.reply)
	if err != nil {
		return nil, err
	}
	return billing.NewRevenueService(repo, inputNativeClock{f.base.clock.at.Add(time.Nanosecond)})
}

// Controlled owning outcomes are injected around real manager reads. This
// separates stage conservation from authenticated provider/network proof.
type executionCheckoutOutcome struct {
	*billingmanager.Service
	receiptErr error
	badReceipt bool
	afterRead  func()
}

func (o *executionCheckoutOutcome) FindCheckoutLifecycleReceipt(ctx context.Context, actor string, i billing.CheckoutIntent) (billing.CheckoutLifecycleAnchor, error) {
	out, err := o.Service.FindCheckoutLifecycleReceipt(ctx, actor, i)
	if o.badReceipt {
		out.IntentID = "unrelated-intent"
	}
	if o.afterRead != nil {
		o.afterRead()
	}
	if o.receiptErr != nil {
		return billing.CheckoutLifecycleAnchor{}, o.receiptErr
	}
	return out, err
}

func TestCheckoutExecutionReceiptBoundary(t *testing.T) {
	outage := errors.New("controlled receipt outage")
	for _, tc := range []struct {
		name string
		want error
		gets int
	}{
		{"wrapped_sole_absence_allows_one_lookup", nil, 1},
		{"joined_absence_outage_never_lookup", outage, 0},
		{"unknown_receipt_read_then_lease_loss", billing.ErrRevenueUncertain, 0},
		{"store_unknown_receipt_then_lease_loss", recordstore.ErrUncertain, 0},
		{"retained_evidence_differs_from_native_receipt", billing.ErrRevenueConflict, 0},
		{"malformed_native_receipt_withheld", billing.ErrRevenueUnavailable, 0},
		{"receipt_read_then_revoked_no_binding", partnermanager.ErrDenied, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, j := nativeCheckoutBindingFixture(t)
			b, ctx := nativeCheckoutBinder(t, f, r, clock, "worker-original")
			native := b.validator.(*billingmanager.Service)
			o := &executionCheckoutOutcome{Service: native}
			switch tc.name {
			case "wrapped_sole_absence_allows_one_lookup":
				o.receiptErr = fmt.Errorf("controlled owning read: %w", billing.ErrRevenueNotFound)
			case "joined_absence_outage_never_lookup":
				o.receiptErr = errors.Join(billing.ErrRevenueNotFound, outage)
			case "unknown_receipt_read_then_lease_loss":
				o.receiptErr = billing.ErrRevenueUncertain
				o.afterRead = func() { clock.set(j.LeasedUntil) }
			case "store_unknown_receipt_then_lease_loss":
				o.receiptErr = recordstore.ErrUncertain
				o.afterRead = func() { clock.set(j.LeasedUntil) }
			case "retained_evidence_differs_from_native_receipt":
				_, e := native.CaptureCheckoutLifecycleEvidence(ctx, "worker-original", f.intent, f.provider.evidence)
				require.NoError(t, e)
				bound, e := b.Bind(ctx, JobLease(j), CheckoutInput{Intent: f.intent})
				require.NoError(t, e)
				j = bound.Job
				changed := f.provider.evidence
				changed.CreatedAt = changed.CreatedAt.Add(time.Second)
				bound, e = b.Bind(ctx, JobLease(j), CheckoutInput{Intent: f.intent, Evidence: &changed})
				require.NoError(t, e)
				j = bound.Job
			case "malformed_native_receipt_withheld":
				_, err := native.CaptureCheckoutLifecycleEvidence(ctx, "worker-original", f.intent, f.provider.evidence)
				require.NoError(t, err)
				o.badReceipt = true
			case "receipt_read_then_revoked_no_binding":
				o.afterRead = func() { f.revoke(t) }
			}
			outbox, err := NewCheckoutOutbox(f.reply, o)
			require.NoError(t, err)
			x, err := NewCheckoutExecution(b.scheduler, r, outbox)
			require.NoError(t, err)
			out, err := x.Observe(ctx, JobLease(j))
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, CheckoutObservation{}, out)
			} else {
				require.NoError(t, err)
			}
			if tc.name == "unknown_receipt_read_then_lease_loss" || tc.name == "store_unknown_receipt_then_lease_loss" {
				require.ErrorIs(t, err, recordstore.ErrConflict)
			}
			require.Equal(t, tc.gets, f.provider.calls)
			if tc.want != nil {
				current, e := r.ReadJob(context.WithoutCancel(f.ctx), j.Source)
				require.NoError(t, e)
				require.Equal(t, j, current, "receipt failures must not bind or retire work")
			}
		})
	}
}

func TestStatusExecutionPaidLifecycle(t *testing.T) {
	for _, state := range []string{"active", "canceled", "trialing"} {
		t.Run(state, func(t *testing.T) {
			f := nativeStatusFixture(t)
			observation, err := f.owner.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: f.p.Scope, EnvelopeID: "evt_first_paid", Facts: []billing.RevenueFact{{Scope: f.p.Scope, Kind: billing.RevenuePayment, PaymentID: "pi_first", InvoiceID: "in_first", AllocationID: "il_first", PrincipalID: f.p.PrincipalID, ProviderCustomerID: f.p.ProviderCustomerID, SubscriptionID: f.p.SubscriptionID, PlanID: f.base.intent.Request.PlanID, CostID: f.base.intent.Request.CostID, Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: f.base.clock.at}}})
			require.NoError(t, err)
			clock := &executionTestClock{at: f.base.clock.at}
			r, err := NewRecordExecutionRepository(f.base.reply, clock)
			require.NoError(t, err)
			source := ScheduledSource{Scope: f.p.Scope, Kind: billing.LifecycleSubscriptionSources, PrincipalID: f.p.PrincipalID, SubscriptionID: f.p.SubscriptionID, FactID: observation.FactIDs[0], SourceID: billing.LifecycleDiscoverySourceID(f.p.Scope, billing.LifecycleSubscriptionSources, f.p.SubscriptionID)}
			j := ScheduledJob{Source: source, Revision: 1, Lane: ColdLane, CreatedAt: clock.at, NextAttemptAt: clock.at}
			require.NoError(t, r.CommitScan(f.ctx, ScheduleCommit{Scope: source.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, j}}}))
			j, err = r.Acquire(f.ctx, LeaseRequest{Source: source, ExpectedRevision: 1, Actor: "worker-original", Token: "paid-lifecycle-token", Until: clock.at.Add(time.Minute)})
			require.NoError(t, err)
			b, ctx := nativeStatusBinder(t, f, r, clock, "worker-original")
			outbox, err := NewStatusOutbox(f.base.reply, b.validator)
			require.NoError(t, err)
			x, err := NewStatusExecution(b.scheduler, r, outbox)
			require.NoError(t, err)
			f.provider.e.Status = state
			out, err := x.Observe(ctx, JobLease(j))
			require.NoError(t, err)
			require.Equal(t, state, out.Receipt.Status)
			require.Empty(t, out.Input.Preparation.Source)
			require.Equal(t, source.FactID, out.Input.Preparation.FactID)
			require.Equal(t, ColdLane, out.Job.Lane, "financial completion cannot retire lifecycle work")
			n, err := f.base.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": "billing_revenue_fact"})
			require.NoError(t, err)
			require.EqualValues(t, 1, n, "status creates no additional paid fact")
		})
	}
}
