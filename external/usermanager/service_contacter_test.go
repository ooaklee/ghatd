package usermanager_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/usermanager"
)

type commsTypesServiceStub struct {
	usermanager.ContacterService
	response *contacter.GetAvailableCommsTypesResponse
	err      error
}

func (s *commsTypesServiceStub) GetAvailableCommsTypes(context.Context) (*contacter.GetAvailableCommsTypesResponse, error) {
	return s.response, s.err
}

func TestService_GetAvailableCommsTypes(t *testing.T) {
	t.Parallel()

	want := contacter.CommsTypeMap{
		contacter.CommsType("service-question"): "Service Question",
	}
	svc := &usermanager.Service{
		ContacterService: &commsTypesServiceStub{
			response: &contacter.GetAvailableCommsTypesResponse{CommsTypes: want},
		},
	}

	response, err := svc.GetAvailableCommsTypes(context.Background())

	require.NoError(t, err)
	assert.Equal(t, want, response.CommsTypes)
}

func TestService_GetAvailableCommsTypesPropagatesErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("types unavailable")
	svc := &usermanager.Service{
		ContacterService: &commsTypesServiceStub{err: wantErr},
	}

	response, err := svc.GetAvailableCommsTypes(context.Background())

	assert.ErrorIs(t, err, wantErr)
	require.NotNil(t, response)
	assert.Empty(t, response.CommsTypes)
}

func TestHandler_GetAvailableCommsTypes(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		getAvailableCommsTypesFunc: func(context.Context) (*usermanager.GetAvailableCommsTypesResponse, error) {
			return &usermanager.GetAvailableCommsTypesResponse{
				CommsTypes: contacter.CommsTypeMap{
					contacter.CommsType("service-question"): "Service Question",
				},
			}, nil
		},
	}
	h := newTestHandler(svc)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ums/comms/types", nil)

	h.GetAvailableCommsTypes(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	data := map[string]string{}
	responseData(t, recorder, &data)
	assert.Equal(t, map[string]string{"service-question": "Service Question"}, data)
}
