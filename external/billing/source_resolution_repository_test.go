package billing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sourceResolutionFallbackRepo struct {
	RevenueRepository
	rows   map[string]RevenueObservation
	errors map[string]error
	reads  []string
}

func (r *sourceResolutionFallbackRepo) GetRevenueObservation(_ context.Context, id string) (RevenueObservation, error) {
	r.reads = append(r.reads, id)
	if err := r.errors[id]; err != nil {
		return RevenueObservation{}, err
	}
	if row, ok := r.rows[id]; ok {
		return row, nil
	}
	return RevenueObservation{}, ErrRevenueNotFound
}
func (*sourceResolutionFallbackRepo) UnresolvedRevenueObservations(context.Context, int) ([]RevenueObservation, error) {
	panic("unused fixture capability")
}
func (*sourceResolutionFallbackRepo) FindPaymentRevenueFacts(context.Context, RevenueScope, string) ([]RevenueFact, error) {
	panic("unused fixture capability")
}

type sourceResolutionOptionalRepo struct {
	*sourceResolutionFallbackRepo
	result RevenueObservation
	err    error
	reads  []string
}

func (r *sourceResolutionOptionalRepo) GetRevenueSourceResolution(_ context.Context, id string) (RevenueObservation, error) {
	r.reads = append(r.reads, id)
	return r.result, r.err
}

func TestSourceResolutionRepositoryCompatibility(t *testing.T) {
	original := RevenueObservation{ID: "original", Scope: RevenueScope{Provider: "fixture", AccountID: "account"}}
	resolution := RevenueObservation{ID: original.ResolutionID(), ResolutionOf: original.ID}
	failed := errors.New("fixture repository failure")
	cases := []struct {
		name                   string
		capability, canceled   bool
		id                     string
		originalErr, resultErr error
		missing                bool
		want                   RevenueObservation
		wantError              error
		wantFallbackReads      []string
		wantOptionalReads      []string
	}{
		{name: "existing_custom_repository_uses_original_then_resolution", want: resolution, wantFallbackReads: []string{original.ID, resolution.ID}},
		{name: "custom_repository_optional_absence", missing: true, wantError: ErrRevenueNotFound, wantFallbackReads: []string{original.ID, resolution.ID}},
		{name: "custom_repository_required_original_absence", originalErr: ErrRevenueNotFound, wantError: ErrRevenueNotFound, wantFallbackReads: []string{original.ID}},
		{name: "custom_repository_required_original_failure", originalErr: failed, wantError: failed, wantFallbackReads: []string{original.ID}},
		{name: "custom_repository_resolution_failure", resultErr: failed, wantError: failed, wantFallbackReads: []string{original.ID, resolution.ID}},
		{name: "optional_repository_owns_complete_snapshot", capability: true, want: resolution, wantOptionalReads: []string{original.ID}},
		{name: "optional_repository_not_found_contract", capability: true, missing: true, wantError: ErrRevenueNotFound, wantOptionalReads: []string{original.ID}},
		{name: "optional_repository_failure_preserved", capability: true, resultErr: failed, wantError: failed, wantOptionalReads: []string{original.ID}},
		{name: "invalid_identity_precedes_optional_repository", capability: true, id: strings.Repeat("x", 257), wantError: ErrRevenueInvalid},
		{name: "cancellation_precedes_optional_repository", capability: true, canceled: true, wantError: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fallback := &sourceResolutionFallbackRepo{rows: map[string]RevenueObservation{original.ID: original, resolution.ID: resolution}, errors: map[string]error{original.ID: tc.originalErr, resolution.ID: tc.resultErr}}
			if tc.missing {
				delete(fallback.rows, resolution.ID)
			}
			optional := &sourceResolutionOptionalRepo{sourceResolutionFallbackRepo: fallback, result: resolution, err: tc.resultErr}
			if tc.missing {
				optional.result, optional.err = RevenueObservation{}, ErrRevenueNotFound
			} else if tc.resultErr != nil {
				optional.result = RevenueObservation{}
			}
			var repo RevenueRepository = fallback
			if tc.capability {
				repo = optional
			}
			service, err := NewRevenueService(repo, revenueTestClock{time.Now().UTC()})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.canceled {
				cancel()
			}
			id := tc.id
			if id == "" {
				id = original.ID
			}
			got, err := service.GetRevenueSourceResolution(ctx, id)
			require.Equal(t, tc.want, got)
			if tc.wantError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantError)
			}
			require.Equal(t, tc.wantFallbackReads, fallback.reads)
			require.Equal(t, tc.wantOptionalReads, optional.reads)
		})
	}
}
