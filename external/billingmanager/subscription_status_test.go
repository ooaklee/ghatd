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

// Audit disposition: named scoped orchestration/recovery cases use the actual
// billing owner and adapter over isolated records. Native encryption/CAS is
// exercised separately by revenuestore's replica-set tests.
type statusManagerStore struct {
	records   map[string]recordstore.Record
	uncertain bool
}
type statusManagerTx struct{ records map[string]recordstore.Record }

func statusRecordKey(kind, id string) string { return kind + ":" + id }
func (s *statusManagerStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return fn(&statusManagerTx{s.records})
}
func (s *statusManagerStore) Transact(ctx context.Context, _ string, fn func(recordstore.Tx) error) error {
	tx := &statusManagerTx{maps.Clone(s.records)}
	if err := fn(tx); err != nil {
		return err
	}
	s.records = tx.records
	if s.uncertain {
		return recordstore.ErrUncertain
	}
	return nil
}
func (t *statusManagerTx) Get(_ context.Context, kind, id string) (recordstore.Record, error) {
	v, ok := t.records[statusRecordKey(kind, id)]
	if !ok {
		return recordstore.Record{}, recordstore.ErrNotFound
	}
	return v, nil
}
func (t *statusManagerTx) Find(_ context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	out := []recordstore.Record{}
	for _, v := range t.records {
		if v.Kind == q.Kind && v.Partition == q.Partition {
			out = append(out, v)
		}
	}
	return out, nil
}
func (t *statusManagerTx) Insert(_ context.Context, v recordstore.Record) error {
	key := statusRecordKey(v.Kind, v.ID)
	if _, ok := t.records[key]; ok {
		return recordstore.ErrConflict
	}
	t.records[key] = v
	return nil
}
func (t *statusManagerTx) Replace(_ context.Context, v recordstore.Record, expected int64) error {
	key := statusRecordKey(v.Kind, v.ID)
	if t.records[key].Revision != expected {
		return recordstore.ErrConflict
	}
	t.records[key] = v
	return nil
}

type statusManagerAuthority struct {
	actors, actions []string
	targets         []SubscriptionStatusTarget
	denyAt          int
	err             error
}

func (a *statusManagerAuthority) AuthorizeSubscriptionStatus(_ context.Context, actor, action string, target SubscriptionStatusTarget) error {
	a.actors = append(a.actors, actor)
	a.actions = append(a.actions, action)
	a.targets = append(a.targets, target)
	if len(a.actors) == a.denyAt {
		return a.err
	}
	return nil
}

type statusManagerProvider struct {
	*revenueBoundaryProvider
	evidence paymentprovider.RevenueSubscriptionEvidence
	calls    int
	failure  error
}

func (p *statusManagerProvider) LookupRevenueSubscription(_ context.Context, scope paymentprovider.RevenueScope, sub string) (paymentprovider.RevenueSubscriptionEvidence, error) {
	p.calls++
	return p.evidence, p.failure
}
func statusManagerFixture(t *testing.T) (*Service, *billing.RevenueService, *statusManagerStore, *statusManagerAuthority, *statusManagerProvider, string) {
	t.Helper()
	records := &statusManagerStore{records: map[string]recordstore.Record{}}
	repo, err := revenuestore.NewRepository(records)
	require.NoError(t, err)
	at := time.Date(2026, 10, 7, 14, 0, 0, 9, time.UTC)
	owner, err := billing.NewRevenueService(repo, fixtureBillingStatusClock{at})
	require.NoError(t, err)
	scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
	o, err := owner.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "evt_paid", Facts: []billing.RevenueFact{{Scope: scope, Kind: billing.RevenuePayment, PaymentID: "pi_paid", InvoiceID: "in_paid", AllocationID: "il_one", PrincipalID: "paying-principal", ProviderCustomerID: "cus_payer", SubscriptionID: "sub_original", PlanID: "plan", CostID: "cost", Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: at.Add(-time.Minute)}}})
	require.NoError(t, err)
	provider := &statusManagerProvider{revenueBoundaryProvider: &revenueBoundaryProvider{}, evidence: paymentprovider.RevenueSubscriptionEvidence{Scope: paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, SubscriptionID: "sub_original", CustomerID: "cus_payer", Status: "active"}}
	s, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: provider}, owner, &revenueBoundaryAssociation{})
	require.NoError(t, err)
	authority := &statusManagerAuthority{}
	s, err = s.WithSubscriptionStatusAuthority(authority)
	require.NoError(t, err)
	return s, owner, records, authority, provider, o.FactIDs[0]
}

type fixtureBillingStatusClock struct{ at time.Time }

func (c fixtureBillingStatusClock) Now() time.Time { return c.at }
func TestSubscriptionStatusManagerStages(t *testing.T) {
	denied := errors.New("status permission revoked")
	type testCase struct {
		name                                                                                             string
		denyAt                                                                                           int
		wrongCustomer, wrongAccount, wrongMode, unknownStatus, providerOutage, replacementActor, lostAck bool
		phase                                                                                            string
		want                                                                                             error
	}
	cases := []testCase{
		{name: "prepared_lookup_capture_and_retained_read"},
		{name: "replacement_operator_recovers_original_author", replacementActor: true},
		{name: "lost_ack_replays_without_provider_lookup", lostAck: true},
		{name: "program_permission_precedes_source_lookup", denyAt: 1, phase: "prepare", want: denied},
		{name: "owning_scope_permission_precedes_preparation_return", denyAt: 2, phase: "prepare", want: denied},
		{name: "lookup_program_permission_precedes_source_validation", denyAt: 3, phase: "lookup", want: denied},
		{name: "scoped_permission_precedes_provider_io", denyAt: 4, phase: "lookup", want: denied},
		{name: "revocation_during_lookup_withholds_evidence", denyAt: 5, phase: "lookup", want: denied},
		{name: "capture_program_permission_precedes_replay", denyAt: 6, phase: "capture", want: denied},
		{name: "capture_scoped_permission_precedes_mutation", denyAt: 7, phase: "capture", want: denied},
		{name: "post_commit_revocation_withholds_response_retains_truth", denyAt: 8, phase: "capture", want: denied},
		{name: "read_program_permission_precedes_status_lookup", denyAt: 9, phase: "read", want: denied},
		{name: "read_scoped_permission_withholds_status", denyAt: 10, phase: "read", want: denied},
		{name: "provider_customer_cannot_change", wrongCustomer: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "provider_account_cannot_change", wrongAccount: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "provider_mode_cannot_change", wrongMode: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "unknown_provider_status_is_not_inactive", unknownStatus: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "provider_outage_does_not_capture", providerOutage: true, phase: "lookup", want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, records, auth, provider, id := statusManagerFixture(t)
			ctx := context.Background()
			auth.denyAt = tc.denyAt
			auth.err = denied
			if tc.wrongCustomer {
				provider.evidence.CustomerID = "cus_other"
			}
			if tc.wrongAccount {
				provider.evidence.Scope.AccountID = "acct_other"
			}
			if tc.wrongMode {
				provider.evidence.Scope.LiveMode = true
			}
			if tc.unknownStatus {
				provider.evidence.Status = "unmapped"
			}
			if tc.providerOutage {
				provider.failure = paymentprovider.ErrPaymentProviderAPIRequestFailed
			}
			p, err := s.PrepareSubscriptionStatus(ctx, "original-author", id)
			if tc.phase == "prepare" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, p)
				require.Zero(t, provider.calls)
				return
			}
			require.NoError(t, err)
			actor := "original-author"
			if tc.replacementActor {
				actor = "current-recovery-operator"
			}
			e, err := s.LookupSubscriptionStatus(ctx, actor, p)
			if tc.phase == "lookup" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, e)
				if tc.denyAt == 3 || tc.denyAt == 4 {
					require.Zero(t, provider.calls)
				}
				return
			}
			require.NoError(t, err)
			if tc.lostAck {
				records.uncertain = true
				v, err := s.CaptureSubscriptionStatus(ctx, actor, p, e)
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				require.Zero(t, v)
				records.uncertain = false
			}
			v, err := s.CaptureSubscriptionStatus(ctx, actor, p, e)
			if tc.phase == "capture" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, v)
				stored, err := owner.GetSubscriptionStatusForFact(ctx, id, time.Minute)
				if tc.denyAt == 8 {
					require.NoError(t, err)
					require.Equal(t, "active", stored.Status)
				} else {
					require.ErrorIs(t, err, billing.ErrRevenueNotFound)
					require.Zero(t, stored)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, "original-author", v.Preparation.ActorID)
			current, err := s.GetSubscriptionStatusForFact(ctx, actor, id, time.Minute)
			if tc.phase == "read" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, current)
				return
			}
			require.NoError(t, err)
			require.Equal(t, v, current)
			require.Equal(t, 1, provider.calls, "capture/replay/read must not refetch")
			for i, target := range auth.targets {
				if target.PrincipalID != "" {
					require.Equal(t, "paying-principal", target.PrincipalID)
					require.Equal(t, "sub_original", target.SubscriptionID)
					require.Equal(t, p.Scope, target.Scope)
				}
				wantAction := SubscriptionStatusRefresh
				if i >= len(auth.actions)-2 {
					wantAction = SubscriptionStatusRead
				}
				require.Equal(t, wantAction, auth.actions[i])
			}
		})
	}
}
func TestSubscriptionStatusManagerCapabilityAndInputs(t *testing.T) {
	type testCase struct {
		name                                                              string
		missingAuthority, unsupportedFeed, changedFeed, typedNilAuthority bool
		invalidActor                                                      string
		nilContext, canceled                                              bool
		want                                                              error
	}
	cases := []testCase{
		{name: "missing_authority", missingAuthority: true, want: billing.ErrRevenueUnavailable},
		{name: "unsupported_feed", unsupportedFeed: true, want: billing.ErrRevenueUnavailable},
		{name: "changed_feed_is_not_stale_owner", changedFeed: true, want: billing.ErrRevenueUnavailable},
		{name: "typed_nil_authority", typedNilAuthority: true, want: billing.ErrRevenueUnavailable},
		{name: "empty_actor", invalidActor: "", want: billing.ErrRevenueInvalid},
		{name: "padded_actor", invalidActor: " actor", want: billing.ErrRevenueInvalid},
		{name: "nil_context", nilContext: true, invalidActor: "actor", want: billing.ErrRevenueInvalid},
		{name: "canceled_context", canceled: true, invalidActor: "actor", want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _, provider, id := statusManagerFixture(t)
			if tc.missingAuthority {
				s.subscriptionStatusAuthority = nil
			}
			if tc.unsupportedFeed || tc.changedFeed {
				s.revenueFeed = &revenueBoundaryFeed{}
			}
			if tc.typedNilAuthority {
				var auth *statusManagerAuthority
				_, err := s.WithSubscriptionStatusAuthority(auth)
				require.ErrorIs(t, err, tc.want)
				return
			}
			actor := tc.invalidActor
			if tc.missingAuthority || tc.unsupportedFeed || tc.changedFeed {
				actor = "actor"
			}
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.canceled {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			p, err := s.PrepareSubscriptionStatus(ctx, actor, id)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, p)
			require.Zero(t, provider.calls)
		})
	}
}

func TestSubscriptionStatusManagerPrivatePreparationIntegrity(t *testing.T) {
	type testCase struct{ name, field string }
	cases := []testCase{
		{name: "changed_source_scope_before_lookup", field: "scope"},
		{name: "changed_customer_before_lookup", field: "customer"},
		{name: "changed_authorship_is_not_original_identity", field: "actor"},
		{name: "changed_fact_fingerprint_before_lookup", field: "fingerprint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _, provider, id := statusManagerFixture(t)
			ctx := context.Background()
			p, err := s.PrepareSubscriptionStatus(ctx, "original-author", id)
			require.NoError(t, err)
			switch tc.field {
			case "scope":
				p.Scope.AccountID = "acct_other"
			case "customer":
				p.ProviderCustomerID = "cus_other"
			case "actor":
				p.ActorID = "changed"
			case "fingerprint":
				p.FactFingerprint = "changed"
			}
			e, err := s.LookupSubscriptionStatus(ctx, "currently-authorized", p)
			require.Error(t, err)
			require.Zero(t, e)
			require.Zero(t, provider.calls)
		})
	}
}
