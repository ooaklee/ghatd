// Package partnermanager orchestrates authorized partner customer, operator and
// background-worker use cases through owning services. It has no driver I/O.
package partnermanager

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

var (
	ErrInvalid     = errors.New("partnermanager/invalid")
	ErrDenied      = errors.New("partnermanager/denied")
	ErrNotFound    = errors.New("partnermanager/not-found")
	ErrNotPartner  = errors.New("partnermanager/not-enrolled")
	ErrUnavailable = errors.New("partnermanager/unavailable")
)

const (
	CapabilitySelf                = "partner.self"
	CapabilityEnroll              = "partner.enroll"
	CapabilityClaims              = "partner.claims.create"
	CapabilityCreateClaimOnBehalf = "partner.admin.claims.create"
	CapabilityPolicy              = "partner.admin.policy"
	CapabilityReporting           = "partner.admin.reporting"
	CapabilityOperations          = "partner.admin.operations"
	CapabilityAttribution         = "partner.admin.attribution"
	CapabilityProcessing          = "partner.admin.claims.process"
	CapabilityRecordPayment       = "partner.admin.payments.record"
	CapabilityAmendPayment        = "partner.admin.payments.amend"
	CapabilityReturnPayment       = "partner.admin.payments.return"
	CapabilityRevenueWorker       = "partner.worker.revenue"
	CapabilitySignupWorker        = "partner.worker.signup"
	CapabilityMaturityWorker      = "partner.worker.maturity"
)

// Principal is current identity-owning output. A transport session is not a
// substitute for active, verified individual customer eligibility.
type Principal struct {
	ID                                                string
	Active, EmailVerified, Individual, RegionEligible bool
}

// SignupFact is immutable creation evidence from the identity owning service.
// CreatedAt, NewAccount and evidence must never be read from a browser body.
type SignupFact struct {
	ID, CustomerID, AttributionEvidence string `json:"-"`
	CreatedAt                           time.Time
	NewAccount, Individual              bool
}

// Identity is the owning identity capability Manager depends on: current
// principal eligibility and immutable signup facts, both keyed by customer ID.
type Identity interface {
	// GetPartnerPrincipal returns the current principal eligibility for the
	// customer ID, including active, individual, email-verified and region-eligible
	// status resolved from owning services.
	GetPartnerPrincipal(context.Context, string) (Principal, error)
	// GetSignupFact returns the immutable signup capture for the customer ID,
	// including creation time and attribution evidence, from the owning identity
	// service.
	GetSignupFact(context.Context, string) (SignupFact, error)
}

// Authority checks current scoped permission for every use case, including
// replay. A generic administrator flag does not confer manual payout authority.
type Authority interface {
	// CheckPartners checks the actor's current scoped permission for the given
	// partners use case, including replay; a generic administrator flag does not
	// confer authority.
	CheckPartners(context.Context, string, string, string) error
}

// Groups resolves a customer's partner group memberships, used only for terms
// resolution.
type Groups interface {
	// PartnerGroupIDs returns the customer's partner group memberships, used only
	// for terms resolution.
	PartnerGroupIDs(context.Context, string) ([]string, error)
}

// Clock supplies the current time, allowing hosts to inject deterministic
// clocks.
type Clock interface {
	// Now returns the current time, allowing hosts to inject deterministic clocks.
	Now() time.Time
}

// Controls are independent host admission switches. ManualRecording admits new
// manual payment handling (entering processing), never attestation or recovery
// of an external transfer already attempted. Pauses preserve current-authority
// reads, financial receipts and existing obligations.
type Controls struct{ Enrollment, Attribution, Accrual, Claims, ManualRecording bool }

// ProgramService is the program-owning capability: partner enrollment, status,
// policy publication and versions, effective/referral terms resolution, and
// payout destination reads and updates. Manager treats it as the authority for
// program state, not a transport.
type ProgramService interface {
	// Config returns the program-owning capability's current partner program
	// configuration.
	Config() partnerprogram.Config
	// Enroll enrolls a partner in the program per the request, returning the
	// resulting partner from the program-owning service.
	Enroll(context.Context, partnerprogram.EnrollRequest) (partnerprogram.Partner, error)
	// GetPartnerForCustomer returns the program partner record associated with the
	// customer ID from the program-owning service.
	GetPartnerForCustomer(context.Context, string) (partnerprogram.Partner, error)
	// GetPartner reads the partner identified by the string ID for ProgramService,
	// the authority on program state, returning the stored partnerprogram.Partner
	// record.
	GetPartner(context.Context, string) (partnerprogram.Partner, error)
	// ChangeStatus applies a StatusChangeRequest transition to a partner and
	// returns the resulting updated Partner, as the program-owning capability's
	// status mutation.
	ChangeStatus(context.Context, partnerprogram.StatusChangeRequest) (partnerprogram.Partner, error)
	// PublishPolicy publishes a new policy version from the PublishPolicyRequest
	// and returns the created PolicyVersion, exercising ProgramService's policy
	// publication ownership.
	PublishPolicy(context.Context, partnerprogram.PublishPolicyRequest) (partnerprogram.PolicyVersion, error)
	// ListPolicyVersions returns all published PolicyVersion records for the
	// program, exposing the immutable policy history owned by ProgramService.
	ListPolicyVersions(context.Context) ([]partnerprogram.PolicyVersion, error)
	// ResolveTerms computes EffectiveTerms for the given Partner, rule selectors
	// and evaluation time, resolving program terms as the program-owning
	// capability.
	ResolveTerms(context.Context, partnerprogram.Partner, []string, time.Time) (partnerprogram.EffectiveTerms, error)
	// ResolveReferralTerms computes EffectiveTerms for the given Partner, rule
	// selectors, and the referral and event times, resolving referral-specific
	// program terms.
	ResolveReferralTerms(context.Context, partnerprogram.Partner, []string, time.Time, time.Time) (partnerprogram.EffectiveTerms, error)
	// UpdatePayoutDestination applies a DestinationRequest and returns the
	// resulting Destination, updating the payout destination owned by
	// ProgramService.
	UpdatePayoutDestination(context.Context, partnerprogram.DestinationRequest) (partnerprogram.Destination, error)
	// GetPayoutDestination reads the payout destination for the identified partner,
	// returning the stored Destination record held by ProgramService.
	GetPayoutDestination(context.Context, string) (partnerprogram.Destination, error)
}

// ReferralService is the referral-owning capability: signup lookup and payment
// binding, link issue/rotate/lookup, click and visit observation, analytics,
// attribution lock/assign/correction and snapshot access, plus per-partner and
// relationship listings. Manager projects its outputs without re-deriving
// attribution.
type ReferralService interface {
	// LookupSignup resolves a Referral for the given identifiers, supplied Evidence
	// and time, performing the signup lookup owned by ReferralService.
	LookupSignup(context.Context, string, string, referral.Evidence, time.Time) (referral.Referral, error)
	// BindPayment binds a payment to its referral attribution at the given time,
	// returning the PaymentAttribution produced by the referral-owning capability.
	BindPayment(context.Context, string, string, time.Time) (referral.PaymentAttribution, error)
	// IssueLink creates a new referral Link from the partner's PartnerState,
	// exercising ReferralService's link issuance ownership.
	IssueLink(context.Context, referral.PartnerState) (referral.Link, error)
	// RotateLink rotates a referral link from the RotateLinkRequest, returning the
	// new Link; the Manager implementation binds identity and permission each
	// attempt, withholding results on late revocation via ErrUncertain while
	// preserving original-key recovery.
	RotateLink(context.Context, referral.RotateLinkRequest) (referral.Link, error)
	// GetLinkByCode reads the referral Link matching the supplied code, a lookup
	// owned by ReferralService.
	GetLinkByCode(context.Context, string) (referral.Link, error)
	// ObserveClick records a click observation for the identified link and click
	// value, returning the stored Click owned by ReferralService.
	ObserveClick(context.Context, string, string) (referral.Click, error)
	// ObserveVisit records a visit from the VisitRequest, returning the resulting
	// VisitObservation as observed by the referral owner.
	ObserveVisit(context.Context, referral.VisitRequest) (referral.VisitObservation, error)
	// GetAnalytics returns Analytics for the identified partner filtered by the
	// AnalyticsQuery, projected from ReferralService's observations.
	GetAnalytics(context.Context, string, referral.AnalyticsQuery) (referral.Analytics, error)
	// ClickCount returns the number of clicks for the identified partner between
	// the two times, as counted by the referral-owning capability.
	ClickCount(context.Context, string, time.Time, time.Time) (int64, error)
	// LockAttribution locks a Referral for the given PartnerState, Link and
	// Eligibility, fixing attribution within the referral owner.
	LockAttribution(context.Context, referral.PartnerState, referral.Link, referral.Eligibility) (referral.Referral, error)
	// AssignAttribution applies a CorrectionRequest and returns the corrected
	// Referral, performing attribution correction owned by ReferralService.
	AssignAttribution(context.Context, referral.CorrectionRequest) (referral.Referral, error)
	// FindCorrection locates a prior correction Referral by its three selector
	// strings, a lookup owned by the referral capability.
	FindCorrection(context.Context, string, string, string) (referral.Referral, error)
	// GetAttributionSnapshot returns the AttributionSnapshot for the identified
	// referral, exposing evidence held by ReferralService.
	GetAttributionSnapshot(context.Context, string) (referral.AttributionSnapshot, error)
	// GetReferralForCustomer returns the Referral associated with the identified
	// customer, a read owned by ReferralService.
	GetReferralForCustomer(context.Context, string) (referral.Referral, error)
	// ListByPartner returns referrals for the identified partner, bounded by the
	// limit and cursor string, from the referral owner's listings.
	ListByPartner(context.Context, string, int, string) ([]referral.Referral, error)
	// ListRelationships returns a RelationshipPage for the identified partner
	// filtered by the RelationshipQuery, projected from ReferralService outputs.
	ListRelationships(context.Context, string, referral.RelationshipQuery) (referral.RelationshipPage, error)
}

// EarningsService is the earnings-owning financial capability: accruals,
// disputes, returns, maturity, balances, journal/statements and financial
// reports, plus the full claim lifecycle (request, find, cancel, get, list,
// decide, record and amend payments). Financial state and receipts stay with
// this owner.
type EarningsService interface {
	// AcceptedAccrual returns the earnings Entry for the identified accrual within
	// a partner scope, a read owned by EarningsService.
	AcceptedAccrual(context.Context, string, string) (partnerearnings.Entry, error)
	// Dispute submits a DisputeRequest and returns the resulting DisputeResult,
	// exercising the dispute handling owned by the earnings capability.
	Dispute(context.Context, partnerearnings.DisputeRequest) (partnerearnings.DisputeResult, error)
	// RecordReturnedTransfer records a returned transfer from the ReturnRequest,
	// returning the ReturnResult as owned by EarningsService.
	RecordReturnedTransfer(context.Context, partnerearnings.ReturnRequest) (partnerearnings.ReturnResult, error)
	// Accrue records an earnings Entry from the AccrualRequest, performing accrual
	// within the earnings-owning financial capability.
	Accrue(context.Context, partnerearnings.AccrualRequest) (partnerearnings.Entry, error)
	// Reverse applies a ReversalRequest and returns the resulting Entries,
	// reversing prior accruals within EarningsService.
	Reverse(context.Context, partnerearnings.ReversalRequest) ([]partnerearnings.Entry, error)
	// Mature processes maturity for the identified partner, returning the Entries
	// produced by the earnings owner's maturity operation.
	Mature(context.Context, string) ([]partnerearnings.Entry, error)
	// Balances returns the current Balances for the identified partner as held by
	// the earnings-owning capability.
	Balances(context.Context, string) (partnerearnings.Balances, error)
	// ListJournal returns the journal Entries for the identified partner, exposing
	// the earnings ledger owned by EarningsService.
	ListJournal(context.Context, string) ([]partnerearnings.Entry, error)
	// GetStatement returns the Statement for the identified partner filtered by the
	// StatementQuery, generated by the earnings owner.
	GetStatement(context.Context, string, partnerearnings.StatementQuery) (partnerearnings.Statement, error)
	// GetPaymentReport returns the PaymentReport for the identified partner
	// filtered by the PaymentQuery, a financial report owned by EarningsService.
	GetPaymentReport(context.Context, string, partnerearnings.PaymentQuery) (partnerearnings.PaymentReport, error)
	// GetReferralAmounts returns the ReferralAmountReport for the identified
	// partner filtered by the ReferralAmountQuery from the earnings owner.
	GetReferralAmounts(context.Context, string, partnerearnings.ReferralAmountQuery) (partnerearnings.ReferralAmountReport, error)
	// GetFinancialMetrics returns FinancialMetrics for the identified partner
	// filtered by the FinancialMetricsQuery, computed by the earnings owner.
	GetFinancialMetrics(context.Context, string, partnerearnings.FinancialMetricsQuery) (partnerearnings.FinancialMetrics, error)
	// RequestClaim creates a Claim from the ClaimRequest; the Manager
	// implementation retains the original owning current-destination selection,
	// with RequestClaimWithDestination binding observed revisions.
	RequestClaim(context.Context, partnerearnings.ClaimRequest) (partnerearnings.Claim, error)
	// FindClaimRequest locates a Claim by its three selector strings, a lookup
	// within the claim lifecycle owned by EarningsService.
	FindClaimRequest(context.Context, string, string, string) (partnerearnings.Claim, error)
	// CancelRequestedClaim cancels a requested claim per the CancelClaimRequest,
	// returning the updated Claim from the earnings owner.
	CancelRequestedClaim(context.Context, partnerearnings.CancelClaimRequest) (partnerearnings.Claim, error)
	// GetClaim reads a Claim by ID; the Manager implementation scopes results to
	// the current partner, treating foreign claims as not-found and consistency
	// mismatches as ErrUnavailable.
	GetClaim(context.Context, string) (partnerearnings.Claim, error)
	// ListClaims returns claims filtered by states, bounded to one page; the
	// Manager implementation enforces limit 1..100 and flags mismatched rows as
	// ErrUnavailable.
	ListClaims(context.Context, string, []string, int, string) ([]partnerearnings.Claim, error)
	// DecideClaim applies a ClaimDecision and returns the resulting Claim, deciding
	// claims within the lifecycle owned by EarningsService.
	DecideClaim(context.Context, partnerearnings.ClaimDecision) (partnerearnings.Claim, error)
	// RecordPayment records payment from the RecordPaymentRequest, returning the
	// updated Claim with its receipt held by the earnings owner.
	RecordPayment(context.Context, partnerearnings.RecordPaymentRequest) (partnerearnings.Claim, error)
	// AmendPayment applies an AmendPaymentRequest to a recorded payment, returning
	// the amended Claim owned by EarningsService.
	AmendPayment(context.Context, partnerearnings.AmendPaymentRequest) (partnerearnings.Claim, error)
}

// RevenueFacts is the owning billing service read capability, not a repository.
type RevenueFacts interface {
	// GetRevenueFact reads the billing RevenueFact for the identified source, a
	// read owned by the billing service capability.
	GetRevenueFact(context.Context, string) (billing.RevenueFact, error)
}

// Dependencies is the trusted wiring for Manager. Claims gates new withdrawals
// while receipts recover first; WorkReporting is optional and only disables the
// backlog read when absent.
type Dependencies struct {
	Program   ProgramService
	Referral  ReferralService
	Earnings  EarningsService
	Identity  Identity
	Authority Authority
	Groups    Groups
	Revenue   RevenueFacts
	Evidence  *referral.EvidenceSigner
	Clock     Clock
	Controls  Controls
	// AcquisitionEligibility gates new enrollment and referrals, never retained earnings.
	AcquisitionEligibility AcquisitionEligibility
	// RequireAcquisitionEligibility rejects omitted policy wiring at startup.
	RequireAcquisitionEligibility bool
	// Claims sets explicit admission for new withdrawals; receipts recover first.
	Claims ClaimsConfig
	// WorkReporting is optional. Missing wiring disables only the backlog read.
	WorkReporting WorkBacklogService
}

// Manager coordinates the owning program, referral, earnings and identity
// services with authority and controls. It owns no storage itself; every
// command re-checks current scoped permission and delegates financial
// transitions to the owners.
type Manager struct {
	deps             Dependencies
	revenueReporting *RevenueReportingConfig
}

// NewManager requires every financial/authority/identity capability. Disabled
// commercial admission is represented by Controls, never by missing services.
func NewManager(deps Dependencies) (*Manager, error) {
	if nilManagerDependency(deps.Program) || nilManagerDependency(deps.Referral) || nilManagerDependency(deps.Earnings) || nilManagerDependency(deps.Identity) || nilManagerDependency(deps.Authority) || nilManagerDependency(deps.Groups) || nilManagerDependency(deps.Revenue) || nilManagerDependency(deps.Evidence) || nilManagerDependency(deps.Clock) {
		return nil, ErrUnavailable
	}
	if deps.Claims.MinimumMinor < 0 {
		return nil, ErrInvalid
	}
	if (deps.RequireAcquisitionEligibility || deps.AcquisitionEligibility != nil) && nilManagerDependency(deps.AcquisitionEligibility) {
		return nil, ErrUnavailable
	}
	return &Manager{deps: deps}, nil
}

// authorize checks one current scoped capability for an actor and target
// through the live Authority. Missing wiring yields ErrUnavailable, malformed
// actor or target yields ErrDenied, and an already-cancelled context error is
// returned before the check.
func (m *Manager) authorize(ctx context.Context, actor, capability, target string) error {
	if m == nil || nilManagerDependency(m.deps.Authority) {
		return ErrUnavailable
	}
	if ctx == nil || strings.TrimSpace(actor) == "" || actor != strings.TrimSpace(actor) || len(actor) > 256 || target != strings.TrimSpace(target) || len(target) > 256 {
		return ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.deps.Authority.CheckPartners(ctx, actor, capability, target)
}

// nilManagerDependency reports whether a dependency port is absent, covering
// nil interfaces and nil pointers, maps, slices, funcs and channels.
func nilManagerDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// self resolves the actor's own partner record under a self capability. It
// requires current authority, an active verified individual principal matching
// the actor, and a program partner for that customer; a sole not-found absence
// maps to ErrNotPartner while any partner/customer mismatch is a denial.
func (m *Manager) self(ctx context.Context, actor, capability string) (partnerprogram.Partner, error) {
	if err := m.authorize(ctx, actor, capability, actor); err != nil {
		return partnerprogram.Partner{}, err
	}
	principal, err := m.deps.Identity.GetPartnerPrincipal(ctx, actor)
	if err != nil {
		return partnerprogram.Partner{}, err
	}
	if principal.ID != actor || !principal.Active || !principal.EmailVerified || !principal.Individual {
		return partnerprogram.Partner{}, ErrDenied
	}
	p, err := m.deps.Program.GetPartnerForCustomer(ctx, actor)
	if singleManagerAbsence(err, partnerprogram.ErrNotFound) {
		return partnerprogram.Partner{}, ErrNotPartner
	}
	if err != nil {
		return partnerprogram.Partner{}, err
	}
	if p.CustomerID != actor {
		return partnerprogram.Partner{}, ErrDenied
	}
	return p, nil
}

// referralState projects partner admission into the referral owner's
// PartnerState, carrying partner/customer identity and referral acquisition
// permission only.
func referralState(p partnerprogram.Partner) referral.PartnerState {
	return referral.PartnerState{PartnerID: p.ID, CustomerID: p.CustomerID, CanAcquireReferrals: p.CanAcquireReferrals}
}

// snapshot freezes effective terms into the referral owner's TermsSnapshot,
// copying policy version and eligible plan slices so later changes cannot alter
// the locked copy.
func snapshot(t partnerprogram.EffectiveTerms) referral.TermsSnapshot {
	return referral.TermsSnapshot{RateBasisPoints: t.RateBasisPoints, HoldDurationDays: int(t.HoldDuration / (24 * time.Hour)), Currency: t.Currency, CurrencyExponent: t.CurrencyExponent, TermsVersion: t.TermsVersion, PolicyVersionIDs: append([]string(nil), t.PolicyVersionIDs...), EligiblePlanIDs: append([]string{}, t.EligiblePlanIDs...), RecurrenceEndsAt: t.RecurrenceEndsAt}
}

// terms resolves a partner's effective terms at a point in time from its group
// memberships via the program owner.
func (m *Manager) terms(ctx context.Context, p partnerprogram.Partner, at time.Time) (partnerprogram.EffectiveTerms, error) {
	groups, err := m.deps.Groups.PartnerGroupIDs(ctx, p.CustomerID)
	if err != nil {
		return partnerprogram.EffectiveTerms{}, err
	}
	return m.deps.Program.ResolveTerms(ctx, p, groups, at)
}

// EnrollSelf binds actual consent to the verified current customer. It never
// silently accepts a terms version merely because that version is configured.
func (m *Manager) EnrollSelf(ctx context.Context, actor, acceptedTerms string) (partnerprogram.Partner, error) {
	if !m.deps.Controls.Enrollment {
		return partnerprogram.Partner{}, ErrDenied
	}
	if err := m.authorize(ctx, actor, CapabilityEnroll, actor); err != nil {
		return partnerprogram.Partner{}, err
	}
	p, err := m.deps.Identity.GetPartnerPrincipal(ctx, actor)
	if err != nil {
		return partnerprogram.Partner{}, err
	}
	if p.ID != actor || !p.Active || !p.EmailVerified || !p.Individual || !p.RegionEligible {
		return partnerprogram.Partner{}, ErrDenied
	}
	if err := m.requireAcquisition(ctx, actor); err != nil {
		return partnerprogram.Partner{}, err
	}
	if err := m.authorize(ctx, actor, CapabilityEnroll, actor); err != nil {
		return partnerprogram.Partner{}, err
	}
	return m.deps.Program.Enroll(ctx, partnerprogram.EnrollRequest{CustomerID: actor, AcceptedTermsVersion: acceptedTerms})
}

// Overview is the member-facing partner snapshot: partner record, effective
// terms, balances, optional payout destination and the as-of instant.
type Overview struct {
	Partner     partnerprogram.Partner        `json:"partner"`
	Terms       partnerprogram.EffectiveTerms `json:"terms"`
	Balances    partnerearnings.Balances      `json:"balances"`
	Destination *partnerprogram.Destination   `json:"destination,omitempty"`
	AsOf        time.Time                     `json:"as_of"`
}

// Overview returns the verified current partner's aggregate snapshot under self
// capability. A missing destination is absence, not an error; inconsistent
// destination identity or version yields ErrUnavailable, and current authority
// is re-checked before returning the assembled result.
func (m *Manager) Overview(ctx context.Context, actor string) (Overview, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return Overview{}, err
	}
	at := m.deps.Clock.Now().UTC()
	terms, err := m.terms(ctx, p, at)
	if err != nil {
		return Overview{}, err
	}
	b, err := m.deps.Earnings.Balances(ctx, p.ID)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{Partner: p, Terms: terms, Balances: b, AsOf: at}
	d, err := m.deps.Program.GetPayoutDestination(ctx, actor)
	if err == nil {
		if d.CustomerID != actor || d.ID == "" || d.Version < 1 {
			return Overview{}, ErrUnavailable
		}
		out.Destination = &d
	} else if !singleManagerAbsence(err, partnerprogram.ErrNotFound) {
		return Overview{}, err
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return Overview{}, err
	}
	return out, nil
}

// GetOrCreateLink returns the partner's referral link under self capability,
// issuing one from the referral owner when attribution admission is enabled;
// disabled attribution control denies the request.
func (m *Manager) GetOrCreateLink(ctx context.Context, actor string) (referral.Link, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return referral.Link{}, err
	}
	if !m.deps.Controls.Attribution {
		return referral.Link{}, ErrDenied
	}
	if err := m.requireAcquisition(ctx, actor); err != nil {
		return referral.Link{}, err
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return referral.Link{}, err
	}
	return m.deps.Referral.IssueLink(ctx, referralState(p))
}

// RotateLink binds current identity and permission on every attempt, including
// receipt replay. Acquisition pauses prevent new rotations but preserve recovery
// of the original committed actor/key request through the owning service.
func (m *Manager) RotateLink(ctx context.Context, actor string, req referral.RotateLinkRequest) (referral.Link, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return referral.Link{}, err
	}
	req.ActorID = actor
	req.Partner = referralState(p)
	req.Partner.CanAcquireReferrals = req.Partner.CanAcquireReferrals && m.deps.Controls.Attribution
	eligible, eligibilityErr := m.acquisitionEligible(ctx, actor)
	// The owner recovers an existing original-key receipt before new admission.
	req.Partner.CanAcquireReferrals = req.Partner.CanAcquireReferrals && eligible && eligibilityErr == nil
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return referral.Link{}, err
	}
	l, err := m.deps.Referral.RotateLink(ctx, req)
	if err != nil {
		if singleManagerAbsence(err, referral.ErrDenied) {
			if eligibilityErr != nil {
				return referral.Link{}, eligibilityErr
			}
			if !eligible {
				return referral.Link{}, ErrIneligible
			}
		}
		return referral.Link{}, err
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		// The receipt is already committed; a late revocation cannot undo it.
		// Withhold the result and preserve explicit original-key recovery instead
		// of implying the write failed and encouraging a new rotation key.
		return referral.Link{}, fmt.Errorf("%w: %w", referral.ErrUncertain, err)
	}
	return l, nil
}

// ConsumeSignup resolves immutable owning identity facts rather than trusting
// a browser-provided customer ID, creation time or eligibility boolean.
func (m *Manager) ConsumeSignup(ctx context.Context, actor, signupID string) (referral.Referral, error) {
	if err := m.authorize(ctx, actor, CapabilitySignupWorker, signupID); err != nil {
		return referral.Referral{}, err
	}
	fact, err := m.deps.Identity.GetSignupFact(ctx, signupID)
	if err != nil {
		return referral.Referral{}, err
	}
	if fact.ID != signupID || !fact.NewAccount || !fact.Individual {
		return referral.Referral{}, ErrDenied
	}
	evidence, err := m.deps.Evidence.Verify(fact.AttributionEvidence, fact.CreatedAt)
	if err != nil {
		return referral.Referral{}, err
	}
	if old, err := m.deps.Referral.LookupSignup(ctx, fact.CustomerID, fact.ID, evidence, fact.CreatedAt); err == nil {
		return old, nil
	} else if !singleManagerAbsence(err, referral.ErrNotFound) {
		return referral.Referral{}, err
	}
	if !m.deps.Controls.Attribution {
		return referral.Referral{}, ErrDenied
	}
	link, err := m.deps.Referral.GetLinkByCode(ctx, evidence.Code)
	if err != nil {
		return referral.Referral{}, err
	}
	owner, err := m.deps.Program.GetPartner(ctx, link.PartnerID)
	if err != nil {
		return referral.Referral{}, err
	}
	if err := m.requireAcquisition(ctx, owner.CustomerID); err != nil {
		return referral.Referral{}, err
	}
	terms, err := m.terms(ctx, owner, fact.CreatedAt)
	if err != nil {
		return referral.Referral{}, err
	}
	if err := m.authorize(ctx, actor, CapabilitySignupWorker, signupID); err != nil {
		return referral.Referral{}, err
	}
	return m.deps.Referral.LockAttribution(ctx, referralState(owner), link, referral.Eligibility{ReferredCustomer: fact.CustomerID, SignupID: fact.ID, IsIndividual: fact.Individual, At: fact.CreatedAt, Evidence: evidence, Terms: snapshot(terms)})
}

// ListReferrals returns one bounded page (limit 1..100) of the current
// partner's referrals. Rows whose partner or program identity do not match
// yield ErrUnavailable, and self authority is re-checked after the read.
func (m *Manager) ListReferrals(ctx context.Context, actor string, limit int, after string) ([]referral.Referral, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return nil, err
	}
	rows, err := m.deps.Referral.ListByPartner(ctx, p.ID, limit, after)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.PartnerID != p.ID || row.ProgramID != referral.ProgramID {
			return nil, ErrUnavailable
		}
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return nil, err
	}
	return rows, nil
}

// Ledger returns the current partner's full earnings journal. Entries whose
// partner or currency do not match the configured program yield ErrUnavailable,
// and self authority is re-checked after the read.
func (m *Manager) Ledger(ctx context.Context, actor string) ([]partnerearnings.Entry, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return nil, err
	}
	rows, err := m.deps.Earnings.ListJournal(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.PartnerID != p.ID || row.Currency != m.deps.Program.Config().Currency {
			return nil, ErrUnavailable
		}
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return nil, err
	}
	return rows, nil
}

// ListClaims returns one bounded page (limit 1..100) of the current partner's
// claims, optionally filtered by states. Rows with mismatched partner or
// currency yield ErrUnavailable, and self authority is re-checked after the
// read.
func (m *Manager) ListClaims(ctx context.Context, actor string, states []string, limit int, after string) ([]partnerearnings.Claim, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return nil, err
	}
	rows, err := m.deps.Earnings.ListClaims(ctx, p.ID, states, limit, after)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.PartnerID != p.ID || row.Currency != m.deps.Program.Config().Currency {
			return nil, ErrUnavailable
		}
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetClaim returns one of the current partner's claims by ID; a claim owned by
// another partner is a plain not-found. Identifier, currency or consistency
// mismatches yield ErrUnavailable, and self authority is re-checked before
// returning.
func (m *Manager) GetClaim(ctx context.Context, actor, id string) (partnerearnings.Claim, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	c, err := m.deps.Earnings.GetClaim(ctx, id)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if c.PartnerID != p.ID {
		return partnerearnings.Claim{}, ErrNotFound
	}
	if c.ID != id || c.Currency != m.deps.Program.Config().Currency {
		return partnerearnings.Claim{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return partnerearnings.Claim{}, err
	}
	return c, nil
}

// Destination returns the current partner's payout destination under self
// capability. Destinations with mismatched ownership, empty ID or non-positive
// version yield ErrUnavailable rather than being exposed, and authority is re-
// checked after the read.
func (m *Manager) Destination(ctx context.Context, actor string) (partnerprogram.Destination, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return partnerprogram.Destination{}, err
	}
	d, err := m.deps.Program.GetPayoutDestination(ctx, p.CustomerID)
	if err != nil {
		return partnerprogram.Destination{}, err
	}
	if d.CustomerID != p.CustomerID || d.ID == "" || d.Version < 1 {
		return partnerprogram.Destination{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return partnerprogram.Destination{}, err
	}
	return d, nil
}

// UpdateDestination replaces the actor's own PayPal payout destination at the
// supplied expected version, delegating enforcement of the version precondition
// to the program owner. It performs only the initial self authorization check.
func (m *Manager) UpdateDestination(ctx context.Context, actor, email string, expected int64) (partnerprogram.Destination, error) {
	if _, err := m.self(ctx, actor, CapabilitySelf); err != nil {
		return partnerprogram.Destination{}, err
	}
	return m.deps.Program.UpdatePayoutDestination(ctx, partnerprogram.DestinationRequest{CustomerID: actor, Method: "paypal", PayPalEmail: email, ExpectedVersion: expected})
}

// CancelClaim is owner-scoped and cannot assert confirmed-unsent authority for
// an in-flight transfer. The earnings service enforces the current state.
func (m *Manager) CancelClaim(ctx context.Context, actor, id, reason string) (partnerearnings.Claim, error) {
	claim, err := m.GetClaim(ctx, actor, id)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if claim.State != partnerearnings.ClaimRequested {
		return partnerearnings.Claim{}, ErrDenied
	}
	return m.deps.Earnings.DecideClaim(ctx, partnerearnings.ClaimDecision{ClaimID: id, NewState: partnerearnings.ClaimCancelled, Reason: reason, ActorID: actor, ExpectedRevision: claim.Revision})
}

// AdminQueue lists claims across all partners under processing capability with
// an empty target, checked before and after the read. Limit must be 1..100;
// rows with a currency not matching the program yield ErrUnavailable.
func (m *Manager) AdminQueue(ctx context.Context, actor string, states []string, limit int, after string) ([]partnerearnings.Claim, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityProcessing, ""); err != nil {
		return nil, err
	}
	rows, err := m.deps.Earnings.ListClaims(ctx, "", states, limit, after)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Currency != m.deps.Program.Config().Currency {
			return nil, ErrUnavailable
		}
	}
	if err := m.authorize(ctx, actor, CapabilityProcessing, ""); err != nil {
		return nil, err
	}
	return rows, nil
}

// AdminDecideClaim forwards a claim decision under current processing
// capability for the exact claim, requiring a positive expected revision.
// Moving a claim into processing additionally requires the ManualRecording
// admission control; state enforcement stays with the earnings owner.
func (m *Manager) AdminDecideClaim(ctx context.Context, req partnerearnings.ClaimDecision) (partnerearnings.Claim, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityProcessing, req.ClaimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if req.ExpectedRevision < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	if req.NewState == partnerearnings.ClaimProcessing && !m.deps.Controls.ManualRecording {
		return partnerearnings.Claim{}, ErrDenied
	}
	return m.deps.Earnings.DecideClaim(ctx, req)
}

// ManualHandlingAdmitted reports host admission of new or resumed external
// payment handling. It grants no permission and proves no transfer; hosts use
// it only as guidance alongside an authorized selected-claim read. Commands
// enforce their own current authority and native financial preconditions.
func (m *Manager) ManualHandlingAdmitted() bool { return m.deps.Controls.ManualRecording }

// AdminRecordPayment records an already-attempted external transfer under
// current selected permission, even while new handling is paused. It never
// sends money. The earnings owner enforces assigned actor, state, revision and
// exact original receipt recovery without another debit or reservation release.
func (m *Manager) AdminRecordPayment(ctx context.Context, req partnerearnings.RecordPaymentRequest) (partnerearnings.Claim, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityRecordPayment, req.ClaimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if req.ExpectedRevision < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	return m.deps.Earnings.RecordPayment(ctx, req)
}

// AdminAmendPayment forwards a payment amendment under amend-payment capability
// for the exact claim, requiring a positive expected revision. Amendment
// semantics and consistency enforcement stay with the earnings owner.
func (m *Manager) AdminAmendPayment(ctx context.Context, req partnerearnings.AmendPaymentRequest) (partnerearnings.Claim, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityAmendPayment, req.ClaimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if req.ExpectedRevision < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	return m.deps.Earnings.AmendPayment(ctx, req)
}

// AdminPublishPolicy forwards a policy draft to the program owner under policy
// capability targeted at the draft's partner scope.
func (m *Manager) AdminPublishPolicy(ctx context.Context, req partnerprogram.PublishPolicyRequest) (partnerprogram.PolicyVersion, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityPolicy, req.Draft.PartnerCustomer); err != nil {
		return partnerprogram.PolicyVersion{}, err
	}
	return m.deps.Program.PublishPolicy(ctx, req)
}

// AdminPolicyVersions lists policy versions under policy capability with an
// empty target, re-checking authority after the read.
func (m *Manager) AdminPolicyVersions(ctx context.Context, actor string) ([]partnerprogram.PolicyVersion, error) {
	if err := m.authorize(ctx, actor, CapabilityPolicy, ""); err != nil {
		return nil, err
	}
	rows, err := m.deps.Program.ListPolicyVersions(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.authorize(ctx, actor, CapabilityPolicy, ""); err != nil {
		return nil, err
	}
	return rows, nil
}

// AdminChangeStatus forwards a partner status change to the program owner under
// policy capability for the selected partner.
func (m *Manager) AdminChangeStatus(ctx context.Context, req partnerprogram.StatusChangeRequest) (partnerprogram.Partner, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityPolicy, req.PartnerID); err != nil {
		return partnerprogram.Partner{}, err
	}
	return m.deps.Program.ChangeStatus(ctx, req)
}

// ProcessRevenueFact consumes an authoritative owning-service fact. Historical
// terms come from locked attribution; current policy cannot rewrite renewal
// entitlement. Dependency failures remain retryable, never silent absence.
func (m *Manager) ProcessRevenueFact(ctx context.Context, actor, factID string) ([]partnerearnings.Entry, error) {
	if err := m.authorize(ctx, actor, CapabilityRevenueWorker, factID); err != nil {
		return nil, err
	}
	fact, err := m.deps.Revenue.GetRevenueFact(ctx, factID)
	if err != nil {
		return nil, err
	}
	if fact.ID != factID {
		return nil, ErrUnavailable
	}
	original := fact
	if fact.Kind != billing.RevenuePayment {
		original, err = m.deps.Revenue.GetRevenueFact(ctx, fact.PaymentFactID())
		if singleManagerAbsence(err, billing.ErrRevenueNotFound) {
			return nil, partnerearnings.ErrUnresolved
		}
		if err != nil {
			return nil, err
		}
	}
	if original.Kind != billing.RevenuePayment || original.ID != fact.PaymentFactID() || original.Scope != fact.Scope || original.PaymentID != fact.PaymentID || original.InvoiceID != fact.InvoiceID || original.AllocationID != fact.AllocationID || original.PrincipalID != fact.PrincipalID || original.SubscriptionID != fact.SubscriptionID || original.PlanID != fact.PlanID || original.CostID != fact.CostID || original.ProviderPriceID != fact.ProviderPriceID || original.ProviderCustomerID != fact.ProviderCustomerID || original.Currency != fact.Currency || original.CurrencyExponent != fact.CurrencyExponent || original.PaidMinor != fact.PaidMinor {
		return nil, billing.ErrRevenueUnassessable
	}
	binding, err := m.deps.Referral.BindPayment(ctx, original.PrincipalID, original.PaymentFactID(), original.EffectiveAt)
	if err != nil {
		return nil, err
	}
	terms := binding.Terms
	if fact.Currency != terms.Currency || fact.CurrencyExponent != terms.CurrencyExponent {
		return nil, partnerearnings.ErrCurrencyMismatch
	}
	switch fact.Kind {
	case billing.RevenuePayment:
		accrualRequest := partnerearnings.AccrualRequest{PartnerID: binding.PartnerID, PaymentID: fact.PaymentFactID(), PaymentMinor: fact.PaidMinor, RateBasisPoints: terms.RateBasisPoints, HoldDuration: time.Duration(terms.HoldDurationDays) * 24 * time.Hour, Currency: terms.Currency, OccurredAt: fact.EffectiveAt, ReferralID: binding.ReferralID, PlanID: fact.PlanID, TermsVersion: terms.TermsVersion, PolicyID: fmt.Sprint(terms.PolicyVersionIDs), SourceKind: "verified-subscription-allocation"}
		_, acceptedErr := m.deps.Earnings.AcceptedAccrual(ctx, binding.PartnerID, fact.PaymentFactID())
		if acceptedErr == nil {
			// Owning immutable acceptance is recovered before current commercial
			// admission, while Accrue still verifies every frozen replay field.
			entry, err := m.deps.Earnings.Accrue(ctx, accrualRequest)
			if err != nil {
				return nil, err
			}
			return []partnerearnings.Entry{entry}, nil
		}
		if !singleManagerAbsence(acceptedErr, partnerearnings.ErrNotFound) {
			return nil, acceptedErr
		}
		if !m.deps.Controls.Accrual {
			return nil, ErrDenied
		}
		partner, err := m.deps.Program.GetPartner(ctx, binding.PartnerID)
		if err != nil {
			return nil, err
		}
		if !partner.CanAccrue {
			return nil, ErrDenied
		}
		eligible := false
		for _, id := range terms.EligiblePlanIDs {
			if id == fact.PlanID {
				eligible = true
				break
			}
		}
		if !eligible {
			return nil, &NoEntitlementError{ReasonCode: "ineligible_plan"}
		}
		if fact.PaidMinor == 0 {
			return nil, &NoEntitlementError{ReasonCode: "zero_net_revenue"}
		}
		if terms.RecurrenceEndsAt != nil && !fact.EffectiveAt.Before(*terms.RecurrenceEndsAt) {
			return nil, &NoEntitlementError{ReasonCode: "recurrence_ended"}
		}
		entry, err := m.deps.Earnings.Accrue(ctx, accrualRequest)
		if err != nil {
			return nil, err
		}
		return []partnerearnings.Entry{entry}, nil
	case billing.RevenueRefund:
		return m.deps.Earnings.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: binding.PartnerID, RefundID: fact.ID, PaymentID: fact.PaymentFactID(), CumulativeRefundedMinor: fact.CumulativeRefundedMinor, Currency: fact.Currency, OccurredAt: fact.EffectiveAt})
	case billing.RevenueDisputeHold, billing.RevenueDisputeWon, billing.RevenueDisputeLost:
		action := "hold"
		if fact.Kind == billing.RevenueDisputeWon {
			action = "won"
		}
		if fact.Kind == billing.RevenueDisputeLost {
			action = "lost"
		}
		result, err := m.deps.Earnings.Dispute(ctx, partnerearnings.DisputeRequest{PartnerID: binding.PartnerID, PaymentID: fact.PaymentFactID(), DisputeID: fact.AdjustmentID, OperationID: fact.ID, Action: action, Reason: "verified-provider-dispute", ActorID: actor, Currency: fact.Currency, OccurredAt: fact.EffectiveAt})
		return result.Entries, err
	default:
		return nil, billing.ErrRevenueUnassessable
	}
}

// MaturePartner moves existing due obligations under dedicated partner-scoped
// maturity authority, independently of new admission. A revoked caller receives
// no result; accepted financial receipts remain recoverable by a current worker.
func (m *Manager) MaturePartner(ctx context.Context, actor, partnerID string) ([]partnerearnings.Entry, error) {
	if !validWorkText(partnerID, 256) {
		return nil, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityMaturityWorker, partnerID); err != nil {
		return nil, err
	}
	entries, err := m.deps.Earnings.Mature(ctx, partnerID)
	if authErr := m.authorize(ctx, actor, CapabilityMaturityWorker, partnerID); authErr != nil {
		return nil, authErr
	}
	return entries, err
}

// singleManagerAbsence walks up to 32 wrapped errors looking for target,
// distinguishing a sole sentinel absence from one joined with other failures.
func singleManagerAbsence(err, target error) bool {
	for i := 0; err != nil && i < 32; i++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// AdminRecordReturnedTransfer records a reasoned obligation adjustment under
// current scoped payment permission, independently from new claim admission.
func (m *Manager) AdminRecordReturnedTransfer(ctx context.Context, req partnerearnings.ReturnRequest) (partnerearnings.ReturnResult, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityReturnPayment, req.ClaimID); err != nil {
		return partnerearnings.ReturnResult{}, err
	}
	if req.ExpectedRevision < 1 {
		return partnerearnings.ReturnResult{}, ErrInvalid
	}
	return m.deps.Earnings.RecordReturnedTransfer(ctx, req)
}
