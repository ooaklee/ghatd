package pricer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/pricer"
)

const (
	testUserID      = "test-user-id-456"
	testPlanID      = "test-plan-id-123"
	testFeatureID   = "test-feature-id-789"
	testPlanSlug    = "pro-plan"
	testPlanName    = "Pro Plan"
	testFeatureSlug = "api-access"
	testFeatureName = "API Access"
)

func stringPtr(s string) *string {
	return &s
}

type mockPricerRepository struct {
	createPricePlanFunc     func(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error)
	updatePricePlanFunc     func(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error)
	getPricePlanByIDFunc    func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error)
	getPricePlanBySlugFunc  func(ctx context.Context, slug string, req *pricer.GetPricePlanBySlugRequest) (*pricer.PricePlan, error)
	getPricePlansFunc       func(ctx context.Context, req *pricer.GetPricePlansRequest) ([]pricer.PricePlan, error)
	getTotalPricePlansFunc  func(ctx context.Context, req *pricer.GetPricePlansRequest) (int64, error)
	publishPricePlanFunc    func(ctx context.Context, id, publishedByID, publishedAt string) error
	archivePricePlanFunc    func(ctx context.Context, id, updatedByID, updatedAt string) error
	softDeletePricePlanFunc func(ctx context.Context, id, deletedByID, deletedAt string) error
	createFeatureFunc       func(ctx context.Context, f *pricer.PriceFeature) (*pricer.PriceFeature, error)
	updateFeatureFunc       func(ctx context.Context, f *pricer.PriceFeature) (*pricer.PriceFeature, error)
	getFeatureByIDFunc      func(ctx context.Context, id string) (*pricer.PriceFeature, error)
	getFeaturesFunc         func(ctx context.Context, req *pricer.GetFeaturesRequest) ([]pricer.PriceFeature, error)
	getTotalFeaturesFunc    func(ctx context.Context, req *pricer.GetFeaturesRequest) (int64, error)
	softDeleteFeatureFunc   func(ctx context.Context, id, deletedByID, deletedAt string) error
}

func (m *mockPricerRepository) CreatePricePlan(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error) {
	if m.createPricePlanFunc != nil {
		return m.createPricePlanFunc(ctx, pp)
	}
	return pp, nil
}

func (m *mockPricerRepository) UpdatePricePlan(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error) {
	if m.updatePricePlanFunc != nil {
		return m.updatePricePlanFunc(ctx, pp)
	}
	return pp, nil
}

func (m *mockPricerRepository) GetPricePlanByID(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
	if m.getPricePlanByIDFunc != nil {
		return m.getPricePlanByIDFunc(ctx, id, req)
	}
	return nil, pricer.ErrPricePlanNotFound
}

func (m *mockPricerRepository) GetPricePlanBySlug(ctx context.Context, slug string, req *pricer.GetPricePlanBySlugRequest) (*pricer.PricePlan, error) {
	if m.getPricePlanBySlugFunc != nil {
		return m.getPricePlanBySlugFunc(ctx, slug, req)
	}
	return nil, pricer.ErrPricePlanNotFound
}

func (m *mockPricerRepository) GetPricePlans(ctx context.Context, req *pricer.GetPricePlansRequest) ([]pricer.PricePlan, error) {
	if m.getPricePlansFunc != nil {
		return m.getPricePlansFunc(ctx, req)
	}
	return []pricer.PricePlan{}, nil
}

func (m *mockPricerRepository) GetTotalPricePlans(ctx context.Context, req *pricer.GetPricePlansRequest) (int64, error) {
	if m.getTotalPricePlansFunc != nil {
		return m.getTotalPricePlansFunc(ctx, req)
	}
	return 0, nil
}

func (m *mockPricerRepository) PublishPricePlan(ctx context.Context, id, publishedByID, publishedAt string) error {
	if m.publishPricePlanFunc != nil {
		return m.publishPricePlanFunc(ctx, id, publishedByID, publishedAt)
	}
	return nil
}

func (m *mockPricerRepository) ArchivePricePlan(ctx context.Context, id, updatedByID, updatedAt string) error {
	if m.archivePricePlanFunc != nil {
		return m.archivePricePlanFunc(ctx, id, updatedByID, updatedAt)
	}
	return nil
}

func (m *mockPricerRepository) SoftDeletePricePlan(ctx context.Context, id, deletedByID, deletedAt string) error {
	if m.softDeletePricePlanFunc != nil {
		return m.softDeletePricePlanFunc(ctx, id, deletedByID, deletedAt)
	}
	return nil
}

func (m *mockPricerRepository) CreateFeature(ctx context.Context, f *pricer.PriceFeature) (*pricer.PriceFeature, error) {
	if m.createFeatureFunc != nil {
		return m.createFeatureFunc(ctx, f)
	}
	return f, nil
}

func (m *mockPricerRepository) UpdateFeature(ctx context.Context, f *pricer.PriceFeature) (*pricer.PriceFeature, error) {
	if m.updateFeatureFunc != nil {
		return m.updateFeatureFunc(ctx, f)
	}
	return f, nil
}

func (m *mockPricerRepository) GetFeatureByID(ctx context.Context, id string) (*pricer.PriceFeature, error) {
	if m.getFeatureByIDFunc != nil {
		return m.getFeatureByIDFunc(ctx, id)
	}
	return nil, pricer.ErrPriceFeatureNotFound
}

func (m *mockPricerRepository) GetFeatures(ctx context.Context, req *pricer.GetFeaturesRequest) ([]pricer.PriceFeature, error) {
	if m.getFeaturesFunc != nil {
		return m.getFeaturesFunc(ctx, req)
	}
	return []pricer.PriceFeature{}, nil
}

func (m *mockPricerRepository) GetTotalFeatures(ctx context.Context, req *pricer.GetFeaturesRequest) (int64, error) {
	if m.getTotalFeaturesFunc != nil {
		return m.getTotalFeaturesFunc(ctx, req)
	}
	return 0, nil
}

func (m *mockPricerRepository) SoftDeleteFeature(ctx context.Context, id, deletedByID, deletedAt string) error {
	if m.softDeleteFeatureFunc != nil {
		return m.softDeleteFeatureFunc(ctx, id, deletedByID, deletedAt)
	}
	return nil
}

func newTestService(repo pricer.PricerRepository) *pricer.Service {
	return pricer.NewService(repo)
}

func makeValidPlan() *pricer.PricePlan {
	return &pricer.PricePlan{
		ID:     testPlanID,
		Slug:   testPlanSlug,
		Name:   testPlanName,
		Status: pricer.PricePlanStatusDraft,
		Costs: []pricer.PriceCost{
			{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
		},
		ProviderRefs: []pricer.PriceProviderRef{
			{Provider: pricer.PriceProviderManual},
		},
	}
}

func TestService_CreatePricePlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		req               *pricer.CreatePricePlanRequest
		mockRepositoryErr error
		expectError       bool
		expectPublished   bool
	}{
		{
			name: "Success - creates draft plan",
			req: &pricer.CreatePricePlanRequest{
				UserID: testUserID,
				Name:   testPlanName,
				Costs: []pricer.PriceCost{
					{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
				},
				ProviderRefs: []pricer.PriceProviderRef{
					{Provider: pricer.PriceProviderManual},
				},
			},
			expectError: false,
		},
		{
			name: "Success - creates published plan with PublishNow",
			req: &pricer.CreatePricePlanRequest{
				UserID: testUserID,
				Name:   testPlanName,
				Costs: []pricer.PriceCost{
					{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
				},
				ProviderRefs: []pricer.PriceProviderRef{
					{Provider: pricer.PriceProviderManual},
				},
				PublishNow: true,
			},
			expectError:     false,
			expectPublished: true,
		},
		{
			name:        "Failure - nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "Failure - empty user ID",
			req: &pricer.CreatePricePlanRequest{
				Name: testPlanName,
			},
			expectError: true,
		},
		{
			name: "Failure - repository error",
			req: &pricer.CreatePricePlanRequest{
				UserID: testUserID,
				Name:   testPlanName,
			},
			mockRepositoryErr: errors.New("db-error"),
			expectError:       true,
		},
		{
			name: "Failure - PublishNow without cost",
			req: &pricer.CreatePricePlanRequest{
				UserID:     testUserID,
				Name:       testPlanName,
				PublishNow: true,
			},
			expectError: true,
		},
		{
			name: "Failure - PublishNow without provider ref",
			req: &pricer.CreatePricePlanRequest{
				UserID: testUserID,
				Name:   testPlanName,
				Costs: []pricer.PriceCost{
					{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
				},
				PublishNow: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				createPricePlanFunc: func(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error) {
					if tt.mockRepositoryErr != nil {
						return nil, tt.mockRepositoryErr
					}
					return pp, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.CreatePricePlan(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.PricePlan)
			assert.Equal(t, testPlanName, response.PricePlan.Name)
			assert.NotEmpty(t, response.PricePlan.Slug)

			if tt.expectPublished {
				assert.Equal(t, pricer.PricePlanStatusPublished, response.PricePlan.Status)
				assert.NotEmpty(t, response.PricePlan.PublishedAt)
				assert.Equal(t, testUserID, response.PricePlan.PublishedByID)
			} else {
				assert.Equal(t, pricer.PricePlanStatusDraft, response.PricePlan.Status)
			}
		})
	}
}

func TestService_UpdatePricePlan(t *testing.T) {
	t.Parallel()

	existingPlan := makeValidPlan()

	tests := []struct {
		name          string
		req           *pricer.UpdatePricePlanRequest
		mockGetErr    error
		mockUpdateErr error
		expectError   bool
	}{
		{
			name: "Success - update plan name",
			req: &pricer.UpdatePricePlanRequest{
				ID:     testPlanID,
				UserID: testUserID,
				Name:   stringPtr("Updated Plan"),
			},
		},
		{
			name: "Failure - plan not found",
			req: &pricer.UpdatePricePlanRequest{
				ID:     testPlanID,
				UserID: testUserID,
				Name:   stringPtr("Updated Plan"),
			},
			mockGetErr:  pricer.ErrPricePlanNotFound,
			expectError: true,
		},
		{
			name: "Failure - empty user ID",
			req: &pricer.UpdatePricePlanRequest{
				ID:   "some-id",
				Name: stringPtr("Updated"),
			},
			expectError: true,
		},
		{
			name: "Success - update via full PricePlan object",
			req: &pricer.UpdatePricePlanRequest{
				UserID:    testUserID,
				PricePlan: makeValidPlan(),
			},
		},
		{
			name:        "Failure - nil request",
			req:         nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
					if tt.mockGetErr != nil {
						return nil, tt.mockGetErr
					}
					return existingPlan, nil
				},
				updatePricePlanFunc: func(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error) {
					if tt.mockUpdateErr != nil {
						return nil, tt.mockUpdateErr
					}
					return pp, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.UpdatePricePlan(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.PricePlan)
			assert.Equal(t, testUserID, response.PricePlan.UpdatedByID)
		})
	}
}

func TestService_GetPricePlanByID(t *testing.T) {
	t.Parallel()

	existingPlan := makeValidPlan()

	tests := []struct {
		name        string
		req         *pricer.GetPricePlanByIDRequest
		mockGetErr  error
		expectError bool
	}{
		{
			name: "Success - retrieves plan by ID",
			req: &pricer.GetPricePlanByIDRequest{
				ID: testPlanID,
			},
		},
		{
			name: "Failure - plan not found",
			req: &pricer.GetPricePlanByIDRequest{
				ID: testPlanID,
			},
			mockGetErr:  pricer.ErrPricePlanNotFound,
			expectError: true,
		},
		{
			name:        "Failure - missing ID",
			req:         &pricer.GetPricePlanByIDRequest{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
					if tt.mockGetErr != nil {
						return nil, tt.mockGetErr
					}
					return existingPlan, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.GetPricePlanByID(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.PricePlan)
			assert.Equal(t, existingPlan.ID, response.PricePlan.ID)
		})
	}
}

func TestService_GetPricePlanBySlug(t *testing.T) {
	t.Parallel()

	existingPlan := makeValidPlan()

	tests := []struct {
		name        string
		req         *pricer.GetPricePlanBySlugRequest
		mockGetErr  error
		expectError bool
	}{
		{
			name: "Success - retrieves plan by slug",
			req: &pricer.GetPricePlanBySlugRequest{
				Slug: testPlanSlug,
			},
		},
		{
			name: "Failure - plan not found",
			req: &pricer.GetPricePlanBySlugRequest{
				Slug: testPlanSlug,
			},
			mockGetErr:  pricer.ErrPricePlanNotFound,
			expectError: true,
		},
		{
			name:        "Failure - missing slug",
			req:         &pricer.GetPricePlanBySlugRequest{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				getPricePlanBySlugFunc: func(ctx context.Context, slug string, req *pricer.GetPricePlanBySlugRequest) (*pricer.PricePlan, error) {
					if tt.mockGetErr != nil {
						return nil, tt.mockGetErr
					}
					return existingPlan, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.GetPricePlanBySlug(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.PricePlan)
			assert.Equal(t, existingPlan.ID, response.PricePlan.ID)
		})
	}
}

func TestService_PublishPricePlan(t *testing.T) {
	t.Parallel()

	existingPlan := makeValidPlan()

	tests := []struct {
		name           string
		req            *pricer.PublishPricePlanRequest
		mockGetErr     error
		mockPublishErr error
		expectError    bool
	}{
		{
			name: "Success - publish with cost and provider ref",
			req: &pricer.PublishPricePlanRequest{
				ID:     testPlanID,
				UserID: testUserID,
			},
		},
		{
			name: "Failure - plan not found",
			req: &pricer.PublishPricePlanRequest{
				ID:     testPlanID,
				UserID: testUserID,
			},
			mockGetErr:  pricer.ErrPricePlanNotFound,
			expectError: true,
		},
		{
			name: "Failure - missing user ID",
			req: &pricer.PublishPricePlanRequest{
				ID: testPlanID,
			},
			expectError: true,
		},
		{
			name:        "Failure - nil request",
			req:         nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
					if tt.mockGetErr != nil {
						return nil, tt.mockGetErr
					}
					return existingPlan, nil
				},
				publishPricePlanFunc: func(ctx context.Context, id, publishedByID, publishedAt string) error {
					return tt.mockPublishErr
				},
			}

			svc := newTestService(repo)
			response, err := svc.PublishPricePlan(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.PricePlan)
		})
	}
}

func TestService_PublishPricePlan_RequiresCostAndProvider(t *testing.T) {
	t.Parallel()

	planWithoutCost := &pricer.PricePlan{
		ID:     testPlanID,
		Slug:   testPlanSlug,
		Name:   testPlanName,
		Status: pricer.PricePlanStatusDraft,
		Costs:  []pricer.PriceCost{},
		ProviderRefs: []pricer.PriceProviderRef{
			{Provider: pricer.PriceProviderManual},
		},
	}

	planWithoutProvider := &pricer.PricePlan{
		ID:     testPlanID + "-2",
		Slug:   "plan-no-provider",
		Name:   "Plan No Provider",
		Status: pricer.PricePlanStatusDraft,
		Costs: []pricer.PriceCost{
			{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
		},
		ProviderRefs: []pricer.PriceProviderRef{},
	}

	t.Run("Failure - publish without cost", func(t *testing.T) {
		t.Parallel()

		repo := &mockPricerRepository{
			getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
				return planWithoutCost, nil
			},
		}

		svc := newTestService(repo)
		_, err := svc.PublishPricePlan(context.Background(), &pricer.PublishPricePlanRequest{
			ID:     testPlanID,
			UserID: testUserID,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), pricer.ErrKeyPricePlanPublishRequiresCost)
	})

	t.Run("Failure - publish without provider ref", func(t *testing.T) {
		t.Parallel()

		repo := &mockPricerRepository{
			getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
				return planWithoutProvider, nil
			},
		}

		svc := newTestService(repo)
		_, err := svc.PublishPricePlan(context.Background(), &pricer.PublishPricePlanRequest{
			ID:     testPlanID + "-2",
			UserID: testUserID,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), pricer.ErrKeyPricePlanPublishRequiresProvider)
	})
}

func TestService_ArchivePricePlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		req         *pricer.ArchivePricePlanRequest
		mockErr     error
		expectError bool
	}{
		{
			name: "Success - archives plan",
			req: &pricer.ArchivePricePlanRequest{
				ID:     testPlanID,
				UserID: testUserID,
			},
		},
		{
			name: "Failure - missing user ID",
			req: &pricer.ArchivePricePlanRequest{
				ID: testPlanID,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				archivePricePlanFunc: func(ctx context.Context, id, updatedByID, updatedAt string) error {
					return tt.mockErr
				},
				getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
					return makeValidPlan(), nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.ArchivePricePlan(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.PricePlan)
		})
	}
}

func TestService_DeletePricePlan(t *testing.T) {
	t.Parallel()

	plan := makeValidPlan()
	plan.DeletedAt = "2025-01-01T00:00:00.000000000Z"
	plan.DeletedByID = testUserID

	tests := []struct {
		name        string
		req         *pricer.DeletePricePlanRequest
		mockErr     error
		expectError bool
	}{
		{
			name: "Success - soft deletes plan",
			req: &pricer.DeletePricePlanRequest{
				ID:     testPlanID,
				UserID: testUserID,
			},
		},
		{
			name: "Failure - missing user ID",
			req: &pricer.DeletePricePlanRequest{
				ID: testPlanID,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				softDeletePricePlanFunc: func(ctx context.Context, id, deletedByID, deletedAt string) error {
					return tt.mockErr
				},
				getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
					return plan, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.DeletePricePlan(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.PricePlan)
		})
	}
}

func TestService_GetPricePlans(t *testing.T) {
	t.Parallel()

	plans := []pricer.PricePlan{*makeValidPlan()}

	tests := []struct {
		name        string
		req         *pricer.GetPricePlansRequest
		mockErr     error
		expectError bool
		expectMeta  bool
	}{
		{
			name: "Success - returns plans with defaults",
			req:  &pricer.GetPricePlansRequest{},
		},
		{
			name: "Success - with display_order_asc",
			req: &pricer.GetPricePlansRequest{
				Order: "display_order_asc",
			},
		},
		{
			name: "Success - with display_order_desc",
			req: &pricer.GetPricePlansRequest{
				Order: "display_order_desc",
			},
		},
		{
			name: "Success - with created_at_asc",
			req: &pricer.GetPricePlansRequest{
				Order: "created_at_asc",
			},
		},
		{
			name: "Success - with published_at_desc",
			req: &pricer.GetPricePlansRequest{
				Order: "published_at_desc",
			},
		},
		{
			name: "Success - with name_asc",
			req: &pricer.GetPricePlansRequest{
				Order: "name_asc",
			},
		},
		{
			name: "Failure - legacy display_priority_asc rejected",
			req: &pricer.GetPricePlansRequest{
				Order: "display_priority_asc",
			},
			expectError: true,
		},
		{
			name: "Failure - legacy display_priority_dsc rejected",
			req: &pricer.GetPricePlansRequest{
				Order: "display_priority_dsc",
			},
			expectError: true,
		},
		{
			name: "Failure - unknown order value rejected",
			req: &pricer.GetPricePlansRequest{
				Order: "bogus_sort",
			},
			expectError: true,
		},
		{
			name: "Success - with custom pagination",
			req: &pricer.GetPricePlansRequest{
				PerPage: 10,
				Page:    1,
			},
		},
		{
			name: "Success - with meta flag",
			req: &pricer.GetPricePlansRequest{
				PerPage: 25,
				Page:    1,
				Meta:    true,
			},
			expectMeta: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				getTotalPricePlansFunc: func(ctx context.Context, req *pricer.GetPricePlansRequest) (int64, error) {
					return int64(len(plans)), nil
				},
				getPricePlansFunc: func(ctx context.Context, req *pricer.GetPricePlansRequest) ([]pricer.PricePlan, error) {
					if tt.mockErr != nil {
						return nil, tt.mockErr
					}
					return plans, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.GetPricePlans(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), pricer.ErrKeyInvalidPriceQueryParam)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.NotNil(t, response.PricePlans)
		})
	}
}

func TestService_CreateFeature(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		req               *pricer.CreateFeatureRequest
		mockRepositoryErr error
		expectError       bool
	}{
		{
			name: "Success - creates feature",
			req: &pricer.CreateFeatureRequest{
				UserID: testUserID,
				Name:   testFeatureName,
				Type:   pricer.PriceFeatureTypeBoolean,
			},
		},
		{
			name:        "Failure - nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "Failure - empty user ID",
			req: &pricer.CreateFeatureRequest{
				Name: testFeatureName,
			},
			expectError: true,
		},
		{
			name: "Failure - invalid feature type",
			req: &pricer.CreateFeatureRequest{
				UserID: testUserID,
				Name:   testFeatureName,
				Type:   "invalid_type",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				createFeatureFunc: func(ctx context.Context, f *pricer.PriceFeature) (*pricer.PriceFeature, error) {
					if tt.mockRepositoryErr != nil {
						return nil, tt.mockRepositoryErr
					}
					return f, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.CreateFeature(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.Feature)
			assert.Equal(t, testFeatureName, response.Feature.Name)
		})
	}
}

func TestService_UpdateFeature(t *testing.T) {
	t.Parallel()

	existingFeature := &pricer.PriceFeature{
		ID:   testFeatureID,
		Slug: testFeatureSlug,
		Name: testFeatureName,
		Type: pricer.PriceFeatureTypeBoolean,
	}

	tests := []struct {
		name        string
		req         *pricer.UpdateFeatureRequest
		mockGetErr  error
		expectError bool
	}{
		{
			name: "Success - update feature name",
			req: &pricer.UpdateFeatureRequest{
				ID:     testFeatureID,
				UserID: testUserID,
				Name:   stringPtr("Updated Feature"),
			},
		},
		{
			name: "Failure - feature not found",
			req: &pricer.UpdateFeatureRequest{
				ID:     testFeatureID,
				UserID: testUserID,
				Name:   stringPtr("Updated Feature"),
			},
			mockGetErr:  pricer.ErrPriceFeatureNotFound,
			expectError: true,
		},
		{
			name:        "Failure - nil request",
			req:         nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				getFeatureByIDFunc: func(ctx context.Context, id string) (*pricer.PriceFeature, error) {
					if tt.mockGetErr != nil {
						return nil, tt.mockGetErr
					}
					return existingFeature, nil
				},
				updateFeatureFunc: func(ctx context.Context, f *pricer.PriceFeature) (*pricer.PriceFeature, error) {
					return f, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.UpdateFeature(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.Feature)
			assert.Equal(t, testUserID, response.Feature.UpdatedByID)
		})
	}
}

func TestService_DeleteFeature(t *testing.T) {
	t.Parallel()

	feature := &pricer.PriceFeature{
		ID:          testFeatureID,
		Slug:        testFeatureSlug,
		Name:        testFeatureName,
		DeletedAt:   "2025-01-01T00:00:00.000000000Z",
		DeletedByID: testUserID,
	}

	tests := []struct {
		name        string
		req         *pricer.DeleteFeatureRequest
		mockErr     error
		expectError bool
	}{
		{
			name: "Success - soft deletes feature",
			req: &pricer.DeleteFeatureRequest{
				ID:     testFeatureID,
				UserID: testUserID,
			},
		},
		{
			name: "Failure - missing ID",
			req: &pricer.DeleteFeatureRequest{
				UserID: testUserID,
			},
			expectError: true,
		},
		{
			name: "Failure - missing user ID",
			req: &pricer.DeleteFeatureRequest{
				ID: testFeatureID,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				softDeleteFeatureFunc: func(ctx context.Context, id, deletedByID, deletedAt string) error {
					return tt.mockErr
				},
				getFeatureByIDFunc: func(ctx context.Context, id string) (*pricer.PriceFeature, error) {
					return feature, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.DeleteFeature(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.Feature)
		})
	}
}

func TestService_GetFeatures(t *testing.T) {
	t.Parallel()

	features := []pricer.PriceFeature{
		{ID: testFeatureID, Slug: testFeatureSlug, Name: testFeatureName, Type: pricer.PriceFeatureTypeBoolean},
	}

	tests := []struct {
		name        string
		req         *pricer.GetFeaturesRequest
		mockErr     error
		expectError bool
	}{
		{
			name: "Success - returns features with defaults",
			req:  &pricer.GetFeaturesRequest{},
		},
		{
			name: "Success - with display_order_asc",
			req: &pricer.GetFeaturesRequest{
				Order: "display_order_asc",
			},
		},
		{
			name: "Success - with display_order_desc",
			req: &pricer.GetFeaturesRequest{
				Order: "display_order_desc",
			},
		},
		{
			name: "Failure - legacy display_priority_asc rejected",
			req: &pricer.GetFeaturesRequest{
				Order: "display_priority_asc",
			},
			expectError: true,
		},
		{
			name: "Failure - unknown order value rejected",
			req: &pricer.GetFeaturesRequest{
				Order: "bogus_sort",
			},
			expectError: true,
		},
		{
			name: "Success - with custom pagination",
			req: &pricer.GetFeaturesRequest{
				PerPage: 10,
				Page:    1,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockPricerRepository{
				getTotalFeaturesFunc: func(ctx context.Context, req *pricer.GetFeaturesRequest) (int64, error) {
					return int64(len(features)), nil
				},
				getFeaturesFunc: func(ctx context.Context, req *pricer.GetFeaturesRequest) ([]pricer.PriceFeature, error) {
					if tt.mockErr != nil {
						return nil, tt.mockErr
					}
					return features, nil
				},
			}

			svc := newTestService(repo)
			response, err := svc.GetFeatures(context.Background(), tt.req)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), pricer.ErrKeyInvalidPriceQueryParam)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.NotNil(t, response.Features)
		})
	}
}
