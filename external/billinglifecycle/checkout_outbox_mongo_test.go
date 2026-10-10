package billinglifecycle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type inputNativeClock struct{ at time.Time }

func (c inputNativeClock) Now() time.Time { return c.at }

// Controlled provider and service identity fixtures. Billing, manager, policy,
// worker authority, provenance codecs and encrypted Mongo transactions are real.
// This is not provider authentication, actual UMS or platform qualification.
type inputNativeProvider struct {
	mu       sync.Mutex
	evidence paymentprovider.RevenueCheckoutEvidence
	calls    int
}

func (*inputNativeProvider) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	panic("no subscription fallback")
}
func (p *inputNativeProvider) LookupRevenueCheckoutSessionEvidence(ctx context.Context, scope paymentprovider.RevenueScope, session string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	if scope != p.evidence.Scope || session != p.evidence.SessionID {
		return paymentprovider.RevenueCheckoutEvidence{}, billing.ErrRevenueConflict
	}
	p.calls++
	return p.evidence, nil
}

type inputNativeRegistry struct{}

func (inputNativeRegistry) GetRevenueProvider(string) (paymentprovider.RevenueProvider, error) {
	panic("outbox recovery cannot fetch financial providers")
}

type inputNativeIdentity struct{}

func (inputNativeIdentity) CheckWorkerIdentity(ctx context.Context, actor string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor != "worker-original" && actor != "worker-replacement" {
		return partnermanager.ErrDenied
	}
	return nil
}

// Lose only the reply AFTER a real successful owning transaction. This tests
// original-input recovery; it does not simulate Mongo server commit labels.
type inputLostReplyStore struct {
	recordstore.Store
	lose  bool
	after func()
}

func (s *inputLostReplyStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	if err := s.Store.Transact(ctx, key, fn); err != nil {
		return err
	}
	if s.after != nil {
		s.after()
	}
	if s.lose {
		return recordstore.ErrUncertain
	}
	return nil
}

type inputNativeFixture struct {
	db       *mongo.Database
	store    *recordstore.MongoStore
	reply    *inputLostReplyStore
	native   *billing.CheckoutService
	manager  *billingmanager.Service
	outbox   *CheckoutOutbox
	policy   *accesspolicy.Service
	grant    accesspolicy.Grant
	provider *inputNativeProvider
	intent   billing.CheckoutIntent
	ctx      context.Context
	clock    inputNativeClock
}

func nativeInputFixture(t *testing.T) *inputNativeFixture {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated native replica set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database(fmt.Sprintf("hostapp_lifecycle_input_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, client.Disconnect(cleanup))
	})
	cipher, err := encryption.NewPayloadCipher(bytes.Repeat([]byte{0x63}, 32))
	require.NoError(t, err)
	store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
	require.NoError(t, err)
	require.NoError(t, store.EnsureIndexes(ctx))
	require.NoError(t, store.Probe(ctx))
	reply := &inputLostReplyStore{Store: store}
	repo, err := revenuestore.NewRepository(reply)
	require.NoError(t, err)
	clock := inputNativeClock{time.Unix(1700000000, 123456789).UTC()}
	provider := &inputNativeProvider{}
	native, err := billing.NewCheckoutService(repo, clock, provider)
	require.NoError(t, err)
	q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "original", PriceID: "price_frozen", PlanID: "plan", CostID: "cost", UserID: "native-payer", UserReference: "native-payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14, Metadata: map[string]string{"purpose": "trial-original"}}
	scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
	i, err := native.PrepareCheckout(ctx, scope, q)
	require.NoError(t, err)
	require.NoError(t, native.AcknowledgeCheckout(ctx, i, "cs_original"))
	i, err = native.FindCheckoutIntent(ctx, scope, q.IdempotencyKey)
	require.NoError(t, err)
	provider.evidence = paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: q.UserID, CustomerID: "cus_original", SubscriptionID: "sub_trial", PriceID: q.PriceID, Currency: "GBP", Mode: q.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
	core, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
	require.NoError(t, err)
	policyStore, err := accesspolicy.NewMongoStore(ctx, core)
	require.NoError(t, err)
	require.NoError(t, policyStore.Initialize(ctx))
	policy, err := accesspolicy.NewService(policyStore, func(context.Context, string) (string, error) { return "fixture-policy-admin", nil })
	require.NoError(t, err)
	scopeGrant, err := partneraccess.LifecycleScope(scope)
	require.NoError(t, err)
	grant, err := policy.ReplaceGrant(ctx, accesspolicy.Grant{Subject: accesspolicy.Subject{System: "hostapp", Kind: accesspolicy.UserSubject, ID: "worker-original"}, Enabled: true, Scopes: []string{scopeGrant}, Permissions: []string{billingmanager.SubscriptionStatusRefresh}}, 0)
	require.NoError(t, err)
	revenue, err := billing.NewRevenueService(repo, clock)
	require.NoError(t, err)
	f := &inputNativeFixture{db: db, store: store, reply: reply, native: native, policy: policy, grant: grant, provider: provider, intent: i, clock: clock}
	f.manager, f.ctx = f.worker(t, ctx, "worker-original", native, revenue)
	f.outbox, err = NewCheckoutOutbox(reply, f.manager)
	require.NoError(t, err)
	return f
}

func (f *inputNativeFixture) worker(t *testing.T, ctx context.Context, actor string, owner *billing.CheckoutService, revenue *billing.RevenueService) (*billingmanager.Service, context.Context) {
	t.Helper()
	a, err := partneraccess.NewLifecycleWorkerAuthority("hostapp", actor, inputNativeIdentity{}, f.policy, []billing.RevenueScope{f.intent.Scope})
	require.NoError(t, err)
	manager, err := (&billingmanager.Service{}).WithRevenueServices(inputNativeRegistry{}, revenue, owner)
	require.NoError(t, err)
	_, err = manager.WithCheckoutLifecycleAuthority(a)
	require.NoError(t, err)
	bound, err := a.Bind(ctx)
	require.NoError(t, err)
	return manager, bound
}

func (f *inputNativeFixture) revoke(t *testing.T) {
	t.Helper()
	g := f.grant
	g.Enabled = false
	var err error
	f.grant, err = f.policy.ReplaceGrant(context.WithoutCancel(f.ctx), g, g.Revision)
	require.NoError(t, err)
}
func (f *inputNativeFixture) restore(t *testing.T) {
	t.Helper()
	g := f.grant
	g.Enabled = true
	var err error
	f.grant, err = f.policy.ReplaceGrant(context.WithoutCancel(f.ctx), g, g.Revision)
	require.NoError(t, err)
}
func (f *inputNativeFixture) raw(t *testing.T) bson.Raw {
	t.Helper()
	id, _ := checkoutIdentity(f.intent)
	var raw bson.Raw
	require.NoError(t, f.db.Collection("ghatd_owned_records").FindOne(context.WithoutCancel(f.ctx), bson.M{"kind": checkoutKind, "id": id}).Decode(&raw))
	return raw
}
func (f *inputNativeFixture) prepare(t *testing.T) {
	t.Helper()
	original, err := f.manager.PrepareCheckoutLifecycle(f.ctx, "worker-original", f.intent)
	require.NoError(t, err)
	_, err = f.outbox.RetainPreparation(f.ctx, "worker-original", original)
	require.NoError(t, err)
}
func TestCheckoutOutboxEncryptedOwningRecovery(t *testing.T) {
	for _, tc := range []struct{ name, scenario string }{
		{"native_original_retained_before_provider_read", "prepare"},
		{"encrypted_evidence_and_exact_replay", "evidence"},
		{"replacement_worker_restart_recovers_original", "replacement"},
		{"replacement_cannot_reuse_old_instance_context", "wrong-context"},
		{"lost_preparation_reply_recovers_committed_original", "lost-prepare"},
		{"lost_evidence_reply_recovers_without_fresh_get", "lost-evidence"},
		{"lost_native_capture_reply_exact_replay_no_provider_get", "lost-capture"},
		{"native_receipt_recovery_does_not_lookup_provider", "receipt"},
		{"current_native_grant_revocation_withholds_durable_input", "revoked"},
		{"post_commit_grant_revocation_keeps_original", "post-revoked"},
		{"lost_commit_reply_plus_revocation_preserves_uncertainty", "lost-revoked"},
		{"lost_commit_reply_plus_cancellation_preserves_uncertainty", "lost-canceled"},
		{"missing_reverse_owner_cannot_use_retained_outbox", "missing-reverse"},
		{"wrong_decryption_key_is_not_absence", "wrong-key"},
		{"tampered_indexed_state_rejects_authenticated_payload", "state"},
		{"tampered_indexed_revision_rejects_authenticated_payload", "revision"},
		{"tampered_partition_rejects_authenticated_payload", "partition"},
		{"tampered_expiration_rejects_authenticated_payload", "expiry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeInputFixture(t)
			var out CheckoutInput
			var err error
			switch tc.scenario {
			case "lost-prepare", "post-revoked", "lost-revoked", "lost-canceled":
				f.reply.lose = tc.scenario != "post-revoked"
				if tc.scenario == "post-revoked" || tc.scenario == "lost-revoked" {
					f.reply.after = func() { f.revoke(t) }
				}
				if tc.scenario == "lost-canceled" {
					ctx, cancel := context.WithCancel(f.ctx)
					t.Cleanup(cancel)
					f.ctx = ctx
					f.reply.after = cancel
				}
				out, err = f.outbox.RetainPreparation(f.ctx, "worker-original", f.intent)
				require.Error(t, err)
				require.Equal(t, CheckoutInput{}, out)
				if f.reply.lose {
					require.ErrorIs(t, err, recordstore.ErrUncertain)
				}
				if tc.scenario == "post-revoked" || tc.scenario == "lost-revoked" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
					f.restore(t)
				}
				if tc.scenario == "lost-canceled" {
					require.ErrorIs(t, err, context.Canceled)
					f.ctx = context.WithoutCancel(f.ctx)
				}
				f.reply.lose = false
				f.reply.after = nil
				out, err = f.outbox.RetainPreparation(f.ctx, "worker-original", f.intent)
				require.NoError(t, err)
				require.Nil(t, out.Evidence)
			default:
				f.prepare(t)
				switch tc.scenario {
				case "prepare":
					out, err = f.outbox.Find(f.ctx, "worker-original", f.intent)
					require.NoError(t, err)
					require.Nil(t, out.Evidence)
				case "evidence", "replacement", "wrong-context", "lost-evidence", "lost-capture", "receipt":
					e, lookupErr := f.manager.LookupCheckoutLifecycleEvidence(f.ctx, "worker-original", f.intent)
					require.NoError(t, lookupErr)
					f.reply.lose = tc.scenario == "lost-evidence"
					out, err = f.outbox.RetainEvidence(f.ctx, "worker-original", f.intent, e)
					if f.reply.lose {
						require.ErrorIs(t, err, recordstore.ErrUncertain)
						require.Equal(t, CheckoutInput{}, out)
						f.reply.lose = false
					}
					out, err = f.outbox.RetainPreparation(f.ctx, "worker-original", f.intent)
					require.NoError(t, err)
					require.NotNil(t, out.Evidence)
					require.Equal(t, e, *out.Evidence)
					before := f.raw(t)
					switch tc.scenario {
					case "replacement", "wrong-context":
						g := f.grant
						g.Subject.ID = "worker-replacement"
						g.Revision = 0
						_, grantErr := f.policy.ReplaceGrant(f.ctx, g, 0)
						require.NoError(t, grantErr)
						repo, repoErr := revenuestore.NewRepository(f.store)
						require.NoError(t, repoErr)
						owner, ownerErr := billing.NewCheckoutService(repo, f.clock, f.provider)
						require.NoError(t, ownerErr)
						revenue, revenueErr := billing.NewRevenueService(repo, f.clock)
						require.NoError(t, revenueErr)
						manager, bound := f.worker(t, f.ctx, "worker-replacement", owner, revenue)
						restarted, newErr := NewCheckoutOutbox(f.store, manager)
						require.NoError(t, newErr)
						if tc.scenario == "wrong-context" {
							out, err = restarted.Find(f.ctx, "worker-replacement", f.intent)
							require.ErrorIs(t, err, partnermanager.ErrDenied)
							require.Equal(t, CheckoutInput{}, out)
						} else {
							out, err = restarted.Find(bound, "worker-replacement", f.intent)
							require.NoError(t, err)
							require.Equal(t, e, *out.Evidence)
						}
					case "lost-capture", "receipt":
						f.reply.lose = tc.scenario == "lost-capture"
						a, captureErr := f.manager.CaptureCheckoutLifecycleEvidence(f.ctx, "worker-original", out.Intent, *out.Evidence)
						if f.reply.lose {
							require.ErrorIs(t, captureErr, billing.ErrRevenueUncertain)
							require.Equal(t, billing.CheckoutLifecycleAnchor{}, a)
							f.reply.lose = false
						} else {
							require.NoError(t, captureErr)
						}
						recovered, recoverErr := f.manager.FindCheckoutLifecycleReceipt(f.ctx, "worker-original", out.Intent)
						require.NoError(t, recoverErr)
						require.NoError(t, recovered.ValidateCapturedEvidence(out.Intent, *out.Evidence))
						replayed, replayErr := f.manager.CaptureCheckoutLifecycleEvidence(f.ctx, "worker-original", out.Intent, *out.Evidence)
						require.NoError(t, replayErr)
						require.Equal(t, recovered, replayed)
					default:
						out, err = f.outbox.RetainEvidence(f.ctx, "worker-original", f.intent, e)
						require.NoError(t, err)
					}
					require.Equal(t, []byte(before), []byte(f.raw(t)))
					require.Equal(t, 1, f.provider.calls)
				case "revoked":
					before := f.raw(t)
					f.revoke(t)
					out, err = f.outbox.Find(f.ctx, "worker-original", f.intent)
					require.ErrorIs(t, err, partnermanager.ErrDenied)
					require.Equal(t, CheckoutInput{}, out)
					require.Equal(t, []byte(before), []byte(f.raw(t)))
				case "missing-reverse":
					result, deleteErr := f.db.Collection("ghatd_owned_records").DeleteMany(f.ctx, bson.M{"kind": "billing_checkout_session"})
					require.NoError(t, deleteErr)
					require.EqualValues(t, 1, result.DeletedCount)
					out, err = f.outbox.Find(f.ctx, "worker-original", f.intent)
					require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
					require.Equal(t, CheckoutInput{}, out)
				case "wrong-key":
					cipher, keyErr := encryption.NewPayloadCipher(bytes.Repeat([]byte{0x64}, 32))
					require.NoError(t, keyErr)
					wrong, storeErr := recordstore.NewMongoStoreFromDatabase(f.db, cipher)
					require.NoError(t, storeErr)
					o, newErr := NewCheckoutOutbox(wrong, f.manager)
					require.NoError(t, newErr)
					out, err = o.Find(f.ctx, "worker-original", f.intent)
					require.Error(t, err)
					require.False(t, soleNotFound(err))
					require.Equal(t, CheckoutInput{}, out)
				default:
					id, _ := checkoutIdentity(f.intent)
					value := any("tampered")
					if tc.scenario == "revision" {
						value = int64(2)
					}
					field := tc.scenario
					if field == "expiry" {
						field = "expires_at"
						value = time.Now().Add(time.Hour)
					}
					result, updateErr := f.db.Collection("ghatd_owned_records").UpdateOne(f.ctx, bson.M{"kind": checkoutKind, "id": id}, bson.M{"$set": bson.M{field: value}})
					require.NoError(t, updateErr)
					require.EqualValues(t, 1, result.ModifiedCount)
					before := f.raw(t)
					out, err = f.outbox.Find(f.ctx, "worker-original", f.intent)
					require.Error(t, err)
					require.False(t, soleNotFound(err))
					require.Equal(t, CheckoutInput{}, out)
					require.Equal(t, []byte(before), []byte(f.raw(t)))
				}
			}
			if tc.scenario == "prepare" || tc.scenario == "evidence" || tc.scenario == "replacement" || tc.scenario == "lost-prepare" || tc.scenario == "lost-evidence" {
				require.NoError(t, out.Intent.ValidateAcknowledgedInput(f.intent))
				require.Equal(t, f.intent.Request, out.Intent.Request)
				require.Equal(t, f.intent.CreatedAt, out.Intent.CreatedAt)
			}
			raw := f.raw(t)
			for _, private := range []string{"payer@example.test", "native-payer", "cs_original", "cus_original", "sub_trial", "price_frozen", "trial-original"} {
				require.NotContains(t, string(raw), private)
			}
			require.Equal(t, bson.TypeBinary, raw.Lookup("payload").Type)
			if tc.scenario != "expiry" {
				require.Equal(t, bson.Type(0), raw.Lookup("expires_at").Type)
			}
			require.Equal(t, bson.Type(0), raw.Lookup("sequence").Type)
			count, countErr := f.db.Collection("ghatd_owned_records").CountDocuments(context.WithoutCancel(f.ctx), bson.M{"kind": bson.M{"$in": []string{"billing_revenue_fact", "billing_subscription_status_head", "billing_subscription_status_capture", "partner_journal", "partner_ledger_head", "partner_earnings_receipt"}}})
			require.NoError(t, countErr)
			require.Zero(t, count)
		})
	}
}

func TestCheckoutOutboxNativeConcurrentEvidence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		different bool
	}{
		{"two_exact_writers_recover_one_immutable_record", false},
		{"different_evidence_writers_have_one_winner", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeInputFixture(t)
			f.prepare(t)
			e, err := f.manager.LookupCheckoutLifecycleEvidence(f.ctx, "worker-original", f.intent)
			require.NoError(t, err)
			e2 := e
			if tc.different {
				e2.CreatedAt = e2.CreatedAt.Add(time.Second)
			}
			type result struct {
				out CheckoutInput
				err error
			}
			results := make(chan result, 2)
			start := make(chan struct{})
			var wait sync.WaitGroup
			for _, evidence := range []paymentprovider.RevenueCheckoutEvidence{e, e2} {
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					out, err := f.outbox.RetainEvidence(f.ctx, "worker-original", f.intent, evidence)
					results <- result{out, err}
				}()
			}
			close(start)
			wait.Wait()
			close(results)
			success, conflict := 0, 0
			for r := range results {
				if r.err == nil {
					success++
					require.NotNil(t, r.out.Evidence)
				} else {
					require.ErrorIs(t, r.err, recordstore.ErrConflict)
					require.Equal(t, CheckoutInput{}, r.out)
					conflict++
				}
			}
			if tc.different {
				require.Equal(t, 1, success)
				require.Equal(t, 1, conflict)
			} else {
				require.Equal(t, 2, success)
				require.Zero(t, conflict)
			}
			out, err := f.outbox.Find(f.ctx, "worker-original", f.intent)
			require.NoError(t, err)
			require.True(t, *out.Evidence == e || *out.Evidence == e2)
			raw := f.raw(t)
			require.EqualValues(t, 2, raw.Lookup("revision").Int64())
			require.Equal(t, "evidence", raw.Lookup("state").StringValue())
			require.Equal(t, 1, f.provider.calls)
		})
	}
}

var _ CheckoutValidator = (*billingmanager.Service)(nil)
