package usermanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/errormanifest/bundles"
	"github.com/ooaklee/ghatd/external/streaker"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// responseCommsRepository permits only CreateComms in this real-service fixture.
// Any unexpected repository call reaches the nil embedded adapter and fails.
type responseCommsRepository struct {
	// Repository supplies unused capabilities; unexpected calls fail immediately.
	*contacter.Repository
	// calls verifies validation happens before persistence.
	calls int
}

// CreateComms retains the service-built record without touching an external store.
func (r *responseCommsRepository) CreateComms(_ context.Context, comms *contacter.Comms) (*contacter.Comms, error) {
	r.calls++
	return comms, nil
}

// responseCommsValidator leaves required-name/email validation to the domain
// service, so these cases exercise its real joined-error behavior.
type responseCommsValidator struct{}

// Validate accepts decoded fixtures; malformed JSON is rejected by the mapper.
func (responseCommsValidator) Validate(interface{}) error { return nil }

// TestCreateCommsDomainErrorFlow exercises handler, manager and contact-service
// composition without requiring an external database or mocking domain validation.
func TestCreateCommsDomainErrorFlow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		payload    string
		omitBundle bool
		override   bool
		status     int
		codes      []string
		writes     int
	}{
		{"both validation errors", `{}`, false, false, 400, []string{"CT00-02", "CT00-03"}, 0},
		{"singleton missing email", `{"full_name":"Example Person"}`, false, false, 400, []string{"CT00-03"}, 0},
		{"singleton missing name", `{"email":"person@example.com"}`, false, false, 400, []string{"CT00-02"}, 0},
		{"malformed payload", `{`, false, false, 400, []string{"CT00-01"}, 0},
		{"default dependency composition", `{}`, true, false, 400, []string{"CT00-02", "CT00-03"}, 0},
		{"host override", `{"full_name":"Example Person"}`, false, true, 422, []string{"HOST-EMAIL"}, 0},
		{"success unchanged", `{"full_name":"Example Person","email":"person@example.com","message":"Hello"}`, false, false, 201, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &responseCommsRepository{}
			service := usermanager.NewService(&usermanager.NewServiceRequest{ContacterService: contacter.NewService(repo)})
			maps := bundles.UserManager()
			if tc.omitBundle {
				maps = nil
			}
			if tc.override {
				maps = append(maps, reply.ErrorManifest{contacter.ErrEmailRequired: {StatusCode: 422, Code: "HOST-EMAIL"}})
			}
			h := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: service, Validator: responseCommsValidator{}, ErrorMaps: maps})
			rec := httptest.NewRecorder()
			h.CreateComms(rec, httptest.NewRequest(http.MethodPost, "/api/v1/ums/comms", strings.NewReader(tc.payload)))
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Equal(t, tc.writes, repo.calls)
			var body struct {
				Errors []struct {
					Code string `json:"code"`
				} `json:"errors"`
				Data any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			var codes []string
			for _, item := range body.Errors {
				codes = append(codes, item.Code)
			}
			require.Equal(t, tc.codes, codes)
			if tc.writes > 0 {
				require.NotNil(t, body.Data)
			}
		})
	}
}

// TestUserManagerDependencyManifestCoverage prevents dependency bundles from
// omitting contact or streak errors even when their owning maps are complete.
func TestUserManagerDependencyManifestCoverage(t *testing.T) {
	for _, domain := range []struct {
		name     string
		manifest reply.ErrorManifest
	}{
		{"contact", contacter.ContacterErrorMap}, {"streak", streaker.StreakErrorMap},
	} {
		t.Run(domain.name, func(t *testing.T) {
			for key, item := range domain.manifest {
				t.Run(item.Code, func(t *testing.T) {
					for _, wrapped := range []bool{false, true} {
						t.Run(fmt.Sprint("wrapped=", wrapped), func(t *testing.T) {
							err := key
							if wrapped {
								err = fmt.Errorf("private diagnostic: %w", key)
							}
							h := usermanager.NewHandler(&usermanager.NewHandlerRequest{ErrorMaps: bundles.UserManager()})
							rec := httptest.NewRecorder()
							require.NoError(t, h.NewHTTPErrorResponse(rec, err))
							require.Equal(t, item.StatusCode, rec.Code)
							require.Contains(t, rec.Body.String(), item.Code)
							require.NotContains(t, rec.Body.String(), "private diagnostic")
						})
					}
				})
			}
		})
	}
}

// TestUserManagerMixedFailuresDoNotHideUnknownCause preserves the distinction
// between an all-validation collection and an unclassified operational failure.
func TestUserManagerMixedFailuresDoNotHideUnknownCause(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"all mapped", errors.Join(contacter.ErrFullNameRequired, contacter.ErrEmailRequired), 400},
		{"mixed", errors.Join(contacter.ErrFullNameRequired, errors.New("private diagnostic")), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := usermanager.NewHandler(&usermanager.NewHandlerRequest{ErrorMaps: bundles.UserManager()})
			rec := httptest.NewRecorder()
			require.NoError(t, h.NewHTTPErrorResponse(rec, tc.err))
			require.Equal(t, tc.status, rec.Code)
			require.NotContains(t, rec.Body.String(), "private diagnostic")
		})
	}
}
