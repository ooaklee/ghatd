// Package partnerprogram is the partner/program owning service for the
// Partners (affiliate) program. It owns enrollment and partner status, the
// immutable published commercial-policy versions and their precedence
// resolution, and versioned payout destinations.
//
// Business rules and
// validation live here; persistence mechanics live in the repository. The
// service depends on a narrow typed repository port, never on a datastore
// driver or another domain's repository. Actor identity arrives from verified
// transport context and is never accepted from public bodies.
package partnerprogram

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// Canonical errors. Absence, conflict, validation and unavailable outcomes are
// always distinct; adapters must preserve native causes internally.
var (
	ErrNotFound        = errors.New("partnerprogram/not-found")
	ErrAlreadyEnrolled = errors.New("partnerprogram/already-enrolled")
	ErrAlreadyExists   = errors.New("partnerprogram/already-exists")
	ErrStaleWrite      = errors.New("partnerprogram/stale-write")
	ErrInvalid         = errors.New("partnerprogram/invalid")
	ErrDenied          = errors.New("partnerprogram/denied")
	ErrUnavailable     = errors.New("partnerprogram/unavailable")
	ErrUncertain       = errors.New("partnerprogram/uncertain")
	ErrAmbiguousPolicy = errors.New("partnerprogram/ambiguous-policy")
)

const (
	// ProgramID is the single v1 program identity. One program/payout currency
	// for the initial single-program contract.
	ProgramID = "partners-v1"

	StatusActive    = "active"
	StatusPending   = "pending" // enrollment approval required
	StatusSuspended = "suspended"
	StatusClosed    = "closed"

	MinRateBasisPoints = 0
	MaxRateBasisPoints = 10000
	// DefaultMaxHoldDays limits new policies when Config.MaxHoldDays is omitted.
	DefaultMaxHoldDays = 30
	// MaxSupportedHoldDays is the stable financial-history safety bound. A
	// programme may admit a smaller maximum without invalidating frozen terms.
	MaxSupportedHoldDays = 365
	// MaxHoldDuration bounds retained holds and protects duration conversion.
	// It is not the programme's configurable limit for new policy publication.
	MaxHoldDuration = MaxSupportedHoldDays * 24 * time.Hour

	DestinationMethodPayPal = "paypal"
)

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

var paypalEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Clock is injectable so tests are deterministic.
type Clock interface {
	// Now returns the current time from the injectable Clock, enabling
	// deterministic tests; implementations return UTC wall-clock or
	// wrapped-function time.
	Now() time.Time
}

// RealClock is the production Clock returning wall-clock time in UTC.
type RealClock struct{}

// Now returns the current wall-clock time in UTC.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// ClockFunc adapts an ordinary function to Clock, mainly for tests and simple
// composition.
type ClockFunc func() time.Time

// Now calls the wrapped function, preserving Clock semantics.
func (f ClockFunc) Now() time.Time { return f() }

// IDGenerator produces non-enumerable identifiers.
type IDGenerator interface {
	// NewID returns a fresh non-enumerable identifier produced by the IDGenerator.
	NewID() string
}

// Partner is one customer's participation in the program. Status transitions
// carry reason and audit timestamps; suspension separately gates acquiring new
// referrals, accruing on existing ones, and requesting payouts.
type Partner struct {
	ID                       string     `json:"id" bson:"id"`
	ProgramID                string     `json:"program_id" bson:"program_id"`
	CustomerID               string     `json:"customer_id" bson:"customer_id"`
	Status                   string     `json:"status" bson:"status"`
	AcceptedTermsVersion     string     `json:"accepted_terms_version" bson:"accepted_terms_version"`
	EnrolledAt               time.Time  `json:"enrolled_at" bson:"enrolled_at"`
	StatusChangedAt          time.Time  `json:"status_changed_at" bson:"status_changed_at"`
	StatusReason             string     `json:"status_reason,omitempty" bson:"status_reason,omitempty"`
	CanAcquireReferrals      bool       `json:"can_acquire_referrals" bson:"can_acquire_referrals"`
	CanAccrue                bool       `json:"can_accrue" bson:"can_accrue"`
	CanRequestPayouts        bool       `json:"can_request_payouts" bson:"can_request_payouts"`
	Revision                 int64      `json:"revision" bson:"revision"`
	PayoutDestinationVersion int64      `json:"payout_destination_version" bson:"payout_destination_version"`
	PayPalEmail              string     `json:"paypal_email,omitempty" bson:"paypal_email,omitempty"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedBy                string     `json:"updated_by,omitempty" bson:"updated_by,omitempty"`
}

// PolicyRuleSource names which rule won for each resolved field.
type PolicyRuleSource struct {
	Kind string `json:"kind"` // global|group|individual
	ID   string `json:"id"`
}

// EffectiveTerms is the complete immutable snapshot resolved for one partner
// at a point in time. Referrals freeze this snapshot at attribution.
type EffectiveTerms struct {
	ProgramID        string           `json:"program_id"`
	PartnerID        string           `json:"partner_id"`
	RateBasisPoints  int              `json:"rate_basis_points"`
	HoldDuration     time.Duration    `json:"hold_duration"`
	Currency         string           `json:"currency"`
	CurrencyExponent int              `json:"currency_exponent"`
	EligiblePlanIDs  []string         `json:"eligible_plan_ids"`
	RecurrenceEndsAt *time.Time       `json:"recurrence_ends_at,omitempty"`
	ResolvedAt       time.Time        `json:"resolved_at"`
	RateSource       PolicyRuleSource `json:"rate_source"`
	HoldSource       PolicyRuleSource `json:"hold_source"`
	TermsVersion     string           `json:"terms_version"`
	PolicyVersionIDs []string         `json:"policy_version_ids"`
}

// PolicyVersion is one published, immutable commercial-policy version. Fields
// with an explicit -1/empty value inherit the global default at resolution
// time; the published record itself is never edited.
type PolicyVersion struct {
	ID              string     `json:"id" bson:"id"`
	Revision        int64      `json:"revision" bson:"revision"`
	ProgramID       string     `json:"program_id" bson:"program_id"`
	Scope           string     `json:"scope" bson:"scope"` // global|group|individual
	GroupID         string     `json:"group_id,omitempty" bson:"group_id,omitempty"`
	PartnerCustomer string     `json:"partner_customer,omitempty" bson:"partner_customer,omitempty"`
	Priority        int        `json:"priority" bson:"priority"`
	RateBasisPoints int        `json:"rate_basis_points" bson:"rate_basis_points"`
	HoldDays        int        `json:"hold_days" bson:"hold_days"`
	Currency        string     `json:"currency" bson:"currency"`
	EligiblePlanIDs []string   `json:"eligible_plan_ids" bson:"eligible_plan_ids"`
	EffectiveFrom   time.Time  `json:"effective_from" bson:"effective_from"`
	EffectiveTo     *time.Time `json:"effective_to,omitempty" bson:"effective_to,omitempty"`
	PublishedAt     time.Time  `json:"published_at" bson:"published_at"`
	PublishedBy     string     `json:"published_by" bson:"published_by"`
	TermsVersion    string     `json:"terms_version" bson:"terms_version"`
	RecurringMonths *int       `json:"recurring_months,omitempty" bson:"recurring_months,omitempty"`
}

// EnrollRequest binds the verified customer. Repeated enrollment is idempotent.
type EnrollRequest struct {
	CustomerID           string
	AcceptedTermsVersion string
}

// StatusChangeRequest is the privileged partner-status transition command.
type StatusChangeRequest struct {
	PartnerID           string
	ExpectedRevision    int64
	NewStatus           string
	Reason              string
	ActorID             string `json:"-"` // verified admin identity
	CanAcquireReferrals *bool
	CanAccrue           *bool
	CanRequestPayouts   *bool
}

// PolicyDraft is validated before publication; rejected values never replace
// the active version.
type PolicyDraft struct {
	Scope           string // global|group|individual
	GroupID         string
	PartnerCustomer string
	Priority        int
	RateBasisPoints int
	HoldDays        int
	Currency        string
	EligiblePlanIDs []string
	EffectiveFrom   time.Time
	EffectiveTo     *time.Time
	TermsVersion    string
	RecurringMonths *int
}

// PublishPolicyRequest carries the actor for the audit trail.
type PublishPolicyRequest struct {
	Draft   PolicyDraft
	ActorID string `json:"-"`
	// ExpectedRevision is zero for the first version of this exact scope;
	// the adapter advances its scope head atomically with the immutable version.
	ExpectedRevision int64
}

// DestinationRequest updates the versioned payout destination.
type DestinationRequest struct {
	CustomerID  string
	Method      string
	PayPalEmail string
	// ExpectedVersion is the active destination version observed by the caller.
	ExpectedVersion int64
}

// Config is explicit host commercial configuration. Construction never invents
// an attribution window, currency, hold or accepted terms. Approval and live
// enablement are host responsibilities; fixture values are not launch approval.
type Config struct {
	EnrollmentApprovalRequired bool
	DefaultRateBasisPoints     int
	DefaultHoldDays            int
	// MaxHoldDays bounds new default and published holds, in elapsed 24-hour
	// days. Zero selects DefaultMaxHoldDays; values above MaxSupportedHoldDays
	// are rejected. Changing this bound does not rewrite retained policies.
	MaxHoldDays int
	// AllowZeroHold must explicitly permit a zero-day policy.
	AllowZeroHold     bool
	Currency          string
	CurrencyExponent  int
	TermsVersion      string
	AttributionWindow time.Duration
	EligiblePlanIDs   []string
}

// validate enforces trusted configuration bounds: rate limits, hold days (zero
// only when explicitly allowed), an approved three-letter currency with
// exponent 0..3, terms version, a positive attribution window of at most 180
// days, and explicit unique eligible plan IDs. Failures return wrapped
// ErrInvalid and clamp nothing.
func (c Config) validate() error {
	if c.DefaultRateBasisPoints < MinRateBasisPoints || c.DefaultRateBasisPoints > MaxRateBasisPoints {
		return fmt.Errorf("%w: default rate out of bounds", ErrInvalid)
	}
	if c.MaxHoldDays < 0 || c.MaxHoldDays > MaxSupportedHoldDays {
		return fmt.Errorf("%w: maximum hold out of bounds", ErrInvalid)
	}
	if c.DefaultHoldDays < 0 || c.DefaultHoldDays > c.MaximumHoldDays() || (c.DefaultHoldDays == 0 && !c.AllowZeroHold) {
		return fmt.Errorf("%w: default hold exceeds maximum", ErrInvalid)
	}
	if !currencyPattern.MatchString(c.Currency) || c.CurrencyExponent < 0 || c.CurrencyExponent > 3 {
		return fmt.Errorf("%w: one approved three-letter currency required", ErrInvalid)
	}
	if c.TermsVersion == "" {
		return fmt.Errorf("%w: terms version required", ErrInvalid)
	}
	if c.AttributionWindow <= 0 || c.AttributionWindow > 180*24*time.Hour {
		return fmt.Errorf("%w: attribution window must be positive and at most 180 days", ErrInvalid)
	}
	if len(c.EligiblePlanIDs) == 0 || !validIDs(c.EligiblePlanIDs) {
		return fmt.Errorf("%w: explicit eligible plan IDs required", ErrInvalid)
	}
	return nil
}

// Validate exposes configuration validation so hosts can reject invalid
// partner settings at startup, before any store is opened.
func (c Config) Validate() error { return c.validate() }

// MaximumHoldDays resolves the omitted maximum without mutating configuration.
// Call Validate before using the result from unvalidated host configuration.
func (c Config) MaximumHoldDays() int {
	if c.MaxHoldDays == 0 {
		return DefaultMaxHoldDays
	}
	return c.MaxHoldDays
}

// Repository is the narrow typed persistence port. Implementations own all
// datastore I/O; they never choose rates or invent eligibility rules.
type Repository interface {
	// GetPartnerByID reads the Partner for the identified partner within the typed
	// persistence port, returning stored data unchanged.
	GetPartnerByID(ctx context.Context, id string) (Partner, error)
	// GetPartnerByCustomer reads the Partner linking the program and customer
	// identifiers, a datastore lookup owned by the Repository.
	GetPartnerByCustomer(ctx context.Context, programID, customerID string) (Partner, error)
	// InsertPartner persists a new Partner, performing the datastore write owned by
	// the narrow typed persistence port.
	InsertPartner(ctx context.Context, p Partner) error
	// ReplacePartner persists the Partner only when its revision matches
	// expectedRevision, returning the stored result of the guarded replacement.
	ReplacePartner(ctx context.Context, p Partner, expectedRevision int64) (Partner, error)
	// ListPolicyVersions returns all PolicyVersion records for the program; the
	// Service implementation exposes this immutable published history for admin
	// reads.
	ListPolicyVersions(ctx context.Context, programID string) ([]PolicyVersion, error)
	// InsertPolicyVersion atomically checks and advances the exact scope head
	// and inserts the immutable revision. No partial head/history writes.
	InsertPolicyVersion(ctx context.Context, v PolicyVersion, expectedRevision int64) error
	// GetDestination reads the payout Destination for the identified customer, a
	// datastore lookup within the Repository port.
	GetDestination(ctx context.Context, customerID string) (Destination, error)
	// InsertDestination atomically compares the active version and appends the
	// next one. Older destination records are never overwritten.
	InsertDestination(ctx context.Context, d Destination, expectedVersion int64) error
}

// Destination is one version of a customer payout destination. Claims snapshot
// the active version; later edits only affect future claims.
type Destination struct {
	ID         string    `json:"id" bson:"id"`
	CustomerID string    `json:"customer_id" bson:"customer_id"`
	Method     string    `json:"method" bson:"method"`
	Email      string    `json:"email" bson:"email"`
	Version    int64     `json:"version" bson:"version"`
	CreatedAt  time.Time `json:"created_at" bson:"created_at"`
}

// Service owns enrollment, status, policy publication and resolution.
type Service struct {
	repo   Repository
	clock  Clock
	ids    IDGenerator
	config Config
}

// NewService validates wiring; missing financial configuration fails closed.
func NewService(repo Repository, clock Clock, ids IDGenerator, config Config) (*Service, error) {
	if nilProgramDependency(repo) {
		return nil, ErrUnavailable
	}
	if clock == nil {
		clock = RealClock{}
	}
	if nilProgramDependency(clock) || nilProgramDependency(ids) {
		return nil, ErrUnavailable
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	config.EligiblePlanIDs = append([]string(nil), config.EligiblePlanIDs...)
	return &Service{repo: repo, clock: clock, ids: ids, config: config}, nil
}

// Config exposes the effective configuration (read-only copy).
func (s *Service) Config() Config {
	c := s.config
	c.EligiblePlanIDs = append([]string(nil), c.EligiblePlanIDs...)
	return c
}

// Clock exposes the injected clock for composing services.
func (s *Service) Clock() Clock { return s.clock }

// Enroll registers one customer per program, idempotently. An invitation to
// join is not an earnings entitlement: pending partners cannot acquire
// referrals until approved.
func (s *Service) Enroll(ctx context.Context, req EnrollRequest) (Partner, error) {
	if err := s.ready(ctx); err != nil {
		return Partner{}, err
	}
	if req.CustomerID == "" {
		return Partner{}, fmt.Errorf("%w: customer required", ErrInvalid)
	}
	if req.AcceptedTermsVersion == "" || req.AcceptedTermsVersion != s.config.TermsVersion {
		return Partner{}, fmt.Errorf("%w: current program terms must be accepted", ErrDenied)
	}
	if existing, err := s.repo.GetPartnerByCustomer(ctx, ProgramID, req.CustomerID); err == nil {
		return existing, nil
	} else if !singleProgramCause(err, ErrNotFound) {
		return Partner{}, err
	}
	status := StatusActive
	if s.config.EnrollmentApprovalRequired {
		status = StatusPending
	}
	now := s.clock.Now()
	p := Partner{
		ID:                   "prt_" + s.ids.NewID(),
		ProgramID:            ProgramID,
		CustomerID:           req.CustomerID,
		Status:               status,
		AcceptedTermsVersion: req.AcceptedTermsVersion,
		EnrolledAt:           now,
		StatusChangedAt:      now,
		CanAcquireReferrals:  status == StatusActive,
		CanAccrue:            status == StatusActive,
		CanRequestPayouts:    status == StatusActive,
		Revision:             1,
	}
	if err := s.repo.InsertPartner(ctx, p); err != nil {
		if singleProgramCause(err, ErrAlreadyEnrolled) {
			return s.repo.GetPartnerByCustomer(ctx, ProgramID, req.CustomerID)
		}
		return Partner{}, err
	}
	return p, nil
}

// GetPartnerForCustomer returns the caller's own participation record.
func (s *Service) GetPartnerForCustomer(ctx context.Context, customerID string) (Partner, error) {
	if err := s.ready(ctx); err != nil {
		return Partner{}, err
	}
	if customerID == "" {
		return Partner{}, fmt.Errorf("%w: customer required", ErrInvalid)
	}
	return s.repo.GetPartnerByCustomer(ctx, ProgramID, customerID)
}

// GetPartner returns one partner by ID (admin or ownership-checked reads).
func (s *Service) GetPartner(ctx context.Context, id string) (Partner, error) {
	if err := s.ready(ctx); err != nil {
		return Partner{}, err
	}
	return s.repo.GetPartnerByID(ctx, id)
}

// ChangeStatus applies the privileged lifecycle transition with revision
// precondition and audit attribution.
func (s *Service) ChangeStatus(ctx context.Context, req StatusChangeRequest) (Partner, error) {
	if err := s.ready(ctx); err != nil {
		return Partner{}, err
	}
	if req.ActorID == "" {
		return Partner{}, fmt.Errorf("%w: actor required", ErrDenied)
	}
	switch req.NewStatus {
	case StatusActive, StatusSuspended, StatusClosed, StatusPending:
	default:
		return Partner{}, fmt.Errorf("%w: unknown status", ErrInvalid)
	}
	if strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 1000 || req.ExpectedRevision < 1 || req.ExpectedRevision == math.MaxInt64 {
		return Partner{}, fmt.Errorf("%w: reason required", ErrInvalid)
	}
	p, err := s.repo.GetPartnerByID(ctx, req.PartnerID)
	if err != nil {
		return Partner{}, err
	}
	if p.Revision != req.ExpectedRevision {
		return Partner{}, ErrStaleWrite
	}
	now := s.clock.Now()
	p.Status = req.NewStatus
	p.StatusChangedAt = now
	p.StatusReason = req.Reason
	active := req.NewStatus == StatusActive
	p.CanAcquireReferrals = active
	p.CanAccrue = active
	p.CanRequestPayouts = active
	if req.CanAcquireReferrals != nil {
		p.CanAcquireReferrals = *req.CanAcquireReferrals
	}
	if req.CanAccrue != nil {
		p.CanAccrue = *req.CanAccrue
	}
	if req.CanRequestPayouts != nil {
		p.CanRequestPayouts = *req.CanRequestPayouts
	}
	if p.Status == StatusPending || p.Status == StatusClosed {
		if p.CanAcquireReferrals || p.CanAccrue {
			return Partner{}, fmt.Errorf("%w: pending or closed partner cannot acquire or accrue", ErrInvalid)
		}
	}
	p.UpdatedAt = &now
	p.UpdatedBy = req.ActorID
	p.Revision = req.ExpectedRevision + 1
	return s.repo.ReplacePartner(ctx, p, req.ExpectedRevision)
}

// ValidatePolicy checks a draft before publication. Typed validation errors
// leave the previous version active; nothing is silently clamped.
func (s *Service) ValidatePolicy(d PolicyDraft) error {
	switch d.Scope {
	case "global":
		if d.GroupID != "" || d.PartnerCustomer != "" {
			return fmt.Errorf("%w: unexpected global identity", ErrInvalid)
		}
	case "group":
		if d.GroupID == "" || d.PartnerCustomer != "" {
			return fmt.Errorf("%w: group identity required", ErrInvalid)
		}
	case "individual":
		if d.PartnerCustomer == "" || d.GroupID != "" {
			return fmt.Errorf("%w: customer identity required", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: invalid policy scope", ErrInvalid)
	}
	min := 0
	if d.Scope != "global" {
		min = -1
	} // explicit inheritance sentinel
	if d.RateBasisPoints < min || d.RateBasisPoints > 10000 {
		return fmt.Errorf("%w: rate outside 0..10000 or inheritance", ErrInvalid)
	}
	// Compare days before duration conversion; a huge integer must not overflow.
	if d.HoldDays < min || d.HoldDays > s.config.MaximumHoldDays() || (d.HoldDays == 0 && !s.config.AllowZeroHold) {
		return fmt.Errorf("%w: hold outside supported range", ErrInvalid)
	}
	if (d.Scope == "global" || d.Currency != "") && d.Currency != s.config.Currency {
		return fmt.Errorf("%w: unsupported currency", ErrInvalid)
	}
	if d.Scope == "global" && (len(d.EligiblePlanIDs) == 0 || strings.TrimSpace(d.TermsVersion) == "") {
		return fmt.Errorf("%w: global plans and terms required", ErrInvalid)
	}
	if !validIDs(d.EligiblePlanIDs) || len(d.TermsVersion) > 128 {
		return fmt.Errorf("%w: invalid plan or terms identity", ErrInvalid)
	}
	if d.EffectiveFrom.IsZero() || (d.EffectiveTo != nil && !d.EffectiveTo.After(d.EffectiveFrom)) {
		return fmt.Errorf("%w: invalid effective interval", ErrInvalid)
	}
	if d.RecurringMonths != nil && (*d.RecurringMonths < 0 || *d.RecurringMonths > 1200) {
		return fmt.Errorf("%w: invalid recurring duration", ErrInvalid)
	}
	return nil
}

// validIDs accepts a non-empty list of trimmed, non-empty IDs of at most 128
// bytes with no duplicates.
func validIDs(ids []string) bool {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 128 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// PublishPolicy validates then publishes one immutable version. Zero-bonus and
// zero-hold values must arrive explicitly; they are supported inputs, not
// clamps. A scope head compare-and-swap prevents two operators publishing from
// one observed revision. History is immutable and event-time resolution explicit.
func (s *Service) PublishPolicy(ctx context.Context, req PublishPolicyRequest) (PolicyVersion, error) {
	if err := s.ready(ctx); err != nil {
		return PolicyVersion{}, err
	}
	if req.ActorID == "" {
		return PolicyVersion{}, fmt.Errorf("%w: actor required", ErrDenied)
	}
	if req.ExpectedRevision < 0 || req.ExpectedRevision == math.MaxInt64 {
		return PolicyVersion{}, ErrInvalid
	}
	if err := s.ValidatePolicy(req.Draft); err != nil {
		return PolicyVersion{}, err
	}
	now := s.clock.Now()
	v := PolicyVersion{
		ID:              "pol_" + s.ids.NewID(),
		Revision:        req.ExpectedRevision + 1,
		ProgramID:       ProgramID,
		Scope:           req.Draft.Scope,
		GroupID:         req.Draft.GroupID,
		PartnerCustomer: req.Draft.PartnerCustomer,
		Priority:        req.Draft.Priority,
		RateBasisPoints: req.Draft.RateBasisPoints,
		HoldDays:        req.Draft.HoldDays,
		Currency:        req.Draft.Currency,
		EligiblePlanIDs: cloneIDs(req.Draft.EligiblePlanIDs),
		EffectiveFrom:   req.Draft.EffectiveFrom,
		EffectiveTo:     cloneTime(req.Draft.EffectiveTo),
		PublishedAt:     now,
		PublishedBy:     req.ActorID,
		TermsVersion:    req.Draft.TermsVersion,
		RecurringMonths: cloneInt(req.Draft.RecurringMonths),
	}
	if err := s.repo.InsertPolicyVersion(ctx, v, req.ExpectedRevision); err != nil {
		return PolicyVersion{}, err
	}
	return v, nil
}

// ListPolicyVersions exposes the immutable published history for admin reads.
func (s *Service) ListPolicyVersions(ctx context.Context) ([]PolicyVersion, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	return s.repo.ListPolicyVersions(ctx, ProgramID)
}

// ResolveTerms computes the complete effective-policy snapshot for a partner
// at a given instant. Precedence: explicit individual rule → matching group
// rule with explicit configured priority → global default. Ambiguous
// equal-priority group matches are rejected, never silently resolved. Group
// membership comes from the caller as verified group IDs (the host composes
// the group owning service); this service never reads group repositories.
func (s *Service) ResolveTerms(ctx context.Context, partner Partner, groupIDs []string, at time.Time) (EffectiveTerms, error) {
	return s.resolveTerms(ctx, partner, groupIDs, at, at)
}

// ResolveReferralTerms selects current policy at policyAt while anchoring a
// finite recurring entitlement to the owning signup time. A reassignment must
// not restart the recurring window. The caller also retains any earlier frozen
// end condition when changing an existing referral's prospective terms.
func (s *Service) ResolveReferralTerms(ctx context.Context, partner Partner, groupIDs []string, policyAt, signupAt time.Time) (EffectiveTerms, error) {
	if signupAt.IsZero() || signupAt.After(policyAt) {
		return EffectiveTerms{}, ErrInvalid
	}
	return s.resolveTerms(ctx, partner, groupIDs, policyAt, signupAt)
}

// resolveTerms computes the effective snapshot at an event time by selecting,
// per scope, the latest applicable policy version, then chaining global →
// highest-priority matching group → individual, with equal-priority group ties
// rejected as ambiguous. Explicit values override; a recurring window is
// anchored to recurrenceFrom so reassignment cannot restart it.
func (s *Service) resolveTerms(ctx context.Context, partner Partner, groupIDs []string, at, recurrenceFrom time.Time) (EffectiveTerms, error) {
	if err := s.ready(ctx); err != nil {
		return EffectiveTerms{}, err
	}
	if partner.ProgramID != ProgramID || partner.ID == "" || at.IsZero() {
		return EffectiveTerms{}, ErrInvalid
	}
	versions, err := s.repo.ListPolicyVersions(ctx, ProgramID)
	if err != nil {
		return EffectiveTerms{}, err
	}
	// Select one event-time version per exact scope before comparing group priorities.
	byScope := map[string]PolicyVersion{}
	for _, v := range versions {
		if v.ProgramID != ProgramID || v.PublishedAt.After(at) || v.EffectiveFrom.After(at) || (v.EffectiveTo != nil && !v.EffectiveTo.After(at)) {
			continue
		}
		key := v.Scope + ":" + v.GroupID + ":" + v.PartnerCustomer
		prev, exists := byScope[key]
		if !exists || v.EffectiveFrom.After(prev.EffectiveFrom) || (v.EffectiveFrom.Equal(prev.EffectiveFrom) && v.Revision > prev.Revision) {
			byScope[key] = v
		}
	}
	global, ok := byScope["global::"]
	if !ok {
		return EffectiveTerms{}, fmt.Errorf("%w: no active global policy", ErrUnavailable)
	}
	chain := []PolicyVersion{global}
	var best *PolicyVersion
	seen := map[string]bool{}
	for _, id := range groupIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if v, exists := byScope["group:"+id+":"]; exists {
			if best == nil || v.Priority > best.Priority {
				c := v
				best = &c
			}
		}
	}
	if best != nil {
		for id := range seen {
			if v, exists := byScope["group:"+id+":"]; exists && v.Priority == best.Priority && v.GroupID != best.GroupID {
				return EffectiveTerms{}, ErrAmbiguousPolicy
			}
		}
		chain = append(chain, *best)
	}
	if v, exists := byScope["individual::"+partner.CustomerID]; exists {
		chain = append(chain, v)
	}
	terms := EffectiveTerms{ProgramID: ProgramID, PartnerID: partner.ID, Currency: s.config.Currency, CurrencyExponent: s.config.CurrencyExponent, ResolvedAt: at}
	var recurring *int
	for _, v := range chain {
		terms.PolicyVersionIDs = append(terms.PolicyVersionIDs, v.ID)
		if v.RateBasisPoints >= 0 {
			terms.RateBasisPoints = v.RateBasisPoints
			terms.RateSource = PolicyRuleSource{Kind: v.Scope, ID: v.ID}
		}
		if v.HoldDays >= 0 {
			terms.HoldDuration = time.Duration(v.HoldDays) * 24 * time.Hour
			terms.HoldSource = PolicyRuleSource{Kind: v.Scope, ID: v.ID}
		}
		if v.EligiblePlanIDs != nil {
			terms.EligiblePlanIDs = cloneIDs(v.EligiblePlanIDs)
		}
		if v.TermsVersion != "" {
			terms.TermsVersion = v.TermsVersion
		}
		if v.RecurringMonths != nil {
			recurring = cloneInt(v.RecurringMonths)
		}
	}
	if recurring != nil {
		until := recurrenceFrom.AddDate(0, *recurring, 0)
		terms.RecurrenceEndsAt = &until
	}
	return terms, nil
}

// cloneIDs preserves nil (inherit) versus an empty list (explicit exclusion).
func cloneIDs(ids []string) []string {
	if ids == nil {
		return nil
	}
	return append([]string{}, ids...)
}

// cloneTime returns a defensive copy of an optional time, preserving nil.
func cloneTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// cloneInt returns a defensive copy of an optional int, preserving nil.
func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// UpdatePayoutDestination records a new destination version. The PayPal email
// is validated for shape only; it is not proof of account ownership.
func (s *Service) UpdatePayoutDestination(ctx context.Context, req DestinationRequest) (Destination, error) {
	if err := s.ready(ctx); err != nil {
		return Destination{}, err
	}
	if req.CustomerID == "" {
		return Destination{}, fmt.Errorf("%w: customer required", ErrInvalid)
	}
	if req.Method != DestinationMethodPayPal {
		return Destination{}, fmt.Errorf("%w: paypal is the first supported destination", ErrInvalid)
	}
	email := strings.TrimSpace(strings.ToLower(req.PayPalEmail))
	if !paypalEmailPattern.MatchString(email) || len(email) > 254 {
		return Destination{}, fmt.Errorf("%w: payout email invalid", ErrInvalid)
	}
	if req.ExpectedVersion < 0 || req.ExpectedVersion == math.MaxInt64 {
		return Destination{}, ErrInvalid
	}
	version := req.ExpectedVersion + 1
	d := Destination{
		ID:         "dst_" + s.ids.NewID(),
		CustomerID: req.CustomerID,
		Method:     req.Method,
		Email:      email,
		Version:    version,
		CreatedAt:  s.clock.Now(),
	}
	if err := s.repo.InsertDestination(ctx, d, req.ExpectedVersion); err != nil {
		return Destination{}, err
	}
	return d, nil
}

// GetPayoutDestination returns the active destination version.
func (s *Service) GetPayoutDestination(ctx context.Context, customerID string) (Destination, error) {
	if err := s.ready(ctx); err != nil {
		return Destination{}, err
	}
	return s.repo.GetDestination(ctx, customerID)
}
