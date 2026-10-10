package partnerhttp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

// These are transport/error-admission checks. Native real-owner financial and
// live scoped-policy dispatch are covered in the server runtime tests.
func TestPartnersDisabledAndUnverifiedTransportAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		p      Principal
		status int
	}{{"disabled", Principal{ActorID: "member", Credential: "fixture-session", Verified: true}, 503}, {"unverified", Principal{ActorID: "member", Credential: "fixture-session"}, 403}, {"public", Principal{}, 401}, {"no_identity", Principal{Verified: true}, 401}} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Service{}
			_, err := m.Read(context.Background(), tc.p, Request{Operation: "partners.overview.read"})
			var e *Error
			require.ErrorAs(t, err, &e)
			require.Equal(t, tc.status, e.Status)
		})
	}
}
func TestPartnersErrorMappingKeepsOutagesAndUncertainOutcomesHonest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"assessed_ineligible", partnermanager.ErrIneligible, 403, "PARTNERS_ACQUISITION_INELIGIBLE"},
		{"outage_overrides_ineligible", errors.Join(partnermanager.ErrIneligible, partnermanager.ErrUnavailable), 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"},
		{"unknown_overrides_ineligible", errors.Join(partnermanager.ErrIneligible, errors.New("private-secret-canary")), 500, "PARTNERS_INTERNAL_ERROR"},
		{"unresolved_source_is_retryable", partnerearnings.ErrUnresolved, 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"},
		{"currency_mismatch_is_conflict", partnerearnings.ErrCurrencyMismatch, 409, "PARTNERS_CONFLICT"},
		{"program_uncertain_is_reconciled", partnerprogram.ErrUncertain, 503, "PARTNERS_OUTCOME_UNCERTAIN"},
		{"owning_absence", partnermanager.ErrNotPartner, 404, "PARTNERS_NOT_FOUND"}, {"wrapped_absence", fmt.Errorf("private diagnostic: %w", referral.ErrNotFound), 404, "PARTNERS_NOT_FOUND"},
		{"outage_joined_to_absence", errors.Join(referral.ErrNotFound, referral.ErrUnavailable), 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"}, {"uncertain_commit_joined_to_denial", errors.Join(partnerearnings.ErrUncertain, partnermanager.ErrDenied), 503, "PARTNERS_OUTCOME_UNCERTAIN"},
		{"capacity_is_not_zero", partnerearnings.ErrReportTooLarge, 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"}, {"stale_revision", partnerearnings.ErrStaleWrite, 412, "PARTNERS_STALE_WRITE"}, {"changed_replay", partnerearnings.ErrConflict, 409, "PARTNERS_CONFLICT"}, {"insufficient", partnerearnings.ErrInsufficient, 409, "PARTNERS_INSUFFICIENT_FUNDS"},
		{"unknown_joined_to_absence", errors.Join(referral.ErrNotFound, errors.New("private-secret-canary")), 500, "PARTNERS_INTERNAL_ERROR"},
		{"unknown_joined_to_denial", errors.Join(partnermanager.ErrDenied, errors.New("private-secret-canary")), 500, "PARTNERS_INTERNAL_ERROR"},
		{"private_host_error_code", &Error{Code: "private-secret-canary", Status: 403}, 500, "PARTNERS_INTERNAL_ERROR"},
		{"private_driver_failure", errors.New("private-secret-canary"), 500, "PARTNERS_INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e *Error
			require.ErrorAs(t, partnerError(tc.err), &e)
			require.Equal(t, tc.status, e.Status)
			require.Equal(t, tc.code, e.Code)
			require.NotContains(t, e.Error(), "private")
		})
	}
}
func TestPartnersQueriesRejectAmbiguousOrInvalidCohorts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		q     url.Values
		valid bool
	}{
		{"default_page", nil, true}, {"valid_half_open_cohort", url.Values{"from": {"2026-10-01T00:00:00+01:00"}, "to": {"2026-10-02T00:00:00Z"}, "limit": {"100"}, "before_sequence": {"42"}}, true},
		{"duplicate_limit", url.Values{"limit": {"10", "20"}}, false}, {"invalid_limit", url.Values{"limit": {"101"}}, false}, {"duplicate_date", url.Values{"from": {"2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z"}}, false}, {"zero_date", url.Values{"from": {"0001-01-01T00:00:00Z"}}, false}, {"equal_dates", url.Values{"from": {"2026-10-01T00:00:00Z"}, "to": {"2026-10-01T00:00:00Z"}}, false}, {"reversed_dates", url.Values{"from": {"2026-10-02T00:00:00Z"}, "to": {"2026-10-01T00:00:00Z"}}, false}, {"invalid_sequence", url.Values{"before_sequence": {"-1"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, err := partnerStatementQuery(Request{Query: tc.q})
			if tc.valid {
				require.NoError(t, err)
				require.Greater(t, q.Limit, 0)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestPartnersClaimStatesRejectUnknownDuplicateAndUnboundedFilters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		states []string
		valid  bool
	}{
		{"all_states_default", nil, true},
		{"known_union", []string{"requested", "needs_review"}, true},
		{"all_six_states", []string{"requested", "processing", "needs_review", "paid", "rejected", "cancelled"}, true},
		{"unknown_state", []string{"pending"}, false},
		{"empty_state", []string{""}, false},
		{"case_mismatch", []string{"PAID"}, false},
		{"duplicate_state", []string{"requested", "requested"}, false},
		{"too_many_states", []string{"requested", "processing", "needs_review", "paid", "rejected", "cancelled", "paid"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := partnerClaimStates(Request{Query: url.Values{"state": tc.states}})
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.states, got)
				if len(got) > 0 {
					got[0] = "changed"
					require.NotEqual(t, got[0], tc.states[0])
				}
			} else {
				var e *Error
				require.ErrorAs(t, err, &e)
				require.Equal(t, 400, e.Status)
			}
		})
	}
}
