package billingmanager

import (
	"context"
	"net/http"
	"strings"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// GetBillingProviderCheckoutStatusRequest selects only an original session.
// ActorID is verified context, never a decoded browser ownership assertion.
type GetBillingProviderCheckoutStatusRequest struct {
	ActorID      string `json:"-"`
	ProviderName string
	SessionID    string
}

// GetBillingProviderCheckoutStatusResponse discloses only outcome and canonical
// catalogue correlation. It is neither an access grant nor a revenue receipt.
type GetBillingProviderCheckoutStatusResponse struct {
	State           string `json:"state"`
	SessionID       string `json:"session_id"`
	PlanID          string `json:"plan_id"`
	CostID          string `json:"cost_id"`
	ProviderPriceID string `json:"provider_price_id"`
}

type acknowledgedCheckoutReader interface {
	FindAcknowledgedCheckout(context.Context, billing.RevenueScope, string) (billing.CheckoutIntent, error)
}

// GetBillingProviderCheckoutStatus verifies the exact retained checkout under
// current paying-account authority. It never creates a checkout, captures facts,
// refreshes provider status storage, reads current access, or grants permissions.
func (s *Service) GetBillingProviderCheckoutStatus(ctx context.Context, req *GetBillingProviderCheckoutStatusRequest) (out *GetBillingProviderCheckoutStatusResponse, err error) {
	if ctx == nil || req == nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !checkoutProviderNamePattern.MatchString(req.ProviderName) || req.ActorID == "" || len(req.ActorID) > 256 || strings.TrimSpace(req.ActorID) != req.ActorID || strings.ContainsAny(req.ActorID, "\r\n\x00") || req.SessionID == "" || len(req.SessionID) > 256 || strings.TrimSpace(req.SessionID) != req.SessionID || strings.ContainsAny(req.SessionID, "\r\n\x00") {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	if s == nil || nilRevenueDependency(s.checkoutPayerAuthority) || nilRevenueDependency(s.checkoutRevenueCapture) || s.CheckoutProviderRegistry == nil {
		return nil, billing.ErrRevenueUnavailable
	}
	if err := s.checkoutPayerAuthority.AuthorizeCheckoutPayer(ctx, req.ActorID); err != nil {
		return nil, err
	}
	// Revocation and cancellation win over provider/owning errors as well as
	// successful results. No partial response survives any failure.
	defer func() {
		if current := s.checkoutPayerAuthority.AuthorizeCheckoutPayer(ctx, req.ActorID); current != nil {
			err = current
		}
		if canceled := ctx.Err(); canceled != nil {
			err = canceled
		}
		if err != nil {
			out = nil
		}
	}()
	reader, ok := s.checkoutRevenueCapture.(acknowledgedCheckoutReader)
	if !ok || nilRevenueDependency(reader) {
		return nil, billing.ErrRevenueUnavailable
	}
	registered, err := s.CheckoutProviderRegistry.GetCheckoutProvider(req.ProviderName)
	if err != nil || nilRevenueDependency(registered) {
		return nil, billing.ErrRevenueUnavailable
	}
	scopeProvider, ok := registered.(paymentprovider.RevenueCheckoutProvider)
	if !ok || nilRevenueDependency(scopeProvider) {
		return nil, billing.ErrRevenueUnavailable
	}
	statusProvider, ok := registered.(paymentprovider.CheckoutStatusProvider)
	if !ok || nilRevenueDependency(statusProvider) {
		return nil, billing.ErrRevenueUnavailable
	}
	scope, err := scopeProvider.CheckoutRevenueScope(ctx)
	if err != nil || scope.Provider != req.ProviderName {
		return nil, billing.ErrRevenueUnavailable
	}
	intent, err := reader.FindAcknowledgedCheckout(ctx, billingRevenueScope(scope), req.SessionID)
	if err != nil || intent.Request.UserID != req.ActorID || intent.Scope != billingRevenueScope(scope) || intent.SessionID != req.SessionID {
		return nil, billing.ErrRevenueUnavailable
	}
	if err := s.checkoutPayerAuthority.AuthorizeCheckoutPayer(ctx, req.ActorID); err != nil {
		return nil, err
	}
	evidence, err := statusProvider.LookupCheckoutStatus(ctx, scope, intent.SessionID)
	if err != nil || intent.ValidateCheckoutStatusEvidence(evidence) != nil {
		return nil, billing.ErrRevenueUnavailable
	}
	state, err := evidence.State()
	if err != nil {
		return nil, billing.ErrRevenueUnavailable
	}
	return &GetBillingProviderCheckoutStatusResponse{State: state, SessionID: intent.SessionID, PlanID: intent.Request.PlanID, CostID: intent.Request.CostID, ProviderPriceID: intent.Request.PriceID}, nil
}

type billingManagerCheckoutStatusService interface {
	GetBillingProviderCheckoutStatus(context.Context, *GetBillingProviderCheckoutStatusRequest) (*GetBillingProviderCheckoutStatusResponse, error)
}

// GetBillingProviderCheckoutStatus binds the caller only from authenticated
// context. Session IDs and arbitrary query fields never confer ownership.
func (h *Handler) GetBillingProviderCheckoutStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodOptions {
		h.getBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusNoContent)
		return
	}
	provider, err := toolbox.GetVariableValueFromUri(r, "providerName")
	if err != nil || r.URL == nil {
		h.NewHTTPErrorResponse(w, ErrInvalidBillingManagerRequestPayload)
		return
	}
	actor := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if actor == "" {
		h.NewHTTPErrorResponse(w, ErrBillingManagerUnableToIdentifyUser)
		return
	}
	values := r.URL.Query()["session_id"]
	if len(values) != 1 {
		h.NewHTTPErrorResponse(w, ErrInvalidBillingManagerRequestPayload)
		return
	}
	service, ok := h.Service.(billingManagerCheckoutStatusService)
	if !ok || nilRevenueDependency(service) {
		h.NewHTTPErrorResponse(w, ErrBillingManagerCheckoutStatusUnavailable)
		return
	}
	response, err := service.GetBillingProviderCheckoutStatus(r.Context(), &GetBillingProviderCheckoutStatusRequest{ActorID: actor, ProviderName: normaliseCheckoutProviderName(provider), SessionID: values[0]})
	if err != nil {
		if singleRevenueError(err, billing.ErrRevenueUnavailable) {
			err = ErrBillingManagerCheckoutStatusUnavailable
		}
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if response == nil {
		h.NewHTTPErrorResponse(w, ErrBillingManagerCheckoutStatusUnavailable)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}
