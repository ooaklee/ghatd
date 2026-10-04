package commsconversation

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// attachContextOwner applies the same session-owner precondition to the existing
// metadata PUT. Follow-up saves must not cross accounts either. Shared policy,
// legacy-field preservation and persistence remain with the original handler.
func attachContextOwner(r *router.Router) error {
	const path = "/api/v1/ums/comms/{id}"
	declared := 0
	for _, def := range r.RouteInventory() {
		if def.Path != path {
			continue
		}
		for _, method := range def.Methods {
			if method != http.MethodPut {
				continue
			}
			if def.Access != router.AdminSession || def.Operation != "usermanager.UpdateComms" {
				return errors.New("communication follow-up requires the shared administrator-session operation")
			}
			declared++
		}
	}
	if declared != 1 {
		return errors.New("communication follow-up requires exactly one protected update route")
	}
	var leaves []*mux.Route
	err := r.GetRouter().Walk(func(leaf *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		template, err := leaf.GetPathTemplate()
		if err != nil || template != path {
			return nil
		}
		methods, err := leaf.GetMethods()
		if err != nil {
			return errors.New("communication follow-up requires explicit methods")
		}
		for _, method := range methods {
			if method == http.MethodPut {
				if leaf.GetHandler() == nil {
					return errors.New("communication follow-up requires an existing handler")
				}
				leaves = append(leaves, leaf)
			} else if method != http.MethodOptions {
				return errors.New("communication follow-up methods changed; review owner binding")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(leaves) != declared {
		return errors.New("communication follow-up route does not match its declared policy inventory")
	}
	leaves[0].Handler(requireOwner(leaves[0].GetHandler()))
	return nil
}
