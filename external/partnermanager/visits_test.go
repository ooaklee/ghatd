package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type visitIDs struct{ n int }

func (s *visitIDs) NewID() string { s.n++; return fmt.Sprintf("nonce-%d", s.n) }

type visitReadStub struct {
	ReferralService
	link   referral.Link
	err    error
	calls  int
	first  map[string]referral.Click
	now    time.Time
	cancel context.CancelFunc
}

func (s *visitReadStub) GetLinkByCode(context.Context, string) (referral.Link, error) {
	return s.link, nil
}
func (s *visitReadStub) ObserveVisit(_ context.Context, r referral.VisitRequest) (referral.VisitObservation, error) {
	s.calls++
	if s.cancel != nil {
		s.cancel()
	}
	if s.err != nil {
		return referral.VisitObservation{}, s.err
	}
	if r.KnownBot {
		return referral.VisitObservation{Click: referral.Click{Classification: referral.VisitKnownBot}}, nil
	}
	if c, ok := s.first[r.Identity.Digest]; ok {
		return referral.VisitObservation{Click: referral.Click{Classification: referral.VisitDuplicate}, Eligible: c}, nil
	}
	c := referral.Click{ID: fmt.Sprintf("click-%d", s.calls), LinkID: s.link.ID, Code: s.link.Code, Classification: referral.VisitEligible, VisitDigest: r.Identity.Digest, OccurredAt: s.now}
	c.MeasuredClickID = c.ID
	s.first[r.Identity.Digest] = c
	return referral.VisitObservation{Click: c, Eligible: c}, nil
}

// Audit disposition: named manager admission, cookie reuse and optional error
// cases verify purpose separation/privacy; real dedupe storage is tested in Mongo.
func TestPrepareVisitCookieAndAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, mode, measurement string
		want                    error
	}{
		{name: "first_measured_cookie", measurement: referral.VisitEligible},
		{name: "reload_preserves_signup_and_visit_expiries", mode: "duplicate", measurement: referral.VisitDuplicate},
		{name: "no_consent_has_no_reads_or_cookies", mode: "consent", measurement: "not_consented"},
		{name: "known_bot_does_not_seed_signup_attribution", mode: "bot", measurement: "known_bot"},
		{name: "optional_analytics_outage_is_unmeasured_attribution", mode: "outage", measurement: "unavailable"},
		{name: "lost_duplicate_observation_preserves_original_evidence", mode: "duplicate_outage", measurement: "unavailable"},
		{name: "disabled_measurement_preserves_attribution", mode: "disabled", measurement: "disabled"},
		{name: "expired_visit_starts_new_bounded_measurement_without_extending_signup", mode: "expired", measurement: referral.VisitEligible},
		{name: "tampered_prior_visit_starts_new_nonce", mode: "tamper", measurement: referral.VisitEligible},
		{name: "foreign_signup_and_visit_tokens_are_not_reused", mode: "foreign", measurement: referral.VisitEligible},
		{name: "commercial_pause_prevents_new_evidence", mode: "pause", want: ErrDenied},
		{name: "cancelled_request_cannot_return_fallback_token", mode: "cancelled", want: context.Canceled},
		{name: "dependency_cancellation_is_not_optional_outage", mode: "dependency_cancelled", want: context.Canceled},
		{name: "request_cancellation_overrides_unrelated_backend_failure", mode: "raced_cancel", want: context.Canceled},
		{name: "successful_bot_observation_does_not_hide_request_cancellation", mode: "bot_cancel", want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, r, _, _, _, _ := managerFixture(t)
			at := m.deps.Clock.Now()
			s := &visitReadStub{link: r.link, first: map[string]referral.Click{}, now: at}
			m.deps.Referral = s
			cfg := referral.EvidenceConfig{ProgramID: referral.ProgramID, ActiveKeyID: "fixture", Keys: map[string][]byte{"fixture": []byte("01234567890123456789012345678901")}, Window: 7 * 24 * time.Hour, VisitWindow: time.Hour, VisitIDs: &visitIDs{}}
			if tc.mode == "disabled" {
				cfg.VisitWindow = 0
			}
			var err error
			m.deps.Evidence, err = referral.NewEvidenceSigner(cfg, m.deps.Clock)
			require.NoError(t, err)
			req := PrepareVisitRequest{Code: s.link.Code, Consented: true}
			ctx := context.Background()
			var first PreparedVisit
			if tc.mode == "duplicate" || tc.mode == "duplicate_outage" || tc.mode == "expired" || tc.mode == "tamper" {
				first, err = m.PrepareVisit(ctx, req)
				require.NoError(t, err)
				req.PriorEvidence, req.PriorVisit = first.Evidence, first.VisitCookie
			}
			switch tc.mode {
			case "consent":
				req.Consented = false
				req.Code = "unknown"
			case "bot":
				req.KnownBot = true
			case "outage", "duplicate_outage":
				s.err = errors.Join(referral.ErrNotFound, referral.ErrUnavailable)
			case "expired":
				m.deps.Clock = managerClock{at.Add(2 * time.Hour)}
				s.now = m.deps.Clock.Now()
				m.deps.Evidence, err = referral.NewEvidenceSigner(cfg, m.deps.Clock)
				require.NoError(t, err)
			case "tamper":
				req.PriorVisit += "X"
			case "foreign":
				foreign := s.link
				foreign.ID, foreign.Code = "foreign-link", "foreign-code"
				req.PriorEvidence, _, err = m.deps.Evidence.Issue(foreign)
				require.NoError(t, err)
				req.PriorVisit, _, err = m.deps.Evidence.IssueVisit(foreign)
				require.NoError(t, err)
			case "pause":
				m.deps.Controls.Attribution = false
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "dependency_cancelled":
				s.err = context.Canceled
			case "raced_cancel", "bot_cancel":
				ctx, s.cancel = context.WithCancel(ctx)
				t.Cleanup(s.cancel)
				if tc.mode == "raced_cancel" {
					s.err = referral.ErrUnavailable
				} else {
					req.KnownBot = true
				}
			}
			out, err := m.PrepareVisit(ctx, req)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				return
			}
			require.Equal(t, tc.measurement, out.Measurement)
			if tc.mode == "foreign" {
				require.NotEqual(t, req.PriorEvidence, out.Evidence)
				require.NotEqual(t, req.PriorVisit, out.VisitCookie)
				e, err := m.deps.Evidence.VerifyCurrent(out.Evidence)
				require.NoError(t, err)
				require.Equal(t, s.link.ID, e.LinkID)
			}
			if tc.mode == "consent" || tc.mode == "bot" {
				require.Empty(t, out.Evidence)
				require.Empty(t, out.VisitCookie)
			} else {
				verified, err := m.deps.Evidence.VerifyCurrent(out.Evidence)
				require.NoError(t, err)
				if tc.mode == "outage" || tc.mode == "disabled" {
					require.Empty(t, verified.MeasuredClickID)
				} else {
					require.NotEmpty(t, verified.MeasuredClickID)
				}
			}
			if first.Evidence != "" {
				require.Equal(t, first.Evidence, out.Evidence)
				require.Equal(t, first.EvidenceExpiresAt, out.EvidenceExpiresAt)
				if tc.mode == "duplicate" || tc.mode == "duplicate_outage" {
					require.Equal(t, first.VisitCookie, out.VisitCookie)
					require.Equal(t, first.VisitExpiresAt, out.VisitExpiresAt)
				} else {
					require.NotEqual(t, first.VisitCookie, out.VisitCookie)
				}
			}
			if tc.mode == "consent" || tc.mode == "disabled" {
				require.Zero(t, s.calls)
			}
			body, err := json.Marshal(out)
			require.NoError(t, err)
			require.NotContains(t, string(body), "fixture.")
			require.NotContains(t, string(body), "click-")
			var decoded PrepareVisitRequest
			require.NoError(t, json.Unmarshal([]byte(`{"Code":"forged","Consented":true,"KnownBot":true,"PriorEvidence":"forged"}`), &decoded))
			require.Empty(t, decoded)
		})
	}
}
