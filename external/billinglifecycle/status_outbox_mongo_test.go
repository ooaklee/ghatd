package billinglifecycle

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ StatusValidator = (*billingmanager.Service)(nil)

// Only provider evidence and service-identity admission are controlled. The
// billing owner, configured manager, native grants/current authority and
// encrypted recordstore run against the disposable replica set per case.
type statusNativeProvider struct {
	paymentprovider.RevenueProvider
	mu    sync.Mutex
	e     paymentprovider.RevenueSubscriptionEvidence
	calls int
}

func (p *statusNativeProvider) LookupRevenueSubscription(ctx context.Context, scope paymentprovider.RevenueScope, subscription string) (paymentprovider.RevenueSubscriptionEvidence, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return paymentprovider.RevenueSubscriptionEvidence{}, err
	}
	if scope != p.e.Scope || subscription != p.e.SubscriptionID {
		return paymentprovider.RevenueSubscriptionEvidence{}, billing.ErrRevenueConflict
	}
	p.calls++
	return p.e, nil
}

type statusNativeRegistry struct{ p *statusNativeProvider }

func (r statusNativeRegistry) GetRevenueProvider(string) (paymentprovider.RevenueProvider, error) {
	return r.p, nil
}

type statusNativeFixture struct {
	base     *inputNativeFixture
	owner    *billing.RevenueService
	provider *statusNativeProvider
	manager  *billingmanager.Service
	outbox   *StatusOutbox
	ctx      context.Context
	p        billing.SubscriptionStatusPreparation
}

func nativeStatusFixture(t *testing.T) *statusNativeFixture {
	t.Helper()
	b := nativeInputFixture(t)
	i, err := b.manager.PrepareCheckoutLifecycle(b.ctx, "worker-original", b.intent)
	require.NoError(t, err)
	e, err := b.manager.LookupCheckoutLifecycleEvidence(b.ctx, "worker-original", i)
	require.NoError(t, err)
	_, err = b.manager.CaptureCheckoutLifecycleEvidence(b.ctx, "worker-original", i, e)
	require.NoError(t, err)
	repo, err := revenuestore.NewRepository(b.reply)
	require.NoError(t, err)
	owner, err := billing.NewRevenueService(repo, b.clock)
	require.NoError(t, err)
	provider := &statusNativeProvider{e: paymentprovider.RevenueSubscriptionEvidence{Scope: e.Scope, SubscriptionID: e.SubscriptionID, CustomerID: e.CustomerID, Status: "trialing", CancellationScheduled: true}}
	f := &statusNativeFixture{base: b, owner: owner, provider: provider}
	g := b.grant
	g.Permissions = append(g.Permissions, billingmanager.SubscriptionStatusRead)
	b.grant, err = b.policy.ReplaceGrant(b.ctx, g, g.Revision)
	require.NoError(t, err)
	f.manager, f.ctx = f.worker(t, "worker-original")
	f.outbox, err = NewStatusOutbox(b.reply, f.manager)
	require.NoError(t, err)
	f.p, err = f.manager.PrepareSubscriptionStatusForCheckout(f.ctx, "worker-original", i.Scope, e.SubscriptionID)
	require.NoError(t, err)
	require.Equal(t, billing.SubscriptionStatusCheckoutSource, f.p.Source)
	require.Empty(t, f.p.FactID)
	return f
}
func (f *statusNativeFixture) worker(t *testing.T, actor string) (*billingmanager.Service, context.Context) {
	t.Helper()
	a, err := partneraccess.NewLifecycleWorkerAuthority("hostapp", actor, inputNativeIdentity{}, f.base.policy, []billing.RevenueScope{f.base.intent.Scope})
	require.NoError(t, err)
	m, err := (&billingmanager.Service{}).WithRevenueServices(statusNativeRegistry{f.provider}, f.owner, f.base.native)
	require.NoError(t, err)
	_, err = m.WithSubscriptionStatusAuthority(a)
	require.NoError(t, err)
	ctx, err := a.Bind(f.base.ctx)
	require.NoError(t, err)
	return m, ctx
}
func (f *statusNativeFixture) evidence(t *testing.T) billing.VerifiedSubscriptionStatusEvidence {
	t.Helper()
	e, err := f.manager.LookupSubscriptionStatus(f.ctx, "worker-original", f.p)
	require.NoError(t, err)
	return e
}
func (f *statusNativeFixture) prepared(t *testing.T) {
	t.Helper()
	_, err := f.outbox.RetainPreparation(f.ctx, "worker-original", f.p)
	require.NoError(t, err)
}
func (f *statusNativeFixture) raw(t *testing.T) bson.Raw {
	t.Helper()
	id, _ := statusIdentity(f.p)
	var raw bson.Raw
	err := f.base.db.Collection("ghatd_owned_records").FindOne(context.WithoutCancel(f.ctx), bson.M{"kind": statusKind, "id": id}).Decode(&raw)
	require.NoError(t, err)
	return raw
}

func TestStatusOutboxEncryptedOwningRecovery(t *testing.T) {
	for _, scenario := range []string{"prepared", "evidence", "replacement_worker", "wrong_instance_context", "lost_preparation_reply", "lost_evidence_reply", "lost_capture_reply", "grant_revoked", "post_commit_revoked", "lost_commit_plus_revoked", "lost_commit_plus_canceled", "wrong_key", "tampered_state", "tampered_revision", "tampered_partition", "missing_checkout_provenance"} {
		t.Run(scenario, func(t *testing.T) {
			f := nativeStatusFixture(t)
			var out StatusInput
			var err error
			if scenario == "prepared" {
				out, err = f.outbox.RetainPreparation(f.ctx, "worker-original", f.p)
			} else {
				f.prepared(t)
				e := f.evidence(t)
				switch scenario {
				case "lost_preparation_reply":
					// The exact initial write is tested on a separate observation ID.
					// Advance the native clock through a replacement owner, not a host hash.
					clock := inputNativeClock{f.base.clock.at.Add(time.Nanosecond)}
					repo, repoErr := revenuestore.NewRepository(f.base.reply)
					require.NoError(t, repoErr)
					f.owner, err = billing.NewRevenueService(repo, clock)
					require.NoError(t, err)
					f.manager, f.ctx = f.worker(t, "worker-original")
					f.outbox, err = NewStatusOutbox(f.base.reply, f.manager)
					require.NoError(t, err)
					f.p, err = f.manager.PrepareSubscriptionStatusForCheckout(f.ctx, "worker-original", f.p.Scope, f.p.SubscriptionID)
					require.NoError(t, err)
					f.base.reply.lose = true
					out, err = f.outbox.RetainPreparation(f.ctx, "worker-original", f.p)
					require.ErrorIs(t, err, recordstore.ErrUncertain)
					require.Equal(t, StatusInput{}, out)
					f.base.reply.lose = false
					out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
				case "grant_revoked":
					f.base.revoke(t)
					out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
					require.ErrorIs(t, err, partnermanager.ErrDenied)
					require.Equal(t, StatusInput{}, out)
					f.base.restore(t)
					out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
				case "post_commit_revoked", "lost_commit_plus_revoked", "lost_commit_plus_canceled":
					f.base.reply.lose = scenario != "post_commit_revoked"
					if scenario == "lost_commit_plus_canceled" {
						var cancel context.CancelFunc
						f.ctx, cancel = context.WithCancel(f.ctx)
						t.Cleanup(cancel)
						f.base.reply.after = cancel
					} else {
						f.base.reply.after = func() { f.base.revoke(t) }
					}
					out, err = f.outbox.RetainEvidence(f.ctx, "worker-original", f.p, e)
					if scenario == "lost_commit_plus_canceled" {
						require.ErrorIs(t, err, context.Canceled)
					} else {
						require.ErrorIs(t, err, partnermanager.ErrDenied)
					}
					if scenario != "post_commit_revoked" {
						require.ErrorIs(t, err, recordstore.ErrUncertain)
					}
					require.Equal(t, StatusInput{}, out)
					f.base.reply.lose = false
					f.base.reply.after = nil
					f.base.restore(t)
					f.manager, f.ctx = f.worker(t, "worker-original")
					f.outbox, err = NewStatusOutbox(f.base.reply, f.manager)
					require.NoError(t, err)
					out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
				case "missing_checkout_provenance":
					_, err = f.base.db.Collection("ghatd_owned_records").DeleteMany(context.WithoutCancel(f.ctx), bson.M{"kind": "billing_checkout_session"})
					require.NoError(t, err)
					out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
					require.Error(t, err)
					require.Equal(t, StatusInput{}, out)
					require.NotEmpty(t, f.raw(t))
					return
				case "wrong_key", "tampered_state", "tampered_revision", "tampered_partition":
					if scenario == "wrong_key" {
						cipher, cipherErr := encryption.NewPayloadCipher(bytes.Repeat([]byte{0x64}, 32))
						require.NoError(t, cipherErr)
						store, storeErr := recordstore.NewMongoStoreFromDatabase(f.base.db, cipher)
						require.NoError(t, storeErr)
						f.outbox, err = NewStatusOutbox(store, f.manager)
						require.NoError(t, err)
					} else {
						id, _ := statusIdentity(f.p)
						field, value := "state", any("evidence")
						if scenario == "tampered_revision" {
							field, value = "revision", int64(2)
						}
						if scenario == "tampered_partition" {
							field, value = "partition", "wrong"
						}
						_, err = f.base.db.Collection("ghatd_owned_records").UpdateOne(context.WithoutCancel(f.ctx), bson.M{"kind": statusKind, "id": id}, bson.M{"$set": bson.M{field: value}})
						require.NoError(t, err)
					}
					out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
					require.Error(t, err)
					require.NotErrorIs(t, err, recordstore.ErrNotFound)
					require.Equal(t, StatusInput{}, out)
					return
				default:
					f.base.reply.lose = scenario == "lost_evidence_reply"
					out, err = f.outbox.RetainEvidence(f.ctx, "worker-original", f.p, e)
					if scenario == "lost_evidence_reply" {
						require.ErrorIs(t, err, recordstore.ErrUncertain)
						require.Equal(t, StatusInput{}, out)
						f.base.reply.lose = false
						out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
					}
					require.NoError(t, err)
					if scenario == "replacement_worker" || scenario == "wrong_instance_context" {
						grant := f.base.grant
						grant.Subject.ID = "worker-replacement"
						grant.Revision = 0
						_, err = f.base.policy.ReplaceGrant(f.ctx, grant, 0)
						require.NoError(t, err)
						manager, ctx := f.worker(t, "worker-replacement")
						o, err := NewStatusOutbox(f.base.reply, manager)
						require.NoError(t, err)
						if scenario == "wrong_instance_context" {
							denied, denial := o.Find(f.ctx, "worker-replacement", f.p)
							require.ErrorIs(t, denial, partnermanager.ErrDenied)
							require.Equal(t, StatusInput{}, denied)
						}
						out, err = o.Find(ctx, "worker-replacement", f.p)
						require.NoError(t, err)
					}
					if scenario == "lost_capture_reply" {
						f.base.reply.lose = true
						captured, unknown := f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", out.Preparation, *out.Evidence)
						require.ErrorIs(t, unknown, billing.ErrRevenueUncertain)
						require.Equal(t, billing.SubscriptionStatus{}, captured)
						f.base.reply.lose = false
						out, err = f.outbox.Find(f.ctx, "worker-original", f.p)
						require.NoError(t, err)
						captured, err = f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", out.Preparation, *out.Evidence)
						require.NoError(t, err)
						require.Equal(t, "trialing", captured.Status)
						require.Equal(t, f.p, captured.Preparation)
					}
				}
			}
			require.NoError(t, err)
			require.Equal(t, f.p, out.Preparation)
			raw := f.raw(t)
			for _, secret := range []string{f.p.ActorID, f.p.PrincipalID, f.p.ProviderCustomerID, f.p.SubscriptionID, f.p.CheckoutIntentID, f.p.CheckoutFingerprint, f.p.CaptureID} {
				require.False(t, bytes.Contains(raw, []byte(secret)), secret)
			}
			for _, kind := range []string{"billing_revenue_fact", "partner_journal", "partner_ledger_head", "partner_earnings_receipt"} {
				n, err := f.base.db.Collection("ghatd_owned_records").CountDocuments(context.WithoutCancel(f.ctx), bson.M{"kind": kind})
				require.NoError(t, err)
				require.Zero(t, n)
			}
			if scenario != "prepared" {
				require.Equal(t, 1, f.provider.calls)
			}
		})
	}
}

func TestStatusOutboxEncryptedConcurrentEvidence(t *testing.T) {
	for _, different := range []bool{false, true} {
		name := "identical_writers_recover_one_evidence"
		if different {
			name = "different_writers_cannot_overwrite_original"
		}
		t.Run(name, func(t *testing.T) {
			f := nativeStatusFixture(t)
			f.prepared(t)
			e := f.evidence(t)
			second := e
			if different {
				f.provider.e.Status = "paused"
				second = f.evidence(t)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, input := range []billing.VerifiedSubscriptionStatusEvidence{e, second} {
				go func(input billing.VerifiedSubscriptionStatusEvidence) {
					<-start
					_, err := f.outbox.RetainEvidence(f.ctx, "worker-original", f.p, input)
					results <- err
				}(input)
			}
			close(start)
			successes, conflicts := 0, 0
			for n := 0; n < 2; n++ {
				err := <-results
				if err == nil {
					successes++
				} else {
					require.ErrorIs(t, err, recordstore.ErrConflict)
					conflicts++
				}
			}
			if different {
				require.Equal(t, 1, successes)
				require.Equal(t, 1, conflicts)
			} else {
				require.Equal(t, 2, successes)
				require.Zero(t, conflicts)
			}
			retained, err := f.outbox.Find(f.ctx, "worker-original", f.p)
			require.NoError(t, err)
			require.Contains(t, []billing.VerifiedSubscriptionStatusEvidence{e, second}, *retained.Evidence)
			_, err = f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", retained.Preparation, *retained.Evidence)
			require.NoError(t, err)
			require.Equal(t, int64(2), f.raw(t).Lookup("revision").Int64())
		})
	}
}

func TestStatusOutboxEncryptedPaidProvenance(t *testing.T) {
	for _, laterHead := range []bool{false, true} {
		name := "paid_original_and_current_manager_read"
		if laterHead {
			name = "paid_original_recovers_after_later_checkout_head"
		}
		t.Run(name, func(t *testing.T) {
			f := nativeStatusFixture(t)
			r, err := f.owner.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: f.p.Scope, EnvelopeID: "evt_first_paid", Facts: []billing.RevenueFact{{Scope: f.p.Scope, Kind: billing.RevenuePayment, PaymentID: "pi_first", InvoiceID: "in_first", AllocationID: "il_first", PrincipalID: f.p.PrincipalID, ProviderCustomerID: f.p.ProviderCustomerID, SubscriptionID: f.p.SubscriptionID, PlanID: "plan", CostID: "cost", Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: f.base.clock.at}}})
			require.NoError(t, err)
			f.p, err = f.manager.PrepareSubscriptionStatus(f.ctx, "worker-original", r.FactIDs[0])
			require.NoError(t, err)
			require.Empty(t, f.p.Source)
			require.Empty(t, f.p.CheckoutIntentID)
			f.prepared(t)
			e := f.evidence(t)
			_, err = f.outbox.RetainEvidence(f.ctx, "worker-original", f.p, e)
			require.NoError(t, err)
			retained, err := f.outbox.Find(f.ctx, "worker-original", f.p)
			require.NoError(t, err)
			first, err := f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", retained.Preparation, *retained.Evidence)
			require.NoError(t, err)
			if laterHead {
				repo, err := revenuestore.NewRepository(f.base.reply)
				require.NoError(t, err)
				clock := inputNativeClock{f.base.clock.at.Add(time.Second)}
				f.owner, err = billing.NewRevenueService(repo, clock)
				require.NoError(t, err)
				f.manager, f.ctx = f.worker(t, "worker-original")
				f.outbox, err = NewStatusOutbox(f.base.reply, f.manager)
				require.NoError(t, err)
				next, err := f.manager.PrepareSubscriptionStatusForCheckout(f.ctx, "worker-original", f.p.Scope, f.p.SubscriptionID)
				require.NoError(t, err)
				later := e
				later.Status = "paused"
				_, err = f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", next, later)
				require.NoError(t, err)
			}
			retained, err = f.outbox.Find(f.ctx, "worker-original", f.p)
			require.NoError(t, err)
			replayed, err := f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", retained.Preparation, *retained.Evidence)
			require.NoError(t, err)
			require.Equal(t, first, replayed)
			current, err := f.manager.GetSubscriptionStatusForFact(f.ctx, "worker-original", f.p.FactID, 10*time.Second)
			require.NoError(t, err)
			if laterHead {
				require.Equal(t, "paused", current.Status)
			} else {
				require.Equal(t, "trialing", current.Status)
			}
			require.Equal(t, 1, f.provider.calls)
			require.False(t, bytes.Contains(f.raw(t), []byte(f.p.FactID)))
		})
	}
}
