package partnermanager

import (
	"context"
	"reflect"
	"sort"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

type conversionEvidenceService interface {
	GetConversionEvidence(context.Context, string, referral.AnalyticsQuery) (referral.ConversionEvidence, error)
}

// ConversionFraction is an observed retained-evidence fraction. Zero denominator
// or missing measured origin withholds ObservedRate; zero is not an unknown rate.
// SourceCoverage separately limits unreceived/unassigned provider deliveries.
type ConversionFraction struct {
	ConfirmedConverted int64    `json:"confirmed_converted"`
	Denominator        int64    `json:"denominator"`
	NetPositive        int64    `json:"net_positive"`
	ObservedRate       *float64 `json:"observed_rate"`
}

type ConversionCohorts struct {
	MeasuredSignups   ConversionFraction `json:"measured_signups"`
	UnmeasuredSignups ConversionFraction `json:"unmeasured_signups"`
	ManualInitial     ConversionFraction `json:"manual_initial_acquisitions"`
	CorrectionEvents  ConversionFraction `json:"correction_acquisition_events"`
	MeasuredVisits    ConversionFraction `json:"measured_visits"`
}

type LinkConversions struct {
	LinkID  string            `json:"link_id"`
	Code    string            `json:"code"`
	Retired bool              `json:"retired"`
	Cohorts ConversionCohorts `json:"cohorts"`
}

// ConversionReport uses ORIGINAL signup/acquisition/visit cohort dates [From,To),
// never payment dates. Eligible payments after To still convert that cohort;
// later refunds preserve historical conversion while NetPositive can fall.
// Global totals precede link pagination; manual events are not click conversions.
type ConversionReport struct {
	Scope               string             `json:"scope"`
	From                *time.Time         `json:"from,omitempty"`
	To                  *time.Time         `json:"to,omitempty"`
	Cohorts             ConversionCohorts  `json:"cohorts"`
	Traffic             referral.Analytics `json:"traffic"`
	AttributionAsOf     time.Time          `json:"attribution_as_of"`
	AttributionRevision string             `json:"attribution_revision"`
	CombinedRevision    string             `json:"combined_revision"`
	SourceAsOf          *time.Time         `json:"source_as_of,omitempty"`
	SourceRevision      string             `json:"source_revision,omitempty"`
	SourceCoverage      string             `json:"source_coverage"`
	UpstreamCoverage    string             `json:"upstream_delivery_coverage"`
	Links               []LinkConversions  `json:"links"`
}

// PlanConversions is private administrator domain output. Each plan's automatic
// denominator includes ALL selected-cohort automatic signups, not signup-plan
// membership. A person can appear under multiple plans; never sum plan people.
type PlanConversions struct {
	PlanID           string             `json:"-"`
	AutomaticSignups ConversionFraction `json:"-"`
	ManualInitial    ConversionFraction `json:"-"`
	CorrectionEvents ConversionFraction `json:"-"`
}

// AdminConversionReport requires a host projection of permitted catalogue labels.
// Customer JSON never receives raw billing plan IDs or these overlapping rows.
type AdminConversionReport struct {
	Summary          ConversionReport  `json:"-"`
	Plans            []PlanConversions `json:"-"`
	MultiPlanSignups int               `json:"-"`
	HasMorePlans     bool              `json:"-"`
	NextAfterPlanID  string            `json:"-"`
}

// AdminConversionQuery pages links and plans independently. Neither cursor
// changes global cohort denominators or turns payment time into signup time.
type AdminConversionQuery struct {
	referral.AnalyticsQuery
	PlanLimit   int
	AfterPlanID string
}

type convertedPeriod struct {
	paid, net bool
	plans     map[string]bool
	netPlans  map[string]bool
}

func conversionPeriod(at time.Time, q referral.AnalyticsQuery) bool {
	return (q.From == nil || !at.Before(*q.From)) && (q.To == nil || at.Before(*q.To))
}

func finishFraction(f *ConversionFraction, complete bool) error {
	if f.Denominator < 0 || f.ConfirmedConverted < 0 || f.NetPositive < 0 || f.NetPositive > f.ConfirmedConverted || f.ConfirmedConverted > f.Denominator {
		return ErrUnavailable
	}
	if complete && f.Denominator > 0 {
		rate := float64(f.ConfirmedConverted) / float64(f.Denominator)
		f.ObservedRate = &rate
	}
	return nil
}

func finishCohorts(c *ConversionCohorts, originsComplete bool) error {
	for _, f := range []*ConversionFraction{&c.MeasuredSignups, &c.UnmeasuredSignups, &c.ManualInitial, &c.CorrectionEvents} {
		if err := finishFraction(f, true); err != nil {
			return err
		}
	}
	return finishFraction(&c.MeasuredVisits, originsComplete)
}

func convertedCount(f *ConversionFraction, period convertedPeriod) {
	if period.paid {
		f.ConfirmedConverted++
	}
	if period.net {
		f.NetPositive++
	}
}

func (m *Manager) conversionReport(ctx context.Context, partner string, q referral.AnalyticsQuery) (AdminConversionReport, error) {
	owner, ok := m.deps.Referral.(conversionEvidenceService)
	source, sourceOK := m.deps.Revenue.(revenueReportingSource)
	if !ok || nilManagerDependency(owner) || !sourceOK || nilManagerDependency(source) || m.revenueReporting == nil {
		return AdminConversionReport{}, ErrUnavailable
	}
	evidence, err := owner.GetConversionEvidence(ctx, partner, q)
	if err != nil {
		return AdminConversionReport{}, err
	}
	a, relationships := evidence.Analytics, evidence.Relationships
	if a.ProgramID != referral.ProgramID || a.PartnerID != partner || a.Validate(q) != nil || relationships.ProgramID != referral.ProgramID || relationships.PartnerID != partner || relationships.Revision == "" || evidence.Revision == "" || relationships.Items == nil || evidence.Links == nil || !relationships.AsOf.Equal(a.AsOf) || a.LifetimeRelationships != len(relationships.Items) {
		return AdminConversionReport{}, ErrUnavailable
	}
	out := AdminConversionReport{Summary: ConversionReport{Scope: "complete_original_cohorts", From: q.From, To: q.To, Traffic: a, AttributionAsOf: relationships.AsOf, AttributionRevision: relationships.Revision, CombinedRevision: evidence.Revision, SourceCoverage: "no_partner_relationships", UpstreamCoverage: "unreceived_and_unassigned_deliveries_not_assessed", Links: []LinkConversions{}}, Plans: []PlanConversions{}}
	links, codes := map[string]*LinkConversions{}, map[string]string{}
	for _, link := range evidence.Links {
		if link.ProgramID != referral.ProgramID || link.PartnerID != partner || link.ID == "" || link.Code == "" || links[link.ID] != nil || codes[link.Code] != "" {
			return AdminConversionReport{}, ErrUnavailable
		}
		links[link.ID] = &LinkConversions{LinkID: link.ID, Code: link.Code, Retired: link.RetiredAt != nil}
		codes[link.Code] = link.ID
	}
	query := billing.RevenueHistoryQuery{Scopes: append([]billing.RevenueScope(nil), m.revenueReporting.Scopes...), Principals: []string{}}
	scopes := map[billing.RevenueScope]bool{}
	for _, scope := range query.Scopes {
		scopes[scope] = true
	}
	byPrincipal := map[string]referral.RelationshipEvidenceItem{}
	bindings := map[string]map[string]referral.PaymentAttribution{}
	periods := map[string]referral.OwnershipPeriod{}
	for _, item := range relationships.Items {
		r := item.Relationship
		if r.ProgramID != referral.ProgramID || r.PartnerID != partner || r.ID != referral.RelationshipReferenceID(referral.ProgramID, partner, r.ReferredCustomer) || r.ReferredCustomer != item.Attribution.ReferredCustomer || item.Attribution.Fingerprint == "" || byPrincipal[r.ReferredCustomer].Relationship.ID != "" {
			return AdminConversionReport{}, ErrUnavailable
		}
		byPrincipal[r.ReferredCustomer] = item
		query.Principals = append(query.Principals, r.ReferredCustomer)
		bindings[r.ReferredCustomer] = map[string]referral.PaymentAttribution{}
		for _, b := range item.Attribution.Bindings {
			if b.ProgramID != referral.ProgramID || b.ReferredCustomer != r.ReferredCustomer || b.PaymentID == "" || bindings[r.ReferredCustomer][b.PaymentID].PaymentID != "" {
				return AdminConversionReport{}, ErrUnavailable
			}
			bindings[r.ReferredCustomer][b.PaymentID] = b
		}
		for _, p := range r.Periods {
			if periods[p.ReferralID].ReferralID != "" {
				return AdminConversionReport{}, ErrUnavailable
			}
			periods[p.ReferralID] = p
		}
	}
	converted := map[string]convertedPeriod{}
	if len(query.Principals) > 0 {
		history, err := source.GetPaymentRevenueHistory(ctx, query)
		if err != nil {
			return AdminConversionReport{}, err
		}
		if history.Items == nil || history.AsOf.IsZero() || history.Revision == "" || history.AcceptanceSequence < 0 || len(history.Items) > billing.RevenueHistoryCapacity || history.ScopedUnresolvedSources < 0 {
			return AdminConversionReport{}, ErrUnavailable
		}
		out.Summary.SourceAsOf, out.Summary.SourceRevision = &history.AsOf, history.Revision
		out.Summary.SourceCoverage = "confirmed_accepted_history"
		seen := map[string]bool{}
		for _, payment := range history.Items {
			if err := ctx.Err(); err != nil {
				return AdminConversionReport{}, err
			}
			f := payment.Original
			if byPrincipal[f.PrincipalID].Relationship.ID == "" || !scopes[f.Scope] || payment.Validate() != nil || seen[f.ID] {
				return AdminConversionReport{}, ErrUnavailable
			}
			seen[f.ID] = true
			b, exists := bindings[f.PrincipalID][f.ID]
			if !exists || b.PartnerID != partner {
				continue // unbound and other-owner revenue cannot establish conversion
			}
			period, exists := periods[b.ReferralID]
			if !exists || b.ReferredCustomer != f.PrincipalID || !b.EffectiveAt.Equal(f.EffectiveAt) || !reflect.DeepEqual(b.Terms, period.Terms) {
				return AdminConversionReport{}, ErrUnavailable
			}
			if !eligiblePaidBinding(f, b) {
				continue
			}
			value := converted[b.ReferralID]
			if value.plans == nil {
				value.plans, value.netPlans = map[string]bool{}, map[string]bool{}
			}
			value.paid, value.plans[f.PlanID] = true, true
			if payment.NetMinor > 0 {
				value.net, value.netPlans[f.PlanID] = true, true
			}
			converted[b.ReferralID] = value
		}
	}
	planRows := map[string]*PlanConversions{}
	paidVisits, netVisits := map[string]string{}, map[string]bool{}
	multiPlanPeople := map[string]bool{}
	for _, item := range relationships.Items {
		for _, revision := range item.Attribution.History {
			if revision.PartnerID != partner {
				continue
			}
			value := converted[revision.ID]
			kind := "correction"
			if revision.Revision == 1 {
				if revision.SourceKind == "admin" {
					kind = "manual"
				} else if revision.SourceKind == "click" {
					kind = "unmeasured"
					if revision.SourceMeasuredClickID != "" {
						kind = "measured"
					}
				} else {
					return AdminConversionReport{}, ErrUnavailable
				}
			}
			linkID := codes[revision.SourceCode]
			if (kind == "measured" || kind == "unmeasured") && linkID == "" {
				return AdminConversionReport{}, ErrUnavailable
			}
			if conversionPeriod(revision.LockedAt, q) {
				switch kind {
				case "measured":
					convertedCount(&out.Summary.Cohorts.MeasuredSignups, value)
					convertedCount(&links[linkID].Cohorts.MeasuredSignups, value)
				case "unmeasured":
					convertedCount(&out.Summary.Cohorts.UnmeasuredSignups, value)
					convertedCount(&links[linkID].Cohorts.UnmeasuredSignups, value)
				case "manual":
					convertedCount(&out.Summary.Cohorts.ManualInitial, value)
				case "correction":
					convertedCount(&out.Summary.Cohorts.CorrectionEvents, value)
				}
				if (kind == "measured" || kind == "unmeasured") && len(value.plans) > 1 {
					multiPlanPeople[item.Relationship.ID] = true
				}
				for plan := range value.plans {
					row := planRows[plan]
					if row == nil {
						row = &PlanConversions{PlanID: plan}
						planRows[plan] = row
					}
					v := convertedPeriod{paid: true, net: value.netPlans[plan]}
					switch kind {
					case "measured", "unmeasured":
						convertedCount(&row.AutomaticSignups, v)
					case "manual":
						convertedCount(&row.ManualInitial, v)
					case "correction":
						convertedCount(&row.CorrectionEvents, v)
					}
				}
			}
			// Visit cohort time differs from signup time. One frozen measured
			// origin may create multiple accounts but converts only one visit.
			if kind == "measured" && revision.SourceMeasuredOccurredAt != nil && conversionPeriod(*revision.SourceMeasuredOccurredAt, q) && value.paid {
				origin := revision.SourceMeasuredClickID
				if previous := paidVisits[origin]; previous != "" && previous != linkID {
					return AdminConversionReport{}, ErrUnavailable
				}
				paidVisits[origin] = linkID
				if value.net {
					netVisits[origin] = true
				}
			}
		}
	}
	out.Summary.Cohorts.MeasuredVisits.ConfirmedConverted = int64(len(paidVisits))
	out.Summary.Cohorts.MeasuredVisits.NetPositive = int64(len(netVisits))
	for origin, linkID := range paidVisits {
		links[linkID].Cohorts.MeasuredVisits.ConfirmedConverted++
		if netVisits[origin] {
			links[linkID].Cohorts.MeasuredVisits.NetPositive++
		}
	}
	c := &out.Summary.Cohorts
	c.MeasuredSignups.Denominator, c.UnmeasuredSignups.Denominator = a.Signups.MeasuredSignups, a.Signups.UnmeasuredSignups
	c.ManualInitial.Denominator, c.CorrectionEvents.Denominator = a.Signups.ManualInitialAcquisitions, a.Signups.CorrectionAcquisitionEvents
	c.MeasuredVisits.Denominator = a.Visits.EligibleMeasuredVisits
	completeOrigins := a.Coverage.MissingMeasuredSignupOrigins == 0
	if err := finishCohorts(c, completeOrigins); err != nil {
		return AdminConversionReport{}, err
	}
	for _, traffic := range a.Links {
		row := links[traffic.LinkID]
		if row == nil || row.Code != traffic.Code {
			return AdminConversionReport{}, ErrUnavailable
		}
		row.Cohorts.MeasuredSignups.Denominator = traffic.Signups.MeasuredSignups
		row.Cohorts.UnmeasuredSignups.Denominator = traffic.Signups.UnmeasuredSignups
		row.Cohorts.MeasuredVisits.Denominator = traffic.Visits.EligibleMeasuredVisits
		if err := finishCohorts(&row.Cohorts, completeOrigins); err != nil {
			return AdminConversionReport{}, err
		}
		out.Summary.Links = append(out.Summary.Links, *row)
	}
	for _, row := range planRows {
		row.AutomaticSignups.Denominator = c.MeasuredSignups.Denominator + c.UnmeasuredSignups.Denominator
		row.ManualInitial.Denominator, row.CorrectionEvents.Denominator = c.ManualInitial.Denominator, c.CorrectionEvents.Denominator
		for _, f := range []*ConversionFraction{&row.AutomaticSignups, &row.ManualInitial, &row.CorrectionEvents} {
			if err := finishFraction(f, true); err != nil {
				return AdminConversionReport{}, err
			}
		}
		out.Plans = append(out.Plans, *row)
	}
	out.MultiPlanSignups = len(multiPlanPeople)
	sort.Slice(out.Plans, func(i, j int) bool { return out.Plans[i].PlanID < out.Plans[j].PlanID })
	confirm, err := owner.GetConversionEvidence(ctx, partner, q)
	if err != nil {
		return AdminConversionReport{}, err
	}
	if confirm.Analytics.ProgramID != a.ProgramID || confirm.Analytics.PartnerID != partner || confirm.Revision != evidence.Revision {
		return AdminConversionReport{}, referral.ErrStaleWrite
	}
	return out, ctx.Err()
}

// ConversionMetrics returns safe original-cohort aggregates for the verified
// current partner. Plan evidence remains administrator-only private output.
func (m *Manager) ConversionMetrics(ctx context.Context, actor string, q referral.AnalyticsQuery) (ConversionReport, error) {
	if q.Validate() != nil {
		return ConversionReport{}, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return ConversionReport{}, err
	}
	if p.ProgramID != partnerprogram.ProgramID {
		return ConversionReport{}, ErrUnavailable
	}
	out, err := m.conversionReport(ctx, p.ID, q)
	if err != nil {
		return ConversionReport{}, err
	}
	current, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return ConversionReport{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return ConversionReport{}, ErrDenied
	}
	return out.Summary, nil
}

// AdminConversionMetrics pages private frozen-plan numerators only after full
// original cohort totals. A plan cursor never narrows the signup denominator.
func (m *Manager) AdminConversionMetrics(ctx context.Context, actor, partner string, q AdminConversionQuery) (AdminConversionReport, error) {
	if !referral.IsCanonicalAnalyticsPartnerID(partner) || q.AnalyticsQuery.Validate() != nil || q.PlanLimit < 1 || q.PlanLimit > 100 || (q.AfterPlanID != "" && !partnerearnings.IsCanonicalReportID(q.AfterPlanID)) {
		return AdminConversionReport{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return AdminConversionReport{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return AdminConversionReport{}, err
	}
	if p.ID != partner || p.ProgramID != partnerprogram.ProgramID {
		return AdminConversionReport{}, ErrUnavailable
	}
	out, err := m.conversionReport(ctx, partner, q.AnalyticsQuery)
	if err != nil {
		return AdminConversionReport{}, err
	}
	page := []PlanConversions{}
	cursorFound := q.AfterPlanID == ""
	for _, row := range out.Plans {
		if row.PlanID == q.AfterPlanID {
			cursorFound = true
		}
		if row.PlanID <= q.AfterPlanID {
			continue
		}
		if len(page) == q.PlanLimit {
			out.HasMorePlans = true
			out.NextAfterPlanID = page[len(page)-1].PlanID
			break
		}
		page = append(page, row)
	}
	if !cursorFound {
		return AdminConversionReport{}, ErrInvalid
	}
	out.Plans = page
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return AdminConversionReport{}, err
	}
	current, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return AdminConversionReport{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return AdminConversionReport{}, ErrDenied
	}
	return out, nil
}
