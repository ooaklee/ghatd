package partnerprogram

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	mu           sync.Mutex
	partners     map[string]Partner
	byID         map[string]Partner
	policies     []PolicyVersion
	destinations map[string][]Destination
	fail         error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{partners: map[string]Partner{}, byID: map[string]Partner{}, destinations: map[string][]Destination{}}
}
func (f *fakeRepo) GetPartnerByID(ctx context.Context, id string) (Partner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Partner{}, f.fail
	}
	p, ok := f.byID[id]
	if !ok {
		return Partner{}, ErrNotFound
	}
	return p, nil
}
func (f *fakeRepo) GetPartnerByCustomer(ctx context.Context, program, customer string) (Partner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Partner{}, f.fail
	}
	p, ok := f.partners[program+":"+customer]
	if !ok {
		return Partner{}, ErrNotFound
	}
	return p, nil
}
func (f *fakeRepo) InsertPartner(ctx context.Context, p Partner) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	key := p.ProgramID + ":" + p.CustomerID
	if _, ok := f.partners[key]; ok {
		return ErrAlreadyEnrolled
	}
	f.partners[key] = p
	f.byID[p.ID] = p
	return nil
}
func (f *fakeRepo) ReplacePartner(ctx context.Context, p Partner, revision int64) (Partner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Partner{}, f.fail
	}
	old, ok := f.byID[p.ID]
	if !ok {
		return Partner{}, ErrNotFound
	}
	if old.Revision != revision {
		return Partner{}, ErrStaleWrite
	}
	f.byID[p.ID] = p
	f.partners[p.ProgramID+":"+p.CustomerID] = p
	return p, nil
}
func (f *fakeRepo) ListPolicyVersions(ctx context.Context, program string) ([]PolicyVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	return append([]PolicyVersion(nil), f.policies...), nil
}
func (f *fakeRepo) InsertPolicyVersion(ctx context.Context, v PolicyVersion, expected int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	var current int64
	for _, p := range f.policies {
		if p.ProgramID == v.ProgramID && p.Scope == v.Scope && p.GroupID == v.GroupID && p.PartnerCustomer == v.PartnerCustomer && p.Revision > current {
			current = p.Revision
		}
	}
	if current != expected {
		return ErrStaleWrite
	}
	f.policies = append(f.policies, v)
	return nil
}
func (f *fakeRepo) GetDestination(ctx context.Context, customer string) (Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Destination{}, f.fail
	}
	ds := f.destinations[customer]
	if len(ds) == 0 {
		return Destination{}, ErrNotFound
	}
	return ds[len(ds)-1], nil
}
func (f *fakeRepo) InsertDestination(ctx context.Context, d Destination, expected int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	ds := f.destinations[d.CustomerID]
	var current int64
	if len(ds) > 0 {
		current = ds[len(ds)-1].Version
	}
	if expected != current {
		return ErrStaleWrite
	}
	f.destinations[d.CustomerID] = append(ds, d)
	return nil
}

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

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }
func testConfig() Config {
	return Config{DefaultRateBasisPoints: 2000, DefaultHoldDays: 14, Currency: "EUR", CurrencyExponent: 2, TermsVersion: "fixture-terms-v1", AttributionWindow: 30 * 24 * time.Hour, EligiblePlanIDs: []string{"plan_fixture"}}
}
func newTestService(t *testing.T) (*Service, *fakeRepo, fixedClock) {
	t.Helper()
	repo := newFakeRepo()
	clock := fixedClock{time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(repo, clock, &seqIDs{}, testConfig())
	require.NoError(t, err)
	return svc, repo, clock
}
func globalDraft(at time.Time) PolicyDraft {
	return PolicyDraft{Scope: "global", RateBasisPoints: 1000, HoldDays: 7, Currency: "EUR", EligiblePlanIDs: []string{"plan_fixture"}, EffectiveFrom: at.Add(-time.Hour), TermsVersion: "fixture-terms-v1"}
}

func TestConfiguration(t *testing.T) {
	cases := []struct {
		name  string
		alter func(*Config)
		want  error
	}{
		{"valid", func(c *Config) {}, nil},
		{"thirty day default", func(c *Config) { c.DefaultHoldDays = 30 }, nil},
		{"default maximum exceeded", func(c *Config) { c.DefaultHoldDays = 31 }, ErrInvalid},
		{"custom maximum", func(c *Config) { c.MaxHoldDays = 90; c.DefaultHoldDays = 90 }, nil},
		{"structural maximum", func(c *Config) { c.MaxHoldDays = 365; c.DefaultHoldDays = 365 }, nil},
		{"custom maximum exceeded", func(c *Config) { c.MaxHoldDays = 10 }, ErrInvalid},
		{"negative maximum", func(c *Config) { c.MaxHoldDays = -1 }, ErrInvalid},
		{"unsupported maximum", func(c *Config) { c.MaxHoldDays = 366 }, ErrInvalid},
		{"maximum integer overflow", func(c *Config) { c.MaxHoldDays = math.MaxInt }, ErrInvalid},
		{"unapproved zero hold", func(c *Config) { c.DefaultHoldDays = 0 }, ErrInvalid},
		{"explicit zero hold", func(c *Config) { c.DefaultHoldDays = 0; c.AllowZeroHold = true }, nil},
		{"hold integer overflow", func(c *Config) { c.DefaultHoldDays = math.MaxInt }, ErrInvalid},
		{"rate overflow", func(c *Config) { c.DefaultRateBasisPoints = 10001 }, ErrInvalid},
		{"non currency letters", func(c *Config) { c.Currency = "E1R" }, ErrInvalid},
		{"unsupported exponent", func(c *Config) { c.CurrencyExponent = 4 }, ErrInvalid},
		{"missing window", func(c *Config) { c.AttributionWindow = 0 }, ErrInvalid},
		{"window upper bound", func(c *Config) { c.AttributionWindow = 181 * 24 * time.Hour }, ErrInvalid},
		{"missing terms", func(c *Config) { c.TermsVersion = "" }, ErrInvalid},
		{"missing eligible plans", func(c *Config) { c.EligiblePlanIDs = nil }, ErrInvalid},
		{"duplicate plan", func(c *Config) { c.EligiblePlanIDs = []string{"plan_fixture", "plan_fixture"} }, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			tc.alter(&cfg)
			_, err := NewService(newFakeRepo(), fixedClock{}, &seqIDs{}, cfg)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
func TestEnrollment(t *testing.T) {
	cases := []struct {
		name, customer, terms string
		approval              bool
		dependency            error
		want                  error
		status                string
	}{
		{name: "automatic and replay", customer: "customer", terms: "fixture-terms-v1", status: StatusActive},
		{name: "pending approval and replay", customer: "customer", terms: "fixture-terms-v1", approval: true, status: StatusPending},
		{name: "missing customer", terms: "fixture-terms-v1", want: ErrInvalid},
		{name: "missing terms", customer: "customer", want: ErrDenied},
		{name: "stale consent", customer: "customer", terms: "old", want: ErrDenied},
		{name: "repository failure stays failure", customer: "customer", terms: "fixture-terms-v1", dependency: ErrUnavailable, want: ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.fail = tc.dependency
			cfg := testConfig()
			cfg.EnrollmentApprovalRequired = tc.approval
			svc, err := NewService(repo, fixedClock{time.Now()}, &seqIDs{}, cfg)
			require.NoError(t, err)
			req := EnrollRequest{tc.customer, tc.terms}
			p, err := svc.Enroll(context.Background(), req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, repo.partners)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.status, p.Status)
			require.Equal(t, !tc.approval, p.CanAcquireReferrals)
			again, err := svc.Enroll(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, p.ID, again.ID)
			require.Len(t, repo.partners, 1)
		})
	}
}
func TestStatusTransitions(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name                    string
		state                   string
		rev                     int64
		reason, actor           string
		acquire, accrue, payout *bool
		want                    error
	}{
		{name: "suspend defaults", state: StatusSuspended, rev: 1, reason: "review", actor: "operator"},
		{name: "suspend only acquisition", state: StatusSuspended, rev: 1, reason: "pause acquisition", actor: "operator", acquire: &no, accrue: &yes, payout: &yes},
		{name: "closed can recover earned payout", state: StatusClosed, rev: 1, reason: "program closed", actor: "operator", payout: &yes},
		{name: "closed cannot accrue", state: StatusClosed, rev: 1, reason: "closed", actor: "operator", accrue: &yes, want: ErrInvalid},
		{name: "missing reason even activation", state: StatusActive, rev: 1, actor: "operator", want: ErrInvalid},
		{name: "missing actor", state: StatusSuspended, rev: 1, reason: "review", want: ErrDenied},
		{name: "stale revision", state: StatusSuspended, rev: 2, reason: "review", actor: "operator", want: ErrStaleWrite},
		{name: "overflow revision", state: StatusSuspended, rev: math.MaxInt64, reason: "review", actor: "operator", want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _ := newTestService(t)
			p, err := svc.Enroll(context.Background(), EnrollRequest{"customer", "fixture-terms-v1"})
			require.NoError(t, err)
			changed, err := svc.ChangeStatus(context.Background(), StatusChangeRequest{PartnerID: p.ID, ExpectedRevision: tc.rev, NewStatus: tc.state, Reason: tc.reason, ActorID: tc.actor, CanAcquireReferrals: tc.acquire, CanAccrue: tc.accrue, CanRequestPayouts: tc.payout})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.EqualValues(t, 1, repo.byID[p.ID].Revision)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 2, changed.Revision)
			require.Equal(t, tc.actor, changed.UpdatedBy)
			require.Equal(t, tc.reason, changed.StatusReason)
			if tc.accrue != nil {
				require.Equal(t, *tc.accrue, changed.CanAccrue)
			}
			if tc.payout != nil {
				require.Equal(t, *tc.payout, changed.CanRequestPayouts)
			}
		})
	}
}
func TestPolicyValidation(t *testing.T) {
	cases := []struct {
		name        string
		alter       func(*PolicyDraft)
		zeroAllowed bool
		want        error
	}{
		{"global", func(d *PolicyDraft) {}, false, nil},
		{"maximum rate and default hold", func(d *PolicyDraft) { d.RateBasisPoints = 10000; d.HoldDays = 30 }, false, nil},
		{"legacy twenty eight day hold", func(d *PolicyDraft) { d.HoldDays = 28 }, false, nil},
		{"fourteen day hold", func(d *PolicyDraft) { d.HoldDays = 14 }, false, nil},
		{"negative global rate", func(d *PolicyDraft) { d.RateBasisPoints = -1 }, false, ErrInvalid},
		{"override explicit inheritance", func(d *PolicyDraft) {
			d.Scope = "group"
			d.GroupID = "group"
			d.RateBasisPoints = -1
			d.HoldDays = -1
			d.Currency = ""
			d.EligiblePlanIDs = nil
			d.TermsVersion = ""
		}, false, nil},
		{"unapproved zero hold", func(d *PolicyDraft) { d.HoldDays = 0 }, false, ErrInvalid},
		{"explicit zero hold and bonus", func(d *PolicyDraft) { d.HoldDays = 0; d.RateBasisPoints = 0 }, true, nil},
		{"hold overflow", func(d *PolicyDraft) { d.HoldDays = math.MaxInt }, false, ErrInvalid},
		{"hold beyond maximum", func(d *PolicyDraft) { d.HoldDays = 31 }, false, ErrInvalid},
		{"rate beyond maximum", func(d *PolicyDraft) { d.RateBasisPoints = 10001 }, false, ErrInvalid},
		{"unsupported currency", func(d *PolicyDraft) { d.Currency = "USD" }, false, ErrInvalid},
		{"global missing plans", func(d *PolicyDraft) { d.EligiblePlanIDs = nil }, false, ErrInvalid},
		{"global missing terms", func(d *PolicyDraft) { d.TermsVersion = "" }, false, ErrInvalid},
		{"missing start", func(d *PolicyDraft) { d.EffectiveFrom = time.Time{} }, false, ErrInvalid},
		{"end equals start", func(d *PolicyDraft) { d.EffectiveTo = &d.EffectiveFrom }, false, ErrInvalid},
		{"invalid scope", func(d *PolicyDraft) { d.Scope = "unknown" }, false, ErrInvalid},
		{"group missing identity", func(d *PolicyDraft) { d.Scope = "group" }, false, ErrInvalid},
		{"group extra identity", func(d *PolicyDraft) { d.Scope = "group"; d.GroupID = "g"; d.PartnerCustomer = "c" }, false, ErrInvalid},
		{"individual missing identity", func(d *PolicyDraft) { d.Scope = "individual" }, false, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			svc.config.AllowZeroHold = tc.zeroAllowed
			d := globalDraft(clock.Now())
			tc.alter(&d)
			err := svc.ValidatePolicy(d)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
func TestPolicyResolution(t *testing.T) {
	cases := []struct {
		name          string
		groups        []string
		individual    bool
		inherit       bool
		ambiguous     bool
		future        bool
		groupRevision bool
		wantRate      int
		wantHold      time.Duration
		wantPlans     []string
		want          error
	}{
		{name: "global", wantRate: 1000, wantHold: 7 * 24 * time.Hour, wantPlans: []string{"plan_fixture"}},
		{name: "group", groups: []string{"vip"}, wantRate: 2500, wantHold: 14 * 24 * time.Hour, wantPlans: []string{"plan_group"}},
		{name: "individual", groups: []string{"vip"}, individual: true, wantRate: 3000, wantHold: 28 * 24 * time.Hour, wantPlans: []string{"plan_individual"}},
		{name: "individual rate inherits group hold and plans", groups: []string{"vip"}, individual: true, inherit: true, wantRate: 3000, wantHold: 14 * 24 * time.Hour, wantPlans: []string{"plan_group"}},
		{name: "ambiguous equal priorities", groups: []string{"vip", "other"}, ambiguous: true, want: ErrAmbiguousPolicy},
		{name: "future rule excluded", groups: []string{"vip"}, individual: true, future: true, wantRate: 2500, wantHold: 14 * 24 * time.Hour, wantPlans: []string{"plan_group"}},
		{name: "duplicate membership and superseded group version", groups: []string{"vip", "vip"}, groupRevision: true, wantRate: 2700, wantHold: 14 * 24 * time.Hour, wantPlans: []string{"plan_group"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, clock := newTestService(t)
			ctx := context.Background()
			p, err := svc.Enroll(ctx, EnrollRequest{"customer", "fixture-terms-v1"})
			require.NoError(t, err)
			pub := func(d PolicyDraft, rev int64) {
				_, err := svc.PublishPolicy(ctx, PublishPolicyRequest{Draft: d, ActorID: "policy-operator", ExpectedRevision: rev})
				require.NoError(t, err)
			}
			pub(globalDraft(clock.Now()), 0)
			group := globalDraft(clock.Now())
			group.Scope = "group"
			group.GroupID = "vip"
			group.Priority = 10
			group.RateBasisPoints = 2500
			group.HoldDays = 14
			group.EligiblePlanIDs = []string{"plan_group"}
			pub(group, 0)
			if tc.groupRevision {
				group.RateBasisPoints = 2700
				pub(group, 1)
			}
			if tc.ambiguous {
				other := group
				other.GroupID = "other"
				pub(other, 0)
			}
			if tc.individual {
				d := globalDraft(clock.Now())
				d.Scope = "individual"
				d.PartnerCustomer = "customer"
				d.RateBasisPoints = 3000
				d.HoldDays = 28
				d.EligiblePlanIDs = []string{"plan_individual"}
				if tc.inherit {
					d.HoldDays = -1
					d.Currency = ""
					d.EligiblePlanIDs = nil
					d.TermsVersion = ""
				}
				if tc.future {
					d.EffectiveFrom = clock.Now().Add(time.Hour)
				}
				pub(d, 0)
			}
			got, err := svc.ResolveTerms(ctx, p, tc.groups, clock.Now())
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRate, got.RateBasisPoints)
			require.Equal(t, tc.wantHold, got.HoldDuration)
			require.Equal(t, tc.wantPlans, got.EligiblePlanIDs)
			require.Equal(t, 2, got.CurrencyExponent)
			require.NotEmpty(t, got.PolicyVersionIDs)
		})
	}
}
func TestConcurrentPolicyPublication(t *testing.T) {
	// A single overlapping compare-and-swap scenario requires concurrent actors.
	svc, repo, clock := newTestService(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := svc.PublishPolicy(context.Background(), PublishPolicyRequest{Draft: globalDraft(clock.Now()), ActorID: "operator", ExpectedRevision: 0})
			results <- err
		}()
	}
	close(start)
	a, b := <-results, <-results
	require.True(t, (a == nil && errors.Is(b, ErrStaleWrite)) || (b == nil && errors.Is(a, ErrStaleWrite)))
	require.Len(t, repo.policies, 1)
}
func TestDestinationPublication(t *testing.T) {
	cases := []struct {
		name, method, email string
		expected            int64
		existing            bool
		want                error
	}{
		{"initial", "paypal", " Partner@Example.com ", 0, false, nil},
		{"second version", "paypal", "other@example.com", 1, true, nil},
		{"stale edit", "paypal", "other@example.com", 0, true, ErrStaleWrite},
		{"unsupported method", "bank", "partner@example.com", 0, false, ErrInvalid},
		{"invalid email", "paypal", "missing", 0, false, ErrInvalid},
		{"overflow version", "paypal", "partner@example.com", math.MaxInt64, false, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _ := newTestService(t)
			ctx := context.Background()
			if tc.existing {
				_, err := svc.UpdatePayoutDestination(ctx, DestinationRequest{CustomerID: "customer", Method: "paypal", PayPalEmail: "first@example.com"})
				require.NoError(t, err)
			}
			d, err := svc.UpdatePayoutDestination(ctx, DestinationRequest{CustomerID: "customer", Method: tc.method, PayPalEmail: tc.email, ExpectedVersion: tc.expected})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expected+1, d.Version)
			if !tc.existing {
				require.Equal(t, "partner@example.com", d.Email)
			}
			require.Len(t, repo.destinations["customer"], int(tc.expected+1))
		})
	}
}
func TestWiringAndDependencyFailures(t *testing.T) {
	cases := []struct {
		name string
		repo Repository
		ids  IDGenerator
		want error
	}{
		{"missing repository", nil, &seqIDs{}, ErrUnavailable},
		{"missing ID generator", newFakeRepo(), nil, ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewService(tc.repo, nil, tc.ids, testConfig())
			require.ErrorIs(t, err, tc.want)
		})
	}
}
