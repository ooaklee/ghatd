package contacter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/contacter"
)

type serviceMockRepository struct {
	getTotalCommsFunc      func(ctx context.Context, req *contacter.GetTotalCommsRequest) (int64, error)
	getCommsFunc           func(ctx context.Context, req *contacter.GetCommsRequest) ([]contacter.Comms, error)
	createCommsFunc        func(ctx context.Context, newComms *contacter.Comms) (*contacter.Comms, error)
	updateCommsFunc        func(ctx context.Context, comms *contacter.Comms) (*contacter.Comms, error)
	getCommsByIdsFunc      func(ctx context.Context, commsIds []string) ([]contacter.Comms, error)
	getCommsStatsCountsFun func(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.CommsStats, error)
}

func (m *serviceMockRepository) GetTotalComms(ctx context.Context, req *contacter.GetTotalCommsRequest) (int64, error) {
	if m.getTotalCommsFunc != nil {
		return m.getTotalCommsFunc(ctx, req)
	}
	return 0, nil
}

func (m *serviceMockRepository) GetComms(ctx context.Context, req *contacter.GetCommsRequest) ([]contacter.Comms, error) {
	if m.getCommsFunc != nil {
		return m.getCommsFunc(ctx, req)
	}
	return nil, nil
}

func (m *serviceMockRepository) CreateComms(ctx context.Context, newComms *contacter.Comms) (*contacter.Comms, error) {
	if m.createCommsFunc != nil {
		return m.createCommsFunc(ctx, newComms)
	}
	return nil, nil
}

func (m *serviceMockRepository) UpdateComms(ctx context.Context, comms *contacter.Comms) (*contacter.Comms, error) {
	if m.updateCommsFunc != nil {
		return m.updateCommsFunc(ctx, comms)
	}
	return nil, nil
}

func (m *serviceMockRepository) GetCommsByIds(ctx context.Context, commsIds []string) ([]contacter.Comms, error) {
	if m.getCommsByIdsFunc != nil {
		return m.getCommsByIdsFunc(ctx, commsIds)
	}
	return nil, nil
}

func (m *serviceMockRepository) GetCommsStatsCounts(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.CommsStats, error) {
	if m.getCommsStatsCountsFun != nil {
		return m.getCommsStatsCountsFun(ctx, req)
	}
	return nil, nil
}

func TestService_CreateComms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		req               *contacter.CreateCommsRequest
		repoErr           error
		expectErrContains string
		expectRepoCalled  bool
		assertCreated     func(t *testing.T, created *contacter.Comms)
	}{
		{
			name: "Success - guest comms validates required fields and keeps feedback-companion type",
			req: &contacter.CreateCommsRequest{
				FullName: "jane doe",
				Email:    "JANE@EXAMPLE.COM",
				Type:     contacter.CommsTypeFeedbackCompanion,
				Message:  "Great app",
			},
			expectRepoCalled: true,
			assertCreated: func(t *testing.T, created *contacter.Comms) {
				t.Helper()
				assert.Equal(t, contacter.CommsTypeFeedbackCompanion, created.Type)
				assert.Equal(t, "", created.ProvidedType)
				assert.Equal(t, "jane@example.com", created.Email)
				assert.Equal(t, "Jane Doe", created.FullName)
				assert.False(t, created.UserLoggedIn)
			},
		},
		{
			name: "Success - logged in request marks user as logged in",
			req: &contacter.CreateCommsRequest{
				UserId:  "user-123",
				Type:    contacter.CommsTypeFeedbackCompanion,
				Message: "Signed-in feedback",
			},
			expectRepoCalled: true,
			assertCreated: func(t *testing.T, created *contacter.Comms) {
				t.Helper()
				assert.True(t, created.UserLoggedIn)
				assert.Equal(t, "user-123", created.UserId)
				assert.Equal(t, contacter.CommsTypeFeedbackCompanion, created.Type)
			},
		},
		{
			name: "Failure - missing guest full name and email",
			req: &contacter.CreateCommsRequest{
				Type:    contacter.CommsTypeFeedbackCompanion,
				Message: "hello",
			},
			expectErrContains: contacter.ErrKeyFullNameRequired,
			expectRepoCalled:  false,
		},
		{
			name: "Failure - repository error bubbles up",
			req: &contacter.CreateCommsRequest{
				FullName: "John Doe",
				Email:    "john@example.com",
				Type:     contacter.CommsTypeFeedbackCompanion,
				Message:  "hello",
			},
			repoErr:           errors.New("insert-failed"),
			expectErrContains: "insert-failed",
			expectRepoCalled:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoCalled := false
			var createdArg *contacter.Comms

			repo := &serviceMockRepository{
				createCommsFunc: func(ctx context.Context, newComms *contacter.Comms) (*contacter.Comms, error) {
					repoCalled = true
					createdArg = newComms
					if tt.repoErr != nil {
						return nil, tt.repoErr
					}
					return newComms, nil
				},
			}

			svc := contacter.NewService(repo)
			resp, err := svc.CreateComms(context.Background(), tt.req)

			if tt.expectErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErrContains)
				assert.Equal(t, tt.expectRepoCalled, repoCalled)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Comms)
			assert.Equal(t, tt.expectRepoCalled, repoCalled)
			if tt.assertCreated != nil {
				tt.assertCreated(t, createdArg)
			}
		})
	}
}

func TestService_ConfigurableCommsTypes(t *testing.T) {
	configuredTypes := contacter.CommsTypeMap{
		contacter.CommsType("service-question"): "Service Question",
	}
	created := make([]*contacter.Comms, 0, 2)
	repo := &serviceMockRepository{
		createCommsFunc: func(ctx context.Context, comms *contacter.Comms) (*contacter.Comms, error) {
			created = append(created, comms)
			return comms, nil
		},
	}

	svc := contacter.NewService(repo, configuredTypes)

	// Mutating the caller's map or a returned map must not change service
	// validation after construction.
	delete(configuredTypes, contacter.CommsType("service-question"))
	serviceTypes := svc.CommsTypes()
	assert.Equal(t, "Service Question", serviceTypes[contacter.CommsType("service-question")])
	delete(serviceTypes, contacter.CommsType("service-question"))
	availableTypes, err := svc.GetAvailableCommsTypes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Service Question", availableTypes.CommsTypes[contacter.CommsType("service-question")])
	delete(availableTypes.CommsTypes, contacter.CommsType("service-question"))
	assert.Equal(t, "Service Question", svc.CommsTypes()[contacter.CommsType("service-question")])

	_, err = svc.CreateComms(context.Background(), &contacter.CreateCommsRequest{
		FullName: "Jane Doe",
		Email:    "jane@example.com",
		Type:     contacter.CommsType("Service Question"),
		Message:  "How does this work?",
	})
	require.NoError(t, err)

	_, err = svc.CreateComms(context.Background(), &contacter.CreateCommsRequest{
		FullName: "John Doe",
		Email:    "john@example.com",
		Type:     contacter.CommsTypeFeedback,
		Message:  "A type not enabled for this service",
	})
	require.NoError(t, err)
	require.Len(t, created, 2)
	assert.Equal(t, contacter.CommsType("service-question"), created[0].Type)
	assert.Empty(t, created[0].ProvidedType)
	assert.Equal(t, contacter.CommsTypeOther, created[1].Type)
	assert.Equal(t, "feedback", created[1].ProvidedType)
}

func TestService_GetComms_DefaultsAndPaging(t *testing.T) {
	t.Parallel()

	req := &contacter.GetCommsRequest{}

	repo := &serviceMockRepository{
		getTotalCommsFunc: func(ctx context.Context, totalReq *contacter.GetTotalCommsRequest) (int64, error) {
			assert.Empty(t, totalReq.FullName)
			return 2, nil
		},
		getCommsFunc: func(ctx context.Context, getReq *contacter.GetCommsRequest) ([]contacter.Comms, error) {
			assert.Equal(t, "created_at_desc", getReq.Order)
			assert.Equal(t, 25, getReq.PerPage)
			assert.Equal(t, 1, getReq.Page)
			return []contacter.Comms{
				{Id: "c1", Type: contacter.CommsTypeFeedbackCompanion},
				{Id: "c2", Type: contacter.CommsTypeFeedback},
			}, nil
		},
	}

	svc := contacter.NewService(repo)
	resp, err := svc.GetComms(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.Total)
	assert.Equal(t, 1, resp.TotalPages)
	assert.Equal(t, 2, len(resp.Comms))
	assert.Equal(t, "created_at_desc", req.Order)
	assert.Equal(t, 25, req.PerPage)
	assert.Equal(t, 1, req.Page)
}

func TestService_UpdateComms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		req               *contacter.UpdateCommsRequest
		lookupResult      []contacter.Comms
		lookupErr         error
		updateErr         error
		expectErrContains string
		assertUpdated     func(t *testing.T, updated *contacter.Comms)
	}{
		{
			name:              "Failure - comms id required",
			req:               &contacter.UpdateCommsRequest{},
			expectErrContains: contacter.ErrKeyCommsIdRequired,
		},
		{
			name: "Failure - comms not found",
			req: &contacter.UpdateCommsRequest{
				CommsId: "missing",
			},
			lookupResult:      []contacter.Comms{},
			expectErrContains: contacter.ErrKeyCommsNotFound,
		},
		{
			name: "Success - updates admin fields and sets reached out",
			req: func() *contacter.UpdateCommsRequest {
				adminNotes := "follow up"
				adminReply := "Thanks for reaching out"
				reachedOut := true
				linked := []string{"comms-2"}
				return &contacter.UpdateCommsRequest{
					CommsId:        "comms-1",
					AdminNotes:     &adminNotes,
					AdminReply:     &adminReply,
					ReachedOut:     &reachedOut,
					LinkedCommsIds: &linked,
				}
			}(),
			lookupResult: []contacter.Comms{{
				Id:           "comms-1",
				Type:         contacter.CommsTypeFeedbackCompanion,
				ReachedOutAt: "",
			}},
			assertUpdated: func(t *testing.T, updated *contacter.Comms) {
				t.Helper()
				assert.Equal(t, "follow up", updated.AdminNotes)
				assert.Equal(t, "Thanks for reaching out", updated.AdminReply)
				assert.Equal(t, []string{"comms-2"}, updated.LinkedCommsIds)
				assert.NotEmpty(t, updated.ReachedOutAt)
			},
		},
		{
			name: "Failure - repository update error",
			req: func() *contacter.UpdateCommsRequest {
				n := "n"
				return &contacter.UpdateCommsRequest{CommsId: "comms-1", AdminNotes: &n}
			}(),
			lookupResult:      []contacter.Comms{{Id: "comms-1"}},
			updateErr:         errors.New("update-failed"),
			expectErrContains: "update-failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var updatedArg *contacter.Comms
			repo := &serviceMockRepository{
				getCommsByIdsFunc: func(ctx context.Context, ids []string) ([]contacter.Comms, error) {
					if tt.lookupErr != nil {
						return nil, tt.lookupErr
					}
					return tt.lookupResult, nil
				},
				updateCommsFunc: func(ctx context.Context, comms *contacter.Comms) (*contacter.Comms, error) {
					updatedArg = comms
					if tt.updateErr != nil {
						return nil, tt.updateErr
					}
					return comms, nil
				},
			}

			svc := contacter.NewService(repo)
			resp, err := svc.UpdateComms(context.Background(), tt.req)

			if tt.expectErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErrContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Comms)
			if tt.assertUpdated != nil {
				tt.assertUpdated(t, updatedArg)
			}
		})
	}
}

func TestService_GetCommsStats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		repoErr           error
		expectErrContains string
	}{
		{
			name: "Success - returns stats from repository",
		},
		{
			name:              "Failure - repository error returns generic error",
			repoErr:           errors.New("aggregate-failed"),
			expectErrContains: "failed to retrieve comms statistics",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &serviceMockRepository{
				getCommsStatsCountsFun: func(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.CommsStats, error) {
					if tt.repoErr != nil {
						return nil, tt.repoErr
					}
					return &contacter.CommsStats{Total: 10, ByType: contacter.CommsTypeStats{FeedbackCompanion: 4}}, nil
				},
			}

			svc := contacter.NewService(repo)
			resp, err := svc.GetCommsStats(context.Background(), &contacter.GetCommsStatsRequest{WithEmailRegex: ".*"})

			if tt.expectErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErrContains)
				assert.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotNil(t, resp.CommsStats)
			assert.Equal(t, int64(10), resp.Total)
			assert.Equal(t, int64(4), resp.ByType.Count(contacter.CommsTypeFeedbackCompanion))
		})
	}
}
