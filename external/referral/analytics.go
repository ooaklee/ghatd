package referral

import (
	"context"
	"math"
	"sort"
	"time"
)

// AnalyticsCapacity independently bounds complete ownership evidence, day
// buckets and raw boundary rows, including every retained ownership revision. Exceeding it is an explicit error, never partial totals.
const AnalyticsCapacity = 10000

// AnalyticsSnapshot is private repository evidence from one read transaction.
// It contains all selected-partner links, anonymous day counts, requested raw
// boundary days and lifetime memberships with complete heads/histories. It is not a transport.
type AnalyticsSnapshot struct {
	Links         []Link
	Clicks        []Click // complete raw rows for partial UTC boundary days only
	Days          []VisitDay
	Relationships []RelationshipSnapshot
}

// AnalyticsQuery selects the reporting window, page size and link cursor for an
// analytics report. From and To are optional bounds and AfterLinkID pages
// through links.
type AnalyticsQuery struct {
	From, To    *time.Time
	Limit       int
	AfterLinkID string
}

// Validate requires a page limit between 1 and 100, an optional non-zero window
// with To strictly after From, and a bounded non-empty AfterLinkID.
func (q AnalyticsQuery) Validate() error {
	if q.Limit < 1 || q.Limit > 100 || (q.AfterLinkID != "" && !analyticsID(q.AfterLinkID, 128)) || (q.From != nil && q.From.IsZero()) || (q.To != nil && q.To.IsZero()) || (q.From != nil && q.To != nil && !q.To.After(*q.From)) {
		return ErrInvalid
	}
	return nil
}

// VisitMetrics use observation time for [From,To). ConvertedMeasuredVisits
// counts distinct eligible visits with at least one accepted new signup at or
// after that observation, including signups after To. SignupsFromVisitCohort
// may exceed it; neither count means a person, paid conversion or subscriber.
type VisitMetrics struct {
	Observations            int64 `json:"observations"`
	EligibleMeasuredVisits  int64 `json:"eligible_measured_visits"`
	DuplicateObservations   int64 `json:"duplicate_observations"`
	KnownBotObservations    int64 `json:"known_bot_observations"`
	UnmeasuredObservations  int64 `json:"unmeasured_observations"`
	ConvertedMeasuredVisits int64 `json:"converted_measured_visits"`
	SignupsFromVisitCohort  int64 `json:"signups_from_visit_cohort"`
}

// SignupMetrics use original immutable signup time, independently of the visit
// cohort. Manual initial acquisitions and prospective correction events use
// their own cutover time and never become click conversions.
type SignupMetrics struct {
	MeasuredSignups             int64 `json:"measured_signups"`
	UnmeasuredSignups           int64 `json:"unmeasured_signups"`
	ManualInitialAcquisitions   int64 `json:"manual_initial_acquisitions"`
	CorrectionAcquisitionEvents int64 `json:"correction_acquisition_events"`
}

// LinkAnalytics carries one selected link's identity, retirement state and
// visit/signup metrics for the report window.
type LinkAnalytics struct {
	LinkID  string        `json:"link_id"`
	Code    string        `json:"code"`
	Retired bool          `json:"retired"`
	Visits  VisitMetrics  `json:"visits"`
	Signups SignupMetrics `json:"signups"`
}

// AnalyticsCoverage describes only retained owning evidence. It does not
// certify all traffic, human identity, source feed, subscriptions or payments.
// Missing origins stay explicit; they are never inferred from current heads.
type AnalyticsCoverage struct {
	Scope                        string `json:"scope"`
	LegacyUnmeasuredObservations int64  `json:"legacy_unmeasured_observations"`
	UnmeasuredSignupOrigins      int64  `json:"unmeasured_signup_origins"`
	MissingMeasuredSignupOrigins int64  `json:"missing_measured_signup_origins"`
}

// Analytics is the derived report for one partner and window: totals,
// relationship counts, coverage limits and a paged link breakdown. AsOf is
// report generation time after the owning snapshot read, not an identity or
// billing watermark.
type Analytics struct {
	ProgramID string     `json:"program_id"`
	PartnerID string     `json:"partner_id"`
	From      *time.Time `json:"from,omitempty"`
	To        *time.Time `json:"to,omitempty"`
	// AsOf is report generation time after the owning snapshot read, not an
	// identity/billing source watermark or multi-owner transaction boundary.
	AsOf                  time.Time         `json:"as_of"`
	Revision              string            `json:"revision"`
	Visits                VisitMetrics      `json:"visits"`
	Signups               SignupMetrics     `json:"signups"`
	LifetimeRelationships int               `json:"lifetime_relationships"`
	CurrentRelationships  int               `json:"current_relationships"`
	RetainedRelationships int               `json:"retained_relationships"`
	Coverage              AnalyticsCoverage `json:"coverage"`
	Links                 []LinkAnalytics   `json:"links"`
	HasMore               bool              `json:"has_more"`
	NextAfterLinkID       string            `json:"next_after_link_id,omitempty"`
}

// analyticsPeriod reports whether at falls in the half-open [From, To) window,
// treating nil bounds as unbounded.
func analyticsPeriod(at time.Time, q AnalyticsQuery) bool {
	return (q.From == nil || !at.Before(*q.From)) && (q.To == nil || at.Before(*q.To))
}

// measuredOrigin identifies the link and observation time of one eligible
// measured visit.
type measuredOrigin struct {
	LinkID string
	At     time.Time
}

// visitDayIdentity keys per-link day aggregation by link ID and UTC date
// string.
type visitDayIdentity struct {
	Link string
	Day  string
}

// dayIdentity returns the link and UTC day string for a timestamp.
func dayIdentity(link string, at time.Time) visitDayIdentity {
	return visitDayIdentity{link, utcDay(at).Format("2006-01-02")}
}

// observationMetric returns single-observation metrics classifying a click as
// eligible, duplicate, known bot or unmeasured based on its stored
// classification.
func observationMetric(c Click) VisitMetrics {
	v := VisitMetrics{Observations: 1}
	switch c.Classification {
	case VisitEligible:
		v.EligibleMeasuredVisits = 1
	case VisitDuplicate:
		v.DuplicateObservations = 1
	case VisitKnownBot:
		v.KnownBotObservations = 1
	case "":
		v.UnmeasuredObservations = 1
	}
	return v
}

// A boundary can be exact only when all raw observations for its UTC day are
// still present in the same snapshot as its persistent classification counts.
func applyObservationDays(ctx context.Context, snapshot AnalyticsSnapshot, links map[string]Link, rows map[string]*LinkAnalytics, q AnalyticsQuery, at time.Time, coverage *AnalyticsCoverage) error {
	partial := map[string]bool{}
	for _, day := range q.PartialDays() {
		partial[day.Format("2006-01-02")] = true
	}
	rawCounts, selected := map[visitDayIdentity]VisitMetrics{}, map[visitDayIdentity]VisitMetrics{}
	for _, c := range snapshot.Clicks {
		key := dayIdentity(c.LinkID, c.OccurredAt)
		if !partial[key.Day] {
			continue
		}
		v := rawCounts[key]
		if !addVisitMetrics(&v, observationMetric(c)) {
			return ErrCapacity
		}
		rawCounts[key] = v
		if analyticsPeriod(c.OccurredAt, q) {
			v = selected[key]
			if !addVisitMetrics(&v, observationMetric(c)) {
				return ErrCapacity
			}
			selected[key] = v
		}
	}
	seen := map[visitDayIdentity]bool{}
	for _, day := range snapshot.Days {
		if err := ctx.Err(); err != nil {
			return err
		}
		link, ok := links[day.LinkID]
		key := dayIdentity(day.LinkID, day.Day)
		if !ok || day.Validate() != nil || day.Day.Before(utcDay(link.CreatedAt)) || day.Day.After(utcDay(at)) || seen[key] {
			return ErrUnavailable
		}
		seen[key] = true
		if coverage.LegacyUnmeasuredObservations > math.MaxInt64-day.Counts.UnmeasuredObservations {
			return ErrCapacity
		}
		coverage.LegacyUnmeasuredObservations += day.Counts.UnmeasuredObservations
		if (q.From != nil && !day.Day.Add(24*time.Hour).After(*q.From)) || (q.To != nil && !day.Day.Before(*q.To)) {
			continue
		}
		v := day.Counts
		if partial[key.Day] {
			if rawCounts[key] != day.Counts {
				return ErrGranularity
			}
			v = selected[key]
		}
		if !addVisitMetrics(&rows[day.LinkID].Visits, v) {
			return ErrCapacity
		}
	}
	for key := range rawCounts {
		if !seen[key] {
			return ErrUnavailable
		}
	}
	return nil
}

// GetAnalytics derives full totals before paging links. Every input is from
// one owning read snapshot; corrections/reacquisition never inflate distinct
// lifetime memberships or move original signup conversions to another owner.
// Unmeasured observations never become measured conversions. Analytics are never money or
// signup admission authority. Cancellation, capacity and malformed evidence
// discard the entire report; unavailable must not be displayed as zero.
func (s *Service) GetAnalytics(ctx context.Context, partner string, q AnalyticsQuery) (Analytics, error) {
	if err := s.ready(ctx); err != nil {
		return Analytics{}, err
	}
	if !analyticsID(partner, 256) || q.Validate() != nil {
		return Analytics{}, ErrInvalid
	}
	snapshot, err := s.repo.ReadAnalyticsSnapshot(ctx, ProgramID, partner, q)
	if err != nil {
		return Analytics{}, err
	}
	return analyticsFromSnapshot(ctx, partner, q, snapshot, s.clock.Now().UTC())
}

// analyticsFromSnapshot preserves the canonical traffic revision and projects
// only validated owning counts. Conversion reads reuse the SAME snapshot.
func analyticsFromSnapshot(ctx context.Context, partner string, q AnalyticsQuery, snapshot AnalyticsSnapshot, at time.Time) (Analytics, error) {
	snapshot.Links = append([]Link(nil), snapshot.Links...)
	snapshot.Clicks = append([]Click(nil), snapshot.Clicks...)
	snapshot.Relationships = append([]RelationshipSnapshot(nil), snapshot.Relationships...)
	snapshot.Days = append([]VisitDay(nil), snapshot.Days...)
	if len(snapshot.Days) > AnalyticsCapacity || len(snapshot.Clicks) > AnalyticsCapacity {
		return Analytics{}, ErrCapacity
	}
	capacity := len(snapshot.Links) + len(snapshot.Relationships)
	if capacity > AnalyticsCapacity {
		return Analytics{}, ErrCapacity
	}
	for _, r := range snapshot.Relationships {
		capacity += len(r.History)
		if capacity > AnalyticsCapacity {
			return Analytics{}, ErrCapacity
		}
	}
	if at.IsZero() {
		return Analytics{}, ErrUnavailable
	}
	out := Analytics{ProgramID: ProgramID, PartnerID: partner, From: q.From, To: q.To, AsOf: at, Coverage: AnalyticsCoverage{Scope: "anonymous_daily_observations"}, Links: []LinkAnalytics{}}
	links := map[string]Link{}
	byCode := map[string]*LinkAnalytics{}
	byID := map[string]*LinkAnalytics{}
	for _, l := range snapshot.Links {
		if l.ProgramID != ProgramID || l.PartnerID != partner || !analyticsID(l.ID, 128) || !analyticsID(l.Code, 128) || l.CreatedAt.IsZero() || l.CreatedAt.After(at) || (l.RetiredAt != nil && (l.RetiredAt.Before(l.CreatedAt) || l.RetiredAt.After(at))) || links[l.ID].ID != "" || byCode[l.Code] != nil {
			return Analytics{}, ErrUnavailable
		}
		links[l.ID] = l
		row := &LinkAnalytics{LinkID: l.ID, Code: l.Code, Retired: l.RetiredAt != nil}
		byID[l.ID], byCode[l.Code] = row, row
	}
	if q.AfterLinkID != "" && byID[q.AfterLinkID] == nil {
		return Analytics{}, ErrInvalid
	}
	clicks := map[string]Click{}
	eligible := map[string]Click{}
	for _, c := range snapshot.Clicks {
		if err := ctx.Err(); err != nil {
			return Analytics{}, err
		}
		l, ok := links[c.LinkID]
		if !ok || !analyticsID(c.ID, 256) || c.Code != l.Code || c.OccurredAt.IsZero() || c.OccurredAt.Before(l.CreatedAt) || c.OccurredAt.After(at) || clicks[c.ID].ID != "" || len(c.UserAgent) > 512 {
			return Analytics{}, ErrUnavailable
		}
		clicks[c.ID] = c
		switch c.Classification {
		case VisitEligible:
			if c.MeasuredClickID != c.ID || !validCorrectionFingerprint(c.VisitDigest) || c.UserAgent != "" {
				return Analytics{}, ErrUnavailable
			}
			eligible[c.ID] = c
		case VisitDuplicate:
			if !analyticsID(c.MeasuredClickID, 256) || c.ID == c.MeasuredClickID || !validCorrectionFingerprint(c.VisitDigest) || c.UserAgent != "" {
				return Analytics{}, ErrUnavailable
			}
		case VisitKnownBot:
			if c.MeasuredClickID != "" || c.VisitDigest != "" || c.UserAgent != "" {
				return Analytics{}, ErrUnavailable
			}
		case "":
			if c.MeasuredClickID != "" || c.VisitDigest != "" || c.UserAgent != "" {
				return Analytics{}, ErrUnavailable
			}
			// Unmeasured coverage is derived from persistent day counts below.
		default:
			return Analytics{}, ErrUnavailable
		}
	}
	for _, c := range snapshot.Clicks {
		if c.Classification == VisitDuplicate {
			first, ok := eligible[c.MeasuredClickID]
			if !ok {
				continue // the first observation may be outside the requested raw boundary days
			}
			if first.LinkID != c.LinkID || first.VisitDigest != c.VisitDigest || first.OccurredAt.After(c.OccurredAt) {
				return Analytics{}, ErrUnavailable
			}
		}
	}
	if err := applyObservationDays(ctx, snapshot, links, byID, q, at, &out.Coverage); err != nil {
		return Analytics{}, err
	}

	converted := map[string]bool{}
	origins := map[string]measuredOrigin{}
	members := map[string]bool{}
	for _, member := range snapshot.Relationships {
		if err := ctx.Err(); err != nil {
			return Analytics{}, err
		}
		if member.ProgramID != ProgramID || member.PartnerID != partner || !analyticsID(member.ReferredCustomer, 256) || member.ID != RelationshipReferenceID(ProgramID, partner, member.ReferredCustomer) || members[member.ID] {
			return Analytics{}, ErrUnavailable
		}
		members[member.ID] = true
		history, err := validatedOwnershipHistory(member.ReferredCustomer, member.Head, member.History)
		if err != nil || len(history) == 0 {
			return Analytics{}, ErrUnavailable
		}
		firstOwned := ""
		for i, r := range history {
			if r.LockedAt.After(at) || r.PostedAt.After(at) || (i > 0 && (r.SourceKind != "admin" || r.SourceMeasuredClickID != "" || r.SourceMeasuredOccurredAt != nil)) {
				return Analytics{}, ErrUnavailable
			}
			if r.PartnerID != partner {
				continue
			}
			if firstOwned == "" {
				firstOwned = r.ID
			}
			if i > 0 {
				if analyticsPeriod(r.LockedAt, q) {
					out.Signups.CorrectionAcquisitionEvents++
				}
				continue
			}
			if r.SourceKind == "admin" {
				if r.SourceMeasuredClickID != "" || r.SourceMeasuredOccurredAt != nil {
					return Analytics{}, ErrUnavailable
				}
				if analyticsPeriod(r.LockedAt, q) {
					out.Signups.ManualInitialAcquisitions++
				}
				continue
			}
			if r.SourceKind != "click" || r.SignupID == "" || r.EvidenceDigest == "" || byCode[r.SourceCode] == nil {
				return Analytics{}, ErrUnavailable
			}
			linkRow := byCode[r.SourceCode]
			if r.SourceMeasuredClickID == "" {
				if r.SourceMeasuredOccurredAt != nil {
					return Analytics{}, ErrUnavailable
				}
				out.Coverage.UnmeasuredSignupOrigins++
				if analyticsPeriod(r.LockedAt, q) {
					out.Signups.UnmeasuredSignups++
					linkRow.Signups.UnmeasuredSignups++
				}
				continue
			}
			if !analyticsID(r.SourceMeasuredClickID, 256) {
				return Analytics{}, ErrUnavailable
			}
			if analyticsPeriod(r.LockedAt, q) {
				out.Signups.MeasuredSignups++
				linkRow.Signups.MeasuredSignups++
			}
			if r.SourceMeasuredOccurredAt == nil {
				out.Coverage.MissingMeasuredSignupOrigins++
				continue
			}
			when := r.SourceMeasuredOccurredAt.UTC()
			link := links[linkRow.LinkID]
			if when.IsZero() || when.Before(link.CreatedAt) || when.After(r.LockedAt) || when.After(at) {
				return Analytics{}, ErrUnavailable
			}
			origin := measuredOrigin{LinkID: link.ID, At: when}
			if old, ok := origins[r.SourceMeasuredClickID]; ok && (old.LinkID != origin.LinkID || !old.At.Equal(origin.At)) {
				return Analytics{}, ErrUnavailable
			}
			origins[r.SourceMeasuredClickID] = origin
			if click, ok := eligible[r.SourceMeasuredClickID]; ok && (click.LinkID != link.ID || !click.OccurredAt.Equal(when)) {
				return Analytics{}, ErrUnavailable
			}
			if analyticsPeriod(when, q) {
				linkRow.Visits.SignupsFromVisitCohort++
				if !converted[r.SourceMeasuredClickID] {
					converted[r.SourceMeasuredClickID] = true
					linkRow.Visits.ConvertedMeasuredVisits++
				}
			}
		}
		if firstOwned == "" || firstOwned != member.FirstReferralID {
			return Analytics{}, ErrUnavailable
		}
		out.LifetimeRelationships++
		if member.Head.PartnerID == partner {
			out.CurrentRelationships++
		} else {
			out.RetainedRelationships++
		}
	}
	if out.Coverage.MissingMeasuredSignupOrigins > 0 {
		out.Coverage.Scope = "incomplete_measured_origins"
	}
	all := make([]LinkAnalytics, 0, len(byID))
	for _, row := range byID {
		all = append(all, *row)
		if !addVisitMetrics(&out.Visits, row.Visits) {
			return Analytics{}, ErrCapacity
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].LinkID < all[j].LinkID })
	for _, row := range all {
		if row.LinkID <= q.AfterLinkID {
			continue
		}
		if len(out.Links) == q.Limit {
			out.HasMore = true
			out.NextAfterLinkID = out.Links[len(out.Links)-1].LinkID
			break
		}
		out.Links = append(out.Links, row)
	}
	// Canonical sorting makes the revision independent of repository order and
	// page cursor, while preserving full owning evidence outside the date filter.
	sort.Slice(snapshot.Links, func(i, j int) bool { return snapshot.Links[i].ID < snapshot.Links[j].ID })
	// Raw rows are query-dependent and expire; they cannot change the persistent
	// owning revision or make identical day reports differ after cleanup.
	snapshot.Clicks = nil
	sort.Slice(snapshot.Days, func(i, j int) bool {
		if snapshot.Days[i].LinkID != snapshot.Days[j].LinkID {
			return snapshot.Days[i].LinkID < snapshot.Days[j].LinkID
		}
		return snapshot.Days[i].Day.Before(snapshot.Days[j].Day)
	})
	sort.Slice(snapshot.Relationships, func(i, j int) bool { return snapshot.Relationships[i].ID < snapshot.Relationships[j].ID })
	for i := range snapshot.Relationships {
		snapshot.Relationships[i].History = append([]Referral(nil), snapshot.Relationships[i].History...)
		sort.Slice(snapshot.Relationships[i].History, func(a, b int) bool {
			return snapshot.Relationships[i].History[a].Revision < snapshot.Relationships[i].History[b].Revision
		})
	}
	var err error
	out.Revision, err = correctionDigest(struct {
		Program, Partner string
		Snapshot         AnalyticsSnapshot
	}{ProgramID, partner, snapshot})
	if err != nil {
		return Analytics{}, err
	}
	if err := ctx.Err(); err != nil {
		return Analytics{}, err
	}
	if err := out.Validate(q); err != nil {
		return Analytics{}, err
	}
	return out, nil
}

// addVisitMetrics accumulates b into a after checking every counter for
// negativity and int64 overflow, returning false without modifying a when the
// sum would not be representable.
func addVisitMetrics(a *VisitMetrics, b VisitMetrics) bool {
	for _, pair := range [][2]int64{{a.Observations, b.Observations}, {a.EligibleMeasuredVisits, b.EligibleMeasuredVisits}, {a.DuplicateObservations, b.DuplicateObservations}, {a.KnownBotObservations, b.KnownBotObservations}, {a.UnmeasuredObservations, b.UnmeasuredObservations}, {a.ConvertedMeasuredVisits, b.ConvertedMeasuredVisits}, {a.SignupsFromVisitCohort, b.SignupsFromVisitCohort}} {
		if pair[0] < 0 || pair[1] < 0 || pair[0] > math.MaxInt64-pair[1] {
			return false
		}
	}
	a.Observations += b.Observations
	a.EligibleMeasuredVisits += b.EligibleMeasuredVisits
	a.DuplicateObservations += b.DuplicateObservations
	a.KnownBotObservations += b.KnownBotObservations
	a.UnmeasuredObservations += b.UnmeasuredObservations
	a.ConvertedMeasuredVisits += b.ConvertedMeasuredVisits
	a.SignupsFromVisitCohort += b.SignupsFromVisitCohort
	return true
}

// IsCanonicalAnalyticsPartnerID is the report-boundary predicate. It never
// trims or aliases another partner's selected identity.
func IsCanonicalAnalyticsPartnerID(id string) bool { return analyticsID(id, 256) }

// validVisitMetrics requires non-negative counters, that the four observation
// classes sum exactly to Observations, that conversions do not exceed eligible
// measured visits and that cohort signups are at least the conversions.
func validVisitMetrics(v VisitMetrics) bool {
	for _, n := range []int64{v.Observations, v.EligibleMeasuredVisits, v.DuplicateObservations, v.KnownBotObservations, v.UnmeasuredObservations, v.ConvertedMeasuredVisits, v.SignupsFromVisitCohort} {
		if n < 0 {
			return false
		}
	}
	total := VisitMetrics{}
	for _, n := range []int64{v.EligibleMeasuredVisits, v.DuplicateObservations, v.KnownBotObservations, v.UnmeasuredObservations} {
		if total.Observations > math.MaxInt64-n {
			return false
		}
		total.Observations += n
	}
	return v.Observations == total.Observations && v.ConvertedMeasuredVisits <= v.EligibleMeasuredVisits && v.SignupsFromVisitCohort >= v.ConvertedMeasuredVisits
}

// visitMetricsWithin reports whether every counter of v is less than or equal
// to the corresponding counter of total.
func visitMetricsWithin(v, total VisitMetrics) bool {
	return v.Observations <= total.Observations && v.EligibleMeasuredVisits <= total.EligibleMeasuredVisits && v.DuplicateObservations <= total.DuplicateObservations && v.KnownBotObservations <= total.KnownBotObservations && v.UnmeasuredObservations <= total.UnmeasuredObservations && v.ConvertedMeasuredVisits <= total.ConvertedMeasuredVisits && v.SignupsFromVisitCohort <= total.SignupsFromVisitCohort
}

// validSignupMetrics requires all signup counters to be non-negative and within
// AnalyticsCapacity.
func validSignupMetrics(v SignupMetrics) bool {
	for _, n := range []int64{v.MeasuredSignups, v.UnmeasuredSignups, v.ManualInitialAcquisitions, v.CorrectionAcquisitionEvents} {
		if n < 0 || n > AnalyticsCapacity {
			return false
		}
	}
	return true
}

// Validate checks the safe aggregate projection without I/O. Managers can
// reject malformed adapter/service results using the owning report contract.
func (a Analytics) Validate(q AnalyticsQuery) error {
	if q.Validate() != nil || a.ProgramID != ProgramID || !IsCanonicalAnalyticsPartnerID(a.PartnerID) || a.AsOf.IsZero() || !validCorrectionFingerprint(a.Revision) || !validVisitMetrics(a.Visits) || !validSignupMetrics(a.Signups) || a.LifetimeRelationships < 0 || a.LifetimeRelationships > AnalyticsCapacity || a.CurrentRelationships < 0 || a.RetainedRelationships < 0 || a.CurrentRelationships+a.RetainedRelationships != a.LifetimeRelationships || len(a.Links) > q.Limit || (a.HasMore && len(a.Links) != q.Limit) || (!a.HasMore && a.NextAfterLinkID != "") {
		return ErrUnavailable
	}
	if (q.From == nil) != (a.From == nil) || (q.To == nil) != (a.To == nil) || (q.From != nil && !q.From.Equal(*a.From)) || (q.To != nil && !q.To.Equal(*a.To)) {
		return ErrUnavailable
	}
	c := a.Coverage
	for _, n := range []int64{c.LegacyUnmeasuredObservations, c.UnmeasuredSignupOrigins, c.MissingMeasuredSignupOrigins} {
		if n < 0 {
			return ErrUnavailable
		}
	}
	expected := "anonymous_daily_observations"
	if c.MissingMeasuredSignupOrigins > 0 {
		expected = "incomplete_measured_origins"
	}
	if c.Scope != expected {
		return ErrUnavailable
	}
	previous := q.AfterLinkID
	pageTotal := VisitMetrics{}
	pageMeasured, pageUnmeasured := int64(0), int64(0)
	for _, row := range a.Links {
		if !analyticsID(row.LinkID, 128) || !analyticsID(row.Code, 128) || row.LinkID <= previous || !validVisitMetrics(row.Visits) || !validSignupMetrics(row.Signups) || !visitMetricsWithin(row.Visits, a.Visits) || row.Signups.MeasuredSignups > a.Signups.MeasuredSignups || row.Signups.UnmeasuredSignups > a.Signups.UnmeasuredSignups || row.Signups.ManualInitialAcquisitions != 0 || row.Signups.CorrectionAcquisitionEvents != 0 {
			return ErrUnavailable
		}
		if !addVisitMetrics(&pageTotal, row.Visits) || pageMeasured > math.MaxInt64-row.Signups.MeasuredSignups || pageUnmeasured > math.MaxInt64-row.Signups.UnmeasuredSignups {
			return ErrUnavailable
		}
		pageMeasured += row.Signups.MeasuredSignups
		pageUnmeasured += row.Signups.UnmeasuredSignups
		previous = row.LinkID
	}
	if !visitMetricsWithin(pageTotal, a.Visits) || pageMeasured > a.Signups.MeasuredSignups || pageUnmeasured > a.Signups.UnmeasuredSignups {
		return ErrUnavailable
	}
	if a.HasMore && a.NextAfterLinkID != previous {
		return ErrUnavailable
	}
	return nil
}
