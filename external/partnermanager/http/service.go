package partnerhttp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"path"
	"strings"

	"github.com/ooaklee/ghatd/external/http/browsersecurity"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
)

// Principal is resolved by trusted host authentication, never decoded from JSON.
// ActorID is the caller, not a selected path/body target. Credential stays inside
// the request boundary; Transport contains only an opaque CSRF/rate binding.
type Principal struct {
	ActorID    string                   `json:"-"`
	Credential string                   `json:"-"`
	Verified   bool                     `json:"-"`
	Transport  browsersecurity.Identity `json:"-"`
}

// Request is built by the explicit route registry. DTO decoding cannot supply
// caller identity, capabilities or an alternative operation.
type Request struct {
	Operation string
	ID        string
	Key       string
	Body      json.RawMessage
	Query     url.Values
}

// Response contains only the bounded public representation, never an owner
// record or financial transaction. Durable receipts remain with owning services.
type Response struct {
	Status int
	Body   json.RawMessage
	ETag   string
}

// ServiceConfig is trusted composition over the same human-authorized owners.
// Program is disclosure/admission configuration, not effective commercial terms.
type ServiceConfig struct {
	Manager        *partnermanager.Manager
	Sessions       partneraccess.SessionVerifier
	Authority      partnermanager.Authority
	Program        ProgramView
	PublicOrigin   string
	ReferralPath   string
	AllowLocalHTTP bool
}

// Service projects and delegates Partners commands. It owns no store, financial
// transitions, background loops, grants, or parallel HTTP idempotency ledger.
type Service struct {
	manager      *partnermanager.Manager
	sessions     partneraccess.SessionVerifier
	authority    partnermanager.Authority
	program      ProgramView
	referralBase string
}

// NewService validates immutable wiring without I/O. Hosts must use the same
// live session/action authority as Manager, never its worker-authorized facade.
func NewService(config ServiceConfig) (*Service, error) {
	if config.Manager == nil || missingPartnersAccessPort(config.Sessions) || missingPartnersAccessPort(config.Authority) {
		return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
	}
	u, err := url.Parse(config.PublicOrigin)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.Opaque != "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(config.PublicOrigin, "*\\") {
		return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(config.AllowLocalHTTP && u.Scheme == "http" && loopback) {
		return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
	}
	referralPath := config.ReferralPath
	if referralPath == "" {
		referralPath = "/ref"
	}
	p, err := url.Parse(referralPath)
	if err != nil || len(referralPath) > 2048 || !strings.HasPrefix(referralPath, "/") || strings.HasPrefix(referralPath, "//") ||
		p.Host != "" || p.Scheme != "" || p.RawQuery != "" || p.ForceQuery || p.Fragment != "" || p.Opaque != "" ||
		p.RawPath != "" || p.Path == "/" || path.Clean(p.Path) != p.Path || strings.ContainsAny(referralPath, "*\\") {
		return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
	}
	return &Service{manager: config.Manager, sessions: config.Sessions, authority: config.Authority,
		program: config.Program, referralBase: u.Scheme + "://" + u.Host + referralPath}, nil
}

// Handle refuses unknown operations and binds each resolved caller to the live
// human authority. Commands and original-intent replay stay with owning managers.
func (s *Service) Handle(ctx context.Context, principal Principal, request Request) (Response, error) {
	if s == nil {
		return Response{}, fail("PARTNERS_DEPENDENCY_UNAVAILABLE", 503)
	}
	switch request.Operation {
	case "partners.program.read", "partners.claim.read", "partners.overview.read", "partners.share-link.read", "partners.referrals.read",
		"partners.ledger.read", "partners.claims.read", "partners.destination.read":
		return s.Read(ctx, principal, request)
	case "partners.claims.cancel", "partners.enroll", "partners.share-link.rotate", "partners.claims.create", "partners.destination.update":
		return s.Command(ctx, principal, request)
	case "admin.partners.access.read", "admin.partners.policy.read", "admin.partners.inspect", "admin.partners.claims.queue",
		"admin.partners.claim.read", "admin.partners.status.read", "admin.partners.claim-preparation.read", "admin.partners.policy.individual.read",
		"admin.partners.policy.publish", "admin.partners.attribution.preview", "admin.partners.attribution.apply", "admin.partners.claims.decide",
		"admin.partners.claims.observe", "admin.partners.claims.payment", "admin.partners.claims.amend", "admin.partners.claims.return", "admin.partners.claims.create", "admin.partners.operations.read", "admin.partners.status":
		return s.Admin(ctx, principal, request)
	default:
		return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
	}
}

// member enforces the minimum Partners admission: a non-empty actor and
// credential (401) and a verified principal (403). It performs no capability or
// session check; partnersSession binds the credential to live authority.
func member(principal Principal) error {
	if principal.ActorID == "" || principal.Credential == "" {
		return fail("PARTNERS_AUTH_REQUIRED", 401)
	}
	if !principal.Verified {
		return fail("PARTNERS_VERIFICATION_REQUIRED", 403)
	}
	return nil
}

// decode unmarshals a body into out, treating an empty body as {}. Unknown
// fields or trailing content fail with PARTNERS_INVALID_REQUEST; it never
// supplies identity or authority fields.
func decode(body json.RawMessage, out any) error {
	if len(body) == 0 {
		body = json.RawMessage(`{}`)
	}
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fail("PARTNERS_INVALID_REQUEST", 400)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fail("PARTNERS_INVALID_REQUEST", 400)
	}
	return nil
}

// reply wraps data in the {"data":...} envelope and returns a Response with the
// given status and optional ETag. Marshaling errors are returned to the caller.
func reply(status int, data any, etag string) (Response, error) {
	body, err := json.Marshal(struct {
		Data any `json:"data"`
	}{data})
	return Response{Status: status, Body: body, ETag: etag}, err
}
