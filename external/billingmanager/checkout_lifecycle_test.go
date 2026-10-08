package billingmanager

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Fresh actual owning services/native codecs per case. The memory record store
// models atomic callbacks; encrypted Mongo/snapshot races are proven separately.
type completionAuthority struct {
	calls, denyAt int
	denied        error
	actor         string
	targets       []CheckoutLifecycleTarget
	actions       []string
}

func (a *completionAuthority) AuthorizeCheckoutLifecycle(_ context.Context, actor, action string, target CheckoutLifecycleTarget) error {
	a.calls++
	a.actor = actor
	a.targets = append(a.targets, target)
	a.actions = append(a.actions, action)
	if a.calls == a.denyAt {
		return a.denied
	}
	return nil
}

type completionProvider struct {
	e     paymentprovider.RevenueCheckoutEvidence
	calls int
	err   error
}

func (*completionProvider) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	panic("no subscription fallback")
}
func (p *completionProvider) LookupRevenueCheckoutSessionEvidence(_ context.Context, scope paymentprovider.RevenueScope, session string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.calls++
	if scope != p.e.Scope || session != p.e.SessionID {
		panic("wrong retained session")
	}
	return p.e, p.err
}

type completionOwner struct {
	*billing.CheckoutService
	prepareCalls, lookupCalls, captureCalls, receiptCalls int
	prepareErr, lookupErr, captureErr, receiptErr         error
	onStage                                               func(string)
	badPrepare, badLookup, badCapture, badReceipt         bool
}

func (o *completionOwner) PrepareCheckoutLifecycle(ctx context.Context, i billing.CheckoutIntent) (billing.CheckoutIntent, error) {
	o.prepareCalls++
	if o.prepareErr != nil {
		if o.onStage != nil {
			o.onStage("prepare")
		}
		return billing.CheckoutIntent{}, o.prepareErr
	}
	out, err := o.CheckoutService.PrepareCheckoutLifecycle(ctx, i)
	if o.onStage != nil {
		o.onStage("prepare")
	}
	if o.badPrepare {
		out.CreatedAt = out.CreatedAt.Add(time.Second)
	}
	return out, err
}
func (o *completionOwner) LookupCheckoutLifecycleEvidence(ctx context.Context, i billing.CheckoutIntent) (paymentprovider.RevenueCheckoutEvidence, error) {
	o.lookupCalls++
	out, err := o.CheckoutService.LookupCheckoutLifecycleEvidence(ctx, i)
	if o.onStage != nil {
		o.onStage("lookup")
	}
	if o.lookupErr != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, o.lookupErr
	}
	if o.badLookup {
		out.ClientReferenceID = "unrelated-payer"
	}
	return out, err
}
func (o *completionOwner) CaptureCheckoutLifecycleEvidence(ctx context.Context, i billing.CheckoutIntent, e paymentprovider.RevenueCheckoutEvidence) (billing.CheckoutLifecycleAnchor, error) {
	o.captureCalls++
	out, err := o.CheckoutService.CaptureCheckoutLifecycleEvidence(ctx, i, e)
	if o.onStage != nil {
		o.onStage("capture")
	}
	if o.captureErr != nil {
		return billing.CheckoutLifecycleAnchor{}, o.captureErr
	}
	if o.badCapture {
		out.IntentFingerprint = "changed"
	}
	return out, err
}
func (o *completionOwner) FindCheckoutLifecycleReceipt(ctx context.Context, i billing.CheckoutIntent) (billing.CheckoutLifecycleAnchor, error) {
	o.receiptCalls++
	out, err := o.CheckoutService.FindCheckoutLifecycleReceipt(ctx, i)
	if o.onStage != nil {
		o.onStage("receipt")
	}
	if o.receiptErr != nil {
		return billing.CheckoutLifecycleAnchor{}, o.receiptErr
	}
	if o.badReceipt {
		out.PrincipalID = "changed"
	}
	return out, err
}

func completionManagerFixture(t *testing.T) (*Service, *completionOwner, *completionProvider, *completionAuthority, *statusManagerStore, billing.CheckoutIntent) {
	t.Helper()
	records := &statusManagerStore{records: map[string]recordstore.Record{}}
	repo, err := revenuestore.NewRepository(records)
	require.NoError(t, err)
	at := time.Unix(1700000000, 0).UTC()
	clock := fixtureBillingStatusClock{at.Add(time.Hour)}
	provider := &completionProvider{}
	native, err := billing.NewCheckoutService(repo, clock, provider)
	require.NoError(t, err)
	q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "original", PriceID: "price_original", PlanID: "plan", CostID: "cost", UserID: "original-payer", UserReference: "original-payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14}
	scope := billing.RevenueScope{Provider: "stripe", AccountID: "merchant-a"}
	i, err := native.PrepareCheckout(t.Context(), scope, q)
	require.NoError(t, err)
	require.NoError(t, native.AcknowledgeCheckout(t.Context(), i, "cs_original"))
	i, err = native.FindCheckoutIntent(t.Context(), scope, q.IdempotencyKey)
	require.NoError(t, err)
	provider.e = paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: q.UserID, CustomerID: "cus_original", SubscriptionID: "sub_trial", PriceID: q.PriceID, Currency: "GBP", Mode: q.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
	owner := &completionOwner{CheckoutService: native}
	revenue, err := billing.NewRevenueService(repo, clock)
	require.NoError(t, err)
	service, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: &revenueBoundaryProvider{}}, revenue, owner)
	require.NoError(t, err)
	authority := &completionAuthority{denied: errors.New("revoked checkout authority")}
	_, err = service.WithCheckoutLifecycleAuthority(authority)
	require.NoError(t, err)
	require.Nil(t, service.checkoutRevenueCapture)
	require.Nil(t, service.checkoutPayerAuthority)
	return service, owner, provider, authority, records, i
}

func TestCheckoutLifecycleManagerStages(t *testing.T) {
	outage := errors.New("owning outage")
	for _, tc := range []struct {
		name, stage, state string
		denyAt             int
		want               error
	}{
		{"prepare_original_detached", "prepare", "", 0, nil}, {"validate_retained_original", "validate", "", 0, nil},
		{"lookup_before_first_payment", "lookup", "", 0, nil}, {"capture_original_evidence", "capture", "", 0, nil},
		{"read_original_receipt", "receipt", "seed", 0, nil}, {"receipt_absence_retains_authority", "receipt", "", 0, billing.ErrRevenueNotFound},
		{"prepare_denied_before_owner", "prepare", "", 1, errCompletionDenied}, {"prepare_revoked_before_disclosure", "prepare", "", 2, errCompletionDenied},
		{"lookup_denied_before_validation", "lookup", "", 1, errCompletionDenied}, {"lookup_denied_before_provider", "lookup", "", 2, errCompletionDenied}, {"lookup_revoked_after_provider", "lookup", "", 3, errCompletionDenied},
		{"capture_denied_before_validation", "capture", "", 1, errCompletionDenied}, {"capture_denied_before_commit", "capture", "", 2, errCompletionDenied}, {"capture_revoked_after_commit", "capture", "", 3, errCompletionDenied},
		{"receipt_denied_before_read", "receipt", "seed", 1, errCompletionDenied}, {"receipt_revoked_after_read", "receipt", "seed", 2, errCompletionDenied},
		{"absence_revocation_overrides_not_found", "receipt", "", 2, errCompletionDenied},
		{"preparation_outage_preserved", "prepare", "prepare-outage", 0, outage}, {"preparation_outage_revocation_overrides", "prepare", "prepare-outage", 2, errCompletionDenied},
		{"lookup_outage_preserved", "lookup", "lookup-outage", 0, outage}, {"lookup_outage_revocation_overrides", "lookup", "lookup-outage", 3, errCompletionDenied},
		{"receipt_joined_absence_outage_preserved", "receipt", "receipt-outage", 0, outage}, {"receipt_error_revocation_overrides", "receipt", "receipt-outage", 2, errCompletionDenied},
		{"wrong_prepared_original_withheld", "prepare", "bad-prepare", 0, billing.ErrRevenueConflict}, {"wrong_provider_evidence_withheld", "lookup", "bad-lookup", 0, billing.ErrRevenueUnassessable},
		{"wrong_capture_receipt_withheld", "capture", "bad-capture", 0, billing.ErrRevenueConflict}, {"wrong_recovered_receipt_withheld", "receipt", "bad-receipt", 0, billing.ErrRevenueConflict},
		{"invalid_frozen_input_before_io", "prepare", "invalid", 0, billing.ErrRevenueInvalid}, {"invalid_capture_evidence_no_write", "capture", "bad-input-evidence", 0, billing.ErrRevenueUnassessable},
		{"nil_context", "prepare", "nil", 0, billing.ErrRevenueInvalid}, {"empty_actor", "prepare", "actor", 0, billing.ErrRevenueInvalid}, {"cancel_before_stage", "lookup", "cancel", 0, context.Canceled},
		{"cancel_after_original_validation_no_provider", "lookup", "cancel-prepare", 0, context.Canceled}, {"cancel_after_lookup_no_evidence", "lookup", "cancel-lookup", 0, context.Canceled},
		{"cancel_after_commit_no_receipt", "capture", "cancel-capture", 0, context.Canceled}, {"missing_reverse_ownership", "lookup", "reverse", 0, billing.ErrRevenueUnavailable},
		{"lost_reply_requires_original_recovery", "capture", "uncertain", 0, billing.ErrRevenueUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, p, a, records, i := completionManagerFixture(t)
			a.denied = errCompletionDenied
			a.denyAt = tc.denyAt
			if tc.state == "seed" || tc.state == "bad-receipt" {
				_, err := owner.CheckoutService.CaptureCheckoutLifecycleEvidence(t.Context(), i, p.e)
				require.NoError(t, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			actor := "current-worker"
			e := p.e
			switch tc.state {
			case "prepare-outage":
				owner.prepareErr = outage
			case "lookup-outage":
				owner.lookupErr = outage
			case "receipt-outage":
				owner.receiptErr = errors.Join(billing.ErrRevenueNotFound, outage)
			case "bad-prepare":
				owner.badPrepare = true
			case "bad-lookup":
				owner.badLookup = true
			case "bad-capture":
				owner.badCapture = true
			case "bad-receipt":
				owner.badReceipt = true
			case "invalid":
				i.Request.ExpectedCurrency = "EUR"
			case "bad-input-evidence":
				e.ClientReferenceID = "different-payer"
			case "nil":
				ctx = nil
			case "actor":
				actor = ""
			case "cancel":
				cancel()
			case "cancel-prepare", "cancel-lookup", "cancel-capture":
				target := tc.state[len("cancel-"):]
				owner.onStage = func(stage string) {
					if stage == target {
						cancel()
					}
				}
			case "reverse":
				for key, row := range records.records {
					if row.Kind == "billing_checkout_session" {
						delete(records.records, key)
					}
				}
			case "uncertain":
				owner.captureErr = billing.ErrRevenueUncertain
			}
			before := maps.Clone(records.records)
			var err error
			switch tc.stage {
			case "prepare":
				out, e := s.PrepareCheckoutLifecycle(ctx, actor, i)
				err = e
				if e != nil {
					require.Zero(t, out)
				} else {
					require.Equal(t, i, out)
					out.Request.Metadata["mutated"] = "external"
					require.NotContains(t, i.Request.Metadata, "mutated")
				}
			case "validate":
				err = s.ValidateCheckoutLifecycle(ctx, actor, i)
			case "lookup":
				out, e := s.LookupCheckoutLifecycleEvidence(ctx, actor, i)
				err = e
				if e != nil {
					require.Zero(t, out)
				} else {
					require.Equal(t, p.e, out)
				}
			case "capture":
				out, ce := s.CaptureCheckoutLifecycleEvidence(ctx, actor, i, e)
				err = ce
				if ce != nil {
					require.Zero(t, out)
				} else {
					require.NoError(t, out.ValidateCapturedEvidence(i, e))
				}
			case "receipt":
				out, e := s.FindCheckoutLifecycleReceipt(ctx, actor, i)
				err = e
				if e != nil {
					require.Zero(t, out)
				} else {
					require.NoError(t, out.ValidateForCheckout(i))
				}
			}
			require.ErrorIs(t, err, tc.want)
			if tc.stage != "capture" || tc.denyAt == 1 || tc.denyAt == 2 || tc.state == "bad-input-evidence" {
				require.Equal(t, before, records.records)
			}
			if tc.denyAt == 1 || tc.state == "invalid" || tc.state == "nil" || tc.state == "actor" || tc.state == "cancel" {
				require.Zero(t, owner.prepareCalls+owner.lookupCalls+owner.captureCalls+owner.receiptCalls)
			}
			if tc.stage != "lookup" || tc.denyAt == 1 || tc.denyAt == 2 || tc.state == "cancel-prepare" || tc.state == "reverse" {
				require.Zero(t, p.calls)
			}
			if tc.state == "uncertain" {
				owner.captureErr = nil
				out, err := s.FindCheckoutLifecycleReceipt(t.Context(), "replacement-worker", i)
				require.NoError(t, err)
				require.NoError(t, out.ValidateCapturedEvidence(i, e))
				again, err := s.CaptureCheckoutLifecycleEvidence(t.Context(), "replacement-worker", i, e)
				require.NoError(t, err)
				require.Equal(t, out, again)
				require.Zero(t, p.calls)
			}
			for _, target := range a.targets {
				require.Equal(t, CheckoutLifecycleTarget{Scope: i.Scope, PrincipalID: i.Request.UserID, IntentID: i.ID}, target)
			}
			for _, action := range a.actions {
				require.Equal(t, SubscriptionStatusRefresh, action)
			}
			if a.calls > 0 {
				require.NotEqual(t, i.Request.UserID, a.actor)
			}
		})
	}
}

var errCompletionDenied = errors.New("completion authority denied")

func TestCheckoutLifecycleManagerCapability(t *testing.T) {
	for _, tc := range []struct{ name, state string }{{"nil_service", "nil"}, {"nil_authority", "authority"}, {"typed_nil_authority", "typed-authority"}, {"missing_same_association", "association"}, {"typed_nil_owner", "typed-owner"}, {"legacy_association_no_completion", "legacy"}, {"paid_capture_is_not_alternate_owner", "alternate"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, _, a, _, _ := completionManagerFixture(t)
			var auth CheckoutLifecycleAuthority = a
			switch tc.state {
			case "nil":
				s = nil
			case "authority":
				auth = nil
			case "typed-authority":
				auth = (*completionAuthority)(nil)
			case "association":
				s.revenueAssociation = nil
			case "typed-owner":
				s.revenueAssociation = (*completionOwner)(nil)
			case "legacy":
				s.revenueAssociation = &revenueBoundaryAssociation{}
			case "alternate":
				s.revenueAssociation = &revenueBoundaryAssociation{}
				s.checkoutRevenueCapture = owner.CheckoutService
			}
			out, err := s.WithCheckoutLifecycleAuthority(auth)
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			require.Nil(t, out)
		})
	}
}
