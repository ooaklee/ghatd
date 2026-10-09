package partnerhttp

// BasePath is the independently mounted Partners API. It has no agreement routes.
const BasePath = "/api/v1/partners"
const BootstrapPath = BasePath + "/csrf"

// Route binds one method/path to a fixed command. Member admission applies to
// every entry; current exact capability/target authority remains with Manager.
type Route struct{ Method, Path, Operation string }

func route(method, path, operation string) Route { return Route{method, BasePath + path, operation} }

// Routes returns a fresh copy of the registry, including selected operator reads.
func Routes() []Route {
	return []Route{
		route("GET", "/program", "partners.program.read"),
		route("GET", "/overview", "partners.overview.read"),
		route("GET", "/share-link", "partners.share-link.read"),
		route("POST", "/share-link/rotate", "partners.share-link.rotate"),
		route("GET", "/referrals", "partners.referrals.read"),
		route("GET", "/ledger", "partners.ledger.read"),
		route("GET", "/claims", "partners.claims.read"),
		route("GET", "/claims/{id}", "partners.claim.read"),
		route("POST", "/claims/{id}/cancel", "partners.claims.cancel"),
		route("POST", "/claims", "partners.claims.create"),
		route("POST", "/enrollment", "partners.enroll"),
		route("GET", "/destination", "partners.destination.read"),
		route("PATCH", "/destination", "partners.destination.update"),
		route("GET", "/admin/access", "admin.partners.access.read"),
		route("GET", "/admin/claims/{id}/action", "admin.partners.claim.read"),
		route("GET", "/admin/{id}/status", "admin.partners.status.read"),
		route("GET", "/admin/{id}/claim-preparation", "admin.partners.claim-preparation.read"),
		route("GET", "/admin/policy/individual/{id}", "admin.partners.policy.individual.read"),
		route("GET", "/admin/policy", "admin.partners.policy.read"),
		route("POST", "/admin/policy", "admin.partners.policy.publish"),
		route("GET", "/admin/{id}/inspect", "admin.partners.inspect"),
		route("GET", "/admin/claims", "admin.partners.claims.queue"),
		route("POST", "/admin/attribution/preview", "admin.partners.attribution.preview"),
		route("POST", "/admin/attribution", "admin.partners.attribution.apply"),
		route("POST", "/admin/claims", "admin.partners.claims.create"),
		route("POST", "/admin/claims/amendment", "admin.partners.claims.amend"),
		route("POST", "/admin/claims/return", "admin.partners.claims.return"),
		route("GET", "/admin/operations", "admin.partners.operations.read"),
		route("POST", "/admin/claims/decision", "admin.partners.claims.decide"),
		route("POST", "/admin/claims/payment-observation", "admin.partners.claims.observe"),
		route("POST", "/admin/claims/payment", "admin.partners.claims.payment"),
		route("POST", "/admin/status", "admin.partners.status"),
	}
}
