package billingmanager

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/pricer"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestBillingIdentityMappersRejectMissingRequest(t *testing.T) {
	for _, route := range []struct {
		name       string
		mapRequest func(*http.Request) (any, error)
	}{
		{"checkout", billingActorMapper(mapRequestToProcessBillingProviderCheckoutRequest)},
		{"portal", billingActorMapper(mapRequestToProcessBillingProviderPortalRequest)},
		{"status", billingActorMapper(mapRequestToGetUserSubscriptionStatusRequest)},
		{"events", billingActorMapper(mapRequestToGetUserBillingEventsRequest)},
		{"detail", billingActorMapper(mapRequestToGetUserBillingDetailRequest)},
		{"plans", billingActorMapper(MapRequestToGetPricingPlansRequest)},
		{"plan", billingActorMapper(MapRequestToGetPricePlanBySlugRequest)},
		{"features", billingActorMapper(MapRequestToGetPriceFeaturesRequest)},
	} {
		t.Run(route.name, func(t *testing.T) {
			_, err := route.mapRequest(nil)
			require.ErrorIs(t, err, ErrInvalidBillingManagerRequestPayload)
		})
	}
}

func TestBillingReadAndPricingEntryGuards(t *testing.T) {
	for _, operation := range []string{"subscription", "events", "detail", "plans", "plan", "features"} {
		t.Run(operation, func(t *testing.T) {
			for _, boundary := range []string{"nil service", "nil context", "nil request", "canceled"} {
				t.Run(boundary, func(t *testing.T) {
					users, store, pricing := &billingActorUsers{}, &billingActorStore{}, &billingActorPricer{}
					s := &Service{UserService: users, BillingService: store, PricerService: pricing}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if boundary == "nil service" {
						s = nil
					}
					if boundary == "nil context" {
						ctx = nil
					}
					if boundary == "canceled" {
						cancel()
					}
					var err error
					switch operation {
					case "subscription":
						r := &GetUserSubscriptionStatusRequest{ActorID: "admin", UserID: "target"}
						if boundary == "nil request" {
							r = nil
						}
						_, err = s.GetUserSubscriptionStatus(ctx, r)
					case "events":
						r := &GetUserBillingEventsRequest{ActorID: "admin", UserID: "target"}
						if boundary == "nil request" {
							r = nil
						}
						_, err = s.GetUserBillingEvents(ctx, r)
					case "detail":
						r := &GetUserBillingDetailRequest{ActorID: "admin", UserID: "target"}
						if boundary == "nil request" {
							r = nil
						}
						_, err = s.GetUserBillingDetail(ctx, r)
					case "plans":
						r := &GetPricingPlansRequest{ActorID: "admin"}
						if boundary == "nil request" {
							r = nil
						}
						_, err = s.GetPricingPlans(ctx, r)
					case "plan":
						r := &GetPricePlanBySlugRequest{ActorID: "admin", GetPricePlanBySlugRequest: &pricer.GetPricePlanBySlugRequest{Slug: "plan"}}
						if boundary == "nil request" {
							r = nil
						}
						_, err = s.GetPricePlanBySlug(ctx, r)
					case "features":
						r := &GetPriceFeaturesRequest{ActorID: "admin"}
						if boundary == "nil request" {
							r = nil
						}
						_, err = s.GetPricingFeatures(ctx, r)
					}
					want := ErrInvalidBillingManagerRequestPayload
					if boundary == "canceled" {
						want = context.Canceled
					}
					require.ErrorIs(t, err, want)
					require.Empty(t, users.ids)
					require.Empty(t, store.targets)
					require.Nil(t, pricing.plans)
					require.Nil(t, pricing.features)
				})
			}
		})
	}
}

// billingAssociationFixture models an authorized admin read which may repair a
// legacy email-only association. Failures are injected before each dependent step.
type billingAssociationFixture struct {
	BillingService
	UserService
	stage        string
	err          error
	calls        []string
	targets      []string
	associations []*billing.AssociateSubscriptionsWithUserRequest
	reads        int
}

func (f *billingAssociationFixture) GetUserByID(_ context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	f.calls = append(f.calls, r.ID)
	if r.ID == "admin" {
		return &user.GetUserByIDResponse{User: &user.UniversalUser{ID: "admin", Roles: []string{"ADMIN"}}}, nil
	}
	if f.stage == "target" {
		return nil, f.err
	}
	model := &user.UniversalUser{ID: r.ID, Email: "target@example.test"}
	if f.stage == "wrong target" {
		model.ID = "other"
	}
	if f.stage == "no email" {
		model.Email = ""
	}
	return &user.GetUserByIDResponse{User: model}, nil
}

func (f *billingAssociationFixture) GetSubscriptions(_ context.Context, r *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
	f.reads++
	f.targets = append(f.targets, r.ForUserIDs...)
	stage := "initial"
	if f.reads > 1 {
		stage = "requery"
	}
	f.calls = append(f.calls, stage)
	if f.stage == stage {
		return nil, f.err
	}
	if f.reads == 1 {
		return &billing.GetSubscriptionsResponse{}, nil
	}
	return &billing.GetSubscriptionsResponse{Total: 1, Subscriptions: []billing.Subscription{{UserID: "target", Status: billing.StatusActive}}}, nil
}

func (f *billingAssociationFixture) GetSubscriptionsByEmail(_ context.Context, r *billing.GetSubscriptionsByEmailRequest) (*billing.GetSubscriptionsByEmailResponse, error) {
	f.calls = append(f.calls, "email")
	if r.Email != "target@example.test" {
		panic("email not bound to selected target")
	}
	if f.stage == "email" {
		return nil, f.err
	}
	if f.stage == "empty email results" {
		return &billing.GetSubscriptionsByEmailResponse{}, nil
	}
	return &billing.GetSubscriptionsByEmailResponse{Subscriptions: []billing.Subscription{{ID: "legacy"}}}, nil
}

func (f *billingAssociationFixture) AssociateSubscriptionsWithUser(_ context.Context, r *billing.AssociateSubscriptionsWithUserRequest) (*billing.AssociateSubscriptionsWithUserResponse, error) {
	f.calls = append(f.calls, "associate")
	f.associations = append(f.associations, r)
	if f.stage == "associate" {
		return nil, f.err
	}
	return &billing.AssociateSubscriptionsWithUserResponse{}, nil
}

func TestBillingAssociationPreservesTargetAndFailures(t *testing.T) {
	outage := errors.New("private association dependency diagnostic")
	for _, tc := range []struct {
		name, stage   string
		failure, want error
		wantCalls     []string
	}{
		{"success", "", nil, nil, []string{"admin", "initial", "target", "email", "associate", "requery"}},
		{"no email", "no email", nil, nil, []string{"admin", "initial", "target"}},
		{"no legacy subscriptions", "empty email results", nil, nil, []string{"admin", "initial", "target", "email"}},
		{"initial outage", "initial", outage, outage, []string{"admin", "initial"}},
		{"initial nil result", "initial", nil, ErrBillingManagerServiceUnavailable, []string{"admin", "initial"}},
		{"target outage", "target", outage, outage, []string{"admin", "initial", "target"}},
		{"target nil result", "target", nil, ErrBillingManagerServiceUnavailable, []string{"admin", "initial", "target"}},
		{"target mismatch", "wrong target", nil, ErrBillingManagerServiceUnavailable, []string{"admin", "initial", "target"}},
		{"email outage", "email", outage, outage, []string{"admin", "initial", "target", "email"}},
		{"email nil result", "email", nil, ErrBillingManagerServiceUnavailable, []string{"admin", "initial", "target", "email"}},
		{"association outage", "associate", outage, outage, []string{"admin", "initial", "target", "email", "associate"}},
		{"requery outage", "requery", outage, outage, []string{"admin", "initial", "target", "email", "associate", "requery"}},
		{"requery nil result", "requery", nil, ErrBillingManagerServiceUnavailable, []string{"admin", "initial", "target", "email", "associate", "requery"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &billingAssociationFixture{stage: tc.stage, err: tc.failure}
			service := &Service{UserService: fixture, BillingService: fixture}
			result, err := service.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{ActorID: "admin", UserID: "target"})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}
			require.Equal(t, tc.wantCalls, fixture.calls)
			for _, target := range fixture.targets {
				require.Equal(t, "target", target)
			}
			for _, association := range fixture.associations {
				require.Equal(t, "target", association.UserID)
				require.Equal(t, "target@example.test", association.Email)
			}
		})
	}
}
