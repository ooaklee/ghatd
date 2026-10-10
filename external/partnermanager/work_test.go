package partnermanager

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: new named boundaries verify strict absence, cancellation,
// immutable private input bounds and bounded retry scheduling independently of Mongo.
type workRepositorySpy struct {
	WorkRepository
	calls     int
	cursorErr error
	retryAt   time.Time
	decision  WorkDecision
}

func (r *workRepositorySpy) GetDiscoveryCursor(context.Context, string, string) (DiscoveryCursor, error) {
	r.calls++
	return DiscoveryCursor{}, r.cursorErr
}
func (r *workRepositorySpy) EnqueueWorkPage(context.Context, string, string, DiscoveryCursor, DiscoveryCursor, []WorkItem) error {
	r.calls++
	return nil
}
func (r *workRepositorySpy) RetryWork(_ context.Context, _ WorkItem, _ string, _ time.Time, next time.Time) error {
	r.calls++
	r.retryAt = next
	return nil
}
func (r *workRepositorySpy) RecordWorkDecision(_ context.Context, _ WorkItem, d WorkDecision, _ time.Time) (WorkDecision, error) {
	r.calls++
	r.decision = d
	return d, nil
}
func workTestConfig() WorkQueueConfig {
	return WorkQueueConfig{ProgramID: "program", LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour}
}
func TestWorkQueueConstruction(t *testing.T) {
	cases := []struct {
		name                       string
		nilRepo, nilClock          bool
		program                    string
		lease, retryBase, retryMax time.Duration
		want                       error
	}{
		{name: "explicit_valid_configuration"},
		{name: "typed_nil_repository", nilRepo: true, want: ErrUnavailable},
		{name: "typed_nil_clock", nilClock: true, want: ErrUnavailable},
		{name: "control_character_in_program", program: "program\nother", want: ErrInvalid},
		{name: "lease_exceeds_operational_bound", lease: 11 * time.Minute, want: ErrInvalid},
		{name: "retry_base_exceeds_max", retryBase: 2 * time.Hour, want: ErrInvalid},
		{name: "retry_max_exceeds_bound", retryMax: 25 * time.Hour, want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := workTestConfig()
			if tc.program != "" {
				cfg.ProgramID = tc.program
			}
			if tc.lease != 0 {
				cfg.LeaseDuration = tc.lease
			}
			if tc.retryBase != 0 {
				cfg.RetryBase = tc.retryBase
			}
			if tc.retryMax != 0 {
				cfg.RetryMax = tc.retryMax
			}
			var repo *workRepositorySpy
			if !tc.nilRepo {
				repo = &workRepositorySpy{}
			}
			var clock *managerClock
			if !tc.nilClock {
				clock = &managerClock{at: time.Unix(1700000000, 0).UTC()}
			}
			_, err := NewWorkQueue(repo, clock, cfg)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
func TestWorkQueueStrictDiscoveryAndSourceBounds(t *testing.T) {
	cases := []struct {
		name                             string
		cursorErr                        error
		source, fingerprint              string
		duplicate, nilContext, cancelled bool
		next                             DiscoveryCursor
		want                             error
		cursor                           bool
	}{
		{name: "conclusive_absence_starts_discovery", cursorErr: ErrNotFound, cursor: true},
		{name: "joined_absence_and_outage_does_not_start_new_sweep", cursorErr: errors.Join(ErrNotFound, ErrUnavailable), cursor: true, want: ErrUnavailable},
		{name: "oversized_source_identity", source: strings.Repeat("x", 257), want: ErrInvalid},
		{name: "missing_private_source_fingerprint", source: "source", want: ErrInvalid},
		{name: "duplicate_page_input", source: "source", fingerprint: "digest", duplicate: true, want: ErrInvalid},
		{name: "wrong_cursor_kind", source: "source", fingerprint: "digest", next: DiscoveryCursor{AfterID: "not_a_revenue_sequence"}, want: ErrInvalid},
		{name: "nil_context", nilContext: true, want: ErrInvalid},
		{name: "cancelled_context", cancelled: true, want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &workRepositorySpy{cursorErr: tc.cursorErr}
			queue, err := NewWorkQueue(repo, managerClock{at: time.Unix(1700000000, 0).UTC()}, workTestConfig())
			require.NoError(t, err)
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if tc.cursor {
				_, err = queue.Cursor(ctx, WorkRevenue)
			} else {
				items := []WorkCandidate{{SourceID: tc.source, SourceFingerprint: tc.fingerprint}}
				if tc.duplicate {
					items = append(items, items[0])
				}
				err = queue.EnqueuePage(ctx, WorkRevenue, DiscoveryCursor{}, tc.next, items)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
			if !tc.cursor {
				require.Zero(t, repo.calls)
			}
		})
	}
}
func TestWorkQueueRetryBoundsAndDecisionPrivatePayload(t *testing.T) {
	cases := []struct {
		name    string
		attempt int64
		delay   time.Duration
		code    string
		want    error
	}{
		{name: "first_attempt_delay", attempt: 1, delay: time.Second, code: "owning_unavailable"},
		{name: "second_attempt_delay", attempt: 2, delay: 2 * time.Second, code: "owning_unavailable"},
		{name: "large_attempt_count_caps_without_overflow", attempt: math.MaxInt64, delay: time.Hour, code: "owning_unavailable"},
		{name: "raw_error_is_not_persisted", attempt: 1, code: "private email@example.test", want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := managerClock{at: time.Unix(1700000000, 0).UTC()}
			repo := &workRepositorySpy{}
			queue, err := NewWorkQueue(repo, clock, workTestConfig())
			require.NoError(t, err)
			item := WorkItem{ID: workID("program", WorkRevenue, "source"), ProgramID: "program", Kind: WorkRevenue, SourceID: "source", SourceFingerprint: "source_digest", LeaseToken: "private_fencing_token", LeasedUntil: clock.Now().Add(time.Minute), Attempts: tc.attempt}
			err = queue.Retry(context.Background(), item, tc.code)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, repo.calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, clock.Now().Add(tc.delay), repo.retryAt)
			d, err := queue.Decide(context.Background(), item, "current_worker", WorkAccepted, "financial_receipt", "financial_accepted")
			require.NoError(t, err)
			require.Equal(t, "current_worker", d.ActorID)
			require.NotEmpty(t, d.Fingerprint)
			other, err := queue.Decide(context.Background(), item, "another_worker", WorkAccepted, "financial_receipt", "financial_accepted")
			require.NoError(t, err)
			require.NotEqual(t, d.ID, other.ID)
		})
	}
}
