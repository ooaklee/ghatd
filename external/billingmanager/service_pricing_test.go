package billingmanager_test

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/pricer"
)

type mockBillingManagerPricerService struct {
	getPricePlansFunc      func(ctx context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error)
	getPricePlanBySlugFunc func(ctx context.Context, req *pricer.GetPricePlanBySlugRequest) (*pricer.GetPricePlanBySlugResponse, error)
	getFeaturesFunc        func(ctx context.Context, req *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error)
}

func (m *mockBillingManagerPricerService) GetPricePlans(ctx context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
	if m.getPricePlansFunc != nil {
		return m.getPricePlansFunc(ctx, req)
	}

	return &pricer.GetPricePlansResponse{}, nil
}

func (m *mockBillingManagerPricerService) GetPricePlanBySlug(ctx context.Context, req *pricer.GetPricePlanBySlugRequest) (*pricer.GetPricePlanBySlugResponse, error) {
	if m.getPricePlanBySlugFunc != nil {
		return m.getPricePlanBySlugFunc(ctx, req)
	}

	return &pricer.GetPricePlanBySlugResponse{GetPricePlanResponse: &pricer.GetPricePlanResponse{}}, nil
}

func (m *mockBillingManagerPricerService) GetFeatures(ctx context.Context, req *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error) {
	if m.getFeaturesFunc != nil {
		return m.getFeaturesFunc(ctx, req)
	}

	return &pricer.GetFeaturesResponse{}, nil
}

func TestServiceGetPricingPlansForNonAdminForcesPublicFilters(t *testing.T) {
	t.Parallel()

	service := (&billingmanager.Service{}).WithPricerService(&mockBillingManagerPricerService{
		getPricePlansFunc: func(ctx context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
			if req == nil {
				t.Fatal("expected pricing request to be initialized")
			}

			if !req.IsNotDeleted {
				t.Fatal("expected IsNotDeleted to be forced for non-admin pricing list requests")
			}

			if !req.IsPublished {
				t.Fatal("expected IsPublished to be forced for non-admin pricing list requests")
			}

			return &pricer.GetPricePlansResponse{}, nil
		},
	})

	_, err := service.GetPricingPlans(context.Background(), &billingmanager.GetPricingPlansRequest{UserID: "user-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestServiceGetPricingFeaturesForNonAdminForcesPublicFilters(t *testing.T) {
	t.Parallel()

	service := (&billingmanager.Service{}).WithPricerService(&mockBillingManagerPricerService{
		getFeaturesFunc: func(ctx context.Context, req *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error) {
			if req == nil {
				t.Fatal("expected features request to be initialized")
			}

			if !req.IsNotDeleted {
				t.Fatal("expected IsNotDeleted to be forced for non-admin feature list requests")
			}

			if !req.IsPublished {
				t.Fatal("expected IsPublished to be forced for non-admin feature list requests")
			}

			return &pricer.GetFeaturesResponse{}, nil
		},
	})

	_, err := service.GetPricingFeatures(context.Background(), &billingmanager.GetPriceFeaturesRequest{UserID: "user-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestServiceGetPricePlanBySlugForNonAdminOnlyReturnsPublicPlans(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		pricePlan   *pricer.PricePlan
		expectedErr string
	}{
		{
			name: "rejects deleted price plans",
			pricePlan: &pricer.PricePlan{
				PublishedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
				DeletedAt:   time.Now().UTC().Format(time.RFC3339),
			},
			expectedErr: pricer.ErrKeyPricePlanNotFound,
		},
		{
			name:        "rejects unpublished price plans",
			pricePlan:   &pricer.PricePlan{},
			expectedErr: pricer.ErrKeyPricePlanNotFound,
		},
		{
			name: "rejects future published price plans",
			pricePlan: &pricer.PricePlan{
				PublishedAt: time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
			},
			expectedErr: pricer.ErrKeyPricePlanNotFound,
		},
		{
			name: "allows active published price plans",
			pricePlan: &pricer.PricePlan{
				PublishedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
			},
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := (&billingmanager.Service{}).WithPricerService(&mockBillingManagerPricerService{
				getPricePlanBySlugFunc: func(ctx context.Context, req *pricer.GetPricePlanBySlugRequest) (*pricer.GetPricePlanBySlugResponse, error) {
					return &pricer.GetPricePlanBySlugResponse{
						GetPricePlanResponse: &pricer.GetPricePlanResponse{PricePlan: tt.pricePlan},
					}, nil
				},
			})

			response, err := service.GetPricePlanBySlug(context.Background(), &billingmanager.GetPricePlanBySlugRequest{
				UserID: "user-1",
				GetPricePlanBySlugRequest: &pricer.GetPricePlanBySlugRequest{
					Slug: "starter",
				},
			})

			if tt.expectedErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.expectedErr)
				}

				if err.Error() != tt.expectedErr {
					t.Fatalf("expected error %q, got %q", tt.expectedErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if response == nil || response.GetPricePlanBySlugResponse == nil || response.PricePlan != tt.pricePlan {
				t.Fatal("expected published price plan response to be returned unchanged")
			}
		})
	}
}
