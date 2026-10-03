package pricer_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// servePriceMutation dispatches the actual handler, mapper, validator, service
// and shared response stack. Authentication is published by the fixture; these
// tests do not substitute for credential verification or route-policy tests.
func servePriceMutation(h *pricer.Handler, op string, w http.ResponseWriter, r *http.Request) {
	switch op {
	case "create plan":
		h.CreatePricePlan(w, r)
	case "update plan":
		h.UpdatePricePlan(w, r)
	case "publish plan":
		h.PublishPricePlan(w, r)
	case "archive plan":
		h.ArchivePricePlan(w, r)
	case "delete plan":
		h.DeletePricePlan(w, r)
	case "create feature":
		h.CreateFeature(w, r)
	case "update feature":
		h.UpdateFeature(w, r)
	case "delete feature":
		h.DeleteFeature(w, r)
	default:
		panic("unknown price operation")
	}
}

func TestPriceMutationHTTPNativeErrors(t *testing.T) {
	for _, op := range []string{"create plan", "update plan", "publish plan", "archive plan", "delete plan", "create feature", "update feature", "delete feature"} {
		for _, tc := range []struct {
			name     string
			failure  error
			status   int
			override bool
		}{
			{"mapped", pricer.ErrInvalidPricePlanPayload, 400, false},
			{"wrapped", fmt.Errorf("private diagnostic: %w", pricer.ErrInvalidPricePlanPayload), 400, false},
			{"unknown", errors.New("private diagnostic"), 500, false},
			{"mixed", errors.Join(pricer.ErrInvalidPricePlanPayload, errors.New("private diagnostic")), 500, false},
			{"unavailable", pricer.ErrPricerUnavailable, 503, false},
			{"host override", fmt.Errorf("private diagnostic: %w", pricer.ErrPricerUnavailable), 502, true},
			{"cancelled", context.Canceled, 500, false},
			{"deadline", context.DeadlineExceeded, 500, false},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				port := newActorPriceStore()
				port.writeError = tc.failure
				var manifests []reply.ErrorManifest
				if tc.override {
					manifests = []reply.ErrorManifest{{pricer.ErrPricerUnavailable: {Title: "Unavailable", Detail: "Host mapping", Code: "HOST-PRICE", StatusCode: 502}}}
				}
				h := pricer.NewHandler(pricer.NewService(port), validator.NewValidator(), manifests...)
				ctx := accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(context.Background(), priceActorID), true)
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Updated","type":"boolean","ActorID":"forged","id":"forged"}`)).WithContext(ctx)
				r = mux.SetURLVars(r, map[string]string{"id": priceTargetID})
				w := httptest.NewRecorder()
				servePriceMutation(h, op, w, r)
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "private diagnostic")
				require.Len(t, port.writes, 1)
				require.Equal(t, priceActorID, port.writes[0].actor)
				if tc.override {
					require.Contains(t, w.Body.String(), "HOST-PRICE")
				}
			})
		}
	}
}
