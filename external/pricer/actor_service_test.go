package pricer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/pricer"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// priceActorWrite records selected-resource attribution independently of the
// existing creator. It is not an authorization provider or a database fake.
type priceActorWrite struct{ operation, target, actor string }

// actorPriceStore records the eight mutation capabilities and required reads.
// Unimplemented capabilities panic through the embedded interface so tests
// cannot silently succeed after an unexpected read or write.
type actorPriceStore struct {
	pricer.PricerRepository
	plan                  *pricer.PricePlan
	feature               *pricer.PriceFeature
	reads                 []string
	writes                []priceActorWrite
	readError, writeError error
	cancelRead            context.CancelFunc
}

func newActorPriceStore() *actorPriceStore {
	plan := makeValidPlan()
	plan.ID, plan.NanoID = priceTargetID, "stored-nano"
	plan.CreatedAt, plan.CreatedByID = "2025-01-01T00:00:00Z", "original-creator"
	plan.PublishedAt, plan.PublishedByID = "2025-01-02T00:00:00Z", "original-publisher"
	plan.DeletedAt, plan.DeletedByID = "2025-01-03T00:00:00Z", "original-deleter"
	return &actorPriceStore{plan: plan, feature: &pricer.PriceFeature{
		ID: priceTargetID, NanoID: "stored-nano", Slug: "api-access", Name: "API access", Type: pricer.PriceFeatureTypeBoolean,
		CreatedAt: plan.CreatedAt, CreatedByID: plan.CreatedByID, PublishedAt: plan.PublishedAt, PublishedByID: plan.PublishedByID, DeletedAt: plan.DeletedAt, DeletedByID: plan.DeletedByID,
	}}
}

func (s *actorPriceStore) GetPricePlanByID(_ context.Context, id string, _ *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
	s.reads = append(s.reads, id)
	if s.cancelRead != nil {
		s.cancelRead()
	}
	return s.plan, s.readError
}
func (s *actorPriceStore) GetFeatureByID(_ context.Context, id string) (*pricer.PriceFeature, error) {
	s.reads = append(s.reads, id)
	if s.cancelRead != nil {
		s.cancelRead()
	}
	return s.feature, s.readError
}
func (s *actorPriceStore) CreatePricePlan(_ context.Context, p *pricer.PricePlan) (*pricer.PricePlan, error) {
	s.writes = append(s.writes, priceActorWrite{"create plan", p.ID, p.CreatedByID})
	return p, s.writeError
}
func (s *actorPriceStore) UpdatePricePlan(_ context.Context, p *pricer.PricePlan) (*pricer.PricePlan, error) {
	s.writes = append(s.writes, priceActorWrite{"update plan", p.ID, p.UpdatedByID})
	return p, s.writeError
}
func (s *actorPriceStore) CreateFeature(_ context.Context, p *pricer.PriceFeature) (*pricer.PriceFeature, error) {
	s.writes = append(s.writes, priceActorWrite{"create feature", p.ID, p.CreatedByID})
	return p, s.writeError
}
func (s *actorPriceStore) UpdateFeature(_ context.Context, p *pricer.PriceFeature) (*pricer.PriceFeature, error) {
	s.writes = append(s.writes, priceActorWrite{"update feature", p.ID, p.UpdatedByID})
	return p, s.writeError
}
func (s *actorPriceStore) PublishPricePlan(_ context.Context, id, actor, _ string) error {
	s.writes = append(s.writes, priceActorWrite{"publish plan", id, actor})
	return s.writeError
}
func (s *actorPriceStore) ArchivePricePlan(_ context.Context, id, actor, _ string) error {
	s.writes = append(s.writes, priceActorWrite{"archive plan", id, actor})
	return s.writeError
}
func (s *actorPriceStore) SoftDeletePricePlan(_ context.Context, id, actor, _ string) error {
	s.writes = append(s.writes, priceActorWrite{"delete plan", id, actor})
	return s.writeError
}
func (s *actorPriceStore) SoftDeleteFeature(_ context.Context, id, actor, _ string) error {
	s.writes = append(s.writes, priceActorWrite{"delete feature", id, actor})
	return s.writeError
}

// invokePriceMutation dispatches actual domain methods with explicit actor and
// target values. The nil-request switch tests typed-nil boundary handling.
func invokePriceMutation(s *pricer.Service, ctx context.Context, op, actor string, nilRequest bool) error {
	switch op {
	case "create plan":
		r := &pricer.CreatePricePlanRequest{ActorID: actor, Name: "Plan"}
		if nilRequest {
			r = nil
		}
		_, err := s.CreatePricePlan(ctx, r)
		return err
	case "update plan":
		r := &pricer.UpdatePricePlanRequest{ActorID: actor, ID: priceTargetID, Name: stringPtr("Updated")}
		if nilRequest {
			r = nil
		}
		_, err := s.UpdatePricePlan(ctx, r)
		return err
	case "publish plan":
		r := &pricer.PublishPricePlanRequest{ActorID: actor, ID: priceTargetID}
		if nilRequest {
			r = nil
		}
		_, err := s.PublishPricePlan(ctx, r)
		return err
	case "archive plan":
		r := &pricer.ArchivePricePlanRequest{ActorID: actor, ID: priceTargetID}
		if nilRequest {
			r = nil
		}
		_, err := s.ArchivePricePlan(ctx, r)
		return err
	case "delete plan":
		r := &pricer.DeletePricePlanRequest{ActorID: actor, ID: priceTargetID}
		if nilRequest {
			r = nil
		}
		_, err := s.DeletePricePlan(ctx, r)
		return err
	case "create feature":
		r := &pricer.CreateFeatureRequest{ActorID: actor, Name: "Feature", Type: pricer.PriceFeatureTypeBoolean}
		if nilRequest {
			r = nil
		}
		_, err := s.CreateFeature(ctx, r)
		return err
	case "update feature":
		r := &pricer.UpdateFeatureRequest{ActorID: actor, ID: priceTargetID, Name: stringPtr("Updated")}
		if nilRequest {
			r = nil
		}
		_, err := s.UpdateFeature(ctx, r)
		return err
	default:
		r := &pricer.DeleteFeatureRequest{ActorID: actor, ID: priceTargetID}
		if nilRequest {
			r = nil
		}
		_, err := s.DeleteFeature(ctx, r)
		return err
	}
}

func TestPriceMutationActorsAndEntryGuards(t *testing.T) {
	for _, op := range []struct {
		name    string
		invalid error
	}{
		{"create plan", pricer.ErrInvalidPricePlanPayload}, {"update plan", pricer.ErrInvalidPricePlanPayload},
		{"publish plan", pricer.ErrPricePlanIDRequired}, {"archive plan", pricer.ErrPricePlanIDRequired}, {"delete plan", pricer.ErrPricePlanIDRequired},
		{"create feature", pricer.ErrInvalidPriceFeaturePayload}, {"update feature", pricer.ErrInvalidPriceFeaturePayload}, {"delete feature", pricer.ErrPriceFeatureIDRequired},
	} {
		for _, mode := range []string{"trusted in-process", "matching context", "matching user object", "empty actor", "whitespace actor", "different context actor", "anonymous context", "ID-only context", "user object only", "contradictory user object", "nil request", "nil context", "cancelled", "nil service", "nil port", "typed nil port"} {
			t.Run(op.name+"/"+mode, func(t *testing.T) {
				port := newActorPriceStore()
				s := pricer.NewService(port)
				ctx := context.Background()
				actor := priceActorID
				var want error
				switch mode {
				case "matching context":
					ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), true)
				case "matching user object":
					ctx = accesshelpers.TransitUserWith(accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), true), &user.UniversalUser{ID: actor})
				case "user object only":
					ctx = accesshelpers.TransitUserWith(ctx, &user.UniversalUser{ID: actor})
					want = pricer.ErrPriceUserIDRequired
				case "contradictory user object":
					ctx = accesshelpers.TransitUserWith(accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), true), &user.UniversalUser{ID: "other"})
					want = pricer.ErrPriceUserIDRequired
				case "empty actor":
					actor = ""
					want = pricer.ErrPriceUserIDRequired
				case "whitespace actor":
					actor = "  "
					want = pricer.ErrPriceUserIDRequired
				case "different context actor":
					ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "other"), true)
					want = pricer.ErrPriceUserIDRequired
				case "anonymous context":
					ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), false)
					want = pricer.ErrPriceUserIDRequired
				case "ID-only context":
					ctx = accesshelpers.TransitWith(ctx, actor)
					want = pricer.ErrPriceUserIDRequired
				case "nil request":
					want = op.invalid
				case "nil context":
					ctx = nil
					want = op.invalid
				case "cancelled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
					want = context.Canceled
				case "nil service":
					s = nil
					want = pricer.ErrPricerUnavailable
				case "nil port":
					s = pricer.NewService(nil)
					want = pricer.ErrPricerUnavailable
				case "typed nil port":
					s = pricer.NewService((*actorPriceStore)(nil))
					want = pricer.ErrPricerUnavailable
				}
				err := invokePriceMutation(s, ctx, op.name, actor, mode == "nil request")
				if want != nil {
					require.Equal(t, want, err)
					require.Empty(t, port.reads)
					require.Empty(t, port.writes)
					return
				}
				require.NoError(t, err)
				require.Len(t, port.writes, 1)
				require.Equal(t, actor, port.writes[0].actor)
				if op.name != "create plan" && op.name != "create feature" {
					require.Equal(t, priceTargetID, port.writes[0].target)
				}
				require.Equal(t, "original-creator", port.plan.CreatedByID)
				require.Equal(t, "original-publisher", port.plan.PublishedByID)
			})
		}
	}
}

func TestPriceReplacementTargetAndHistory(t *testing.T) {
	for _, kind := range []string{"plan", "feature"} {
		for _, mode := range []string{"same selected target", "replacement-only target", "different selected target", "empty replacement ID", "missing result", "wrong result ID", "lookup failure", "cancelled after lookup"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				port := newActorPriceStore()
				plan := *port.plan
				feature := *port.feature
				plan.CreatedAt, plan.CreatedByID, plan.PublishedByID, plan.DeletedByID, plan.NanoID = "forged", "forged", "forged", "forged", "forged"
				feature.CreatedAt, feature.CreatedByID, feature.PublishedByID, feature.DeletedByID, feature.NanoID = "forged", "forged", "forged", "forged", "forged"
				selected := priceTargetID
				var want error
				ctx := context.Background()
				switch mode {
				case "replacement-only target":
					selected = ""
				case "different selected target":
					selected = "other"
					if kind == "plan" {
						want = pricer.ErrInvalidPricePlanPayload
					} else {
						want = pricer.ErrInvalidPriceFeaturePayload
					}
				case "empty replacement ID":
					plan.ID = ""
					feature.ID = ""
					if kind == "plan" {
						want = pricer.ErrPricePlanIDRequired
					} else {
						want = pricer.ErrPriceFeatureIDRequired
					}
				case "missing result":
					port.plan = nil
					port.feature = nil
					want = pricer.ErrPricerUnavailable
				case "wrong result ID":
					port.plan.ID = "other"
					port.feature.ID = "other"
					want = pricer.ErrPricerUnavailable
				case "lookup failure":
					want = fmt.Errorf("private storage diagnostic: %w", pricer.ErrDatabaseError)
					port.readError = want
				case "cancelled after lookup":
					ctx, port.cancelRead = context.WithCancel(ctx)
					defer port.cancelRead()
					want = context.Canceled
				}
				s := pricer.NewService(port)
				var err error
				if kind == "plan" {
					var got *pricer.UpdatePricePlanResponse
					got, err = s.UpdatePricePlan(ctx, &pricer.UpdatePricePlanRequest{ActorID: priceActorID, ID: selected, PricePlan: &plan})
					if want == nil {
						require.NoError(t, err)
						require.NotSame(t, &plan, got.PricePlan)
						require.Equal(t, port.plan.CreatedAt, got.PricePlan.CreatedAt)
						require.Equal(t, "original-creator", got.PricePlan.CreatedByID)
						require.Equal(t, "original-publisher", got.PricePlan.PublishedByID)
						require.Equal(t, "original-deleter", got.PricePlan.DeletedByID)
						require.Equal(t, "stored-nano", got.PricePlan.NanoID)
						require.Equal(t, priceActorID, got.PricePlan.UpdatedByID)
					}
					require.Equal(t, "forged", plan.CreatedByID)
				} else {
					var got *pricer.UpdateFeatureResponse
					got, err = s.UpdateFeature(ctx, &pricer.UpdateFeatureRequest{ActorID: priceActorID, ID: selected, Feature: &feature})
					if want == nil {
						require.NoError(t, err)
						require.NotSame(t, &feature, got.Feature)
						require.Equal(t, port.feature.CreatedAt, got.Feature.CreatedAt)
						require.Equal(t, "original-creator", got.Feature.CreatedByID)
						require.Equal(t, "original-publisher", got.Feature.PublishedByID)
						require.Equal(t, "original-deleter", got.Feature.DeletedByID)
						require.Equal(t, "stored-nano", got.Feature.NanoID)
						require.Equal(t, priceActorID, got.Feature.UpdatedByID)
					}
					require.Equal(t, "forged", feature.CreatedByID)
				}
				if want != nil {
					require.True(t, err == want, "must retain exact native cause: %v", err)
					require.Empty(t, port.writes)
				} else {
					require.Len(t, port.writes, 1)
				}
				if mode == "different selected target" || mode == "empty replacement ID" {
					require.Empty(t, port.reads)
				} else {
					require.Equal(t, []string{priceTargetID}, port.reads)
				}
			})
		}
	}
}

func TestPriceCostNormalizationDoesNotMutateInput(t *testing.T) {
	for _, mode := range []string{"create", "field update", "replacement"} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failure=%t", mode, failed), func(t *testing.T) {
				port := newActorPriceStore()
				costs := []pricer.PriceCost{{Currency: "USD", Amount: 100, BillingCadence: pricer.PriceBillingCadenceMonthly}}
				if failed {
					port.writeError = errors.New("write failure")
				}
				s := pricer.NewService(port)
				var err error
				var result *pricer.PricePlan
				switch mode {
				case "create":
					r, e := s.CreatePricePlan(context.Background(), &pricer.CreatePricePlanRequest{ActorID: priceActorID, Name: "Plan", Costs: costs})
					err = e
					if e == nil {
						result = r.PricePlan
					}
				case "field update":
					r, e := s.UpdatePricePlan(context.Background(), &pricer.UpdatePricePlanRequest{ActorID: priceActorID, ID: priceTargetID, Costs: costs})
					err = e
					if e == nil {
						result = r.PricePlan
					}
				case "replacement":
					p := *port.plan
					p.Costs = costs
					r, e := s.UpdatePricePlan(context.Background(), &pricer.UpdatePricePlanRequest{ActorID: priceActorID, ID: priceTargetID, PricePlan: &p})
					err = e
					if e == nil {
						result = r.PricePlan
					}
				}
				require.True(t, err == port.writeError)
				require.Empty(t, costs[0].ID)
				if !failed {
					require.NotEmpty(t, result.Costs[0].ID)
				}
			})
		}
	}
}

func TestPricePublishConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name               string
		nilResult, wrongID bool
		failure            error
	}{
		{"confirmed", false, false, nil},
		{"missing result", true, false, nil},
		{"wrong resource", false, true, nil},
		{"read failure", false, false, errors.New("confirmation failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, writes := 0, 0
			port := &mockPricerRepository{
				getPricePlanByIDFunc: func(_ context.Context, id string, _ *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
					reads++
					plan := makeValidPlan()
					plan.ID = id
					if reads == 2 {
						if tc.nilResult {
							return nil, nil
						}
						if tc.wrongID {
							plan.ID = "other"
						}
						if tc.failure != nil {
							return nil, tc.failure
						}
					}
					return plan, nil
				},
				publishPricePlanFunc: func(_ context.Context, id, actor, _ string) error {
					writes++
					require.Equal(t, priceTargetID, id)
					require.Equal(t, priceActorID, actor)
					return nil
				},
			}
			got, err := pricer.NewService(port).PublishPricePlan(context.Background(), &pricer.PublishPricePlanRequest{ActorID: priceActorID, ID: priceTargetID})
			require.Equal(t, 2, reads)
			require.Equal(t, 1, writes, "a confirmation failure must not retry the write")
			switch {
			case tc.failure != nil:
				require.True(t, err == tc.failure)
			case tc.nilResult || tc.wrongID:
				require.ErrorIs(t, err, pricer.ErrPricerUnavailable)
			default:
				require.NoError(t, err)
				require.Equal(t, priceTargetID, got.PricePlan.ID)
			}
		})
	}
}
