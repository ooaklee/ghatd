package revenuestore

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type optionalNativeCommands struct {
	mu                sync.Mutex
	succeeded         map[string]int
	failed            int
	lastCommitSession bson.Raw
}

func (c *optionalNativeCommands) commitSession() bson.Raw {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append(bson.Raw(nil), c.lastCommitSession...)
}

func (c *optionalNativeCommands) snapshot() (map[string]int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	counts := make(map[string]int, len(c.succeeded))
	for name, count := range c.succeeded {
		counts[name] = count
	}
	return counts, c.failed
}

type optionalNativePort struct {
	recordstore.MongoPort
	kind, id string
	failFind bool
	cancel   context.CancelFunc
}

func (p *optionalNativePort) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter any, result any, name string, logError bool, onFailure error) error {
	selector, _ := filter.(bson.M)
	selected := name == "owned-record" && selector["kind"] == p.kind && selector["id"] == p.id
	if selected && p.failFind {
		filter = bson.M{"$invalid_optional_read_test_operator": 1}
	}
	err := p.MongoPort.ExecuteFindOneCommandDecodeResult(ctx, collection, filter, result, name, logError, onFailure)
	if selected && p.cancel != nil {
		p.cancel()
	}
	return err
}

type optionalNativeFixture struct {
	repo     *Repository
	service  *billing.RevenueService
	db       *mongo.Database
	ctx      context.Context
	logs     *observer.ObservedLogs
	commands *optionalNativeCommands
	port     *optionalNativePort
	appName  string
	source   billing.RevenueObservation
}

func newOptionalNativeFixture(t *testing.T, uri string) *optionalNativeFixture {
	t.Helper()
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	core, logs := observer.New(zap.DebugLevel)
	ctx = logger.TransitWith(ctx, zap.New(core))
	ctx = logger.WithPackage(ctx, "optional-read-test", zap.String("owner_operation", "fixture.optional_read"))
	commands := &optionalNativeCommands{succeeded: map[string]int{}}
	monitor := &event.CommandMonitor{
		Started: func(_ context.Context, e *event.CommandStartedEvent) {
			if e.CommandName != "commitTransaction" {
				return
			}
			commands.mu.Lock()
			defer commands.mu.Unlock()
			commands.lastCommitSession = append(bson.Raw(nil), e.Command.Lookup("lsid").Document()...)
		},
		Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) {
			commands.mu.Lock()
			defer commands.mu.Unlock()
			commands.succeeded[e.CommandName]++
		},
		Failed: func(context.Context, *event.CommandFailedEvent) {
			commands.mu.Lock()
			defer commands.mu.Unlock()
			commands.failed++
		},
	}
	appName := "ghatd-optional-read-" + rand.Text()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetAppName(appName).SetMonitor(monitor))
	require.NoError(t, err)
	db := client.Database("ghatd_optional_test_" + strings.ToLower(rand.Text()))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, client.Disconnect(cleanup))
	})
	cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	port, err := repository.NewMongoDbRepositoryFromDatabase(db, repository.NewZapRepositoryLogger())
	require.NoError(t, err)
	fault := &optionalNativePort{MongoPort: port}
	store, err := recordstore.NewMongoStore(db, fault, cipher)
	require.NoError(t, err)
	require.NoError(t, store.EnsureIndexes(ctx))
	require.NoError(t, store.Probe(ctx))
	repo, err := NewRepository(store)
	require.NoError(t, err)
	service := revenueService(t, repo)
	source, err := service.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account"}, EnvelopeID: "original-event", SourceFingerprint: "fixture-source-fingerprint", QuarantineReason: "historical_payer_plan_pending"})
	require.NoError(t, err)
	logs.TakeAll()
	return &optionalNativeFixture{repo: repo, service: service, db: db, ctx: ctx, logs: logs, commands: commands, port: fault, appName: appName, source: source}
}

func (f *optionalNativeFixture) seedResolution(t *testing.T) {
	t.Helper()
	_, err := f.service.ResolveQuarantinedRevenue(f.ctx, billing.ResolveRevenueRequest{ObservationID: f.source.ID, ExpectedFingerprint: f.source.Fingerprint, ActorID: "fixture-worker", Reason: "no_subscription_revenue"})
	require.NoError(t, err)
}

func (f *optionalNativeFixture) associationService(t *testing.T, seed bool) (*billing.CheckoutService, *checkoutEvidenceFixture) {
	t.Helper()
	provider := &checkoutEvidenceFixture{}
	service := checkoutServiceFixture(t, f.repo, provider)
	if seed {
		request := checkoutResolverRequest()
		intent, err := service.PrepareCheckout(f.ctx, request.Scope, historicalCheckoutRequest())
		require.NoError(t, err)
		provider.evidence = checkoutEvidence(intent)
		_, err = service.ResolveRevenueAssociation(f.ctx, request)
		require.NoError(t, err)
	}
	provider.calls = 0
	provider.err = paymentprovider.ErrRevenueUnassessable
	return service, provider
}

func requireOptionalNativeFailure(t *testing.T, logs *observer.ObservedLogs, lookup, callback, outcome string) {
	t.Helper()
	entries := logs.FilterMessage("mongo-operation-failed").All()
	require.Len(t, entries, 1)
	entry := entries[0]
	require.Equal(t, zap.ErrorLevel, entry.Level)
	fields := entry.ContextMap()
	require.Equal(t, "fixture.optional_read", fields["owner_operation"])
	if lookup != "" {
		require.Equal(t, lookup, fields["lookup_site"])
	}
	require.Equal(t, "transaction", fields["operation"])
	require.Equal(t, outcome, fields["outcome"])
	require.Equal(t, callback, fields["last_callback_error_identity"])
	require.EqualValues(t, 1, fields["callback_attempts"])
	require.NotContains(t, fields, "error")
}

func TestMongoBillingOptionalReadsPreserveFailures(t *testing.T) {
	type testCase struct {
		name, operation, fault string
		seed                   bool
		wantLogs               bool
	}
	cases := []testCase{
		{"resolution_absence", "resolution", "", false, false},
		{"existing_resolution", "resolution", "", true, false},
		{"required_original_absence", "resolution", "missing-original", false, true},
		{"required_original_native_error", "resolution", "original-driver", false, true},
		{"optional_resolution_native_error", "resolution", "resolution-driver", false, true},
		{"required_original_unreadable", "resolution", "original-corrupt", false, true},
		{"optional_resolution_unreadable", "resolution", "resolution-corrupt", true, true},
		{"required_observation_getter_still_logs_absence", "required-observation", "", false, true},
		{"association_absence_preserves_provider_refusal", "association", "", false, false},
		{"existing_association", "association", "", true, false},
		{"optional_association_native_error", "association", "association-driver", false, true},
		{"optional_association_unreadable", "association", "association-corrupt", true, true},
		{"resolution_cancelled_after_find", "resolution", "cancel", false, true},
		{"association_cancelled_after_find", "association", "cancel", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOptionalNativeFixture(t, os.Getenv("GHATD_TEST_MONGO_URI"))
			var association *billing.CheckoutService
			var provider *checkoutEvidenceFixture
			lookup := "billing.revenue_source.resolution"
			if tc.operation == "association" {
				association, provider = f.associationService(t, tc.seed)
				request := checkoutResolverRequest()
				f.port.kind, f.port.id = kindCheckoutAssociation, associationKey(request.Scope, request.SubscriptionID, request.ProviderPriceID)
				lookup = "billing.revenue_association.stored"
			} else {
				if tc.seed {
					f.seedResolution(t)
				}
				f.port.kind, f.port.id = kindObservation, f.source.ResolutionID()
			}
			if strings.HasPrefix(tc.fault, "original-") {
				f.port.id = f.source.ID
			}
			if tc.fault == "missing-original" {
				_, err := f.db.Collection("ghatd_owned_records").DeleteOne(f.ctx, bson.M{"kind": kindObservation, "id": f.source.ID})
				require.NoError(t, err)
			}
			if strings.HasSuffix(tc.fault, "corrupt") {
				_, err := f.db.Collection("ghatd_owned_records").UpdateOne(f.ctx, bson.M{"kind": f.port.kind, "id": f.port.id}, bson.M{"$set": bson.M{"payload": []byte("fixture-unreadable")}})
				require.NoError(t, err)
			}
			f.port.failFind = strings.HasSuffix(tc.fault, "driver")
			ctx := f.ctx
			if tc.fault == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
				f.port.cancel = cancel
			}
			f.logs.TakeAll()
			before, beforeFailed := f.commands.snapshot()
			var err error
			switch tc.operation {
			case "resolution":
				_, err = f.service.GetRevenueSourceResolution(ctx, f.source.ID)
			case "required-observation":
				_, err = f.service.GetRevenueObservation(ctx, "missing-required-observation")
				lookup = ""
			case "association":
				_, err = association.ResolveRevenueAssociation(ctx, checkoutResolverRequest())
			}
			after, afterFailed := f.commands.snapshot()
			if tc.seed && tc.fault == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if !tc.wantLogs {
				require.Empty(t, f.logs.FilterMessage("mongo-operation-failed").All())
				require.Equal(t, 1, after["commitTransaction"]-before["commitTransaction"])
				require.Equal(t, before["abortTransaction"], after["abortTransaction"])
				require.Equal(t, beforeFailed, afterFailed)
			} else {
				callback, outcome := "internal", "database"
				if tc.fault == "cancel" {
					require.ErrorIs(t, err, context.Canceled)
					require.False(t, errors.Is(err, billing.ErrRevenueNotFound))
					callback, outcome = "cancelled", "cancelled"
				}
				requireOptionalNativeFailure(t, f.logs, lookup, callback, outcome)
				require.Equal(t, before["commitTransaction"], after["commitTransaction"], "unexpected failure must not commit")
				if f.port.failFind {
					var native mongo.ServerError
					require.ErrorAs(t, err, &native)
					require.Greater(t, afterFailed, beforeFailed)
				}
			}
			if provider != nil && tc.wantLogs {
				require.Zero(t, provider.calls, "failed owning read must not fall through to provider evidence")
			}
			t.Logf("operation=%s fault=%s: successful finds=%d, successful commits=%d, native failures=%d, repository errors=%d", tc.operation, tc.fault, after["find"]-before["find"], after["commitTransaction"]-before["commitTransaction"], afterFailed-beforeFailed, f.logs.FilterMessage("mongo-operation-failed").Len())
		})
	}
}

func TestMongoBillingOptionalReadCommitOutcomes(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_FAILPOINT_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_FAILPOINT_URI to a dedicated disposable replica set with enableTestCommands")
	}
	type testCase struct {
		name        string
		labels      []string
		wantError   bool
		invocations int
	}
	cases := []testCase{
		{name: "nonretryable_commit_error", wantError: true, invocations: 1},
		{name: "unknown_commit_result_retries_only_commit", labels: []string{"UnknownTransactionCommitResult"}, invocations: 1},
		{name: "transient_commit_error_retries_callback", labels: []string{"TransientTransactionError"}, invocations: 2},
	}
	for _, operation := range []string{"resolution", "association"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					f := newOptionalNativeFixture(t, uri)
					var association *billing.CheckoutService
					var provider *checkoutEvidenceFixture
					lookup, findsPerRead := "billing.revenue_source.resolution", 2
					if operation == "association" {
						association, provider = f.associationService(t, false)
						lookup, findsPerRead = "billing.revenue_association.stored", 1
					}
					admin := f.db.Client().Database("admin")
					disable := func() {
						cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						require.NoError(t, admin.RunCommand(cleanup, bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: "off"}}).Err())
					}
					t.Cleanup(disable)
					if tc.wantError {
						// A failed native commit can leave its server transaction
						// open after the driver marks the session committed. Kill
						// only this fixture's captured session before dropping its DB.
						t.Cleanup(func() {
							cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							session := f.commands.commitSession()
							if session != nil {
								require.NoError(t, admin.RunCommand(cleanup, bson.D{{Key: "killSessions", Value: bson.A{session}}}).Err())
							}
						})
					}
					data := bson.M{"failCommands": []string{"commitTransaction"}, "errorCode": 2, "appName": f.appName}
					if len(tc.labels) != 0 {
						data["errorLabels"] = tc.labels
					}
					require.NoError(t, admin.RunCommand(f.ctx, bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: bson.M{"times": 1}}, {Key: "data", Value: data}}).Err())
					f.logs.TakeAll()
					before, beforeFailed := f.commands.snapshot()
					var err error
					if operation == "resolution" {
						_, err = f.service.GetRevenueSourceResolution(f.ctx, f.source.ID)
					} else {
						_, err = association.ResolveRevenueAssociation(f.ctx, checkoutResolverRequest())
					}
					after, afterFailed := f.commands.snapshot()
					require.Equal(t, 1, afterFailed-beforeFailed)
					require.Equal(t, findsPerRead*tc.invocations, after["find"]-before["find"])
					if tc.wantError {
						var native mongo.ServerError
						require.ErrorAs(t, err, &native)
						require.True(t, native.HasErrorCode(2))
						require.False(t, errors.Is(err, billing.ErrRevenueNotFound))
						require.Equal(t, before["commitTransaction"], after["commitTransaction"])
						requireOptionalNativeFailure(t, f.logs, lookup, "success", "database")
						if provider != nil {
							require.Zero(t, provider.calls)
						}
					} else {
						if operation == "resolution" {
							require.ErrorIs(t, err, billing.ErrRevenueNotFound)
						} else {
							require.ErrorIs(t, err, billing.ErrRevenueUnassessable)
							require.Equal(t, 1, provider.calls)
						}
						require.Equal(t, 1, after["commitTransaction"]-before["commitTransaction"])
						require.Empty(t, f.logs.FilterMessage("mongo-operation-failed").All())
					}
					t.Logf("operation=%s commit outcome=%s: finds=%d, failed native commands=%d, successful commits=%d, repository errors=%d", operation, tc.name, after["find"]-before["find"], afterFailed-beforeFailed, after["commitTransaction"]-before["commitTransaction"], f.logs.FilterMessage("mongo-operation-failed").Len())
					disable()
					count, err := f.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": bson.M{"$in": []string{kindObservation, kindCheckoutAssociation, kindFact}}})
					require.NoError(t, err)
					require.EqualValues(t, 1, count, "optional reads must preserve only the original source")
				})
			}
		})
	}
}

func TestMongoOptionalResolutionReadersOverlapRecovery(t *testing.T) {
	cases := []struct {
		name             string
		readers, writers int
	}{
		{"optional_read_overlaps_one_recovery", 4, 1},
		{"optional_reads_overlap_identical_recoveries", 8, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOptionalNativeFixture(t, os.Getenv("GHATD_TEST_MONGO_URI"))
			type result struct {
				observation billing.RevenueObservation
				err         error
				optional    bool
			}
			results := make(chan result, tc.readers+tc.writers)
			start := make(chan struct{})
			var group sync.WaitGroup
			for range tc.readers {
				group.Go(func() {
					<-start
					row, err := f.service.GetRevenueSourceResolution(f.ctx, f.source.ID)
					results <- result{row, err, true}
				})
			}
			for range tc.writers {
				group.Go(func() {
					<-start
					row, err := f.service.ResolveQuarantinedRevenue(f.ctx, billing.ResolveRevenueRequest{ObservationID: f.source.ID, ExpectedFingerprint: f.source.Fingerprint, ActorID: "fixture-worker", Reason: "no_subscription_revenue"})
					results <- result{row, err, false}
				})
			}
			close(start)
			group.Wait()
			close(results)
			for result := range results {
				if result.optional && errors.Is(result.err, billing.ErrRevenueNotFound) {
					require.Equal(t, billing.RevenueObservation{}, result.observation)
					continue
				}
				require.NoError(t, result.err)
				require.Equal(t, f.source.ResolutionID(), result.observation.ID)
				require.Equal(t, f.source.ID, result.observation.ResolutionOf)
				require.Equal(t, f.source.Scope, result.observation.Scope)
				require.Equal(t, f.source.EnvelopeID, result.observation.EnvelopeID)
			}
			original, err := f.service.GetRevenueObservation(f.ctx, f.source.ID)
			require.NoError(t, err)
			require.Equal(t, f.source, original)
			resolution, err := f.service.GetRevenueSourceResolution(f.ctx, f.source.ID)
			require.NoError(t, err)
			require.Equal(t, f.source.ID, resolution.ResolutionOf)
			count, err := f.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": kindObservation})
			require.NoError(t, err)
			require.EqualValues(t, 2, count, "one immutable original and one shared resolution must remain")
			require.Empty(t, f.logs.FilterMessage("mongo-operation-failed").FilterField(zap.String("lookup_site", "billing.revenue_source.resolution")).All(), "optional readers must not turn absence into failure")
			// Concurrent recovery writers can emit real transient write-conflict
			// command errors even when their owning transaction later succeeds.
			// Those errors retain the pre-existing telemetry contract.
			for _, entry := range f.logs.FilterMessage("mongo-operation-failed").All() {
				require.NotEqual(t, "transaction", entry.ContextMap()["operation"])
				require.Equal(t, true, entry.ContextMap()["mongo_transient_transaction"])
			}
		})
	}
}
