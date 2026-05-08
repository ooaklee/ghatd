package migrations_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/ooaklee/ghatd/external/pricer"
	pricermigrations "github.com/ooaklee/ghatd/external/pricer/migrations"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/response"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
)

// updateGolden controls whether TestE2E_PricingCardsGolden rewrites the golden
// fixture from the live response. Run `go test ./external/pricer/migrations/...
// -update` after intentional seed changes.
var updateGolden = flag.Bool("update", false, "rewrite golden fixtures from current responses")

const goldenFixturePath = "testdata/pricing_cards.golden.json"

// cardProjection is the stable, frontend-facing slice of a price plan response
// that pricing cards render. Only fields used by the UI are projected so the
// golden fixture stays readable and resilient to non-card metadata churn.
type cardProjection struct {
	Slug        string             `json:"slug"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Status      string             `json:"status"`
	UI          cardUI             `json:"ui"`
	Costs       []cardCost         `json:"costs"`
	Features    []cardFeatureEntry `json:"features"`
}

type cardUI struct {
	Type      string `json:"type"`
	IsPopular bool   `json:"is_popular"`
	CTALabel  string `json:"cta_label"`
}

type cardCost struct {
	Amount             int64  `json:"amount"`
	Currency           string `json:"currency"`
	BillingCadence     string `json:"billing_cadence"`
	DiscountPercentage int    `json:"discount_percentage"`
	PerSeat            bool   `json:"per_seat"`
}

type cardFeatureEntry struct {
	Slug     string `json:"slug"`
	Included bool   `json:"included"`
	Quantity int64  `json:"quantity"`
	Label    string `json:"label"`
}

type goldenFile struct {
	Version     int              `json:"version"`
	Description string           `json:"description"`
	Plans       []cardProjection `json:"plans"`
}

// TestE2E_PricingCardsGolden boots a memongo-backed pricer stack, runs the
// pricing index + Fireflies-style test plans seed, exercises the public price
// plan HTTP endpoints, and compares the card projection against a golden
// fixture. This is the source-of-truth regression for the Companion app
// pricing card surface.
func TestE2E_PricingCardsGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing card test in short mode")
	}

	ctx, mongoHandler, db, dbName := setupE2EMongo(t)

	require.NoError(t, pricermigrations.InitPricingIndexesUp(db), "pricing indexes seed must succeed")
	require.NoError(t, pricermigrations.InitTestPlansSeedUp(db), "test plans seed must succeed")
	_ = ctx

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := pricer.NewRepository(store)
	service := pricer.NewService(repo)
	handler := pricer.NewHandler(service, nil, pricer.PricerErrorMap)

	httpRouter := ghatdrouter.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response)
	pricer.AttachRoutes(&pricer.AttachRoutesRequest{Router: httpRouter, Handler: handler})

	got := goldenFile{
		Version:     1,
		Description: "Stable card-projection for the Fireflies-style E2E pricing seed. Golden fixture for /api/v1/pricing/plans/{slug} responses. Update via -update flag on TestE2E_PricingCardsGolden.",
		Plans:       make([]cardProjection, 0, 3),
	}

	for _, slug := range []string{"free", "pro", "enterprise"} {
		plan := fetchPlanBySlug(t, httpRouter, slug)
		got.Plans = append(got.Plans, projectPlanToCard(plan))
	}

	if *updateGolden {
		writeGolden(t, got)
		t.Log("golden fixture updated; re-run without -update to verify")
		return
	}

	expected := readGolden(t)

	expectedJSON, err := json.MarshalIndent(expected, "", "  ")
	require.NoError(t, err)
	gotJSON, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	assert.JSONEq(t, string(expectedJSON), string(gotJSON), "card projection diverged from golden fixture; re-run with -update if intentional")

	// Also exercise the list endpoint and feature catalog to make sure both
	// continue to return data for the seed. We do not snapshot these — the
	// per-plan golden above already pins the card-critical surface.
	plans := fetchAllPlans(t, httpRouter)
	assert.Len(t, plans, 3, "list endpoint should return all 3 published seed plans")

	features := fetchAllFeatures(t, httpRouter)
	assert.GreaterOrEqual(t, len(features), 8, "feature catalog should contain at least the 8 seed features")
}

// TestE2E_PricingTestPlansSeedDownRollsBackCleanly confirms that the down
// migration removes only documents tagged with the test plans seed marker so
// the migration is reversible without affecting other seed data.
func TestE2E_PricingTestPlansSeedDownRollsBackCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing migration rollback test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)

	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitPricingSeedUp(db))
	require.NoError(t, pricermigrations.InitTestPlansSeedUp(db))

	plansBefore, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	featuresBefore, err := db.Collection(pricer.PriceFeaturesCollection).CountDocuments(ctx, bson.M{})
	require.NoError(t, err)

	require.NoError(t, pricermigrations.InitTestPlansSeedDown(db))

	plansAfter, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	featuresAfter, err := db.Collection(pricer.PriceFeaturesCollection).CountDocuments(ctx, bson.M{})
	require.NoError(t, err)

	assert.Equal(t, plansBefore-3, plansAfter, "rollback should remove exactly the 3 test plans")
	assert.Equal(t, featuresBefore-8, featuresAfter, "rollback should remove exactly the 8 test features")

	// Starter seed plan must remain untouched.
	starterPlanCount, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{"slug": "starter"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), starterPlanCount, "starter seed plan should survive test plans rollback")
}

// fetchPlanBySlug performs a real HTTP call through the gorilla mux router to
// the public GetPricePlanBySlug endpoint. Costs, features and providers are
// always included for single-plan reads.
func fetchPlanBySlug(t *testing.T, r *ghatdrouter.Router, slug string) *pricer.PricePlan {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/plans/"+slug+"?include_costs=true&include_features=true&include_providers=true", nil)
	rec := httptest.NewRecorder()
	r.GetRouter().ServeHTTP(rec, req)
	require.Equalf(t, http.StatusOK, rec.Code, "GET /plans/%s body=%s", slug, rec.Body.String())

	var envelope struct {
		Data *pricer.PricePlan `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), "decode plan body for %s", slug)
	require.NotNil(t, envelope.Data, "plan %s response data must not be nil", slug)
	return envelope.Data
}

func fetchAllPlans(t *testing.T, r *ghatdrouter.Router) []pricer.PricePlan {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/plans?include_costs=true&include_features=true&include_providers=true", nil)
	rec := httptest.NewRecorder()
	r.GetRouter().ServeHTTP(rec, req)
	require.Equalf(t, http.StatusOK, rec.Code, "GET /plans body=%s", rec.Body.String())

	var envelope struct {
		Data []pricer.PricePlan `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	return envelope.Data
}

func fetchAllFeatures(t *testing.T, r *ghatdrouter.Router) []pricer.PriceFeature {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/features", nil)
	rec := httptest.NewRecorder()
	r.GetRouter().ServeHTTP(rec, req)
	require.Equalf(t, http.StatusOK, rec.Code, "GET /features body=%s", rec.Body.String())

	var envelope struct {
		Data []pricer.PriceFeature `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	return envelope.Data
}

// projectPlanToCard reduces a full PricePlan response to the small,
// deterministic surface the frontend pricing cards consume. Feature ordering
// follows the catalog sort_order via slug-keyed lookup, while costs are sorted
// by billing cadence (month before year) for stable comparison.
func projectPlanToCard(plan *pricer.PricePlan) cardProjection {
	ui := readUIBlock(plan.Metadata)

	costs := make([]cardCost, 0, len(plan.Costs))
	for _, c := range plan.Costs {
		flags := readPricingFlags(c.Metadata)
		costs = append(costs, cardCost{
			Amount:             c.Amount,
			Currency:           c.Currency,
			BillingCadence:     string(c.BillingCadence),
			DiscountPercentage: flags.DiscountPercentage,
			PerSeat:            flags.PerSeat,
		})
	}
	sort.SliceStable(costs, func(i, j int) bool {
		return cadenceOrder(costs[i].BillingCadence) < cadenceOrder(costs[j].BillingCadence)
	})

	features := make([]cardFeatureEntry, 0, len(plan.Features))
	for _, f := range plan.Features {
		features = append(features, cardFeatureEntry{
			Slug:     f.FeatureSlug,
			Included: f.Included,
			Quantity: f.Quantity,
			Label:    f.Label,
		})
	}

	return cardProjection{
		Slug:        plan.Slug,
		Name:        plan.Name,
		Description: plan.Description,
		Status:      string(plan.Status),
		UI:          ui,
		Costs:       costs,
		Features:    features,
	}
}

func readUIBlock(metadata map[string]interface{}) cardUI {
	out := cardUI{}
	if metadata == nil {
		return out
	}
	rawUI, ok := metadata["ui"].(map[string]interface{})
	if !ok {
		return out
	}
	if v, ok := rawUI["type"].(string); ok {
		out.Type = v
	}
	if v, ok := rawUI["is_popular"].(bool); ok {
		out.IsPopular = v
	}
	if v, ok := rawUI["cta_label"].(string); ok {
		out.CTALabel = v
	}
	return out
}

type pricingFlags struct {
	PerSeat            bool
	DiscountPercentage int
}

func readPricingFlags(metadata map[string]interface{}) pricingFlags {
	out := pricingFlags{}
	if metadata == nil {
		return out
	}
	raw, ok := metadata["pricing_flags"].(map[string]interface{})
	if !ok {
		return out
	}
	if v, ok := raw["per_seat"].(bool); ok {
		out.PerSeat = v
	}
	if v, ok := readInt(raw["discount_percentage"]); ok {
		out.DiscountPercentage = v
	}
	return out
}

// readInt accepts the variety of numeric types BSON/JSON decoding may produce
// for an `int` field stored in a generic metadata map.
func readInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

func cadenceOrder(cadence string) int {
	switch cadence {
	case string(pricer.PriceBillingCadenceOneTime):
		return 0
	case string(pricer.PriceBillingCadenceWeekly):
		return 1
	case string(pricer.PriceBillingCadenceMonthly):
		return 2
	case string(pricer.PriceBillingCadenceYearly):
		return 3
	}
	return 99
}

func readGolden(t *testing.T) goldenFile {
	t.Helper()

	raw, err := os.ReadFile(filepath.FromSlash(goldenFixturePath))
	require.NoError(t, err, "read golden fixture")
	var g goldenFile
	require.NoError(t, json.Unmarshal(raw, &g), "decode golden fixture")
	return g
}

func writeGolden(t *testing.T, g goldenFile) {
	t.Helper()

	raw, err := json.MarshalIndent(g, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.FromSlash(goldenFixturePath), append(raw, '\n'), 0o644))
}

// setupE2EMongo provisions a Mongo database for an E2E test.
//
// If the PRICER_E2E_MONGO_URI env var is set (e.g. pointing at the local
// docker/compose/docker-compose.test.yaml mongo on mongodb://localhost:47027),
// the helper
// connects to that real Mongo, picks a random database name to isolate this
// test run, and drops the database on cleanup. This is the recommended path
// for arm64 macOS where memongo cannot auto-download a mongod binary.
//
// Otherwise it falls back to memongo, which downloads and supervises an
// in-process mongod (works on linux/amd64 CI runners). If neither path is
// available, the test is skipped — never failed — so the suite stays green
// for environments without a Mongo backend.
func setupE2EMongo(t *testing.T) (context.Context, *repositoryhelpers.Handler, *mongo.Database, string) {
	t.Helper()

	ctx := context.Background()

	if uri := os.Getenv("PRICER_E2E_MONGO_URI"); uri != "" {
		dbName := fmt.Sprintf("pricer_e2e_%d", time.Now().UnixNano())

		mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, dbName))
		require.NoError(t, err, "create mongo handler for PRICER_E2E_MONGO_URI=%s", uri)

		db, err := mongoHandler.GetDatabase(ctx, dbName)
		if err != nil {
			t.Skipf("skipping E2E pricing test: unable to reach PRICER_E2E_MONGO_URI=%s: %v", uri, err)
		}

		t.Cleanup(func() {
			dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = db.Drop(dropCtx)
			_ = mongoHandler.Close(dropCtx)
		})

		return ctx, mongoHandler, db, dbName
	}

	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err != nil {
		t.Skipf("skipping E2E pricing test: set PRICER_E2E_MONGO_URI to a real Mongo (e.g. mongodb://localhost:47027 from ghatd/docker/compose/docker-compose.test.yaml) to run locally; memongo fallback failed: %v", err)
	}
	t.Cleanup(mongoServer.Stop)

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoServer.URI(), dbName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mongoHandler.Close(context.Background()) })

	db, err := mongoHandler.GetDatabase(ctx, dbName)
	require.NoError(t, err)

	return ctx, mongoHandler, db, dbName
}
