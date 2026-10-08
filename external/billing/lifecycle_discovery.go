package billing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Discovery is unavailable until the owning migration has established coverage.
// Missing preparation is not an empty subscription set.
var ErrLifecycleDiscoveryUnprepared = errors.New("billing/lifecycle-discovery-unprepared")

const (
	LifecycleCheckoutSources     = "acknowledged-checkouts-v1"
	LifecycleSubscriptionSources = "subscription-owners-v1"
)

// LifecycleDiscoveryQuery is a trusted manager input. Cursor binds the selected
// scope and source kind, but is not an authorization credential.
type LifecycleDiscoveryQuery struct {
	Scope        RevenueScope `json:"-"`
	Kind, Cursor string       `json:"-"`
	Limit        int          `json:"-"`
}

func lifecycleDiscoveryScope(scope RevenueScope) string {
	b, _ := json.Marshal(scope)
	return subscriptionDigest([]string{string(b)})
}

// LifecycleDiscoverySourceID is the versioned opaque native projection key.
// It is not evidence or permission; the owning read must still join originals.
func LifecycleDiscoverySourceID(scope RevenueScope, kind, original string) string {
	if kind == LifecycleCheckoutSources {
		return subscriptionDigest([]string{"billing-lifecycle-checkout-v1", lifecycleDiscoveryScope(scope), original})
	}
	if kind == LifecycleSubscriptionSources {
		return subscriptionDigest([]string{"billing-lifecycle-source-v1", lifecycleDiscoveryScope(scope), original})
	}
	return ""
}
func discoveryDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// AfterID validates page bounds and scope/kind binding before adapter I/O.
func (q LifecycleDiscoveryQuery) AfterID() (string, error) {
	if !validRevenueScope(q.Scope) || !cleanStatusID(q.Scope.Provider) || !cleanStatusID(q.Scope.AccountID) || (q.Kind != LifecycleCheckoutSources && q.Kind != LifecycleSubscriptionSources) || q.Limit < 1 || q.Limit > 200 {
		return "", ErrRevenueInvalid
	}
	if q.Cursor == "" {
		return "", nil
	}
	parts := strings.Split(q.Cursor, ".")
	if len(parts) != 3 || parts[0] != subscriptionDigest([]any{"lifecycle-discovery-v1", q.Scope, q.Kind}) || parts[1] != "1" || !discoveryDigest(parts[2]) {
		return "", ErrRevenueInvalid
	}
	return parts[2], nil
}

// CursorFor encodes a verified native row position. It does not sign authority
// or certify full coverage; collectors must durably hand off before advancing.
func (q LifecycleDiscoveryQuery) CursorFor(id string) string {
	return subscriptionDigest([]any{"lifecycle-discovery-v1", q.Scope, q.Kind}) + ".1." + id
}

// Joined candidates come from ONE owning snapshot; the service validates their
// original canonical provenance before disclosing the whole bounded page.
// Zero Fact/Anchor means only an immutable paid checkout binding exists, not a
// refreshable status source. Such rows still count towards cursor progress.
type LifecycleDiscoveryCandidate struct {
	ID                                      string                  `json:"-"`
	Revision                                int64                   `json:"-"`
	Scope                                   RevenueScope            `json:"-"`
	PrincipalID, CustomerID, SubscriptionID string                  `json:"-"`
	Intent                                  CheckoutIntent          `json:"-"`
	Fact                                    RevenueFact             `json:"-"`
	Anchor, AnchorReceipt                   CheckoutLifecycleAnchor `json:"-"`
	AnchorIntent                            CheckoutIntent          `json:"-"`
	PaidOwner                               CheckoutAssociation     `json:"-"`
	PaidOwnerIntent                         CheckoutIntent          `json:"-"`
	HasPaidOwner                            bool                    `json:"-"`
}
type LifecycleDiscoverySnapshot struct {
	Items    []LifecycleDiscoveryCandidate `json:"-"`
	Prepared bool                          `json:"-"`
}
type LifecycleDiscoveryRepository interface {
	ReadLifecycleDiscovery(context.Context, LifecycleDiscoveryQuery) (LifecycleDiscoverySnapshot, error)
}
type LifecycleDiscoveryPage struct {
	Items      []LifecycleDiscoveryCandidate `json:"-"`
	NextCursor string                        `json:"-"`
	// ReachedEnd applies only to this prepared projection's current read. Repeat
	// full sweeps to find late inserts behind cursors; it is not provider coverage.
	ReachedEnd bool `json:"-"`
}

// Validate checks the owning page contract before a composing manager can
// disclose private candidates. Canonical business provenance remains in billing.
// A continuation may pass the last visible item because binding-only native
// rows advance the raw position without becoming refreshable candidates.
func (p LifecycleDiscoveryPage) Validate(q LifecycleDiscoveryQuery) error {
	after, err := q.AfterID()
	if err != nil {
		return err
	}
	if len(p.Items) > q.Limit || p.ReachedEnd != (p.NextCursor == "") {
		return ErrRevenueUnavailable
	}
	for _, c := range p.Items {
		if c.ID <= after {
			return ErrRevenueUnavailable
		}
		if err := validateDiscoveryCandidate(q, c); err != nil {
			return err
		}
		if q.Kind == LifecycleSubscriptionSources && c.Fact.ID == "" && c.Anchor.IntentID == "" {
			return ErrRevenueUnavailable
		}
		after = c.ID
	}
	if p.NextCursor != "" {
		nextQuery := q
		nextQuery.Cursor = p.NextCursor
		next, err := nextQuery.AfterID()
		if err != nil {
			return ErrRevenueUnavailable
		}
		start, _ := q.AfterID()
		if next <= start || next < after {
			return ErrRevenueUnavailable
		}
	}
	return nil
}

func validateDiscoveryCandidate(q LifecycleDiscoveryQuery, c LifecycleDiscoveryCandidate) error {
	if c.Scope != q.Scope || c.Revision < 1 || !cleanStatusID(c.PrincipalID) {
		return ErrRevenueUnavailable
	}
	original := c.SubscriptionID
	if q.Kind == LifecycleCheckoutSources {
		original = c.Intent.ID
		if c.Revision != 1 || !validStoredCheckout(c.Intent) || c.Intent.Scope != q.Scope || c.Intent.Request.Mode != "subscription" || c.Intent.Request.UserID != c.PrincipalID || c.Intent.SessionID == "" || c.SubscriptionID != "" || c.CustomerID != "" || c.Fact.ID != "" || c.Anchor.IntentID != "" {
			return ErrRevenueUnavailable
		}
	} else {
		if !cleanStatusID(c.SubscriptionID) || !cleanStatusID(c.CustomerID) {
			return ErrRevenueUnavailable
		}
		if c.HasPaidOwner && (c.PaidOwner.Scope != q.Scope || c.PaidOwner.SubscriptionID != c.SubscriptionID || c.PaidOwner.PrincipalID != c.PrincipalID || c.PaidOwner.CustomerID != c.CustomerID || c.PaidOwner.LinkedAt.IsZero()) {
			return ErrRevenueConflict
		}
		if c.HasPaidOwner {
			b, i := c.PaidOwner, c.PaidOwnerIntent
			if !validStoredCheckout(i) || i.Scope != q.Scope || i.Request.Mode != "subscription" || i.ID != b.IntentID || i.SessionID == "" || i.SessionID != b.SessionID || i.Request.UserID != b.PrincipalID || i.Request.PlanID != b.PlanID || i.Request.CostID != b.CostID || i.Request.PriceID != b.ProviderPriceID || strings.ToUpper(i.Request.ExpectedCurrency) != b.Currency || b.CheckoutCreatedAt.IsZero() || b.CheckoutCreatedAt.Before(i.CreatedAt.Truncate(time.Second)) {
				return ErrRevenueUnavailable
			}
		}
		if c.Fact.ID != "" {
			f := c.Fact
			canonical, err := canonicalRevenueFact(f)
			if err != nil || canonical.ID != f.ID || canonical.Fingerprint != f.Fingerprint || f.Kind != RevenuePayment || f.Scope != q.Scope || f.Sequence < 1 || f.AcceptedAt.IsZero() || f.PrincipalID != c.PrincipalID || f.ProviderCustomerID != c.CustomerID || f.SubscriptionID != c.SubscriptionID {
				return ErrRevenueUnavailable
			}
		}
		if c.Anchor.IntentID != "" {
			a := c.Anchor
			r := c.AnchorReceipt
			i := c.AnchorIntent
			if a.Validate() != nil || r.Validate() != nil || a.Fingerprint != r.Fingerprint || !a.AnchoredAt.Equal(r.AnchoredAt) || a.IntentID != r.IntentID || a.Evidence.SubscriptionID != c.SubscriptionID || lifecycleScope(a.Evidence) != q.Scope || i.Fingerprint != a.IntentFingerprint || i.Request.UserID != a.PrincipalID || i.Request.PlanID != a.PlanID || i.Request.CostID != a.CostID || !matchesLifecycleIntent(i, a.Evidence) {
				return ErrRevenueUnavailable
			}
			if a.PrincipalID != c.PrincipalID || a.Evidence.CustomerID != c.CustomerID {
				return ErrRevenueConflict
			}
		}
		if c.Fact.ID == "" && c.Anchor.IntentID == "" && !c.HasPaidOwner {
			return ErrRevenueUnavailable
		}
	}
	if c.ID != LifecycleDiscoverySourceID(q.Scope, q.Kind, original) {
		return ErrRevenueUnavailable
	}
	return nil
}

// DiscoverLifecycleSources uses the SAME owning revenue repository. It makes
// no provider requests/writes and establishes neither freshness nor eligibility.
// Authority is enforced by the manager before and after this private read.
func (s *RevenueService) DiscoverLifecycleSources(ctx context.Context, q LifecycleDiscoveryQuery) (LifecycleDiscoveryPage, error) {
	if err := revenueContext(ctx); err != nil {
		return LifecycleDiscoveryPage{}, err
	}
	after, err := q.AfterID()
	if err != nil {
		return LifecycleDiscoveryPage{}, err
	}
	if s == nil || revenueNil(s.repo) {
		return LifecycleDiscoveryPage{}, ErrRevenueUnavailable
	}
	repo, ok := s.repo.(LifecycleDiscoveryRepository)
	if !ok || revenueNil(repo) {
		return LifecycleDiscoveryPage{}, ErrRevenueUnavailable
	}
	snapshot, err := repo.ReadLifecycleDiscovery(ctx, q)
	if err != nil {
		return LifecycleDiscoveryPage{}, err
	}
	if !snapshot.Prepared {
		return LifecycleDiscoveryPage{}, ErrLifecycleDiscoveryUnprepared
	}
	if len(snapshot.Items) > q.Limit {
		return LifecycleDiscoveryPage{}, ErrRevenueUnavailable
	}
	out := LifecycleDiscoveryPage{Items: []LifecycleDiscoveryCandidate{}, ReachedEnd: len(snapshot.Items) < q.Limit}
	for _, c := range snapshot.Items {
		if err := ctx.Err(); err != nil {
			return LifecycleDiscoveryPage{}, err
		}
		if c.ID <= after {
			return LifecycleDiscoveryPage{}, ErrRevenueUnavailable
		}
		if err := validateDiscoveryCandidate(q, c); err != nil {
			return LifecycleDiscoveryPage{}, err
		}
		after = c.ID
		if q.Kind == LifecycleCheckoutSources || c.Fact.ID != "" || c.Anchor.IntentID != "" {
			c.Intent.Request = cloneCheckoutRequest(c.Intent.Request)
			c.AnchorIntent.Request = cloneCheckoutRequest(c.AnchorIntent.Request)
			c.PaidOwnerIntent.Request = cloneCheckoutRequest(c.PaidOwnerIntent.Request)
			out.Items = append(out.Items, c)
		}
	}
	if !out.ReachedEnd {
		out.NextCursor = q.CursorFor(after)
	}
	if err := ctx.Err(); err != nil {
		return LifecycleDiscoveryPage{}, err
	}
	if err := out.Validate(q); err != nil {
		return LifecycleDiscoveryPage{}, err
	}
	return out, nil
}
