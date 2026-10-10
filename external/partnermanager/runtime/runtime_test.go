package partnerruntime

import (
	"bytes"
	"context"
	"fmt"
	"github.com/google/uuid"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// These narrow fixtures cover composition, not platform identity or permission
// verification. Those contracts have separate current owning-service tests.
// Revenue, financial services and persistence below are their real owners.
type sharedPartnersIdentityFixture struct{}

func (*sharedPartnersIdentityFixture) GetPartnerPrincipal(_ context.Context, id string) (partnermanager.Principal, error) {
	if id != "fixture-customer" {
		return partnermanager.Principal{}, partnermanager.ErrDenied
	}
	return partnermanager.Principal{ID: id, Active: true, EmailVerified: true, Individual: true, RegionEligible: true}, nil
}
func (*sharedPartnersIdentityFixture) GetSignupFact(context.Context, string) (partnermanager.SignupFact, error) {
	return partnermanager.SignupFact{}, partnermanager.ErrUnavailable
}

type sharedPartnersAuthorityFixture struct{}

func (*sharedPartnersAuthorityFixture) CheckPartners(_ context.Context, actor, capability, target string) error {
	if actor == "fixture-customer" && target == actor && (capability == partnermanager.CapabilitySelf || capability == partnermanager.CapabilityEnroll || capability == partnermanager.CapabilityClaims) {
		return nil
	}
	return partnermanager.ErrDenied
}

type sharedPartnersGroupsFixture struct{}

func (*sharedPartnersGroupsFixture) PartnerGroupIDs(context.Context, string) ([]string, error) {
	return []string{}, nil
}

func sharedPartnersCompositionFixture() (Config, Dependencies) {
	window := 30 * 24 * time.Hour
	cfg := Config{
		Program:      partnerprogram.Config{DefaultRateBasisPoints: 2000, DefaultHoldDays: 7, Currency: "EUR", CurrencyExponent: 2, TermsVersion: "fixture-terms", AttributionWindow: window, EligiblePlanIDs: []string{"fixture-plan"}},
		PayloadKey:   bytes.Repeat([]byte{0x56}, 32),
		ReservedKeys: [][]byte{bytes.Repeat([]byte{0x12}, 32), bytes.Repeat([]byte{0x34}, 32)},
		Evidence:     referral.EvidenceConfig{ProgramID: partnerprogram.ProgramID, ActiveKeyID: "fixture-v1", Keys: map[string][]byte{"fixture-v1": bytes.Repeat([]byte{0x78}, 32)}, Window: window},
		Controls:     partnermanager.Controls{Enrollment: true, Attribution: true},
		Queue:        partnermanager.WorkQueueConfig{ProgramID: partnerprogram.ProgramID, LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour},
	}
	deps := Dependencies{Identity: &sharedPartnersIdentityFixture{}, Authority: &sharedPartnersAuthorityFixture{}, Groups: &sharedPartnersGroupsFixture{}, Clock: partnerearnings.ClockFunc(func() time.Time { return time.Date(2026, 10, 7, 21, 0, 0, 0, time.UTC) }), IDs: runtimeTestIDs{}}
	return cfg, deps
}

func TestSharedPartnersCompositionRejectsInvalidAdmissionBeforeStorage(t *testing.T) {
	type testCase struct {
		name, state string
		want        error
	}
	for _, tc := range []testCase{
		{name: "nil_context", state: "nil-context", want: partnermanager.ErrUnavailable},
		{name: "cancelled_context", state: "cancelled", want: context.Canceled},
		{name: "missing_database", state: "nil-db", want: partnermanager.ErrUnavailable},
		{name: "typed_nil_identity", state: "nil-identity", want: partnermanager.ErrUnavailable},
		{name: "typed_nil_authority", state: "nil-authority", want: partnermanager.ErrUnavailable},
		{name: "typed_nil_groups", state: "nil-groups", want: partnermanager.ErrUnavailable},
		{name: "nil_clock_function", state: "nil-clock", want: partnermanager.ErrUnavailable},
		{name: "missing_id_generator", state: "nil-ids", want: partnermanager.ErrUnavailable},
		{name: "no_generated_payload_key", state: "no-key", want: partnermanager.ErrInvalid},
		{name: "zero_payload_key", state: "zero-key", want: partnermanager.ErrInvalid},
		{name: "reused_agreement_key", state: "reused-agreement", want: partnermanager.ErrInvalid},
		{name: "missing_host_key_separation", state: "missing-reserved", want: partnermanager.ErrInvalid},
		{name: "reused_reserved_keys", state: "same-reserved", want: partnermanager.ErrInvalid},
		{name: "evidence_key_is_not_payload_key", state: "reused-evidence", want: partnermanager.ErrInvalid},
		{name: "retained_evidence_key_is_not_csrf_key", state: "reused-retained", want: partnermanager.ErrInvalid},
		{name: "missing_signing_key", state: "no-evidence-key", want: referral.ErrUnavailable},
		{name: "signed_program_must_match", state: "wrong-program", want: partnermanager.ErrInvalid},
		{name: "signed_window_must_match", state: "wrong-window", want: partnermanager.ErrInvalid},
		{name: "queue_program_must_match", state: "wrong-queue", want: partnermanager.ErrInvalid},
		{name: "commercial_plan_required", state: "no-plan", want: partnerprogram.ErrInvalid},
		{name: "zero_hold_is_not_implicit", state: "zero-hold", want: partnerprogram.ErrInvalid},
		{name: "optional_reporting_requires_explicit_scope", state: "bad-reporting", want: partnermanager.ErrInvalid},
		{name: "negative_claim_minimum_rejected", state: "negative-minimum", want: partnermanager.ErrInvalid},
		{name: "visit_measurement_subsecond_rejected_before_storage", state: "subsecond-visit", want: referral.ErrInvalid},
		{name: "visit_measurement_unbounded_rejected_before_storage", state: "unbounded-visit", want: referral.ErrInvalid},
		{name: "visit_measurement_requires_explicit_retention", state: "missing-retention", want: partnermanager.ErrInvalid},
		{name: "raw_observation_retention_bounded_by_owner", state: "invalid-retention", want: referral.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, deps := sharedPartnersCompositionFixture()
			ctx := t.Context()
			// A disconnected native database must never be touched in these
			// rejected cases. A missed precondition would reach an owning error.
			db := new(mongo.Database)
			switch tc.state {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-db":
				db = nil
			case "nil-identity":
				var missing *sharedPartnersIdentityFixture
				deps.Identity = missing
			case "nil-authority":
				var missing *sharedPartnersAuthorityFixture
				deps.Authority = missing
			case "nil-groups":
				var missing *sharedPartnersGroupsFixture
				deps.Groups = missing
			case "nil-clock":
				deps.Clock = partnerearnings.ClockFunc(nil)
			case "nil-ids":
				deps.IDs = nil
			case "no-key":
				cfg.PayloadKey = nil
			case "zero-key":
				cfg.PayloadKey = make([]byte, 32)
			case "reused-agreement":
				cfg.PayloadKey = cfg.ReservedKeys[0]
			case "missing-reserved":
				cfg.ReservedKeys = nil
			case "same-reserved":
				cfg.ReservedKeys[1] = cfg.ReservedKeys[0]
			case "reused-evidence":
				cfg.Evidence.Keys["fixture-v1"] = cfg.PayloadKey
			case "reused-retained":
				cfg.Evidence.Keys["old-v1"] = cfg.ReservedKeys[1]
			case "no-evidence-key":
				cfg.Evidence.Keys = nil
			case "wrong-program":
				cfg.Evidence.ProgramID = "another-program"
			case "wrong-window":
				cfg.Evidence.Window += time.Hour
			case "wrong-queue":
				cfg.Queue.ProgramID = "another-program"
			case "no-plan":
				cfg.Program.EligiblePlanIDs = nil
			case "zero-hold":
				cfg.Program.DefaultHoldDays = 0
			case "bad-reporting":
				cfg.RevenueReporting = &partnermanager.RevenueReportingConfig{StatusMaxAge: time.Hour}
			case "negative-minimum":
				cfg.Claims.MinimumMinor = -1
			case "subsecond-visit":
				cfg.VisitWindow = time.Millisecond
				cfg.Analytics = &referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour}
			case "unbounded-visit":
				cfg.VisitWindow = 25 * time.Hour
				cfg.Analytics = &referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour}
			case "missing-retention":
				cfg.VisitWindow = time.Hour
			case "invalid-retention":
				cfg.Analytics = &referral.AnalyticsConfig{ObservationRetention: time.Hour}
			}
			runtime, err := initialiseSharedPartners(ctx, db, cfg, deps)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, runtime)
		})
	}
}

func TestSharedPartnersNativeCompositionAndRestart(t *testing.T) {
	type testCase struct {
		name             string
		reopen           bool
		wrongKey         bool
		invalidQueue     bool
		pauseReferral    bool
		reporting        bool
		invalidMinimum   bool
		minimumAdmission bool
	}
	for _, tc := range []testCase{
		{name: "prepared_native_owners"},
		{name: "stable_keys_preserve_enrollment_and_link", reopen: true},
		{name: "changed_payload_key_cannot_invent_new_enrollment", reopen: true, wrongKey: true},
		{name: "queue_bounds_rejected_before_preparation", invalidQueue: true},
		{name: "attribution_pause_does_not_enable_links", pauseReferral: true},
		{name: "reporting_uses_same_real_revenue_owner", reporting: true},
		{name: "negative_claim_minimum_rejected_before_preparation", invalidMinimum: true},
		{name: "configured_minimum_reaches_actual_owning_manager", minimumAdmission: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("hostapp_partner_compose_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			cfg, deps := sharedPartnersCompositionFixture()
			if tc.invalidQueue {
				cfg.Queue.LeaseDuration = 0
			}
			if tc.invalidMinimum {
				cfg.Claims.MinimumMinor = -1
			}
			if tc.minimumAdmission {
				cfg.Claims.MinimumMinor = 100
				cfg.Controls.Claims = true
			}
			if tc.pauseReferral {
				cfg.Controls.Attribution = false
			}
			if tc.reporting {
				// This exercises WithRevenueReporting's actual service-port
				// assertion without inventing a paid fact or fresh provider status.
				cfg.RevenueReporting = &partnermanager.RevenueReportingConfig{Scopes: []billing.RevenueScope{{Provider: "stripe", AccountID: "acct_fixture", LiveMode: false}}, StatusMaxAge: time.Hour}
			}
			runtime, err := initialiseSharedPartners(ctx, db, cfg, deps)
			if tc.invalidQueue || tc.invalidMinimum {
				require.ErrorIs(t, err, partnermanager.ErrInvalid)
				require.Nil(t, runtime)
				collections, err := db.ListCollectionNames(ctx, bson.M{})
				require.NoError(t, err)
				require.Empty(t, collections, "invalid queue or minimum must not prepare indexes or run a write probe")
				return
			}
			require.NoError(t, err)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count, "construction must not mint policy, participant or financial history")
			_, err = runtime.Revenue.GetRevenueFact(ctx, "missing-revenue")
			require.ErrorIs(t, err, billing.ErrRevenueNotFound, "actual owning absence, no legacy access projection")
			partner, err := runtime.Manager.EnrollSelf(ctx, "fixture-customer", "fixture-terms")
			require.NoError(t, err)
			if tc.minimumAdmission {
				// No destination is needed to reject a new below-minimum intent.
				// If factory propagation were missing, this would return owning
				// destination absence instead of the configured admission error.
				claim, err := runtime.Manager.RequestClaimWithDestination(ctx, "fixture-customer", partnermanager.SelfClaimRequest{AmountMinor: 99, ExpectedDestinationVersion: 1, IdempotencyKey: "below-minimum-intent"})
				require.ErrorIs(t, err, partnermanager.ErrInvalid)
				require.Empty(t, claim.ID)
				claims, err := runtime.Manager.ListClaims(ctx, "fixture-customer", nil, 10, "")
				require.NoError(t, err)
				require.Empty(t, claims)
			}
			link, err := runtime.Manager.GetOrCreateLink(ctx, "fixture-customer")
			if tc.pauseReferral {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
				require.Empty(t, link.ID)
				return
			}
			require.NoError(t, err)
			if tc.reopen {
				if tc.wrongKey {
					cfg.PayloadKey = bytes.Repeat([]byte{0x9a}, 32)
				}
				runtime, err = initialiseSharedPartners(ctx, db, cfg, deps)
				require.NoError(t, err)
			}
			current, err := runtime.Manager.EnrollSelf(ctx, "fixture-customer", "fixture-terms")
			if tc.wrongKey {
				// The readiness probe checks transactions, not every retained
				// ciphertext. A key mismatch must remain an owning read failure;
				// it cannot become absence and mint another participant.
				require.ErrorIs(t, err, encryption.ErrInvalidPayload)
				require.Empty(t, current.ID)
				count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": "partner_program_participant"})
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
				return
			}
			require.NoError(t, err)
			require.Equal(t, partner.ID, current.ID)
			currentLink, err := runtime.Manager.GetOrCreateLink(ctx, "fixture-customer")
			require.NoError(t, err)
			require.Equal(t, link.ID, currentLink.ID)
			require.Equal(t, link.Code, currentLink.Code)
			balances, err := runtime.Earnings.Balances(ctx, partner.ID)
			require.NoError(t, err)
			require.Zero(t, balances.AvailableMinor, "enrollment and links do not invent earnings")
			versions, err := runtime.Program.ListPolicyVersions(ctx)
			require.NoError(t, err)
			require.Empty(t, versions, "no privileged startup policy seed")
			backlog, err := runtime.Work.GetBacklog(ctx)
			require.NoError(t, err)
			require.Zero(t, backlog.Counts.Pending)
			require.Equal(t, "discovered_pending_work", backlog.Coverage, "empty discovery is not proof of provider completeness")
		})
	}
}

type runtimeTestIDs struct{}

func (runtimeTestIDs) NewID() string { return uuid.NewString() }

// Test setup deliberately mirrors the public two-step startup contract.
func initialiseSharedPartners(ctx context.Context, db *mongo.Database, cfg Config, deps Dependencies) (*Runtime, error) {
	if ctx == nil {
		return nil, partnermanager.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := NewRuntime(db, cfg, deps)
	if err != nil {
		return nil, err
	}
	if err := r.Prepare(ctx); err != nil {
		return nil, err
	}
	return r, nil
}
