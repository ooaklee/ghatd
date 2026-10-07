package referral

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type visitExpiryClock struct {
	first, expired time.Time
	calls          int
}

func (c *visitExpiryClock) Now() time.Time {
	c.calls++
	if c.calls == 1 {
		return c.first
	}
	return c.expired
}

type rejectedVisitIDs struct{ value string }

func (s rejectedVisitIDs) NewID() string { return s.value }

type visitRepositoryFault struct {
	Repository
	receiptErr, recordErr, dayErr error
}

func (s visitRepositoryFault) WithVisitTransaction(ctx context.Context, link string, fn func(Repository) error) error {
	return s.Repository.WithVisitTransaction(ctx, link, func(tx Repository) error {
		return fn(visitRepositoryFault{Repository: tx, receiptErr: s.receiptErr, recordErr: s.recordErr, dayErr: s.dayErr})
	})
}
func (s visitRepositoryFault) PutVisitDay(ctx context.Context, d VisitDay, expected int64) error {
	if s.dayErr != nil {
		return s.dayErr
	}
	return s.Repository.PutVisitDay(ctx, d, expected)
}
func (s visitRepositoryFault) GetVisitReceipt(ctx context.Context, link, digest string) (VisitReceipt, error) {
	if s.receiptErr != nil {
		return VisitReceipt{}, s.receiptErr
	}
	return s.Repository.GetVisitReceipt(ctx, link, digest)
}
func (s visitRepositoryFault) RecordClick(ctx context.Context, c Click, expires time.Time) error {
	if s.recordErr != nil {
		return s.recordErr
	}
	return s.Repository.RecordClick(ctx, c, expires)
}

func visitSigner(t *testing.T, clock Clock) *EvidenceSigner {
	t.Helper()
	s, err := NewEvidenceSigner(EvidenceConfig{ProgramID: ProgramID, ActiveKeyID: "fixture", Keys: map[string][]byte{"fixture": []byte("01234567890123456789012345678901")}, Window: 7 * 24 * time.Hour, VisitWindow: time.Hour, VisitIDs: &seqIDs{}}, clock)
	require.NoError(t, err)
	return s
}

// Audit disposition: named admitted/excluded/replay/error cases use independent
// actual services and transactional mutable fakes; no inferred person identity.
func TestVisitObservationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode, class string
		rows, receipts    int
		want              error
	}{
		{name: "first_consent_records_one_measured_visit", class: VisitEligible, rows: 1, receipts: 1},
		{name: "reload_reuses_first_measurement", mode: "duplicate", class: VisitDuplicate, rows: 2, receipts: 1},
		{name: "known_bot_never_seeds_measurement", mode: "bot", class: VisitKnownBot, rows: 1},
		{name: "no_consent_has_no_observation", mode: "consent"},
		{name: "retired_link_does_not_measure", mode: "retired", want: ErrCodeRetired},
		{name: "foreign_identity_cannot_be_aliased", mode: "link", want: ErrInvalid},
		{name: "expiry_is_exclusive", mode: "expiry", want: ErrDenied},
		{name: "expiry_during_transaction_is_exclusive", mode: "transaction_expiry", want: ErrDenied},
		{name: "malformed_digest_is_not_a_visit", mode: "digest", want: ErrInvalid},
		{name: "dependency_failure_discards_partial_state", mode: "outage", want: ErrUnavailable},
		{name: "joined_link_absence_and_outage_is_not_missing", mode: "joined_link", want: ErrUnavailable},
		{name: "joined_receipt_absence_and_outage_is_not_missing", mode: "joined_receipt", want: ErrUnavailable},
		{name: "failed_observation_rolls_back_new_receipt", mode: "record_failure", want: ErrUnavailable},
		{name: "failed_daily_count_rolls_back_new_receipt", mode: "day_failure", want: ErrUnavailable},
		{name: "empty_generated_id_does_not_commit", mode: "empty_id", want: ErrInvalid},
		{name: "control_character_generated_id_does_not_commit", mode: "invalid_id", want: ErrInvalid},
		{name: "known_bot_generated_id_is_validated", mode: "bot_id", want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, _, link, _ := fixture(t)
			signer := visitSigner(t, s.clock)
			_, identity, err := signer.IssueVisit(link)
			require.NoError(t, err)
			req := VisitRequest{Code: link.Code, Identity: identity, Consented: true}
			var first VisitObservation
			switch tc.mode {
			case "duplicate":
				first, err = s.ObserveVisit(context.Background(), req)
				require.NoError(t, err)
			case "bot":
				req.KnownBot = true
				req.Identity = VisitIdentity{}
			case "consent":
				req.Consented = false
				req.Code = "missing"
			case "retired":
				require.NoError(t, s.RetireLink(context.Background(), link.ID, "reviewed rotation", "operator"))
			case "link":
				req.Identity.LinkID = "other"
			case "expiry":
				req.Identity.ExpiresAt = s.clock.Now()
			case "transaction_expiry":
				s.clock = &visitExpiryClock{first: s.clock.Now(), expired: req.Identity.ExpiresAt}
			case "digest":
				req.Identity.Digest = "not-a-digest"
			case "outage":
				repo.fail = ErrUnavailable
			case "joined_link":
				repo.fail = errors.Join(ErrNotFound, ErrUnavailable)
			case "joined_receipt":
				s.repo = visitRepositoryFault{Repository: repo, receiptErr: errors.Join(ErrNotFound, ErrUnavailable)}
			case "record_failure":
				s.repo = visitRepositoryFault{Repository: repo, recordErr: ErrUnavailable}
			case "day_failure":
				s.repo = visitRepositoryFault{Repository: repo, dayErr: ErrUnavailable}
			case "empty_id":
				s.ids = rejectedVisitIDs{}
			case "invalid_id", "bot_id":
				s.ids = rejectedVisitIDs{value: "invalid\n"}
				req.KnownBot = tc.mode == "bot_id"
			}
			out, err := s.ObserveVisit(context.Background(), req)
			require.ErrorIs(t, err, tc.want)
			require.Len(t, repo.clicks, tc.rows)
			require.Len(t, repo.visits, tc.receipts)
			if tc.rows == 0 {
				require.Empty(t, repo.days)
			} else {
				require.Len(t, repo.days, 1)
				for _, day := range repo.days {
					require.EqualValues(t, tc.rows, day.Counts.Observations)
				}
			}
			if tc.want != nil {
				require.Empty(t, out)
				return
			}
			require.Equal(t, tc.class, out.Click.Classification)
			if tc.class == VisitEligible || tc.class == VisitDuplicate {
				require.NotEmpty(t, out.Eligible.ID)
				require.Equal(t, out.Eligible.ID, out.Click.MeasuredClickID)
				require.Empty(t, out.Click.UserAgent)
			} else {
				require.Empty(t, out.Eligible)
				require.Empty(t, out.Click.MeasuredClickID)
			}
			if tc.mode == "duplicate" {
				require.Equal(t, first.Eligible, out.Eligible)
				require.NotEqual(t, first.Click.ID, out.Click.ID)
			}
		})
	}
}

func TestMeasuredEvidenceAndImmutableSignup(t *testing.T) {
	for _, measured := range []bool{false, true} {
		name := "unmeasured"
		if measured {
			name = "measured"
		}
		t.Run(name, func(t *testing.T) {
			s, repo, p, link, eligibility := fixture(t)
			signer := visitSigner(t, s.clock)
			var token string
			var evidence Evidence
			var err error
			if measured {
				_, identity, e := signer.IssueVisit(link)
				require.NoError(t, e)
				visit, e := s.ObserveVisit(context.Background(), VisitRequest{Code: link.Code, Identity: identity, Consented: true})
				require.NoError(t, e)
				token, evidence, err = signer.IssueMeasured(link, visit.Eligible)
			} else {
				token, evidence, err = signer.Issue(link)
			}
			require.NoError(t, err)
			verified, err := signer.Verify(token, eligibility.At)
			require.NoError(t, err)
			require.Equal(t, evidence, verified)
			eligibility.Evidence = verified
			first, err := s.LockAttribution(context.Background(), p, link, eligibility)
			require.NoError(t, err)
			require.Equal(t, verified.MeasuredClickID, first.SourceMeasuredClickID)
			// Analytics disappearance cannot become an eligibility dependency.
			repo.clicks = nil
			repo.visits = map[string]VisitReceipt{}
			again, err := s.LockAttribution(context.Background(), p, link, eligibility)
			require.NoError(t, err)
			require.Equal(t, first, again)
			if measured {
				require.NotEmpty(t, first.SourceMeasuredClickID)
			} else {
				require.Empty(t, first.SourceMeasuredClickID)
			}
		})
	}
}

func TestEvidenceMeasurementCompatibilityAndScope(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{name: "legacy_json_and_digest_remain_byte_identical", mode: "legacy"},
		{name: "foreign_link_click_cannot_be_signed", mode: "foreign", want: ErrDenied},
		{name: "duplicate_observation_cannot_become_a_new_origin", mode: "duplicate", want: ErrDenied},
		{name: "missing_digest_cannot_be_signed", mode: "digest", want: ErrDenied},
		{name: "observation_before_link_creation_cannot_be_signed", mode: "before", want: ErrDenied},
		{name: "signer_uses_trusted_structural_origin_without_repository_IO", mode: "trusted"},
		{name: "future_observation_cannot_be_signed", mode: "future", want: ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, link, _ := fixture(t)
			signer := visitSigner(t, s.clock)
			if tc.mode == "legacy" {
				_, evidence, err := signer.Issue(link)
				require.NoError(t, err)
				type oldEvidence struct {
					ProgramID string    `json:"program_id"`
					Audience  string    `json:"audience"`
					Code      string    `json:"code"`
					LinkID    string    `json:"link_id"`
					IssuedAt  time.Time `json:"issued_at"`
					ExpiresAt time.Time `json:"expires_at"`
				}
				old := oldEvidence{evidence.ProgramID, evidence.Audience, evidence.Code, evidence.LinkID, evidence.IssuedAt, evidence.ExpiresAt}
				prior, err := json.Marshal(old)
				require.NoError(t, err)
				current, err := json.Marshal(evidence)
				require.NoError(t, err)
				require.Equal(t, prior, current)
				require.NotContains(t, string(current), "measured_click_id")
				return
			}
			click := Click{ID: "click", LinkID: link.ID, Code: link.Code, OccurredAt: s.clock.Now(), Classification: VisitEligible, MeasuredClickID: "click", VisitDigest: strings.Repeat("a", 64)}
			switch tc.mode {
			case "foreign":
				click.LinkID = "other"
			case "duplicate":
				click.Classification = VisitDuplicate
			case "digest":
				click.VisitDigest = ""
			case "before":
				click.OccurredAt = link.CreatedAt.Add(-time.Second)
			case "future":
				click.OccurredAt = click.OccurredAt.Add(time.Second)
			}
			token, evidence, err := signer.IssueMeasured(link, click)
			require.ErrorIs(t, err, tc.want)
			if tc.mode == "trusted" {
				require.NotEmpty(t, token)
				require.Equal(t, click.ID, evidence.MeasuredClickID)
			} else {
				require.Empty(t, token)
				require.Empty(t, evidence)
			}
		})
	}
}
