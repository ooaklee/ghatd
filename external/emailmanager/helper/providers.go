package emailmanagerhelper

import (
	"errors"
	"net/http"

	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/emailprovider"
)

// ErrConfiguration is a safe construction failure with no credentials or raw
// vendor diagnostics. Routing-policy failures belong to the manager constructor.
var ErrConfiguration = errors.New("emailmanager/helper-invalid-configuration")

// BirdConfig supplies one resolved account for the registry-free fallback.
// Keys remain trusted server configuration; construction verifies syntax only.
type BirdConfig struct {
	Token   string
	BaseURL string
}

// ProviderConfig describes a resolved vendor account. Hosts resolve defaults,
// environment names and secrets before calling BuildServices. No field is decoded
// from a public request; never log this value or expose it through an API.
type ProviderConfig struct {
	ID          string
	Vendor      string
	Preferences []emailprovider.MailType
	Token       string
	// BirdBaseURL optionally asserts the region of Token. Ignored for local capture.
	BirdBaseURL string
	// Streams are resolved host policy; empty transactional uses the vendor default.
	TransactionalStream string
	MarketingStream     string
}

// Config is explicit construction policy, independent of a host environment.
// CaptureLocally is a trusted opt-in for development: all manager-routed sends
// use one shared local inbox, preserving the selected account's identity.
type Config struct {
	CaptureLocally bool
	// MaxStoredEmails caps the local inbox; zero uses the provider's default.
	MaxStoredEmails int
	// FallbackBird is used only without Providers and without local capture.
	FallbackBird BirdConfig
	// Providers may contain up to 16 resolved bird/postmark/local accounts.
	Providers         []ProviderConfig
	DefaultProviderID string
	Routes            map[emailprovider.MailType]string
	// HTTPClient is borrowed. Vendor constructors copy its policy; the transport
	// remains caller-owned and must not replay a mutation automatically.
	HTTPClient *http.Client
}

// Services is unfinished manager composition. In registry mode Default is the
// first decorated provider, Routing carries all named accounts and Local is the
// same inbox used for local registrations/interception. Pass both Default and
// Routing to NewStandardEmailManager, which validates IDs/routes/preferences.
type Services struct {
	Default emailprovider.EmailProvider
	Local   *emailprovider.LoggingEmailProvider
	Routing *emailmanager.RoutingConfig
}

// BuildServices constructs vendor instances and optional local capture without
// reading environment/files, network calls, readiness probes or background work.
// Credentials are resolved by the host. Capture replaces remote credentials with
// inert constructor tokens and ignores Bird's remote region assertion. No runtime
// send fallback is added. Failure returns empty Services and a safe error.
//
// Route/preference/capability validation remains with NewStandardEmailManager;
// callers must complete it before admitting requests. Provider handles are not
// themselves intercepted: local capture applies when used through that manager.
func BuildServices(cfg Config) (Services, error) {
	if len(cfg.Providers) > 16 || cfg.MaxStoredEmails < 0 {
		return Services{}, ErrConfiguration
	}
	var services Services
	if cfg.CaptureLocally {
		services.Local = emailprovider.NewLoggingEmailProvider(&emailprovider.LoggingEmailProviderConfig{MaxStoredEmails: cfg.MaxStoredEmails})
	}
	if len(cfg.Providers) == 0 {
		if cfg.CaptureLocally {
			services.Default = services.Local
			return services, nil
		}
		client, err := emailprovider.NewBirdClient((&emailprovider.NewBirdClientRequest{APIKey: cfg.FallbackBird.Token, BaseURL: cfg.FallbackBird.BaseURL}).WithHTTPClient(cfg.HTTPClient))
		if err != nil {
			return Services{}, ErrConfiguration
		}
		services.Default = emailprovider.NewBirdEmailProvider(client)
		return services, nil
	}
	routing := &emailmanager.RoutingConfig{LocalCapture: services.Local, DefaultProviderID: cfg.DefaultProviderID, Routes: make(map[emailprovider.MailType]string, len(cfg.Routes))}
	for purpose, id := range cfg.Routes {
		routing.Routes[purpose] = id
	}
	for _, entry := range cfg.Providers {
		var provider emailprovider.EmailProvider
		switch entry.Vendor {
		case "bird":
			token, baseURL := entry.Token, entry.BirdBaseURL
			if cfg.CaptureLocally {
				token, baseURL = "bk_eu1_localpreview", ""
			}
			client, err := emailprovider.NewBirdClient((&emailprovider.NewBirdClientRequest{APIKey: token, BaseURL: baseURL}).WithHTTPClient(cfg.HTTPClient))
			if err != nil {
				return Services{}, ErrConfiguration
			}
			provider = emailprovider.NewBirdEmailProvider(client)
		case "postmark":
			token := entry.Token
			if cfg.CaptureLocally {
				token = "local-preview-token"
			}
			client, err := emailprovider.NewPostmarkClient((&emailprovider.NewPostmarkClientRequest{ServerToken: token, TransactionalStream: entry.TransactionalStream, MarketingStream: entry.MarketingStream}).WithHTTPClient(cfg.HTTPClient))
			if err != nil {
				return Services{}, ErrConfiguration
			}
			provider = emailprovider.NewPostmarkEmailProvider(client)
		case "local":
			if !cfg.CaptureLocally {
				return Services{}, ErrConfiguration
			}
			provider = services.Local
		default:
			return Services{}, ErrConfiguration
		}
		routing.Providers = append(routing.Providers, emailmanager.ProviderRegistration{ID: entry.ID, Provider: emailprovider.WithMailTypePreference(provider, entry.Preferences)})
	}
	services.Routing = routing
	services.Default = routing.Providers[0].Provider
	return services, nil
}
