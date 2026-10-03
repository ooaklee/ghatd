package billingmanager_test

import (
	"context"
	"fmt"
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

// TestServicePublicPricingFilters covers both list projections with independent
// caller-owned filters. Applying public restrictions must not mutate those inputs.
func TestServicePublicPricingFilters(t *testing.T) {
	for _, operation := range []string{"plans", "features"} {
		t.Run(operation, func(t *testing.T) {
			for _, actor := range []string{"", "member"} {
				name := actor
				if name == "" {
					name = "anonymous"
				}
				t.Run(name, func(t *testing.T) {
					for _, initialized := range []bool{false, true} {
						t.Run(fmt.Sprintf("initialized_%t", initialized), func(t *testing.T) {
							calls := 0
							service := (&billingmanager.Service{}).WithPricerService(&mockBillingManagerPricerService{
								getPricePlansFunc: func(_ context.Context, r *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
									calls++
									if r == nil || !r.IsPublished || !r.IsNotDeleted || r.WithStatus != string(pricer.PricePlanStatusPublished) {
										t.Fatalf("public plans filters = %#v", r)
									}
									return &pricer.GetPricePlansResponse{}, nil
								},
								getFeaturesFunc: func(_ context.Context, r *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error) {
									calls++
									if r == nil || !r.IsPublished || !r.IsNotDeleted {
										t.Fatalf("public feature filters = %#v", r)
									}
									return &pricer.GetFeaturesResponse{}, nil
								},
							})
							var err error
							if operation == "plans" {
								request := &billingmanager.GetPricingPlansRequest{ActorID: actor}
								if initialized {
									request.GetPricePlansRequest = &pricer.GetPricePlansRequest{WithStatus: "draft"}
								}
								_, err = service.GetPricingPlans(context.Background(), request)
								if initialized {
									if request.IsPublished || request.IsNotDeleted || request.WithStatus != "draft" {
										t.Fatal("caller plan filters mutated")
									}
								} else if request.GetPricePlansRequest != nil {
									t.Fatal("caller plan request mutated")
								}
							} else {
								request := &billingmanager.GetPriceFeaturesRequest{ActorID: actor}
								if initialized {
									request.GetFeaturesRequest = &pricer.GetFeaturesRequest{}
								}
								_, err = service.GetPricingFeatures(context.Background(), request)
								if initialized {
									if request.IsPublished || request.IsNotDeleted {
										t.Fatal("caller feature filters mutated")
									}
								} else if request.GetFeaturesRequest != nil {
									t.Fatal("caller feature request mutated")
								}
							}
							if err != nil || calls != 1 {
								t.Fatalf("err=%v calls=%d", err, calls)
							}
						})
					}
				})
			}
		})
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
				Status:      pricer.PricePlanStatusPublished,
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
				Status:      pricer.PricePlanStatusPublished,
				PublishedAt: time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
			},
			expectedErr: pricer.ErrKeyPricePlanNotFound,
		},
		{
			name: "rejects archived plans while preserving publication timestamp",
			pricePlan: &pricer.PricePlan{
				Status:      pricer.PricePlanStatusArchived,
				PublishedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
			},
			expectedErr: pricer.ErrKeyPricePlanNotFound,
		},
		{
			name: "allows active published price plans",
			pricePlan: &pricer.PricePlan{
				Status:      pricer.PricePlanStatusPublished,
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
				ActorID: "user-1",
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
