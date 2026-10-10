package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type statusResolutionTestRepo struct {
	*statusTestRepo
	captureErr, headErr error
	after               func()
}

func (r *statusResolutionTestRepo) ReadSubscriptionStatusOriginal(ctx context.Context, scope RevenueScope, sub, id string) (SubscriptionStatusOriginalSnapshot, error) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	if r.after != nil {
		defer r.after()
	}
	tx := &statusTestTx{heads: r.heads, captures: r.captures, scope: scope, sub: sub, key: SubscriptionStatusIdentity(scope, sub)}
	capture, err := tx.GetCapture(ctx, id)
	if r.captureErr != nil {
		err = r.captureErr
	}
	if err == nil {
		return SubscriptionStatusOriginalSnapshot{CaptureFound: true, Capture: capture}, nil
	}
	if !singleRevenueCause(err, ErrRevenueNotFound) {
		return SubscriptionStatusOriginalSnapshot{}, err
	}
	current, err := tx.GetCurrent(ctx)
	if r.headErr != nil {
		err = r.headErr
	}
	if err == nil {
		return SubscriptionStatusOriginalSnapshot{CurrentFound: true, Current: current}, nil
	}
	if singleRevenueCause(err, ErrRevenueNotFound) {
		return SubscriptionStatusOriginalSnapshot{}, nil
	}
	return SubscriptionStatusOriginalSnapshot{}, err
}

func TestSubscriptionStatusOriginalResolution(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		want        error
	}{
		{"pending_first", SubscriptionStatusPending, nil},
		{"pending_current", SubscriptionStatusPending, nil},
		{"captured", SubscriptionStatusCaptured, nil},
		{"captured_before_corrupt_later_head", SubscriptionStatusCaptured, nil},
		{"superseded", SubscriptionStatusSuperseded, nil},
		{"missing_expected_head", "", ErrRevenueUnavailable},
		{"lower_head", "", ErrRevenueConflict},
		{"joined_capture_absence_outage", "", ErrRevenueUnavailable},
		{"joined_capture_absence_unknown", "", ErrRevenueUncertain},
		{"wrapped_capture_absence", SubscriptionStatusPending, nil},
		{"joined_head_absence_outage", "", ErrRevenueUnavailable},
		{"corrupt_head_receipt", "", ErrRevenueConflict},
		{"cancel_after_read", "", context.Canceled},
		{"legacy_optional_unavailable", "", ErrRevenueUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, base, clock, f := statusFixture(t)
			repo := &statusResolutionTestRepo{statusTestRepo: base}
			service, err := NewRevenueService(repo, clock)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, err := s.PrepareSubscriptionStatus(ctx, "original", f.ID)
			require.NoError(t, err)
			switch tc.name {
			case "pending_current", "missing_expected_head", "lower_head", "corrupt_head_receipt":
				first, e := s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, "trialing"))
				require.NoError(t, e)
				clock.at = clock.at.Add(time.Second)
				p, e = s.PrepareSubscriptionStatus(ctx, "original", f.ID)
				require.NoError(t, e)
				key := SubscriptionStatusIdentity(p.Scope, p.SubscriptionID)
				if tc.name == "missing_expected_head" {
					delete(base.heads, key)
				}
				if tc.name == "corrupt_head_receipt" {
					v := base.captures[first.Preparation.CaptureID]
					v.Status = "canceled"
					base.captures[first.Preparation.CaptureID] = v
				}
				if tc.name == "lower_head" {
					_, e = s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, "active"))
					require.NoError(t, e)
					clock.at = clock.at.Add(time.Second)
					p, e = s.PrepareSubscriptionStatus(ctx, "original", f.ID)
					require.NoError(t, e)
					base.heads[key] = first
				}
			case "captured", "captured_before_corrupt_later_head":
				_, e := s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, "trialing"))
				require.NoError(t, e)
				if tc.name == "captured_before_corrupt_later_head" {
					clock.at = clock.at.Add(time.Second)
					later, e := s.PrepareSubscriptionStatus(ctx, "later", f.ID)
					require.NoError(t, e)
					_, e = s.CaptureVerifiedSubscriptionStatus(ctx, later, statusEvidence(later, "active"))
					require.NoError(t, e)
					key := SubscriptionStatusIdentity(p.Scope, p.SubscriptionID)
					v := base.heads[key]
					v.Status = "corrupt"
					base.heads[key] = v
				}
			case "superseded":
				clock.at = clock.at.Add(time.Second)
				other, e := s.PrepareSubscriptionStatus(ctx, "other", f.ID)
				require.NoError(t, e)
				_, e = s.CaptureVerifiedSubscriptionStatus(ctx, other, statusEvidence(other, "active"))
				require.NoError(t, e)
			case "joined_capture_absence_outage":
				repo.captureErr = errors.Join(ErrRevenueNotFound, ErrRevenueUnavailable)
			case "joined_capture_absence_unknown":
				repo.captureErr = errors.Join(ErrRevenueNotFound, ErrRevenueUncertain)
			case "wrapped_capture_absence":
				repo.captureErr = fmt.Errorf("wrapped: %w", ErrRevenueNotFound)
			case "joined_head_absence_outage":
				repo.headErr = errors.Join(ErrRevenueNotFound, ErrRevenueUnavailable)
			case "cancel_after_read":
				repo.after = cancel
			case "legacy_optional_unavailable":
				service = s
			}
			beforeCaptures, beforeHeads := len(base.captures), len(base.heads)
			out, err := service.ResolveSubscriptionStatus(ctx, p)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, SubscriptionStatusResolution{}, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.state, out.State)
				require.NoError(t, out.Validate())
				require.True(t, sameStatusOriginal(p, out.Preparation))
				if out.State == SubscriptionStatusSuperseded {
					_, e := s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, "trialing"))
					require.ErrorIs(t, e, ErrRevenueConflict)
				}
			}
			require.Equal(t, beforeCaptures, len(base.captures))
			require.Equal(t, beforeHeads, len(base.heads))
		})
	}
}
