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
type Identity interface {
	GetPartnerPrincipal(context.Context, string) (Principal, error)
	GetSignupFact(context.Context, string) (SignupFact, error)
}

// Authority checks current scoped permission for every use case, including
// replay. A generic administrator flag does not confer manual payout authority.
type Authority interface {
	CheckPartners(context.Context, string, string, string) error
}
type Groups interface {
	PartnerGroupIDs(context.Context, string) ([]string, error)
}
type Clock interface{ Now() time.Time }

// Controls are independent host switches; pause preserves reads and recovery
// of existing obligations when record/payment processing remains enabled.
type Controls struct{ Enrollment, Attribution, Accrual, Claims, ManualRecording bool }
type ProgramService interface {
	Config() partnerprogram.Config
	Enroll(context.Context, partnerprogram.EnrollRequest) (partnerprogram.Partner, error)
	GetPartnerForCustomer(context.Context, string) (partnerprogram.Partner, error)
	GetPartner(context.Context, string) (partnerprogram.Partner, error)
	ChangeStatus(context.Context, partnerprogram.StatusChangeRequest) (partnerprogram.Partner, error)
	PublishPolicy(context.Context, partnerprogram.PublishPolicyRequest) (partnerprogram.PolicyVersion, error)
	ListPolicyVersions(context.Context) ([]partnerprogram.PolicyVersion, error)
	ResolveTerms(context.Context, partnerprogram.Partner, []string, time.Time) (partnerprogram.EffectiveTerms, error)
	ResolveReferralTerms(context.Context, partnerprogram.Partner, []string, time.Time, time.Time) (partnerprogram.EffectiveTerms, error)
	UpdatePayoutDestination(context.Context, partnerprogram.DestinationRequest) (partnerprogram.Destination, error)
	GetPayoutDestination(context.Context, string) (partnerprogram.Destination, error)
}
type ReferralService interface {
	LookupSignup(context.Context, string, string, referral.Evidence, time.Time) (referral.Referral, error)
	BindPayment(context.Context, string, string, time.Time) (referral.PaymentAttribution, error)
	IssueLink(context.Context, referral.PartnerState) (referral.Link, error)
	RetireLink(context.Context, string, string, string) error
	GetLinkByCode(context.Context, string) (referral.Link, error)
	ObserveClick(context.Context, string, string) (referral.Click, error)
	ObserveVisit(context.Context, referral.VisitRequest) (referral.VisitObservation, error)
	GetAnalytics(context.Context, string, referral.AnalyticsQuery) (referral.Analytics, error)
	ClickCount(context.Context, string, time.Time, time.Time) (int64, error)
	LockAttribution(context.Context, referral.PartnerState, referral.Link, referral.Eligibility) (referral.Referral, error)
	AssignAttribution(context.Context, referral.CorrectionRequest) (referral.Referral, error)
	FindCorrection(context.Context, string, string, string) (referral.Referral, error)
	GetAttributionSnapshot(context.Context, string) (referral.AttributionSnapshot, error)
	GetReferralForCustomer(context.Context, string) (referral.Referral, error)
	ListByPartner(context.Context, string, int, string) ([]referral.Referral, error)
	ListRelationships(context.Context, string, referral.RelationshipQuery) (referral.RelationshipPage, error)
}
type EarningsService interface {
	AcceptedAccrual(context.Context, string, string) (partnerearnings.Entry, error)
	Dispute(context.Context, partnerearnings.DisputeRequest) (partnerearnings.DisputeResult, error)
	RecordReturnedTransfer(context.Context, partnerearnings.ReturnRequest) (partnerearnings.ReturnResult, error)
	Accrue(context.Context, partnerearnings.AccrualRequest) (partnerearnings.Entry, error)
	Reverse(context.Context, partnerearnings.ReversalRequest) ([]partnerearnings.Entry, error)
	Mature(context.Context, string) ([]partnerearnings.Entry, error)
	Balances(context.Context, string) (partnerearnings.Balances, error)
	ListJournal(context.Context, string) ([]partnerearnings.Entry, error)
	GetStatement(context.Context, string, partnerearnings.StatementQuery) (partnerearnings.Statement, error)
	GetPaymentReport(context.Context, string, partnerearnings.PaymentQuery) (partnerearnings.PaymentReport, error)
	GetReferralAmounts(context.Context, string, partnerearnings.ReferralAmountQuery) (partnerearnings.ReferralAmountReport, error)
	GetFinancialMetrics(context.Context, string, partnerearnings.FinancialMetricsQuery) (partnerearnings.FinancialMetrics, error)
	RequestClaim(context.Context, partnerearnings.ClaimRequest) (partnerearnings.Claim, error)
	FindClaimRequest(context.Context, string, string, string) (partnerearnings.Claim, error)
	GetClaim(context.Context, string) (partnerearnings.Claim, error)
	ListClaims(context.Context, string, []string, int, string) ([]partnerearnings.Claim, error)
	DecideClaim(context.Context, partnerearnings.ClaimDecision) (partnerearnings.Claim, error)
	RecordPayment(context.Context, partnerearnings.RecordPaymentRequest) (partnerearnings.Claim, error)
	AmendPayment(context.Context, partnerearnings.AmendPaymentRequest) (partnerearnings.Claim, error)
}

// RevenueFacts is the owning billing service read capability, not a repository.
type RevenueFacts interface {
	GetRevenueFact(context.Context, string) (billing.RevenueFact, error)
}
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
	// Claims sets explicit admission for new withdrawals; receipts recover first.
	Claims ClaimsConfig
	// WorkReporting is optional. Missing wiring disables only the backlog read.
	WorkReporting WorkBacklogService
}
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
	return &Manager{deps: deps}, nil
}
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
func referralState(p partnerprogram.Partner) referral.PartnerState {
	return referral.PartnerState{PartnerID: p.ID, CustomerID: p.CustomerID, CanAcquireReferrals: p.CanAcquireReferrals}
}
func snapshot(t partnerprogram.EffectiveTerms) referral.TermsSnapshot {
	return referral.TermsSnapshot{RateBasisPoints: t.RateBasisPoints, HoldDurationDays: int(t.HoldDuration / (24 * time.Hour)), Currency: t.Currency, CurrencyExponent: t.CurrencyExponent, TermsVersion: t.TermsVersion, PolicyVersionIDs: append([]string(nil), t.PolicyVersionIDs...), EligiblePlanIDs: append([]string{}, t.EligiblePlanIDs...), RecurrenceEndsAt: t.RecurrenceEndsAt}
}
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
	return m.deps.Program.Enroll(ctx, partnerprogram.EnrollRequest{CustomerID: actor, AcceptedTermsVersion: acceptedTerms})
}

type Overview struct {
	Partner     partnerprogram.Partner        `json:"partner"`
	Terms       partnerprogram.EffectiveTerms `json:"terms"`
	Balances    partnerearnings.Balances      `json:"balances"`
	Destination *partnerprogram.Destination   `json:"destination,omitempty"`
	AsOf        time.Time                     `json:"as_of"`
}

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
func (m *Manager) GetOrCreateLink(ctx context.Context, actor string) (referral.Link, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return referral.Link{}, err
	}
	if !m.deps.Controls.Attribution {
		return referral.Link{}, ErrDenied
	}
	return m.deps.Referral.IssueLink(ctx, referralState(p))
}
func (m *Manager) RotateLink(ctx context.Context, actor, reason string) (referral.Link, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return referral.Link{}, err
	}
	if !m.deps.Controls.Attribution {
		return referral.Link{}, ErrDenied
	}
	l, err := m.deps.Referral.IssueLink(ctx, referralState(p))
	if err != nil {
		return referral.Link{}, err
	}
	if err := m.deps.Referral.RetireLink(ctx, l.ID, reason, actor); err != nil {
		return referral.Link{}, err
	}
	return m.deps.Referral.IssueLink(ctx, referralState(p))
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
	terms, err := m.terms(ctx, owner, fact.CreatedAt)
	if err != nil {
		return referral.Referral{}, err
	}
	return m.deps.Referral.LockAttribution(ctx, referralState(owner), link, referral.Eligibility{ReferredCustomer: fact.CustomerID, SignupID: fact.ID, IsIndividual: fact.Individual, At: fact.CreatedAt, Evidence: evidence, Terms: snapshot(terms)})
}
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
func (m *Manager) AdminDecideClaim(ctx context.Context, req partnerearnings.ClaimDecision) (partnerearnings.Claim, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityProcessing, req.ClaimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if req.ExpectedRevision < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	return m.deps.Earnings.DecideClaim(ctx, req)
}
func (m *Manager) AdminRecordPayment(ctx context.Context, req partnerearnings.RecordPaymentRequest) (partnerearnings.Claim, error) {
	if !m.deps.Controls.ManualRecording {
		return partnerearnings.Claim{}, ErrDenied
	}
	if err := m.authorize(ctx, req.ActorID, CapabilityRecordPayment, req.ClaimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if req.ExpectedRevision < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	return m.deps.Earnings.RecordPayment(ctx, req)
}
func (m *Manager) AdminAmendPayment(ctx context.Context, req partnerearnings.AmendPaymentRequest) (partnerearnings.Claim, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityAmendPayment, req.ClaimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if req.ExpectedRevision < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	return m.deps.Earnings.AmendPayment(ctx, req)
}
func (m *Manager) AdminPublishPolicy(ctx context.Context, req partnerprogram.PublishPolicyRequest) (partnerprogram.PolicyVersion, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityPolicy, req.Draft.PartnerCustomer); err != nil {
		return partnerprogram.PolicyVersion{}, err
	}
	return m.deps.Program.PublishPolicy(ctx, req)
}
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
