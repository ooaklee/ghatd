package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// LegacyCheckoutAssociationVersion identifies reviewed historical ownership,
// separately from pre-submission checkout authorization.
const LegacyCheckoutAssociationVersion = "legacy_checkout_association_v1"

// LegacyCheckoutMigrationReceipt is private owning provenance. It records no
// synthetic intent, acknowledgement, financial fact or delivery authentication.
// Persistence adapters explicitly encode private fields into encrypted records.
type LegacyCheckoutMigrationReceipt struct {
	ID                           string                                  `json:"id"`
	Version                      string                                  `json:"version"`
	MigratedAt                   time.Time                               `json:"migrated_at"`
	ActorID                      string                                  `json:"-"`
	Reason                       string                                  `json:"-"`
	SourceID                     string                                  `json:"-"`
	EnvelopeID                   string                                  `json:"-"`
	SourceObservationFingerprint string                                  `json:"-"`
	SourceFingerprint            string                                  `json:"-"`
	RequestFingerprint           string                                  `json:"-"`
	ProofFingerprint             string                                  `json:"-"`
	Association                  CheckoutAssociation                     `json:"-"`
	Checkout                     paymentprovider.RevenueCheckoutEvidence `json:"-"`
	Invoice                      paymentprovider.RevenueInvoiceEvidence  `json:"-"`
}

// LegacyCheckoutMigrationID is the opaque receipt identity for one scope/session.
// It intentionally shares the native session reservation selector with modern
// acknowledgements, while the receipt remains a distinct historical record.
func LegacyCheckoutMigrationID(scope RevenueScope, session string) string {
	body, _ := json.Marshal([]any{LegacyCheckoutAssociationVersion, scope, session})
	sum := sha256.Sum256(body)
	return "legacy_checkout_" + hex.EncodeToString(sum[:])
}

// ProofDigest binds stable reviewed input, actor, source and current provider
// economics. Commit timestamps are excluded so exact retries recover one receipt.
func (r LegacyCheckoutMigrationReceipt) ProofDigest() string {
	a := r.Association
	a.LinkedAt = time.Time{}
	body, _ := json.Marshal([]any{r.Version, r.ActorID, r.Reason, r.SourceID, r.EnvelopeID, r.SourceObservationFingerprint, r.SourceFingerprint, r.RequestFingerprint, a, r.Checkout, r.Invoice})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Validate rejects incomplete or self-inconsistent private provenance. It does
// not replace scoped authority, retained-proof verification or provider reads.
func (r LegacyCheckoutMigrationReceipt) Validate() error {
	a, e, i := r.Association, r.Checkout, r.Invoice
	digest := func(v string) bool {
		b, err := hex.DecodeString(v)
		return err == nil && len(b) == 32 && strings.ToLower(v) == v
	}
	pscope := paymentprovider.RevenueScope{Provider: a.Scope.Provider, AccountID: a.Scope.AccountID, LiveMode: a.Scope.LiveMode}
	if r.Version != LegacyCheckoutAssociationVersion || r.ID != LegacyCheckoutMigrationID(a.Scope, a.SessionID) || !validRevenueScope(a.Scope) || !validRevenueIdentity(r.ActorID) || !validCheckoutText(r.Reason, 256, true) || !validRevenueIdentity(r.SourceID) || !validRevenueIdentity(r.EnvelopeID) || r.SourceObservationFingerprint == "" || r.SourceFingerprint == "" || len(r.SourceFingerprint) > 256 || !digest(r.RequestFingerprint) || !digest(r.ProofFingerprint) || r.ProofFingerprint != r.ProofDigest() || r.MigratedAt.IsZero() || !a.LinkedAt.Equal(r.MigratedAt) || a.IntentID != "" || !validRevenueIdentity(a.SessionID) || !validRevenueIdentity(a.PrincipalID) || !validRevenueIdentity(a.PlanID) || !validRevenueIdentity(a.CostID) {
		return ErrRevenueInvalid
	}
	if e.Scope != pscope || i.Scope != pscope || e.IntentID != "" || e.SessionID != a.SessionID || e.Mode != paymentprovider.CheckoutModeSubscription || e.Status != "complete" || e.ClientReferenceID != a.PrincipalID || e.CustomerID != a.CustomerID || e.SubscriptionID != a.SubscriptionID || e.PriceID != a.ProviderPriceID || e.Currency != a.Currency || !e.CreatedAt.Equal(a.CheckoutCreatedAt) || e.CreatedAt.IsZero() || i.PaidAt.Before(e.CreatedAt) || e.UnitAmountMinor <= 0 || e.IntervalCount != 1 || (e.BillingCadence != "week" && e.BillingCadence != "month" && e.BillingCadence != "year") {
		return ErrRevenueInvalid
	}
	if !validRevenueIdentity(i.InvoiceID) || !validRevenueIdentity(i.PaymentID) || i.CustomerID != a.CustomerID || i.SubscriptionID != a.SubscriptionID || i.Currency != a.Currency || !revenueCurrency.MatchString(i.Currency) || i.CurrencyExponent < 0 || i.CurrencyExponent > 6 || i.GrossPaidMinor <= 0 || i.PaidAt.IsZero() || len(i.Lines) != 1 {
		return ErrRevenueInvalid
	}
	l := i.Lines[0]
	if !validRevenueIdentity(l.ID) || l.SubscriptionID != a.SubscriptionID || l.PriceID != a.ProviderPriceID || l.NetPaidMinor <= 0 || l.NetPaidMinor > i.GrossPaidMinor || l.CumulativeRefundedMinor != 0 {
		return ErrRevenueInvalid
	}
	return nil
}

// LegacyCheckoutMigrationTx is an optional owning transaction surface. Readers
// validate receipt/reservation/association consistency in one snapshot. Install
// atomically commits historical provenance, the shared reservation and native
// association projections; callers must first verify current scoped authority
// and complete provider proof. No provider requests occur inside callbacks.
type LegacyCheckoutMigrationTx interface {
	GetLegacyCheckoutMigration(context.Context, RevenueScope, string) (LegacyCheckoutMigrationReceipt, error)
	InstallLegacyCheckoutAssociation(context.Context, LegacyCheckoutMigrationReceipt) (LegacyCheckoutMigrationReceipt, error)
}

// ResolutionID returns the immutable owning resolution selector for this source.
func (r RevenueObservation) ResolutionID() string {
	return revenueID(r.Scope, "resolution", r.ID, "", "", "")
}
