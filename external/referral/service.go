// Package referral owns stable share links and immutable signup attribution.
// Business rules depend only on typed ports; adapters own persistence and CAS.
package referral

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("referral/not-found")
	ErrAlreadyExists     = errors.New("referral/already-exists")
	ErrCodeTaken         = errors.New("referral/code-taken")
	ErrCodeRetired       = errors.New("referral/code-retired")
	ErrInvalid           = errors.New("referral/invalid")
	ErrDenied            = errors.New("referral/denied")
	ErrUnavailable       = errors.New("referral/unavailable")
	ErrUncertain         = errors.New("referral/uncertain")
	ErrSelfReferral      = errors.New("referral/self-referral")
	ErrAlreadyReferred   = errors.New("referral/already-referred")
	ErrStaleWrite        = errors.New("referral/stale-write")
	ErrAttributionLocked = errors.New("referral/attribution-locked")
	ErrCapacity          = errors.New("referral/analytics-capacity")
	ErrGranularity       = errors.New("referral/analytics-granularity")
)

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

const ProgramID = "partners-v1"

// Link is immutable apart from retirement. Code uniqueness includes retired
// rows; an active-partner unique guard prevents competing initial links.
type Link struct {
	ID           string     `json:"id" bson:"id"`
	ProgramID    string     `json:"program_id" bson:"program_id"`
	PartnerID    string     `json:"partner_id" bson:"partner_id"`
	Code         string     `json:"code" bson:"code"`
	CreatedAt    time.Time  `json:"created_at" bson:"created_at"`
	RetiredAt    *time.Time `json:"retired_at,omitempty" bson:"retired_at,omitempty"`
	RetireReason string     `json:"retire_reason,omitempty" bson:"retire_reason,omitempty"`
	RetiredBy    string     `json:"retired_by,omitempty" bson:"retired_by,omitempty"`
}

// Click is bounded, non-authoritative analytics. Observations never establish
// enrollment, a new signup or a paid subscription. UserAgent is not collected;
// non-empty values are rejected by persistence and reporting boundaries.
type Click struct {
	ID              string    `json:"id" bson:"id"`
	LinkID          string    `json:"link_id" bson:"link_id"`
	Code            string    `json:"code" bson:"code"`
	OccurredAt      time.Time `json:"occurred_at" bson:"occurred_at"`
	UserAgent       string    `json:"user_agent,omitempty" bson:"user_agent,omitempty"`
	Classification  string    `json:"classification,omitempty" bson:"classification,omitempty"`
	MeasuredClickID string    `json:"measured_click_id,omitempty" bson:"measured_click_id,omitempty"`
	VisitDigest     string    `json:"visit_digest,omitempty" bson:"visit_digest,omitempty"` // link-scoped keyed digest; never a raw nonce
}

// TermsSnapshot freezes the full policy calculation and provenance at signup.
// RecurrenceEndsAt is derived at attribution, never moved forward by renewals.
type TermsSnapshot struct {
	RateBasisPoints  int        `json:"rate_basis_points"`
	HoldDurationDays int        `json:"hold_duration_days"`
	Currency         string     `json:"currency"`
	CurrencyExponent int        `json:"currency_exponent"`
	TermsVersion     string     `json:"terms_version"`
	PolicyVersionIDs []string   `json:"policy_version_ids"`
	EligiblePlanIDs  []string   `json:"eligible_plan_ids"`
	RecurrenceEndsAt *time.Time `json:"recurrence_ends_at,omitempty"`
}

// Referral is one immutable ownership revision. The adapter updates a separate
// current head and appends this record atomically; corrections never replace it.
type Referral struct {
	ID               string        `json:"id" bson:"id"`
	Revision         int64         `json:"revision" bson:"revision"`
	ProgramID        string        `json:"program_id" bson:"program_id"`
	PartnerID        string        `json:"partner_id" bson:"partner_id"`
	ReferredCustomer string        `json:"referred_customer" bson:"referred_customer"`
	SignupID         string        `json:"signup_id,omitempty" bson:"signup_id,omitempty"`
	EvidenceDigest   string        `json:"evidence_digest,omitempty" bson:"evidence_digest,omitempty"`
	SourceCode       string        `json:"source_code" bson:"source_code"`
	SourceKind       string        `json:"source_kind" bson:"source_kind"`
	LockedAt         time.Time     `json:"locked_at" bson:"locked_at"`
	PostedAt         time.Time     `json:"posted_at" bson:"posted_at"`
	TermsSnapshot    TermsSnapshot `json:"terms_snapshot" bson:"terms_snapshot"`
	CorrectionOf     string        `json:"correction_of,omitempty" bson:"correction_of,omitempty"`
	CorrectionBy     string        `json:"correction_by,omitempty" bson:"correction_by,omitempty"`
	CorrectionReason string        `json:"correction_reason,omitempty" bson:"correction_reason,omitempty"`
	PriorPartnerID   string        `json:"prior_partner_id,omitempty" bson:"prior_partner_id,omitempty"`
	// Correction is private receipt evidence. Persistence adapters must encode
	// it explicitly; customer transports must project their permitted fields.
	Correction               *CorrectionReceipt `json:"-" bson:"-"`
	SourceMeasuredClickID    string             `json:"source_measured_click_id,omitempty" bson:"source_measured_click_id,omitempty"`
	SourceMeasuredOccurredAt *time.Time         `json:"source_measured_occurred_at,omitempty" bson:"source_measured_occurred_at,omitempty"`
}

// PartnerState comes from the program owning service, not browser input.
type PartnerState struct {
	PartnerID           string
	CustomerID          string
	CanAcquireReferrals bool
}

// Eligibility is a trusted account-creation fact plus already authenticated
// evidence. The manager must resolve signup ID/time and principal type through
// the identity owning service, and verify the token with EvidenceSigner.
type Eligibility struct {
	ReferredCustomer   string
	SignupID           string
	IsExistingCustomer bool
	IsIndividual       bool
	At                 time.Time
	Evidence           Evidence
	Terms              TermsSnapshot
}

type Clock interface{ Now() time.Time }
type IDGenerator interface{ NewID() string }

// Repository operations preserve absence/conflict/unavailability and context.
// Inserts require atomic unique/head checks; history listing must be complete.
type Repository interface {
	// WithAttributionTransaction serializes event-time binding selection and
	// ownership correction for one program/customer. The callback uses only its
	// bound repository and may retry; it performs no external effects.
	WithAttributionTransaction(context.Context, string, string, func(Repository) error) error
	GetPaymentAttribution(context.Context, string, string) (PaymentAttribution, error)
	// InsertPaymentAttribution enforces unique program/payment ID, including competing writers.
	InsertPaymentAttribution(context.Context, PaymentAttribution) error
	// ListPaymentAttributionsByCustomer returns complete immutable bindings,
	// including future-effective ones that a prospective cutover cannot move.
	ListPaymentAttributionsByCustomer(context.Context, string, string) ([]PaymentAttribution, error)
	GetLinkByCode(context.Context, string) (Link, error)
	InsertLink(context.Context, Link) error
	RetireLink(context.Context, string, time.Time, string, string) error
	ListLinksByPartner(context.Context, string, string) ([]Link, error)
	GetReferralByCustomer(context.Context, string, string) (Referral, error)
	InsertReferral(context.Context, Referral, int64) error
	ListReferralHistory(context.Context, string, string) ([]Referral, error)
	ListReferralsByPartner(context.Context, string, string, int, string) ([]Referral, error)
	// ListRelationshipSnapshots returns a bounded page of lifetime partner/customer
	// membership and each member's complete head/history in one read snapshot.
	ListRelationshipSnapshots(context.Context, string, string, int, string) ([]RelationshipSnapshot, bool, error)
	// ReadAnalyticsSnapshot returns the complete selected scope in one read;
	// capacity, cancellation or any failed member invalidates the entire report.
	ReadAnalyticsSnapshot(context.Context, string, string, AnalyticsQuery) (AnalyticsSnapshot, error)
	// CountClicks is a raw stored-observation count, never a conversion denominator.
	CountClicks(context.Context, string, time.Time, time.Time) (int64, error)
	RecordClick(context.Context, Click, time.Time) error
	GetClick(context.Context, string, string) (Click, error)
	WithVisitTransaction(context.Context, string, func(Repository) error) error
	GetVisitReceipt(context.Context, string, string) (VisitReceipt, error)
	InsertVisitReceipt(context.Context, VisitReceipt) error
	GetVisitDay(context.Context, string, time.Time) (VisitDay, error)
	PutVisitDay(context.Context, VisitDay, int64) error
}
type Service struct {
	repo      Repository
	clock     Clock
	ids       IDGenerator
	window    time.Duration
	analytics *AnalyticsConfig
}
type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// NewService rejects missing commercial timing instead of inventing a window.
func NewService(repo Repository, clock Clock, ids IDGenerator, window time.Duration) (*Service, error) {
	if nilReferralDependency(repo) || nilReferralDependency(ids) {
		return nil, ErrUnavailable
	}
	if window <= 0 || window > 180*24*time.Hour {
		return nil, ErrInvalid
	}
	if clock == nil {
		clock = realClock{}
	}
	if nilReferralDependency(clock) {
		return nil, ErrUnavailable
	}
	return &Service{repo: repo, clock: clock, ids: ids, window: window}, nil
}
func (s *Service) activeLink(ctx context.Context, partnerID string) (Link, error) {
	links, err := s.repo.ListLinksByPartner(ctx, ProgramID, partnerID)
	if err != nil {
		return Link{}, err
	}
	for _, l := range links {
		if l.RetiredAt == nil {
			return l, nil
		}
	}
	return Link{}, ErrNotFound
}

// IssueLink returns one stable active link, including competing-create replay.
func (s *Service) IssueLink(ctx context.Context, p PartnerState) (Link, error) {
	if err := s.ready(ctx); err != nil {
		return Link{}, err
	}
	if !p.CanAcquireReferrals {
		return Link{}, ErrDenied
	}
	if p.PartnerID == "" || p.CustomerID == "" {
		return Link{}, ErrInvalid
	}
	if l, err := s.activeLink(ctx, p.PartnerID); err == nil {
		return l, nil
	} else if !singleReferralCause(err, ErrNotFound) {
		return Link{}, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		code := s.newCode()
		l := Link{ID: "lnk_" + s.ids.NewID(), ProgramID: ProgramID, PartnerID: p.PartnerID, Code: code, CreatedAt: s.clock.Now().UTC()}
		err := s.repo.InsertLink(ctx, l)
		if singleReferralCause(err, ErrAlreadyExists) {
			return s.activeLink(ctx, p.PartnerID)
		}
		if singleReferralCause(err, ErrCodeTaken) {
			continue
		}
		if err != nil {
			return Link{}, err
		}
		return l, nil
	}
	return Link{}, ErrCodeTaken
}

// newCode consumes the entire non-enumerable ID; fixed prefixes retain entropy.
func (s *Service) newCode() string {
	digest := sha256.Sum256([]byte(s.ids.NewID()))
	return base64.RawURLEncoding.EncodeToString(digest[:])[:22]
}
func (s *Service) RetireLink(ctx context.Context, id, reason, actorID string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if actorID == "" || strings.TrimSpace(reason) == "" || len(reason) > 1000 {
		return ErrDenied
	}
	return s.repo.RetireLink(ctx, id, s.clock.Now().UTC(), reason, actorID)
}

// ObserveClick is optional reporting. Callers must keep safe navigation working
// when analytics fail, and apply consent, deduplication and abuse limits first.
func (s *Service) ObserveClick(ctx context.Context, code, userAgent string) (Click, error) {
	if err := s.ready(ctx); err != nil {
		return Click{}, err
	}
	if s.analytics == nil {
		return Click{}, ErrUnavailable
	}
	link, err := s.repo.GetLinkByCode(ctx, code)
	if err != nil {
		return Click{}, err
	}
	if link.RetiredAt != nil {
		return Click{}, ErrCodeRetired
	}
	if link.ProgramID != ProgramID || !analyticsID(link.ID, 128) || !analyticsID(link.Code, 128) || link.Code != code || link.CreatedAt.IsZero() {
		return Click{}, ErrUnavailable
	}
	at := s.clock.Now().UTC()
	id := s.ids.NewID()
	if !analyticsID(id, 252) || at.IsZero() || at.Before(link.CreatedAt) {
		return Click{}, ErrInvalid
	}
	// The legacy unmeasured API no longer stores user-agent strings.
	c := Click{ID: "clk_" + id, LinkID: link.ID, Code: code, OccurredAt: at}
	err = s.repo.WithVisitTransaction(ctx, link.ID, func(tx Repository) error {
		if nilReferralDependency(tx) {
			return ErrUnavailable
		}
		return s.recordObservation(ctx, tx, c)
	})
	if err != nil {
		return Click{}, err
	}
	return c, nil
}
func (s *Service) GetLinkByCode(ctx context.Context, code string) (Link, error) {
	if err := s.ready(ctx); err != nil {
		return Link{}, err
	}
	return s.repo.GetLinkByCode(ctx, code)
}
func (s *Service) ClickCount(ctx context.Context, id string, from, to time.Time) (int64, error) {
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if from.IsZero() || !to.After(from) {
		return 0, ErrInvalid
	}
	return s.repo.CountClicks(ctx, id, from, to)
}

// LockAttribution binds one eligible new signup to its authenticated evidence.
// Repeat delivery of the same signup replays its frozen result. A competing
// link or existing relationship conflicts rather than silently changing owner.
func (s *Service) LockAttribution(ctx context.Context, p PartnerState, l Link, e Eligibility) (Referral, error) {
	if err := s.ready(ctx); err != nil {
		return Referral{}, err
	}
	if e.ReferredCustomer == "" || e.SignupID == "" || e.At.IsZero() {
		return Referral{}, ErrInvalid
	}
	if old, err := s.LookupSignup(ctx, e.ReferredCustomer, e.SignupID, e.Evidence, e.At); err == nil {
		return old, nil
	} else if !singleReferralCause(err, ErrNotFound) {
		return Referral{}, err
	}
	if !validTerms(e.Terms) {
		return Referral{}, ErrInvalid
	}
	if e.IsExistingCustomer || !e.IsIndividual || !p.CanAcquireReferrals {
		return Referral{}, ErrDenied
	}
	if p.CustomerID == e.ReferredCustomer {
		return Referral{}, ErrSelfReferral
	}
	if l.RetiredAt != nil {
		return Referral{}, ErrCodeRetired
	}
	if l.ProgramID != ProgramID || l.PartnerID != p.PartnerID || l.ID != e.Evidence.LinkID || l.Code != e.Evidence.Code || e.Evidence.ProgramID != ProgramID || e.Evidence.Audience != "partner-signup" || l.CreatedAt.After(e.Evidence.IssuedAt) || e.Evidence.IssuedAt.After(e.At) || !e.Evidence.ExpiresAt.After(e.At) || e.Evidence.ExpiresAt.Sub(e.Evidence.IssuedAt) <= 0 || e.Evidence.ExpiresAt.Sub(e.Evidence.IssuedAt) > s.window || !validMeasuredOrigin(e.Evidence.MeasuredClickID, e.Evidence.MeasuredOccurredAt, e.Evidence.IssuedAt) || (e.Evidence.MeasuredOccurredAt != nil && e.Evidence.MeasuredOccurredAt.Before(l.CreatedAt)) {
		return Referral{}, ErrDenied
	}
	digest, err := signupDigest(e.Evidence)
	if err != nil {
		return Referral{}, err
	}
	if _, err := s.repo.GetReferralByCustomer(ctx, ProgramID, e.ReferredCustomer); err == nil {
		return Referral{}, ErrAlreadyReferred
	} else if !singleReferralCause(err, ErrNotFound) {
		return Referral{}, err
	}
	r := Referral{ID: "ref_" + s.ids.NewID(), Revision: 1, ProgramID: ProgramID, PartnerID: p.PartnerID, ReferredCustomer: e.ReferredCustomer, SignupID: e.SignupID, EvidenceDigest: digest, SourceCode: l.Code, SourceKind: "click", LockedAt: e.At.UTC(), PostedAt: s.clock.Now().UTC(), TermsSnapshot: cloneTerms(e.Terms), SourceMeasuredClickID: e.Evidence.MeasuredClickID}
	if e.Evidence.MeasuredOccurredAt != nil {
		at := e.Evidence.MeasuredOccurredAt.UTC()
		r.SourceMeasuredOccurredAt = &at
	}
	if err := s.repo.InsertReferral(ctx, r, 0); err != nil {
		if singleReferralCause(err, ErrStaleWrite) || singleReferralCause(err, ErrAlreadyExists) {
			return s.LookupSignup(ctx, e.ReferredCustomer, e.SignupID, e.Evidence, e.At)
		}
		return Referral{}, err
	}
	return r, nil
}

func validTerms(t TermsSnapshot) bool {
	return t.RateBasisPoints >= 0 && t.RateBasisPoints <= 10000 && t.HoldDurationDays >= 0 && t.HoldDurationDays <= 28 && currencyPattern.MatchString(t.Currency) && t.CurrencyExponent >= 0 && t.CurrencyExponent <= 3 && t.TermsVersion != "" && len(t.PolicyVersionIDs) > 0 && t.EligiblePlanIDs != nil
}
func cloneTerms(t TermsSnapshot) TermsSnapshot {
	t.PolicyVersionIDs = append([]string(nil), t.PolicyVersionIDs...)
	if t.EligiblePlanIDs != nil {
		t.EligiblePlanIDs = append([]string{}, t.EligiblePlanIDs...)
	}
	if t.RecurrenceEndsAt != nil {
		v := *t.RecurrenceEndsAt
		t.RecurrenceEndsAt = &v
	}
	return t
}
func (s *Service) GetReferralForCustomer(ctx context.Context, customer string) (Referral, error) {
	if err := s.ready(ctx); err != nil {
		return Referral{}, err
	}
	return s.repo.GetReferralByCustomer(ctx, ProgramID, customer)
}
func (s *Service) History(ctx context.Context, customer string) ([]Referral, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	return s.repo.ListReferralHistory(ctx, ProgramID, customer)
}
func (s *Service) ListByPartner(ctx context.Context, partnerID string, limit int, after string) ([]Referral, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	return s.repo.ListReferralsByPartner(ctx, ProgramID, partnerID, limit, after)
}
func (s *Service) AttributionWindow() time.Duration { return s.window }

// NormalizeCode trims whitespace without changing the case-sensitive code.
func NormalizeCode(code string) string { return strings.TrimSpace(code) }
