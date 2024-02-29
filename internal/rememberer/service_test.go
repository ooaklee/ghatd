package rememberer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/rememberer"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/testing/stubs/repositorystubs"
	"github.com/stretchr/testify/assert"
)

func TestService_GetWordById(t *testing.T) {
	tests := []struct {
		name             string
		repository       *repositorystubs.Repository
		request          *rememberer.GetWordByIdRequest
		expectedResponse *rememberer.GetWordByIdResponse
		expectedError    error
	}{
		{
			name: "Success - Word found",
			repository: &repositorystubs.Repository{
				GetWordByIdResponse: &getMockWords()[0],
			},
			request: &rememberer.GetWordByIdRequest{
				Id: "8ba655eb-bcc6-4246-9c78-ac070cf3ac8e",
			},
			expectedResponse: &rememberer.GetWordByIdResponse{
				Word: &rememberer.Word{
					Id:        "8ba655eb-bcc6-4246-9c78-ac070cf3ac8e",
					Name:      "fire truck",
					CreatedAt: "2021-04-01T15:04:05",
				},
			},
			expectedError: nil,
		},
		{
			name: "Repository Error",
			repository: &repositorystubs.Repository{
				GetWordByIdError: errors.New("boom boom pow"),
			},
			request: &rememberer.GetWordByIdRequest{
				Id: "8ba655eb-bcc6-4246-9c78-ac070cf3ac8e",
			},
			expectedError: errors.New("boom boom pow"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := rememberer.NewService(test.repository)
			res, err := service.GetWordById(context.Background(), test.request)

			assert.Equal(t, test.expectedError, err)
			assert.Equal(t, test.expectedResponse, res)

		})
	}
}
