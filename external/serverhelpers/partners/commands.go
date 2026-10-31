package partners

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billinglifecycle"
	paymenthelpers "github.com/ooaklee/ghatd/external/paymentprovider/helpers"
	"github.com/spf13/cobra"
)

// PreparationOptions contains acknowledged prerequisites and bounded scan limits.
type PreparationOptions struct {
	Limits                                            billinglifecycle.PreparationConfig
	WritersDrained, MigrationsApplied, BackupVerified bool
}

// ErrPreparationPrerequisites indicates missing operator acknowledgements.
var ErrPreparationPrerequisites = errors.New("partners/preparation-prerequisites-required: attest older writer drain, applied migrations and verified backup; acknowledgements are not verification")

// Validate checks acknowledgements and bounded scan limits.
func (o PreparationOptions) Validate() error {
	if !o.WritersDrained || !o.MigrationsApplied || !o.BackupVerified {
		return ErrPreparationPrerequisites
	}
	return o.Limits.ValidateLimits()
}

// PreparationExecutor owns host configuration, authority and actual store access.
type PreparationExecutor func(context.Context, PreparationOptions) (billinglifecycle.PreparationReport, error)
type commandError struct {
	message string
	cause   error
}

func (e commandError) Error() string { return e.message }
func (e commandError) Unwrap() error { return e.cause }

// NewPreparationCommand constructs the explicit bounded operator command.
func NewPreparationCommand(run PreparationExecutor) *cobra.Command {
	opts := PreparationOptions{Limits: billinglifecycle.PreparationConfig{PageSize: 50, MaxPages: 1000, Timeout: 30 * time.Minute}}
	cmd := &cobra.Command{Use: "partners-prepare", Short: "Explicit bounded preparation of native Partners billing history", Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true}
	cmd.Flags().IntVar(&opts.Limits.PageSize, "page-size", 50, "Native rows per page (1..200)")
	cmd.Flags().IntVar(&opts.Limits.MaxPages, "max-pages", 1000, "Total page budget across scopes (1..10000); rerun resumes")
	cmd.Flags().DurationVar(&opts.Limits.Timeout, "timeout", 30*time.Minute, "Work deadline including composition (1s..1h); cleanup is bounded to 10s")
	cmd.Flags().BoolVar(&opts.WritersDrained, "older-writers-drained", false, "Operator acknowledgement that older billing writers were drained")
	cmd.Flags().BoolVar(&opts.MigrationsApplied, "migrations-applied", false, "Operator acknowledgement that the reviewed migration chain was applied")
	cmd.Flags().BoolVar(&opts.BackupVerified, "backup-verified", false, "Operator acknowledgement that database/history/key backup and restore were verified")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), opts.Limits.Timeout)
		defer cancel()
		var report billinglifecycle.PreparationReport
		var err error
		if run == nil {
			err = billing.ErrRevenueUnavailable
		} else {
			report, err = run(ctx, opts)
		}
		if err == nil || err == billinglifecycle.ErrPreparationBudget {
			if _, e := fmt.Fprintf(cmd.OutOrStdout(), "pages=%d restarts=%d scopes_completed=%d complete=%t operator_prerequisites=attested\n", report.Pages, report.Restarts, report.ScopesCompleted, report.Complete); e != nil {
				return commandError{"partners/preparation-incomplete: preserve native history and rerun under current authority to resume; inspect operator evidence before activation", errors.Join(err, e)}
			}
		}
		if err != nil {
			return commandError{"partners/preparation-incomplete: preserve native history and rerun under current authority to resume; inspect operator evidence before activation", err}
		}
		return nil
	}
	return cmd
}

// RefundRecoveryOptions identifies the retained original request and deadline.
type RefundRecoveryOptions struct {
	ObservationID, ExpectedFingerprint, SnapshotFile string
	Timeout                                          time.Duration
}

func validID(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// Validate rejects malformed identifiers, absent snapshot paths and out-of-range timeouts.
func (o RefundRecoveryOptions) Validate() error {
	if !validID(o.ObservationID) || !validID(o.ExpectedFingerprint) || o.SnapshotFile == "" || o.Timeout < time.Second || o.Timeout > 5*time.Minute {
		return billing.ErrRevenueInvalid
	}
	return nil
}

// RefundRecoveryExecutor supplies the host's admitted recovery implementation.
type RefundRecoveryExecutor func(context.Context, RefundRecoveryOptions) (billing.RevenueObservation, error)

// NewRefundRecoveryCommand builds the explicit bounded recovery command.
func NewRefundRecoveryCommand(run RefundRecoveryExecutor) *cobra.Command {
	opts := RefundRecoveryOptions{Timeout: time.Minute}
	cmd := &cobra.Command{Use: "partners-recover-refund", Short: "Recover a retained legacy refund quarantine with original snapshot proof", Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true}
	cmd.Flags().StringVar(&opts.ObservationID, "observation-id", "", "Owning quarantine observation ID")
	cmd.Flags().StringVar(&opts.ExpectedFingerprint, "expected-fingerprint", "", "Owning observation fingerprint, unchanged from the original request")
	cmd.Flags().StringVar(&opts.SnapshotFile, "original-snapshot-file", "", "Private retained original webhook JSON file (regular file, at most 2 MiB)")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", time.Minute, "Bounded recovery deadline (1s..5m)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		wrap := func(err error) error {
			return commandError{"partners/refund-recovery-incomplete: retain the original snapshot and request; inspect owning evidence before retry", err}
		}
		if err := opts.Validate(); err != nil {
			return wrap(err)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), opts.Timeout)
		defer cancel()
		var resolution billing.RevenueObservation
		var err error
		if run == nil {
			err = billing.ErrRevenueUnavailable
		} else {
			resolution, err = run(ctx, opts)
		}
		if err == nil {
			err = ctx.Err()
		}
		if err == nil && (resolution.ResolutionOf != opts.ObservationID || resolution.QuarantineReason != "" || resolution.ID == "" || resolution.AcceptedAt.IsZero()) {
			err = billing.ErrRevenueUnavailable
		}
		if err == nil {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "resolved=true facts=%d original_history=preserved\n", len(resolution.FactIDs))
		}
		if err != nil {
			return wrap(err)
		}
		return nil
	}
	return cmd
}

// ReadRefundSnapshot uses the provider owner's bounded file screening while
// retaining filesystem causes and the billing invalid-input classification.
func ReadRefundSnapshot(path string) ([]byte, error) {
	data, err := paymenthelpers.ReadRetainedStripeRefundSnapshotFile(path)
	if errors.Is(err, paymenthelpers.ErrStripeRetainedSnapshotInvalid) {
		return nil, errors.Join(billing.ErrRevenueInvalid, err)
	}
	return data, err
}
