package referral

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	bindings map[string]PaymentAttribution
	mu       sync.Mutex
	links    map[string]Link
	history  map[string][]Referral
	clicks   []Click
	visits   map[string]VisitReceipt
	days     map[string]VisitDay
	fail     error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{bindings: map[string]PaymentAttribution{}, links: map[string]Link{}, history: map[string][]Referral{}, visits: map[string]VisitReceipt{}, days: map[string]VisitDay{}}
}
func (f *fakeRepo) WithAttributionTransaction(ctx context.Context, program, customer string, fn func(Repository) error) error {
	if ctx == nil || program != ProgramID || customer == "" || fn == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	tx := newFakeRepo()
	tx.fail = f.fail
	for k, v := range f.links {
		tx.links[k] = v
	}
	for k, v := range f.history {
		tx.history[k] = append([]Referral(nil), v...)
	}
	for k, v := range f.bindings {
		tx.bindings[k] = v
	}
	for k, v := range f.days {
		tx.days[k] = v
	}
	tx.clicks = append([]Click(nil), f.clicks...)
	for k, v := range f.visits {
		tx.visits[k] = v
	}
	if err := fn(tx); err != nil {
		return err
	}
	f.links, f.history, f.bindings, f.clicks = tx.links, tx.history, tx.bindings, tx.clicks
	f.visits, f.days = tx.visits, tx.days
	return nil
}
func (f *fakeRepo) GetLinkByCode(ctx context.Context, code string) (Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Link{}, f.fail
	}
	l, ok := f.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}
	return l, nil
}
func (f *fakeRepo) InsertLink(ctx context.Context, l Link) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if _, ok := f.links[l.Code]; ok {
		return ErrCodeTaken
	}
	for _, old := range f.links {
		if old.ProgramID == l.ProgramID && old.PartnerID == l.PartnerID && old.RetiredAt == nil {
			return ErrAlreadyExists
		}
	}
	f.links[l.Code] = l
	return nil
}
func (f *fakeRepo) RetireLink(ctx context.Context, id string, at time.Time, reason, actor string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	for code, l := range f.links {
		if l.ID == id {
			if l.RetiredAt == nil {
				l.RetiredAt = &at
				l.RetireReason = reason
				l.RetiredBy = actor
				f.links[code] = l
			}
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeRepo) ListLinksByPartner(ctx context.Context, program, partner string) ([]Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	ls := []Link{}
	for _, l := range f.links {
		if l.ProgramID == program && l.PartnerID == partner {
			ls = append(ls, l)
		}
	}
	return ls, nil
}
func (f *fakeRepo) GetReferralByCustomer(ctx context.Context, program, customer string) (Referral, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Referral{}, f.fail
	}
	rows := f.history[program+":"+customer]
	if len(rows) == 0 {
		return Referral{}, ErrNotFound
	}
	return rows[len(rows)-1], nil
}
func (f *fakeRepo) InsertReferral(ctx context.Context, r Referral, expected int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	key := r.ProgramID + ":" + r.ReferredCustomer
	rows := f.history[key]
	var revision int64
	if len(rows) > 0 {
		revision = rows[len(rows)-1].Revision
	}
	if revision != expected {
		return ErrStaleWrite
	}
	f.history[key] = append(rows, r)
	return nil
}
func (f *fakeRepo) ListReferralHistory(ctx context.Context, program, customer string) ([]Referral, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	return append([]Referral(nil), f.history[program+":"+customer]...), nil
}
func (f *fakeRepo) ListPaymentAttributionsByCustomer(_ context.Context, program, customer string) ([]PaymentAttribution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	var out []PaymentAttribution
	for _, v := range f.bindings {
		if v.ProgramID == program && v.ReferredCustomer == customer {
			out = append(out, v)
		}
	}
	return out, nil
}
func (f *fakeRepo) ListReferralsByPartner(ctx context.Context, program, partner string, limit int, after string) ([]Referral, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := []Referral{}
	for _, rows := range f.history {
		r := rows[len(rows)-1]
		if r.ProgramID == program && r.PartnerID == partner {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeRepo) ListRelationshipSnapshots(_ context.Context, program, partner string, limit int, after string) ([]RelationshipSnapshot, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, false, f.fail
	}
	out := []RelationshipSnapshot{}
	for _, history := range f.history {
		for _, row := range history {
			if row.ProgramID != program || row.PartnerID != partner {
				continue
			}
			id := RelationshipReferenceID(program, partner, row.ReferredCustomer)
			if id > after {
				out = append(out, RelationshipSnapshot{ID: id, ProgramID: program, PartnerID: partner, ReferredCustomer: row.ReferredCustomer, FirstReferralID: row.ID, Head: history[len(history)-1], History: append([]Referral(nil), history...)})
			}
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}
func (f *fakeRepo) RecordClick(ctx context.Context, c Click, expires time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.clicks = append(f.clicks, c)
	return nil
}
func (f *fakeRepo) CountClicks(ctx context.Context, link string, from, to time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return 0, f.fail
	}
	var n int64
	for _, c := range f.clicks {
		if c.LinkID == link && !c.OccurredAt.Before(from) && c.OccurredAt.Before(to) {
			n++
		}
	}
	return n, nil
}

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) NewID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return fmt.Sprintf("test_%d", s.n)
}
func fixtureTerms() TermsSnapshot {
	return TermsSnapshot{RateBasisPoints: 2000, HoldDurationDays: 7, Currency: "EUR", CurrencyExponent: 2, TermsVersion: "fixture-terms-v1", PolicyVersionIDs: []string{"policy_fixture"}, EligiblePlanIDs: []string{"plan_fixture"}}
}
func fixture(t *testing.T) (*Service, *fakeRepo, PartnerState, Link, Eligibility) {
	t.Helper()
	repo := newFakeRepo()
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	svc, err := NewService(repo, fakeClock{at}, &seqIDs{}, 7*24*time.Hour)
	require.NoError(t, err)
	svc, err = svc.WithAnalytics(AnalyticsConfig{ObservationRetention: 24 * time.Hour})
	require.NoError(t, err)
	p := PartnerState{"partner", "owner", true}
	l, err := svc.IssueLink(context.Background(), p)
	require.NoError(t, err)
	e := Eligibility{ReferredCustomer: "new-customer", SignupID: "signup_fixture", IsIndividual: true, At: at.Add(time.Hour), Evidence: Evidence{ProgramID: ProgramID, Audience: "partner-signup", Code: l.Code, LinkID: l.ID, IssuedAt: at, ExpiresAt: at.Add(7 * 24 * time.Hour)}, Terms: fixtureTerms()}
	return svc, repo, p, l, e
}

func TestAttributionEligibility(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*PartnerState, *Link, *Eligibility)
		want  error
	}{
		{"eligible signup", func(p *PartnerState, l *Link, e *Eligibility) {}, nil},
		{"existing customer", func(p *PartnerState, l *Link, e *Eligibility) { e.IsExistingCustomer = true }, ErrDenied},
		{"seat principal", func(p *PartnerState, l *Link, e *Eligibility) { e.IsIndividual = false }, ErrDenied},
		{"self referral", func(p *PartnerState, l *Link, e *Eligibility) { e.ReferredCustomer = p.CustomerID }, ErrSelfReferral},
		{"disabled partner", func(p *PartnerState, l *Link, e *Eligibility) { p.CanAcquireReferrals = false }, ErrDenied},
		{"retired code", func(p *PartnerState, l *Link, e *Eligibility) { at := e.At; l.RetiredAt = &at }, ErrCodeRetired},
		{"missing signup authority", func(p *PartnerState, l *Link, e *Eligibility) { e.SignupID = "" }, ErrInvalid},
		{"missing account time", func(p *PartnerState, l *Link, e *Eligibility) { e.At = time.Time{} }, ErrInvalid},
		{"expired at boundary", func(p *PartnerState, l *Link, e *Eligibility) { e.At = e.Evidence.ExpiresAt }, ErrDenied},
		{"account before evidence", func(p *PartnerState, l *Link, e *Eligibility) { e.At = e.Evidence.IssuedAt.Add(-time.Second) }, ErrDenied},
		{"wrong audience", func(p *PartnerState, l *Link, e *Eligibility) { e.Evidence.Audience = "login" }, ErrDenied},
		{"wrong link", func(p *PartnerState, l *Link, e *Eligibility) { e.Evidence.LinkID = "other" }, ErrDenied},
		{"wrong partner ownership", func(p *PartnerState, l *Link, e *Eligibility) { l.PartnerID = "other" }, ErrDenied},
		{"window extended", func(p *PartnerState, l *Link, e *Eligibility) {
			e.Evidence.ExpiresAt = e.Evidence.IssuedAt.Add(8 * 24 * time.Hour)
		}, ErrDenied},
		{"missing frozen policy", func(p *PartnerState, l *Link, e *Eligibility) { e.Terms.PolicyVersionIDs = nil }, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, p, l, e := fixture(t)
			tc.alter(&p, &l, &e)
			r, err := svc.LockAttribution(context.Background(), p, l, e)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, repo.history)
				return
			}
			require.NoError(t, err)
			require.Equal(t, e.At, r.LockedAt)
			require.Equal(t, e.Terms, r.TermsSnapshot)
			require.NotEmpty(t, r.EvidenceDigest)
			require.EqualValues(t, 1, r.Revision)
		})
	}
}
func TestAttributionReplayAndConflict(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*Eligibility)
		want  error
	}{
		{"same signup freezes original terms", func(e *Eligibility) { e.Terms.RateBasisPoints = 8000 }, nil},
		{"different signup operation", func(e *Eligibility) { e.SignupID = "another" }, ErrAlreadyReferred},
		{"competing link", func(e *Eligibility) { e.Evidence.IssuedAt = e.Evidence.IssuedAt.Add(time.Minute) }, ErrAlreadyReferred},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, p, l, e := fixture(t)
			first, err := svc.LockAttribution(context.Background(), p, l, e)
			require.NoError(t, err)
			tc.alter(&e)
			second, err := svc.LockAttribution(context.Background(), p, l, e)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
				require.Equal(t, first, second)
				require.Equal(t, 2000, second.TermsSnapshot.RateBasisPoints)
			}
			require.Len(t, repo.history[ProgramID+":"+e.ReferredCustomer], 1)
		})
	}
}
func TestAttributionCorrection(t *testing.T) {
	cases := []struct {
		name, actor, reason, mode string
		revision                  int64
		want                      error
	}{
		{"reasoned prospective revision", "operator", "verified correction", CorrectionProspective, 1, nil},
		{"historical compensation mode is unsupported", "operator", "verified correction", "compensated", 1, ErrInvalid},
		{"stale ownership revision", "operator", "verified correction", CorrectionProspective, 0, ErrInvalid},
		{"missing actor", "", "verified correction", CorrectionProspective, 1, ErrDenied},
		{"missing reason", "operator", "", CorrectionProspective, 1, ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, p, l, e := fixture(t)
			original, err := svc.LockAttribution(context.Background(), p, l, e)
			require.NoError(t, err)
			svc.clock = fakeClock{e.At.Add(time.Hour)}
			req := reviewedCorrection(t, svc, CorrectionRequest{Partner: PartnerState{"other-partner", "other-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: tc.actor, Reason: tc.reason, ExpectedRevision: tc.revision, ExpectedReferralID: original.ID, Terms: fixtureTerms()})
			req.Mode = tc.mode
			out, err := svc.AssignAttribution(context.Background(), req)
			rows := repo.history[ProgramID+":"+e.ReferredCustomer]
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				require.Len(t, rows, 1)
				return
			}
			require.Len(t, rows, 2)
			require.Equal(t, original, rows[0])
			require.Equal(t, original.ID, out.CorrectionOf)
			require.Equal(t, p.PartnerID, out.PriorPartnerID)
			require.EqualValues(t, 2, out.Revision)
			require.Equal(t, CorrectionProspective, out.Correction.Mode)
		})
	}
}
func TestAttributionCorrectionClockBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		offset time.Duration
		zero   bool
		want   error
	}{
		{name: "later_cutover", offset: time.Hour},
		{name: "same_instant_uses_revision_order"},
		{name: "cutover_before_prior_revision_is_denied", offset: -time.Nanosecond, want: ErrInvalid},
		{name: "zero_clock_is_unavailable_cutover", zero: true, want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, p, l, e := fixture(t)
			original, err := s.LockAttribution(context.Background(), p, l, e)
			require.NoError(t, err)
			at := original.LockedAt.Add(tc.offset)
			if tc.zero {
				at = time.Time{}
			}
			s.clock = fakeClock{at}
			out, err := s.AssignAttribution(context.Background(), reviewedCorrection(t, s, CorrectionRequest{Partner: PartnerState{"new-partner", "new-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: "operator", Reason: "prospective correction", ExpectedRevision: original.Revision, ExpectedReferralID: original.ID, Terms: fixtureTerms()}))
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				require.Equal(t, []Referral{original}, repo.history[ProgramID+":"+e.ReferredCustomer])
			} else {
				require.Equal(t, at, out.LockedAt)
				require.Equal(t, original, repo.history[ProgramID+":"+e.ReferredCustomer][0])
			}
		})
	}
}

func TestLinkRetirementAndReissue(t *testing.T) {
	// One history lifecycle proves that retirement cannot reassign an old code.
	svc, repo, p, l, _ := fixture(t)
	ctx := context.Background()
	require.NoError(t, svc.RetireLink(ctx, l.ID, "rotation", "owner"))
	next, err := svc.IssueLink(ctx, p)
	require.NoError(t, err)
	require.NotEqual(t, l.Code, next.Code)
	old, err := svc.GetLinkByCode(ctx, l.Code)
	require.NoError(t, err)
	require.NotNil(t, old.RetiredAt)
	require.Equal(t, "owner", old.RetiredBy)
	require.Len(t, repo.links, 2)
}
func TestConcurrentLinkIssuance(t *testing.T) {
	// Concurrent overlapping creation verifies the adapter's active-partner guard.
	svc, repo, p, existing, _ := fixture(t)
	repo.links = map[string]Link{}
	start := make(chan struct{})
	results := make(chan Link, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; l, err := svc.IssueLink(context.Background(), p); results <- l; errs <- err }()
	}
	close(start)
	a, b := <-results, <-results
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.Equal(t, a.ID, b.ID)
	require.NotEqual(t, existing.ID, a.ID)
	require.Len(t, repo.links, 1)
}
func TestReportingAndErrors(t *testing.T) {
	cases := []struct {
		name    string
		fail    error
		retired bool
		want    error
	}{
		{"click observation", nil, false, nil},
		{"dependency not absence", ErrUnavailable, false, ErrUnavailable},
		{"retired code not observed", nil, true, ErrCodeRetired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, l, e := fixture(t)
			if tc.retired {
				require.NoError(t, svc.RetireLink(context.Background(), l.ID, "rotation", "owner"))
			}
			repo.fail = tc.fail
			c, err := svc.ObserveClick(context.Background(), l.Code, "agent")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, repo.clicks)
				return
			}
			require.NoError(t, err)
			n, err := svc.ClickCount(context.Background(), c.LinkID, e.Evidence.IssuedAt, e.At)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
		})
	}
}
func TestNormalizeCodePreservesCase(t *testing.T) {
	cases := []struct{ input, want string }{{" AbC_DEF-1 ", "AbC_DEF-1"}, {"abc", "abc"}, {"  ", ""}}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) { require.Equal(t, tc.want, NormalizeCode(tc.input)) })
	}
}

func (f *fakeRepo) GetPaymentAttribution(ctx context.Context, program, payment string) (PaymentAttribution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return PaymentAttribution{}, f.fail
	}
	p, ok := f.bindings[program+":"+payment]
	if !ok {
		return PaymentAttribution{}, ErrNotFound
	}
	return p, nil
}
func (f *fakeRepo) InsertPaymentAttribution(ctx context.Context, p PaymentAttribution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	key := p.ProgramID + ":" + p.PaymentID
	if _, ok := f.bindings[key]; ok {
		return ErrAlreadyExists
	}
	f.bindings[key] = p
	return nil
}
