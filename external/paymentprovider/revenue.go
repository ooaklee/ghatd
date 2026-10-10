package paymentprovider

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// RevenueProvider is an optional authenticated economic-evidence capability.
// Access state, checkout completion and catalogue prices are not paid revenue.
// Implementations must authenticate each webhook before returning its scope.
type RevenueProvider interface {
	// ResolveRevenueWebhook authenticates an incoming webhook request and returns
	// its RevenueEvidence; the Stripe implementation verifies the signed envelope
	// before using any field.
	ResolveRevenueWebhook(context.Context, *http.Request) (*RevenueEvidence, error)
	// LookupRevenueInvoice retrieves paid invoice evidence within the accepted
	// scope; the Stripe implementation requires exactly one matching payment intent
	// and line nets summing to the invoice net, otherwise returns
	// ErrRevenueUnassessable.
	LookupRevenueInvoice(context.Context, RevenueInvoiceRequest) (*RevenueInvoiceEvidence, error)
}

// RevenueReconciliationProvider can re-fetch an already authenticated source
// without a fresh delivery signature. Billing supplies only persisted source
// scope/identity after its current reconciliation authority check.
type RevenueReconciliationProvider interface {
	// ReconcileRevenueEvent re-fetches the identified event within the persisted
	// scope without a fresh delivery signature; the Stripe implementation binds
	// retrieval to the already accepted scope and refuses forged signatures.
	ReconcileRevenueEvent(context.Context, RevenueScope, string) (*RevenueEvidence, error)
}

// RevenueSnapshotVerifier locally checks a retained original snapshot against
// its previously authenticated source fingerprint. It provides no new delivery
// authentication and does not replace authenticated provider reconciliation.
// Billing uses this optional native capability only after current authority.
type RevenueSnapshotVerifier interface {
	// VerifyRetainedRevenueSnapshot checks a retained original snapshot against its
	// previously authenticated source fingerprint and derives a versioned
	// RevenueSnapshotIdentity. Per the RevenueSnapshotVerifier contract it provides
	// local verification only, without provider requests or new delivery
	// authentication.
	VerifyRetainedRevenueSnapshot(context.Context, RevenueSnapshotRequest) (RevenueSnapshotIdentity, error)
}

// RevenueSnapshotRequest binds private recovery material to one owning source.
// Hosts must obtain the original snapshot through a trusted recovery procedure;
// raw payloads must not be exposed through customer/operator HTTP contracts.
type RevenueSnapshotRequest struct {
	Scope               RevenueScope
	EnvelopeID          string
	OriginalFingerprint string `json:"-"`
	OriginalSnapshot    []byte `json:"-"`
}

// RevenueSnapshotIdentity preserves the original hash and derives a stable
// hash from that exact snapshot. Neither value is public financial reporting.
type RevenueSnapshotIdentity struct {
	Scope                RevenueScope
	EnvelopeID           string
	OriginalFingerprint  string `json:"-"`
	CanonicalFingerprint string `json:"-"`
}

var (
	ErrRevenueNotEnabled       = errors.New("paymentprovider/revenue-not-enabled")
	ErrRevenueEventNotRelevant = errors.New("paymentprovider/revenue-event-not-relevant")
	ErrRevenueUnassessable     = errors.New("paymentprovider/revenue-unassessable")
)

// RevenueConfig explicitly opts an account/mode into economic evidence. The
// account is verified using the authenticated API; it is not inferred from an
// environment label or an API-key prefix. Connected accounts are an allowlist.
type RevenueConfig struct {
	AccountID           string
	LiveMode            bool
	ConnectedAccountIDs []string
	CurrencyExponents   map[string]int
}

// RevenueScope names the authenticated provider account and mode that own a
// piece of economic evidence. It is derived by the provider after
// authentication, never supplied by callers as input.
type RevenueScope struct {
	Provider  string
	AccountID string
	LiveMode  bool
}

// RevenueEvidence contains only authenticated immutable delivery identity and
// economic references. A nonempty quarantine reason is a durable owning-service
// decision, not a transport outage. No raw payload, email or API key is exposed.
type RevenueEvidence struct {
	SourceFingerprint            string `json:"-"`
	LegacySourceFingerprint      string `json:"-"`
	Scope                        RevenueScope
	EnvelopeID                   string
	Kind                         string
	EffectiveAt                  time.Time
	PaymentID                    string
	InvoiceID                    string
	AdjustmentID                 string
	AffectedMinor                int64
	Currency                     string
	CumulativeRefundedGrossMinor int64
	Invoice                      *RevenueInvoiceEvidence
	QuarantineReason             string
}

// RevenueInvoiceRequest asks a provider to re-fetch one invoice within an
// already authenticated scope, optionally including refunds checked against an
// expected cumulative refunded amount.
type RevenueInvoiceRequest struct {
	Scope                                RevenueScope
	InvoiceID                            string
	PaymentID                            string
	IncludeRefunds                       bool
	ExpectedCumulativeRefundedGrossMinor int64
}

// RevenueInvoiceEvidence represents a completely collected, reconciled paid
// invoice. Each line retains its provider price identity; billing must resolve
// the historical server-owned payer/plan/cost separately before accepting it.
type RevenueInvoiceEvidence struct {
	Scope            RevenueScope
	InvoiceID        string
	PaymentID        string
	CustomerID       string
	SubscriptionID   string
	Currency         string
	CurrencyExponent int
	PaidAt           time.Time
	GrossPaidMinor   int64
	Lines            []RevenueLineEvidence
}

// RevenueLineEvidence carries one invoice line's provider identities and minor-
// unit net/refunded amounts. Billing resolves plan and price meaning separately
// from these references.
type RevenueLineEvidence struct {
	ID                      string
	SubscriptionID          string
	PriceID                 string
	NetPaidMinor            int64
	CumulativeRefundedMinor int64
	// PeriodStart and PeriodEnd are optional authenticated service-period
	// boundaries. Zero means unavailable; financial allocation does not infer them.
	PeriodStart, PeriodEnd time.Time
}

// GetRevenueProvider resolves a registered provider by name and returns it only
// when it implements the revenue capability; otherwise
// ErrPaymentProviderUnsupportedProvider.
func (r *ProviderRegistry) GetRevenueProvider(name string) (RevenueProvider, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	capability, ok := p.(RevenueProvider)
	if !ok || isNilProviderCapability(capability) {
		return nil, ErrPaymentProviderUnsupportedProvider
	}
	return capability, nil
}
