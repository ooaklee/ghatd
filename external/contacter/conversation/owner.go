// Package commsconversation provides optional conversation ownership and voting
// adapters. Authentication, live authority and immutable history stay with their
// existing managers; hosts opt in at composition time before serving requests.
package commsconversation

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/reply/v2"
)

const (
	// OwnerHeader binds browser mutations to the account which opened the editor.
	OwnerHeader = "X-Comms-Expected-Owner"
	// OwnerChangedCode is a stable compatibility code; it is not an auth credential.
	OwnerChangedCode = "HOST_COMMS_OWNER_CHANGED"
	conversationPath = "/api/v1/ums/comms/{id}/conversation"
	appendOperation  = "usermanager.AppendCommsConversationEntry"
)

var (
	errOwnerRequired   = errors.New("commsconversation/owner-required")
	errOwnerInvalid    = errors.New("commsconversation/owner-invalid")
	errOwnerChanged    = errors.New("commsconversation/owner-changed")
	errSessionRequired = errors.New("commsconversation/session-required")
)

func ownerErrors() reply.ErrorManifest {
	return reply.ErrorManifest{
		errOwnerRequired:   {Title: "A conversation owner is required.", StatusCode: http.StatusPreconditionRequired, Code: "HOST_COMMS_OWNER_REQUIRED"},
		errOwnerInvalid:    {Title: "Provide one valid conversation owner.", StatusCode: http.StatusBadRequest, Code: "HOST_COMMS_OWNER_INVALID"},
		errOwnerChanged:    {Title: "The signed-in account changed.", StatusCode: http.StatusPreconditionFailed, Code: OwnerChangedCode},
		errSessionRequired: {Title: "A verified session is required.", StatusCode: http.StatusUnauthorized, Code: "HOST_COMMS_OWNER_SESSION_REQUIRED"},
	}
}

// RequireRoutes verifies the current framework's native conversation routes and
// attaches the owner guard. Missing capabilities fail startup rather than exposing
// a composer backed by an absent or unprotected append operation.
func RequireRoutes(r *router.Router) error {
	count, err := Attach(r)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("conversation API requires exactly one protected append route")
	}
	reads := 0
	for _, def := range r.RouteInventory() {
		if def.Path != conversationPath {
			continue
		}
		for _, method := range def.Methods {
			if method == http.MethodGet {
				if def.Access != router.AdminSession || def.Operation != "usermanager.GetCommsConversation" {
					return errors.New("conversation read requires the shared administrator-session operation")
				}
				reads++
			}
		}
	}
	if reads != 1 {
		return errors.New("conversation API requires exactly one protected read route")
	}
	return attachContextOwner(r)
}

// Attach adds the owner precondition to the existing protected POST leaf before
// serving. It never creates routes or replaces authentication/policy handling.
func Attach(r *router.Router) (int, error) {
	if r == nil || r.GetRouter() == nil {
		return 0, errors.New("conversation owner guard requires a router")
	}
	if err := r.ValidateRoutePolicies(); err != nil {
		return 0, fmt.Errorf("conversation owner guard requires valid route policies: %w", err)
	}
	sharedErrors := append(usermanager.DependencyErrorMaps(), usermanager.UsermanagerErrorMap, contacter.ContacterErrorMap, accessmanager.AccessmanagerErrorMap)
	sharedErrors = append(sharedErrors, accessmanager.DependencyErrorMaps()...)
	if err := uniqueOwnerCodes(sharedErrors...); err != nil {
		return 0, err
	}
	expected := 0
	conversationPresent := false
	for _, def := range r.RouteInventory() {
		if def.Operation == appendOperation && def.Path != conversationPath {
			return 0, errors.New("conversation append path changed; review the owner guard contract")
		}
		if def.Path != conversationPath {
			continue
		}
		conversationPresent = true
		if !validConversationMethods(def.Methods) {
			return 0, errors.New("conversation routes require separate GET/POST definitions with optional OPTIONS; review the owner guard contract")
		}
		for _, method := range def.Methods {
			if method != http.MethodPost {
				continue
			}
			if def.Access != router.AdminSession || def.Operation != appendOperation {
				return 0, errors.New("conversation append requires the shared administrator-session operation")
			}
			expected++
		}
	}
	if expected > 1 || (conversationPresent && expected != 1) {
		return 0, errors.New("conversation owner guard requires exactly one declared append route")
	}
	var leaves []*mux.Route
	probe := mux.NewRouter().NewRoute().Path(conversationPath)
	pathRegexp, err := probe.GetPathRegexp()
	if err != nil {
		return 0, err
	}
	err = r.GetRouter().Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		pattern, err := route.GetPathRegexp()
		if err != nil || pattern != pathRegexp {
			return nil
		}
		template, err := route.GetPathTemplate()
		if err != nil || template != conversationPath {
			return errors.New("conversation route template changed; review the owner guard contract")
		}
		methods, err := route.GetMethods()
		if err != nil {
			return errors.New("conversation route requires explicit methods")
		}
		if !validConversationMethods(methods) {
			return errors.New("conversation routes require separate GET/POST definitions with optional OPTIONS; review the owner guard contract")
		}
		for _, method := range methods {
			if method != http.MethodPost {
				continue
			}
			if route.GetHandler() == nil {
				return errors.New("conversation append requires an existing handler")
			}
			leaves = append(leaves, route)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(leaves) != expected {
		return 0, errors.New("conversation append route does not match its declared policy inventory")
	}
	for _, leaf := range leaves {
		leaf.Handler(requireOwner(leaf.GetHandler()))
	}
	return len(leaves), nil
}

// Separate native read/append leaves keep their distinct operations and policies.
// OPTIONS-only mux leaves are allowed; a merged GET+POST leaf is rejected.
func validConversationMethods(methods []string) bool {
	get, post := false, false
	for _, method := range methods {
		switch method {
		case http.MethodGet:
			get = true
		case http.MethodPost:
			post = true
		case http.MethodOptions:
		default:
			return false
		}
	}
	return len(methods) > 0 && (!get || !post)
}

func uniqueOwnerCodes(shared ...reply.ErrorManifest) error {
	for _, host := range ownerErrors() {
		for _, manifest := range shared {
			for _, definition := range manifest {
				if definition.Code == host.Code {
					return errors.New("conversation owner error code collides with a shared manifest")
				}
			}
		}
	}
	return nil
}

func expectedOwner(header http.Header) (string, error) {
	var values []string
	for name, candidates := range header {
		if strings.EqualFold(name, OwnerHeader) {
			values = append(values, candidates...)
		}
	}
	if len(values) == 0 {
		return "", errOwnerRequired
	}
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 128 {
		return "", errOwnerInvalid
	}
	for _, c := range []byte(values[0]) {
		if c < 0x21 || c > 0x7e || c == ',' {
			return "", errOwnerInvalid
		}
	}
	return values[0], nil
}

func requireOwner(next http.Handler) http.Handler {
	if _, wrapped := next.(*ownerGuard); wrapped {
		return next
	}
	return &ownerGuard{next: next}
}

// ownerGuard is a typed wrapper so repeated composition is idempotent. It is
// installed only before serving; requests never mutate the route or inventory.
type ownerGuard struct{ next http.Handler }

func (g *ownerGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		g.next.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	owner, err := expectedOwner(r.Header)
	if err == nil {
		session := helpers.AcquireSessionFrom(r.Context())
		if session == nil || session.AccessUUID == "" {
			err = errSessionRequired
		} else if session.UserID != owner {
			err = errOwnerChanged
		}
	}
	if err != nil {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = errormanifest.WriteHTTPError(w, err, []reply.ErrorManifest{ownerErrors()}, reply.WithContext(r.Context()))
		return
	}
	g.next.ServeHTTP(w, r)
}
