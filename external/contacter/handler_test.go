package contacter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/contacter"
)

type handlerMockContacterService struct {
	getCommsStatsFunc          func(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error)
	getAvailableCommsTypesFunc func(ctx context.Context) (*contacter.GetAvailableCommsTypesResponse, error)
}

func (m *handlerMockContacterService) GetAvailableCommsTypes(ctx context.Context) (*contacter.GetAvailableCommsTypesResponse, error) {
	if m.getAvailableCommsTypesFunc != nil {
		return m.getAvailableCommsTypesFunc(ctx)
	}
	return &contacter.GetAvailableCommsTypesResponse{CommsTypes: contacter.DefaultCommsTypeMap()}, nil
}

func (m *handlerMockContacterService) CreateComms(ctx context.Context, req *contacter.CreateCommsRequest) (*contacter.CreateCommsResponse, error) {
	return nil, nil
}

func (m *handlerMockContacterService) GetComms(ctx context.Context, req *contacter.GetCommsRequest) (*contacter.GetCommsResponse, error) {
	return nil, nil
}

func (m *handlerMockContacterService) UpdateComms(ctx context.Context, req *contacter.UpdateCommsRequest) (*contacter.UpdateCommsResponse, error) {
	return nil, nil
}

func (m *handlerMockContacterService) GetCommsStats(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error) {
	if m.getCommsStatsFunc != nil {
		return m.getCommsStatsFunc(ctx, req)
	}
	return &contacter.GetCommsStatsResponse{CommsStats: &contacter.CommsStats{}}, nil
}

type handlerMockValidator struct {
	validateFunc func(s interface{}) error
}

func (m *handlerMockValidator) Validate(s interface{}) error {
	if m.validateFunc != nil {
		return m.validateFunc(s)
	}
	return nil
}

func TestHandler_GetCommsStats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		query               string
		validatorErr        error
		serviceErr          error
		expectStatus        int
		expectServiceCalled bool
		expectRegexValue    string
	}{
		{
			name:                "Success - maps query and returns stats",
			query:               "?with_email_regex=.*@example.com",
			expectStatus:        http.StatusOK,
			expectServiceCalled: true,
			expectRegexValue:    ".*@example.com",
		},
		{
			name:                "Failure - validator rejects payload",
			query:               "?with_email_regex=[",
			validatorErr:        contacter.ErrInvalidCommsPayload,
			expectStatus:        http.StatusBadRequest,
			expectServiceCalled: false,
		},
		{
			name:                "Failure - service returns unmapped error",
			query:               "?with_email_regex=.*",
			serviceErr:          errors.New("db-down"),
			expectStatus:        http.StatusInternalServerError,
			expectServiceCalled: true,
			expectRegexValue:    ".*",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			serviceCalled := false
			var capturedReq *contacter.GetCommsStatsRequest

			svc := &handlerMockContacterService{
				getCommsStatsFunc: func(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error) {
					serviceCalled = true
					capturedReq = req
					if tt.serviceErr != nil {
						return nil, tt.serviceErr
					}
					return &contacter.GetCommsStatsResponse{CommsStats: &contacter.CommsStats{Total: 3}}, nil
				},
			}

			validator := &handlerMockValidator{
				validateFunc: func(s interface{}) error {
					if tt.validatorErr != nil {
						return tt.validatorErr
					}
					return nil
				},
			}

			h := contacter.NewHandler(svc, validator)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/comms/stats"+tt.query, nil)
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.GetCommsStats(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Equal(t, tt.expectServiceCalled, serviceCalled)
			if tt.expectServiceCalled {
				require.NotNil(t, capturedReq)
				assert.Equal(t, tt.expectRegexValue, capturedReq.WithEmailRegex)
			}
		})
	}
}

func TestHandler_GetAvailableCommsTypes(t *testing.T) {
	t.Parallel()

	svc := &handlerMockContacterService{
		getAvailableCommsTypesFunc: func(context.Context) (*contacter.GetAvailableCommsTypesResponse, error) {
			return &contacter.GetAvailableCommsTypesResponse{
				CommsTypes: contacter.CommsTypeMap{
					contacter.CommsType("service-question"): "Service Question",
				},
			}, nil
		},
	}
	h := contacter.NewHandler(svc, &handlerMockValidator{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/comms/types", nil)
	rec := httptest.NewRecorder()

	h.GetAvailableCommsTypes(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var envelope struct {
		Data map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	assert.Equal(t, map[string]string{"service-question": "Service Question"}, envelope.Data)
}
