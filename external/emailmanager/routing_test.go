package emailmanager

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/stretchr/testify/require"
)

type routingFake struct {
	calls        atomic.Int64
	state        emailprovider.SendState
	unsuccessful bool
	err          error
}

func (*routingFake) Name() string                   { return "TEST" }
func (*routingFake) IsHealthy(context.Context) bool { return true }
func (*routingFake) SupportedMailTypes() []emailprovider.MailType {
	return []emailprovider.MailType{emailprovider.Transactional, emailprovider.Marketing}
}
func (*routingFake) MailTypePreference() []emailprovider.MailType { return nil }
func (p *routingFake) Send(_ context.Context, e *emailprovider.Email) (*emailprovider.SendResult, error) {
	p.calls.Add(1)
	return &emailprovider.SendResult{Provider: "TEST", MessageID: "receipt", Success: p.err == nil && !p.unsuccessful, State: p.state}, p.err
}
func (p *routingFake) SubmitCampaign(ctx context.Context, _ *emailprovider.CampaignRequest) (*emailprovider.SendResult, error) {
	return p.Send(ctx, &emailprovider.Email{})
}

func standardRoutingRequest(cfg *RoutingConfig) *NewStandardEmailManagerRequest {
	return &NewStandardEmailManagerRequest{Routing: cfg, FrontendBaseURL: "https://app.example.test", EmailVerificationFullEndpoint: "https://api.example.test/verify", DashboardVerificationURIPath: "https://api.example.test/verify", Environment: "local", BusinessEntityName: "Example", BusinessEntityWebsite: "https://example.test", FromEmailAddress: "from@example.test", NoReplyEmailAddress: "no-reply@example.test", LoginEmailSubject: "Sign in", WelcomeEmailSubject: "Verify", Config: &Config{ShouldSendEmail: true}}
}
func routingEmail(purpose emailprovider.MailType) *SendEmailRequest {
	return &SendEmailRequest{To: "to@example.test", From: "from@example.test", ReplyTo: "reply@example.test", Subject: "Test", HTMLBody: "<p>Test</p>", TextBody: "Test", MailType: purpose}
}

func TestPurposeRoutingSelection(t *testing.T) {
	for _, tc := range []struct {
		name             string
		first, second    []emailprovider.MailType
		purpose          emailprovider.MailType
		route, defaultID string
		want             []string
	}{
		{name: "no preference round robin", purpose: emailprovider.Transactional, want: []string{"a", "b", "a", "b"}},
		{name: "transactional ranks first", first: []emailprovider.MailType{emailprovider.Transactional, emailprovider.Marketing}, second: []emailprovider.MailType{emailprovider.Marketing, emailprovider.Transactional}, purpose: emailprovider.Transactional, want: []string{"a", "a"}},
		{name: "marketing ranks first", first: []emailprovider.MailType{emailprovider.Transactional, emailprovider.Marketing}, second: []emailprovider.MailType{emailprovider.Marketing, emailprovider.Transactional}, purpose: emailprovider.Marketing, want: []string{"b", "b"}},
		{name: "explicit route dominates", first: []emailprovider.MailType{emailprovider.Transactional}, purpose: emailprovider.Transactional, route: "b", want: []string{"b", "b"}},
		{name: "preference overrides transactional default", first: []emailprovider.MailType{emailprovider.Transactional}, purpose: emailprovider.Transactional, defaultID: "b", want: []string{"a", "a"}},
		{name: "transactional default", purpose: emailprovider.Transactional, defaultID: "b", want: []string{"b", "b"}},
		{name: "marketing has no implicit transactional default", purpose: emailprovider.Marketing, defaultID: "b", want: []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := &routingFake{}, &routingFake{}
			cfg := &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: emailprovider.WithMailTypePreference(a, tc.first)}, {ID: "b", Provider: emailprovider.WithMailTypePreference(b, tc.second)}}, DefaultProviderID: tc.defaultID}
			if tc.route != "" {
				cfg.Routes = map[emailprovider.MailType]string{tc.purpose: tc.route}
			}
			m, err := NewStandardEmailManager(standardRoutingRequest(cfg))
			require.NoError(t, err)
			for _, want := range tc.want {
				result, err := m.SendEmailWithResult(context.Background(), routingEmail(tc.purpose))
				require.NoError(t, err)
				require.Equal(t, want, result.ProviderID)
				require.Equal(t, emailprovider.Accepted, result.State)
			}
		})
	}
}
func TestRoutingConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *RoutingConfig
	}{
		{"empty registry", &RoutingConfig{}},
		{"duplicate instance", &RoutingConfig{Providers: []ProviderRegistration{{ID: "same", Provider: &routingFake{}}, {ID: "same", Provider: &routingFake{}}}}},
		{"invalid instance", &RoutingConfig{Providers: []ProviderRegistration{{ID: "private@address", Provider: &routingFake{}}}}},
		{"nil provider", &RoutingConfig{Providers: []ProviderRegistration{{ID: "a"}}}},
		{"typed nil", &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: (*routingFake)(nil)}}}},
		{"duplicate preferences", &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: emailprovider.WithMailTypePreference(&routingFake{}, []emailprovider.MailType{emailprovider.Transactional, emailprovider.Transactional})}}}},
		{"unsupported preference", &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: emailprovider.NewBirdEmailProvider(nil).WithMailTypePreference([]emailprovider.MailType{emailprovider.Marketing})}}}},
		{"unknown purpose", &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: emailprovider.WithMailTypePreference(&routingFake{}, []emailprovider.MailType{"other"})}}}},
		{"missing route provider", &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: &routingFake{}}}, Routes: map[emailprovider.MailType]string{emailprovider.Transactional: "missing"}}},
		{"unsupported route", &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: emailprovider.NewBirdEmailProvider(nil)}}, Routes: map[emailprovider.MailType]string{emailprovider.Marketing: "a"}}},
	} {
		t.Run(tc.name, func(t *testing.T) { _, err := newProviderRouter(tc.cfg); require.ErrorIs(t, err, ErrRoutingInvalid) })
	}
}
func TestRoutedFunctionLocalCapture(t *testing.T) {
	for _, tc := range []struct {
		name     string
		function string
		purpose  emailprovider.MailType
		want     string
	}{
		{"login", "login", emailprovider.Transactional, "operational"},
		{"verification", "verification", emailprovider.Transactional, "operational"},
		{"custom transactional", "custom", emailprovider.Transactional, "operational"},
		{"custom marketing", "custom", emailprovider.Marketing, "promotional"},
		{"rendered transactional", "rendered", emailprovider.Transactional, "operational"},
		{"rendered marketing", "rendered", emailprovider.Marketing, "promotional"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := emailprovider.NewLoggingEmailProvider(nil)
			a, b := &routingFake{}, &routingFake{}
			cfg := &RoutingConfig{Providers: []ProviderRegistration{{ID: "operational", Provider: emailprovider.WithMailTypePreference(a, []emailprovider.MailType{emailprovider.Transactional, emailprovider.Marketing})}, {ID: "promotional", Provider: emailprovider.WithMailTypePreference(b, []emailprovider.MailType{emailprovider.Marketing, emailprovider.Transactional})}}, LocalCapture: capture}
			req := standardRoutingRequest(cfg)
			req.Config.ShouldSendEmail = false
			m, err := NewStandardEmailManager(req)
			require.NoError(t, err)
			var receipt *SendReceipt
			switch tc.function {
			case "login":
				receipt, err = m.SendLoginEmailWithResult(context.Background(), &SendLoginEmailRequest{Email: "to@example.test", Token: "test-token", Code: "12345678"})
			case "verification":
				receipt, err = m.SendVerificationEmailWithResult(context.Background(), &SendVerificationEmailRequest{FirstName: "Test", LastName: "Person", Email: "to@example.test", Token: "test-token", Code: "12345678"})
			case "custom":
				receipt, err = m.SendCustomEmailWithResult(context.Background(), &SendCustomEmailRequest{EmailTo: "to@example.test", EmailSubject: "Test", EmailBody: "Test", TextBody: "Test", MailType: tc.purpose})
			default:
				receipt, err = m.SendEmailWithResult(context.Background(), routingEmail(tc.purpose))
			}
			require.NoError(t, err)
			require.Equal(t, emailprovider.Captured, receipt.State)
			require.Equal(t, tc.want, receipt.ProviderID)
			require.Zero(t, a.calls.Load()+b.calls.Load())
			messages := capture.Inbox().List()
			require.Len(t, messages, 1)
			require.Equal(t, tc.want, messages[0].ProviderID)
			require.Equal(t, "TEST", messages[0].Provider)
			require.Equal(t, tc.purpose, messages[0].MailType)
			require.Equal(t, receipt.MessageID, messages[0].MessageID)
		})
	}
}
func TestRoutedOutcomesNeverFailOver(t *testing.T) {
	for _, tc := range []struct {
		name                string
		enabled             bool
		err                 error
		providerState, want emailprovider.SendState
		purpose             emailprovider.MailType
		wantCalls           int64
	}{
		{"disabled skipped", false, nil, "", emailprovider.Skipped, emailprovider.Transactional, 0},
		{"accepted", true, nil, "", emailprovider.Accepted, emailprovider.Transactional, 1},
		{"uncertain timeout", true, context.DeadlineExceeded, emailprovider.Uncertain, emailprovider.Uncertain, emailprovider.Transactional, 1},
		{"known rejected", true, errors.New("rejected"), emailprovider.Failed, emailprovider.Failed, emailprovider.Transactional, 1},
		{"missing purpose", true, nil, "", emailprovider.Failed, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := &routingFake{err: tc.err, state: tc.providerState}, &routingFake{}
			cfg := &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: a}, {ID: "b", Provider: b}}, Routes: map[emailprovider.MailType]string{emailprovider.Transactional: "a"}}
			req := standardRoutingRequest(cfg)
			req.Config.ShouldSendEmail = tc.enabled
			m, err := NewStandardEmailManager(req)
			require.NoError(t, err)
			receipt, err := m.SendEmailWithResult(context.Background(), routingEmail(tc.purpose))
			require.Equal(t, tc.want, receipt.State)
			require.Equal(t, tc.wantCalls, a.calls.Load())
			require.Zero(t, b.calls.Load())
			if tc.err != nil || tc.purpose == "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Concurrency is the behavior under test: one shared selector must balance exactly.
func TestRoutingConcurrentRoundRobin(t *testing.T) {
	a, b := &routingFake{}, &routingFake{}
	m, err := NewStandardEmailManager(standardRoutingRequest(&RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: a}, {ID: "b", Provider: b}}}))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			_, err := m.SendEmailWithResult(context.Background(), routingEmail(emailprovider.Transactional))
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 50, a.calls.Load())
	require.EqualValues(t, 50, b.calls.Load())
}
func TestCampaignCapabilityAndDisabledPolicy(t *testing.T) {
	for _, tc := range []struct {
		name             string
		capable, enabled bool
		want             emailprovider.SendState
		wantErr          bool
	}{
		{"inline is not campaign", false, true, emailprovider.Failed, true}, {"campaign accepted", true, true, emailprovider.Accepted, false}, {"campaign disabled", true, false, emailprovider.Skipped, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &routingFake{}
			registration := ProviderRegistration{ID: "a", Provider: provider}
			if tc.capable {
				registration.Campaign = provider
			}
			req := standardRoutingRequest(&RoutingConfig{Providers: []ProviderRegistration{registration}})
			req.Config.ShouldSendEmail = tc.enabled
			m, err := NewStandardEmailManager(req)
			require.NoError(t, err)
			receipt, err := m.SubmitCampaign(context.Background(), &emailprovider.CampaignRequest{Name: "Test", AudienceID: "reviewed-audience"})
			require.Equal(t, tc.want, receipt.State)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.want != emailprovider.Accepted {
				require.Zero(t, provider.calls.Load())
			}
		})
	}
}

func TestPreferenceAndConfigurationSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		mutatePreference, mutateRoute bool
	}{{"preference slice", true, false}, {"route map", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			preferences := []emailprovider.MailType{emailprovider.Transactional, emailprovider.Marketing}
			a, b := &routingFake{}, &routingFake{}
			decorated := emailprovider.WithMailTypePreference(a, preferences)
			routes := map[emailprovider.MailType]string{emailprovider.Transactional: "a"}
			cfg := &RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: decorated}, {ID: "b", Provider: b}}, Routes: routes}
			m, err := NewStandardEmailManager(standardRoutingRequest(cfg))
			require.NoError(t, err)
			if tc.mutatePreference {
				preferences[0] = emailprovider.Marketing
				returned := decorated.MailTypePreference()
				returned[0] = emailprovider.Marketing
				require.Equal(t, emailprovider.Transactional, decorated.MailTypePreference()[0])
			}
			if tc.mutateRoute {
				routes[emailprovider.Transactional] = "b"
				cfg.Providers[0].ID = "changed"
			}
			receipt, err := m.SendEmailWithResult(context.Background(), routingEmail(emailprovider.Transactional))
			require.NoError(t, err)
			require.Equal(t, "a", receipt.ProviderID)
			require.Zero(t, b.calls.Load())
		})
	}
}

func TestTrustedPurposeProviderBridge(t *testing.T) {
	for _, tc := range []struct {
		name             string
		bound, requested emailprovider.MailType
		want             string
	}{{"marketing bridge overrides claimed transactional", emailprovider.Marketing, emailprovider.Transactional, "marketing"}, {"transactional bridge overrides claimed marketing", emailprovider.Transactional, emailprovider.Marketing, "transactional"}} {
		t.Run(tc.name, func(t *testing.T) {
			capture := emailprovider.NewLoggingEmailProvider(nil)
			cfg := &RoutingConfig{Providers: []ProviderRegistration{{ID: "transactional", Provider: &routingFake{}}, {ID: "marketing", Provider: &routingFake{}}}, Routes: map[emailprovider.MailType]string{emailprovider.Transactional: "transactional", emailprovider.Marketing: "marketing"}, LocalCapture: capture}
			m, err := NewStandardEmailManager(standardRoutingRequest(cfg))
			require.NoError(t, err)
			provider := m.ProviderForMailType(tc.bound)
			require.True(t, provider.IsHealthy(context.Background()))
			result, err := provider.Send(context.Background(), &emailprovider.Email{To: "to@example.test", From: "from@example.test", Subject: "Test", TextBody: "Test", MailType: tc.requested})
			require.NoError(t, err)
			require.Equal(t, emailprovider.Captured, result.State)
			require.Equal(t, tc.want, capture.Inbox().List()[0].ProviderID)
		})
	}
}

type routingAudit struct {
	records []*audit.UserEmailOutboundEventDetails
}

func (a *routingAudit) LogAuditEvent(_ context.Context, r *audit.LogAuditEventRequest) error {
	a.records = append(a.records, r.Details.(*audit.UserEmailOutboundEventDetails))
	return nil
}

func TestSubmissionReceiptAudit(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		state, want                     emailprovider.SendState
		unsuccessful, enabled, campaign bool
	}{
		{"inline contradictory success", emailprovider.Failed, emailprovider.Failed, false, true, false},
		{"inline known rejected without error", emailprovider.Failed, emailprovider.Failed, true, true, false},
		{"inline ambiguous failure without error", "", emailprovider.Uncertain, true, true, false},
		{"inline accepted", "", emailprovider.Accepted, false, true, false},
		{"inline skipped", "", emailprovider.Skipped, false, false, false},
		{"campaign known rejected without error", emailprovider.Failed, emailprovider.Failed, true, true, true},
		{"campaign ambiguous failure without error", "", emailprovider.Uncertain, true, true, true},
		{"campaign skipped", "", emailprovider.Skipped, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &routingFake{state: tc.state, unsuccessful: tc.unsuccessful}
			recorder := &routingAudit{}
			req := standardRoutingRequest(&RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: provider, Campaign: provider}}})
			req.Config = &Config{ShouldSendEmail: tc.enabled, EnableAuditLogging: true}
			req.AuditService = recorder
			m, err := NewStandardEmailManager(req)
			require.NoError(t, err)
			var receipt *SendReceipt
			if tc.campaign {
				receipt, err = m.SubmitCampaign(context.Background(), &emailprovider.CampaignRequest{Name: "Review", AudienceID: "reviewed"})
			} else {
				receipt, err = m.SendEmailWithResult(context.Background(), routingEmail(emailprovider.Transactional))
			}
			require.Equal(t, tc.want, receipt.State)
			if tc.want == emailprovider.Failed || tc.want == emailprovider.Uncertain {
				require.ErrorIs(t, err, ErrEmailMailerSendFailed)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, recorder.records, 1)
			record := recorder.records[0]
			require.Equal(t, string(tc.want), record.SendState)
			require.Equal(t, "a", record.ProviderID)
			require.Equal(t, tc.want == emailprovider.Accepted, record.SentAt != "")
		})
	}
}

func TestLegacyPurposeAndLocalDecoration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		local bool
	}{{"legacy transactional default", false}, {"decorated local capture", true}} {
		t.Run(tc.name, func(t *testing.T) {
			var provider emailprovider.EmailProvider = &routingFake{}
			if tc.local {
				provider = emailprovider.NewLoggingEmailProvider(nil).WithMailTypePreference(nil)
			}
			m := NewEmailManager(nil, provider, nil, &Config{ShouldSendEmail: !tc.local})
			receipt, err := m.SendEmailWithResult(context.Background(), routingEmail(""))
			require.NoError(t, err)
			require.Equal(t, emailprovider.Transactional, receipt.MailType)
			require.Empty(t, receipt.ProviderID)
			if tc.local {
				require.Equal(t, emailprovider.Captured, receipt.State)
			} else {
				require.Equal(t, emailprovider.Accepted, receipt.State)
			}
		})
	}
}

func TestCampaignRouteFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		route   string
		wantErr bool
	}{{"inline-only explicit route", "inline", true}, {"capable explicit route", "campaign", false}} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := &routingFake{}, &routingFake{}
			m, err := NewStandardEmailManager(standardRoutingRequest(&RoutingConfig{Providers: []ProviderRegistration{{ID: "inline", Provider: a}, {ID: "campaign", Provider: b, Campaign: b}}, Routes: map[emailprovider.MailType]string{emailprovider.Marketing: tc.route}}))
			require.NoError(t, err)
			receipt, err := m.SubmitCampaign(context.Background(), &emailprovider.CampaignRequest{Name: "Review", AudienceID: "reviewed"})
			require.Zero(t, a.calls.Load())
			if tc.wantErr {
				require.ErrorIs(t, err, ErrCapabilityUnavailable)
				require.Equal(t, emailprovider.Failed, receipt.State)
				require.Zero(t, b.calls.Load())
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, b.calls.Load())
			}
		})
	}
}

func TestInvalidWrappedProvider(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cycle bool
	}{{"nested typed nil", false}, {"wrapper cycle", true}} {
		t.Run(tc.name, func(t *testing.T) {
			p := emailprovider.WithMailTypePreference(emailprovider.WithMailTypePreference((*routingFake)(nil), nil), nil)
			if tc.cycle {
				p.EmailProvider = p
			}
			_, err := newProviderRouter(&RoutingConfig{Providers: []ProviderRegistration{{ID: "a", Provider: p}}})
			require.ErrorIs(t, err, ErrRoutingInvalid)
		})
	}
}

type unhealthyRoutingFake struct{ routingFake }

func (*unhealthyRoutingFake) IsHealthy(context.Context) bool { return false }

func TestPurposeBridgeSelectedReadiness(t *testing.T) {
	for _, tc := range []struct {
		name  string
		local bool
	}{{"selected provider unhealthy", false}, {"local interception healthy", true}} {
		t.Run(tc.name, func(t *testing.T) {
			bad, good := &unhealthyRoutingFake{}, &routingFake{}
			cfg := &RoutingConfig{Providers: []ProviderRegistration{{ID: "bad", Provider: bad}, {ID: "good", Provider: good}}}
			if tc.local {
				cfg.LocalCapture = emailprovider.NewLoggingEmailProvider(nil)
			}
			m, err := NewStandardEmailManager(standardRoutingRequest(cfg))
			require.NoError(t, err)
			bridge := m.ProviderForMailType(emailprovider.Transactional)
			require.Equal(t, tc.local, bridge.IsHealthy(context.Background()))
			require.Equal(t, tc.local, bridge.IsHealthy(context.Background()))
			receipt, err := m.SendEmailWithResult(context.Background(), routingEmail(emailprovider.Transactional))
			require.Equal(t, "bad", receipt.ProviderID)
			if tc.local {
				require.NoError(t, err)
				require.Equal(t, emailprovider.Captured, receipt.State)
			} else {
				require.ErrorIs(t, err, ErrEmailMailerProviderUnavailable)
			}
			require.Zero(t, good.calls.Load())
		})
	}
}
