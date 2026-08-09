package paymentprovider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripeUpcomingInvoicePreviewUsesServerSubscriptionAndIncludesTax(t *testing.T) {
	var receivedSubscription string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/invoices/create_preview" || request.Method != http.MethodPost {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer sk_test" || request.Header.Get("Stripe-Version") == "" {
			t.Fatalf("provider headers missing")
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		receivedSubscription = request.PostForm.Get("subscription")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"upcoming_in_1","object":"invoice","subtotal":1800,"total":2160,"amount_due":2160,"currency":"usd","due_date":1701209600,"total_taxes":[{"amount":360,"taxability_reason":"standard_rated"}],"lines":{"data":[]}}`)
	}))
	defer server.Close()

	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test", APIKey: "sk_test", APIBaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := provider.CreateUpcomingInvoicePreview(context.Background(), &UpcomingInvoicePreviewRequest{SubscriptionID: "sub_1"})
	if err != nil {
		t.Fatalf("CreateUpcomingInvoicePreview() error = %v", err)
	}
	if receivedSubscription != "sub_1" {
		t.Fatalf("subscription form = %q", receivedSubscription)
	}
	if preview.Subtotal != 1800 || preview.TaxAmount != 360 || preview.Total != 2160 || preview.AmountDue != 2160 || preview.Currency != "USD" || preview.DueDate == "" {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestStripeUpcomingInvoicePreviewPreservesExplicitZeroAmountDue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"object":"invoice","subtotal":1800,"total":1800,"amount_due":0,"currency":"usd","total_taxes":[]}`)
	}))
	defer server.Close()
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test", APIKey: "sk_test", APIBaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := provider.CreateUpcomingInvoicePreview(context.Background(), &UpcomingInvoicePreviewRequest{SubscriptionID: "sub_1"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.AmountDue != 0 || preview.Total != 1800 {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestStripeUpcomingInvoicePreviewRejectsInvalidAndOversizedResponses(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "invalid currency", body: `{"object":"invoice","amount_due":10,"currency":"not-a-currency"}`},
		{name: "negative amount due", body: `{"object":"invoice","amount_due":-1,"currency":"usd"}`},
		{name: "oversized", body: `{"object":"invoice","amount_due":10,"currency":"usd","padding":"` + strings.Repeat("x", (256<<10)+1) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, test.body) }))
			defer server.Close()
			provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test", APIKey: "sk_test", APIBaseURL: server.URL, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.CreateUpcomingInvoicePreview(context.Background(), &UpcomingInvoicePreviewRequest{SubscriptionID: "sub_1"}); err == nil {
				t.Fatal("CreateUpcomingInvoicePreview() error = nil")
			}
		})
	}
}

type registryInvoicePreviewProvider struct{ *MockProvider }

func (*registryInvoicePreviewProvider) GetProviderName() string { return "preview" }
func (*registryInvoicePreviewProvider) CreateUpcomingInvoicePreview(context.Context, *UpcomingInvoicePreviewRequest) (*UpcomingInvoicePreview, error) {
	return &UpcomingInvoicePreview{AmountDue: 0, Currency: "USD"}, nil
}

func TestProviderRegistryDiscoversUpcomingInvoicePreviewCapability(t *testing.T) {
	registry := NewProviderRegistry()
	provider := &registryInvoicePreviewProvider{MockProvider: NewMockProvider("preview")}
	registry.Register(provider)
	registry.Register(NewMockProvider("webhook-only"))
	got, err := registry.GetUpcomingInvoicePreviewProvider("preview")
	if err != nil || got != provider {
		t.Fatalf("GetUpcomingInvoicePreviewProvider() = %#v, %v", got, err)
	}
	if _, err := registry.GetUpcomingInvoicePreviewProvider("mock-webhook-only"); err != ErrPaymentProviderUnsupportedProvider {
		t.Fatalf("webhook-only error = %v", err)
	}
}
