package accesspolicymanager

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/errormanifest"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// managementService is the handler's narrow orchestration port, not persistence.
type managementService interface {
	Preview(context.Context, string, accesspolicy.TokenLimits) (accesspolicy.TokenLimitPreview, error)
	Apply(context.Context, string, int64, accesspolicy.TokenLimits) (accesspolicy.Grant, error)
}

// Handler maps bounded administrative JSON/If-Match requests to the manager.
// AttachRoutes supplies the required explicit-session middleware. There is no
// client-supplied actor, system, permission list or executable plan in the body.
type Handler struct {
	// service performs live authority, stored target and lower-domain checks.
	service managementService
	// manifests owns an independent ordered set of canonical keys and overrides.
	manifests []reply.ErrorManifest
}

// ErrorMap contains stable public identities; dependency diagnostics stay private.
var ErrorMap = reply.ErrorManifest{
	ErrInvalidRequest:                {Title: "Invalid policy request", Detail: "Provide all five integer token limits and a valid user identifier", StatusCode: 400, Code: "APM0-001"},
	ErrUserNotFound:                  {Title: "User not found", Detail: "The selected stored user is unavailable", StatusCode: 404, Code: "APM0-002"},
	ErrPreconditionRequired:          {Title: "Revision required", Detail: "Review the policy and send its strong ETag in If-Match", StatusCode: 428, Code: "APM0-003"},
	accesspolicy.ErrConflict:         {Title: "Policy changed", Detail: "Review the current policy before applying a new change", StatusCode: 412, Code: "APM0-004"},
	accesspolicy.ErrDenied:           {Title: "Access denied", Detail: "Current policy-management authority is required", StatusCode: 403, Code: "APM0-005"},
	accesspolicy.ErrConfiguration:    {Title: "Policy unavailable", Detail: "The policy configuration cannot be applied", StatusCode: 503, Code: "APM0-006"},
	apitoken.ErrInventoryUnavailable: {Title: "Inventory unavailable", Detail: "Token inventory preparation is unavailable", StatusCode: 503, Code: "APM0-007"},
	userv2.ErrUserNotFound:           {Title: "Authentication required", Detail: "The authenticated account is unavailable", StatusCode: 401, Code: "APM0-008"},
}

// NewHandler builds the shared reply boundary with optional host overrides.
func NewHandler(service managementService, overrides ...reply.ErrorManifest) (*Handler, error) {
	if service == nil {
		return nil, accesspolicy.ErrConfiguration
	}
	manifests := errormanifest.CloneManifests(errormanifest.NewComposer().Add(auth.AuthErrorMap, accessmanager.AccessmanagerErrorMap, accesspolicy.AccessPolicyErrorMap, ErrorMap).AddOverrides(overrides...).Build()...)
	return &Handler{service: service, manifests: manifests}, nil
}

// PreviewTokenLimits returns a read-only before/after report and the current
// ETag. Revision zero represents the absence of a grant, not blanket authority.
func (h *Handler) PreviewTokenLimits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	limits, err := readLimits(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	preview, err := h.service.Preview(r.Context(), mux.Vars(r)["userID"], limits)
	if err != nil {
		h.fail(w, err)
		return
	}
	revision := int64(0)
	if preview.Before != nil {
		revision = preview.Before.Revision
	}
	w.Header().Set("ETag", revisionTag(revision))
	_ = reply.NewReplier(h.manifests).NewHTTPDataResponse(w, http.StatusOK, preview)
}

// ApplyTokenLimits requires the exact reviewed revision and returns only a
// known-success policy. An uncertain error never becomes a success receipt.
func (h *Handler) ApplyTokenLimits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	expected, err := readRevision(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	limits, err := readLimits(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	grant, err := h.service.Apply(r.Context(), mux.Vars(r)["userID"], expected, limits)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("ETag", revisionTag(grant.Revision))
	_ = reply.NewReplier(h.manifests).NewHTTPDataResponse(w, http.StatusOK, grant)
}

// fail delegates formatting to reply after removing non-manifest diagnostics.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	_ = reply.NewReplier(h.manifests).NewHTTPErrorResponse(w, errormanifest.CanonicalError(err, h.manifests))
}

// revisionTag emits one canonical strong entity tag for a policy revision.
func revisionTag(revision int64) string { return `"` + strconv.FormatInt(revision, 10) + `"` }
