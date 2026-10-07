package user

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type signupFixtureRepository struct {
	UserRepository
	calls   int
	fact    SignupAttribution
	err     error
	receipt SignupConsumption
}

func (r *signupFixtureRepository) GetSignupAttribution(context.Context, string, string) (SignupAttribution, error) {
	r.calls++
	return cloneSignupAttribution(r.fact), r.err
}
func (r *signupFixtureRepository) PendingSignupAttributions(context.Context, string, int) ([]SignupAttribution, error) {
	r.calls++
	return []SignupAttribution{cloneSignupAttribution(r.fact)}, r.err
}
func (r *signupFixtureRepository) ConsumeSignupAttribution(_ context.Context, _ string, _ string, c SignupConsumption) error {
	r.calls++
	r.receipt = c
	return r.err
}

type signupFixtureClock struct{ at time.Time }

func (c signupFixtureClock) Now() time.Time { return c.at }
func (c signupFixtureClock) NowUTC() string { return c.at.Format(time.RFC3339Nano) }

func TestSignupConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		config SignupAttributionConfig
		repo   UserRepository
		clock  TimeProvider
		want   error
	}{
		{name: "explicit", config: SignupAttributionConfig{"program", []string{"individual"}}, repo: &signupFixtureRepository{}, clock: signupFixtureClock{}},
		{name: "no repository", config: SignupAttributionConfig{"program", []string{"individual"}}, clock: signupFixtureClock{}, want: ErrSignupEvidenceUnavailable},
		{name: "typed nil repository", config: SignupAttributionConfig{"program", []string{"individual"}}, repo: (*signupFixtureRepository)(nil), clock: signupFixtureClock{}, want: ErrSignupEvidenceUnavailable},
		{name: "no clock", config: SignupAttributionConfig{"program", []string{"individual"}}, repo: &signupFixtureRepository{}, want: ErrSignupEvidenceUnavailable},
		{name: "typed nil clock", config: SignupAttributionConfig{"program", []string{"individual"}}, repo: &signupFixtureRepository{}, clock: (*DefaultTimeProvider)(nil), want: ErrSignupEvidenceUnavailable},
		{name: "empty program", config: SignupAttributionConfig{" ", []string{"individual"}}, repo: &signupFixtureRepository{}, clock: signupFixtureClock{}, want: ErrSignupEvidenceInvalid},
		{name: "missing eligibility", config: SignupAttributionConfig{"program", nil}, repo: &signupFixtureRepository{}, clock: signupFixtureClock{}, want: ErrSignupEvidenceInvalid},
		{name: "duplicate types", config: SignupAttributionConfig{"program", []string{"individual", "individual"}}, repo: &signupFixtureRepository{}, clock: signupFixtureClock{}, want: ErrSignupEvidenceInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{UserRepository: tc.repo, TimeProvider: tc.clock}
			_, err := s.WithSignupAttribution(tc.config)
			require.ErrorIs(t, err, tc.want)
			if err == nil {
				tc.config.IndividualAccountTypes[0] = "organization"
				require.Equal(t, "individual", s.signupAttribution.IndividualAccountTypes[0])
			}
		})
	}
}

func TestSignupCapture(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name, kind, stamp, evidence string
		individual                  bool
		want                        error
	}{
		{"individual signed", "individual", at.Format(time.RFC3339Nano), "signed", true, nil},
		{"owning legacy UTC format", "individual", at.Format(DefaultTimeFormatRFC3339NanoUTC), "signed", true, nil},
		{"organization is ineligible", "organization", at.Format(time.RFC3339Nano), "signed", false, nil},
		{"cookie absent", "individual", at.Format(time.RFC3339Nano), "", true, nil},
		{"bad owning timestamp", "individual", "browser-time", "signed", false, ErrSignupEvidenceInvalid},
		{"zero owning timestamp", "individual", time.Time{}.Format(time.RFC3339Nano), "signed", false, ErrSignupEvidenceInvalid},
		{"oversize evidence", "individual", at.Format(time.RFC3339Nano), strings.Repeat("a", 2049), false, ErrSignupEvidenceInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := (&Service{UserRepository: &signupFixtureRepository{}, TimeProvider: signupFixtureClock{at}}).WithSignupAttribution(SignupAttributionConfig{"program", []string{"individual"}})
			require.NoError(t, err)
			u := &UniversalUser{ID: "new-owning-id", Type: tc.kind, Metadata: &UserMetadata{CreatedAt: tc.stamp}}
			err = s.captureSignup(u, tc.evidence)
			require.ErrorIs(t, err, tc.want)
			if err != nil {
				require.Nil(t, u.SignupAttribution)
				return
			}
			require.Equal(t, at, u.SignupAttribution.CreatedAt)
			require.Equal(t, tc.individual, u.SignupAttribution.Individual)
			require.Equal(t, u.ID, u.SignupAttribution.CustomerID)
			require.Equal(t, "pending", u.SignupAttribution.State)
			payload, err := json.Marshal(u)
			require.NoError(t, err)
			require.NotContains(t, string(payload), "signup_attribution")
		})
	}
}

func TestSignupFeedAdmission(t *testing.T) {
	cases := []struct {
		name, customer, actor, outcome string
		canceled                       bool
		repoErr, want                  error
	}{
		{"accepted", "customer", "worker", "attributed", false, nil, nil},
		{"empty customer", "", "worker", "attributed", false, nil, ErrSignupEvidenceInvalid},
		{"empty actor", "customer", " ", "attributed", false, nil, ErrSignupEvidenceInvalid},
		{"unknown decision", "customer", "worker", "paid", false, nil, ErrSignupEvidenceInvalid},
		{"cancelled", "customer", "worker", "attributed", true, nil, context.Canceled},
		{"storage outage", "customer", "worker", "attributed", false, ErrDatabaseError, ErrDatabaseError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			r := &signupFixtureRepository{err: tc.repoErr}
			s, err := (&Service{UserRepository: r, TimeProvider: signupFixtureClock{at}}).WithSignupAttribution(SignupAttributionConfig{"program", []string{"individual"}})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			err = s.ConsumeSignupAttribution(ctx, tc.customer, SignupConsumption{ActorID: tc.actor, ReceiptID: "durable-lock", Outcome: tc.outcome, RecordedAt: at.Add(-time.Hour)})
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, at, r.receipt.RecordedAt)
			} else if !errors.Is(tc.want, tc.repoErr) {
				require.Zero(t, r.calls)
			}
		})
	}
}

func TestSignupTransportIgnoresPrivateContext(t *testing.T) {
	cases := []struct{ name, payload string }{
		{"snake case", `{"attribution_evidence":"forged","email":"member@example.test"}`},
		{"exported name", `{"AttributionEvidence":"forged","email":"member@example.test"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var regular CreateUserRequest
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &regular))
			require.Empty(t, regular.AttributionEvidence)
			var oauth CreateOAuthUserRequest
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &oauth))
			require.Empty(t, oauth.AttributionEvidence)
		})
	}
}
