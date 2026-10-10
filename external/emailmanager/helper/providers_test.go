package emailmanagerhelper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/stretchr/testify/require"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testManager(t *testing.T, services Services) (*emailmanager.EmailManager, error) {
	t.Helper()
	return emailmanager.NewStandardEmailManager(&emailmanager.NewStandardEmailManagerRequest{
		Provider: services.Default, Routing: services.Routing, FrontendBaseURL: "https://app.example.test",
		EmailVerificationFullEndpoint: "https://api.example.test/verify", DashboardVerificationURIPath: "/verify",
		Environment: "test", BusinessEntityName: "Example", BusinessEntityWebsite: "https://example.test",
		FromEmailAddress: "from@example.test", NoReplyEmailAddress: "from@example.test", Config: &emailmanager.Config{ShouldSendEmail: true},
	})
}

func TestBuildServicesConstructionAndBorrowedClient(t *testing.T) {
	cases := []struct {
		name, vendor                                   string
		capture                                        bool
		count                                          int
		fallback, invalidToken, negativeInbox, wantErr bool
	}{
		{name: "local_fallback", capture: true, fallback: true},
		{name: "bird_fallback", fallback: true},
		{name: "bird_registry", vendor: "bird", count: 1},
		{name: "postmark_registry", vendor: "postmark", count: 1},
		{name: "shared_postmark_client", vendor: "postmark", count: 2},
		{name: "local_registry", vendor: "local", capture: true, count: 1},
		{name: "local_forbidden", vendor: "local", count: 1, wantErr: true},
		{name: "unknown_vendor", vendor: "unknown", count: 1, wantErr: true},
		{name: "missing_resolved_token", vendor: "bird", count: 1, invalidToken: true, wantErr: true},
		{name: "invalid_postmark_token", vendor: "postmark", count: 1, invalidToken: true, wantErr: true},
		{name: "missing_fallback", fallback: true, invalidToken: true, wantErr: true},
		{name: "sixteen_accounts", vendor: "postmark", count: 16},
		{name: "seventeen_accounts", vendor: "postmark", count: 17, wantErr: true},
		{name: "negative_inbox", capture: true, negativeInbox: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			transport := transportFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, fmt.Errorf("unexpected I/O") })
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			redirect := func(*http.Request, []*http.Request) error { return io.EOF }
			client := &http.Client{Timeout: 7 * time.Second, Transport: transport, Jar: jar, CheckRedirect: redirect}
			cfg := Config{CaptureLocally: tc.capture, HTTPClient: client, FallbackBird: BirdConfig{Token: "bk_eu1_fixture"}}
			if tc.negativeInbox {
				cfg.MaxStoredEmails = -1
			}
			if tc.invalidToken {
				cfg.FallbackBird.Token = ""
			}
			for i := 0; i < tc.count; i++ {
				token := "synthetic-postmark-token"
				if tc.vendor == "bird" {
					token = "bk_eu1_fixture"
				}
				if tc.invalidToken {
					token = ""
				}
				cfg.Providers = append(cfg.Providers, ProviderConfig{ID: fmt.Sprintf("account-%d", i), Vendor: tc.vendor, Token: token})
			}
			services, err := BuildServices(cfg)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrConfiguration)
				require.Equal(t, Services{}, services)
				require.Equal(t, ErrConfiguration.Error(), err.Error())
			} else {
				require.NoError(t, err)
				require.NotNil(t, services.Default)
				if tc.capture {
					require.NotNil(t, services.Local)
				} else {
					require.Nil(t, services.Local)
				}
				if tc.fallback {
					require.Nil(t, services.Routing)
				} else {
					require.Len(t, services.Routing.Providers, tc.count)
					require.Same(t, services.Routing.Providers[0].Provider, services.Default)
				}
			}
			require.Zero(t, calls.Load(), "construction cannot probe or send")
			require.Equal(t, 7*time.Second, client.Timeout)
			require.Same(t, jar, client.Jar)
			require.Equal(t, reflect.ValueOf(transport).Pointer(), reflect.ValueOf(client.Transport).Pointer())
			require.Equal(t, reflect.ValueOf(redirect).Pointer(), reflect.ValueOf(client.CheckRedirect).Pointer())
			require.ErrorIs(t, client.CheckRedirect(nil, nil), io.EOF)
		})
	}
}

func TestBuildServicesManagerPolicyValidation(t *testing.T) {
	cases := []struct {
		name, vendor                                          string
		duplicate, marketingPreference, missingRoute, wantErr bool
	}{
		{name: "duplicate_ids", vendor: "postmark", duplicate: true, wantErr: true},
		{name: "unsupported_preference", vendor: "bird", marketingPreference: true, wantErr: true},
		{name: "missing_route", vendor: "bird", missingRoute: true, wantErr: true},
		{name: "local_marketing", vendor: "local", marketingPreference: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := ProviderConfig{ID: "account", Vendor: tc.vendor}
			if tc.marketingPreference {
				entry.Preferences = []emailprovider.MailType{emailprovider.Marketing}
			}
			cfg := Config{CaptureLocally: true, Providers: []ProviderConfig{entry}, Routes: map[emailprovider.MailType]string{}}
			if tc.duplicate {
				cfg.Providers = append(cfg.Providers, entry)
			}
			if tc.missingRoute {
				cfg.Routes[emailprovider.Transactional] = "absent"
			}
			services, err := BuildServices(cfg)
			require.NoError(t, err, "manager owns final policy validation")
			manager, err := testManager(t, services)
			if tc.wantErr {
				require.ErrorIs(t, err, emailmanager.ErrRoutingInvalid)
				require.Nil(t, manager)
			} else {
				require.NoError(t, err)
				require.NotNil(t, manager)
			}
		})
	}
}

func TestBuildServicesLocalCaptureAndConfigCopies(t *testing.T) {
	cases := []struct {
		name, vendor string
		purpose      emailprovider.MailType
	}{
		{"bird_US_region_capture", "bird", emailprovider.Transactional},
		{"postmark_marketing_capture", "postmark", emailprovider.Marketing},
		{"logging_marketing_capture", "local", emailprovider.Marketing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			prefs := []emailprovider.MailType{tc.purpose}
			routes := map[emailprovider.MailType]string{tc.purpose: "selected"}
			cfg := Config{CaptureLocally: true, Providers: []ProviderConfig{{ID: "selected", Vendor: tc.vendor,
				Token: "credential-canary-with-invalid-syntax\n", BirdBaseURL: "https://us1.platform.bird.com",
				MarketingStream: "broadcast", Preferences: prefs}}, Routes: routes,
				HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, io.EOF })}}
			services, err := BuildServices(cfg)
			require.NoError(t, err)
			routes[tc.purpose] = "mutated"
			prefs[0] = "mutated"
			manager, err := testManager(t, services)
			require.NoError(t, err)
			receipt, err := manager.SendEmailWithResult(context.Background(), &emailmanager.SendEmailRequest{
				MailType: tc.purpose, From: "from@example.test", To: "to@example.test", Subject: "Fixture", TextBody: "Local capture"})
			require.NoError(t, err)
			require.Equal(t, emailprovider.Captured, receipt.State)
			require.Equal(t, "selected", receipt.ProviderID)
			inbox := services.Local.Inbox().List()
			require.Len(t, inbox, 1)
			require.Equal(t, "selected", inbox[0].ProviderID)
			require.Zero(t, calls.Load())
			require.NotContains(t, fmt.Sprint(inbox), "credential-canary")
			if tc.vendor == "local" {
				wrapper := services.Default.(*emailprovider.PreferredProvider)
				require.Same(t, services.Local, wrapper.EmailProvider)
			}
		})
	}
}

func TestBuildServicesResolvedCredentialsOnSyntheticWire(t *testing.T) {
	cases := []struct {
		name, vendor, token, url, header, value, response string
		fallback                                          bool
		status                                            int
	}{
		{"bird_fallback", "bird", "bk_eu1_resolved", "https://eu1.platform.bird.com/v1/email/messages", "Authorization", "Bearer bk_eu1_resolved", `{"id":"em_fixture","status":"accepted","accepted_count":1}`, true, 202},
		{"bird_registry_US", "bird", "bk_us1_resolved", "https://us1.platform.bird.com/v1/email/messages", "Authorization", "Bearer bk_us1_resolved", `{"id":"em_fixture","status":"accepted","accepted_count":1}`, false, 202},
		{"postmark_registry", "postmark", "resolved-postmark", "https://api.postmarkapp.com/email", "X-Postmark-Server-Token", "resolved-postmark", `{"ErrorCode":0,"MessageID":"fixture"}`, false, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			cfg := Config{FallbackBird: BirdConfig{Token: tc.token}, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				require.Equal(t, tc.url, r.URL.String())
				require.Equal(t, tc.value, r.Header.Get(tc.header))
				require.Equal(t, http.MethodPost, r.Method)
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.response)), Request: r}, nil
			})}}
			if !tc.fallback {
				cfg.Providers = []ProviderConfig{{ID: "account", Vendor: tc.vendor, Token: tc.token}}
			}
			services, err := BuildServices(cfg)
			require.NoError(t, err)
			require.Zero(t, calls.Load())
			result, err := services.Default.Send(context.Background(), &emailprovider.Email{MailType: emailprovider.Transactional, From: "from@example.test", To: "to@example.test", Subject: "Fixture", TextBody: "Synthetic wire"})
			require.NoError(t, err)
			require.Equal(t, emailprovider.Accepted, result.State)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}
