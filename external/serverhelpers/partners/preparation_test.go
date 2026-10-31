package partners

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billinglifecycle"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

func TestPartnersPreparationCommandPreflightAndPrivateOutput(t *testing.T) {
	for _, tc := range []string{"help", "missing_drain", "missing_migrations", "missing_backup", "zero_page", "unbounded_pages", "zero_timeout", "unbounded_timeout", "unexpected_argument", "complete", "budget_progress", "uncertain_private", "joined_budget_uncertainty"} {
		t.Run(tc, func(t *testing.T) {
			calls := 0
			run := func(ctx context.Context, opts PreparationOptions) (billinglifecycle.PreparationReport, error) {
				calls++
				require.NoError(t, opts.Validate())
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 30*time.Minute)
				switch tc {
				case "budget_progress":
					return billinglifecycle.PreparationReport{Pages: 2}, billinglifecycle.ErrPreparationBudget
				case "uncertain_private":
					return billinglifecycle.PreparationReport{Pages: 99, Complete: true}, errors.Join(recordstore.ErrUncertain, errors.New("private-source secret@example.test mongodb://credentials"))
				case "joined_budget_uncertainty":
					return billinglifecycle.PreparationReport{Pages: 99}, errors.Join(billinglifecycle.ErrPreparationBudget, recordstore.ErrUncertain)
				}
				return billinglifecycle.PreparationReport{Pages: 6, ScopesCompleted: 1, Complete: true}, nil
			}
			cmd := NewPreparationCommand(run)
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			args := []string{"--older-writers-drained", "--migrations-applied", "--backup-verified"}
			switch tc {
			case "help":
				args = []string{"--help"}
			case "missing_drain":
				args = args[1:]
			case "missing_migrations":
				args = []string{args[0], args[2]}
			case "missing_backup":
				args = args[:2]
			case "zero_page":
				args = append(args, "--page-size=0")
			case "unbounded_pages":
				args = append(args, "--max-pages=10001")
			case "zero_timeout":
				args = append(args, "--timeout=0s")
			case "unbounded_timeout":
				args = append(args, "--timeout=2h")
			case "unexpected_argument":
				args = append(args, "untrusted")
			}
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(t.Context())
			switch tc {
			case "help":
				require.NoError(t, err)
				require.Zero(t, calls)
				require.Contains(t, output.String(), "Usage:")
			case "complete":
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				require.Contains(t, output.String(), "complete=true")
			case "budget_progress":
				require.ErrorIs(t, err, billinglifecycle.ErrPreparationBudget)
				require.Equal(t, 1, calls)
				require.Contains(t, output.String(), "pages=2")
				require.Contains(t, output.String(), "complete=false")
			case "uncertain_private", "joined_budget_uncertainty":
				require.ErrorIs(t, err, recordstore.ErrUncertain)
				require.Equal(t, 1, calls)
				require.Empty(t, output.String())
				require.NotContains(t, err.Error(), "secret@example.test")
				require.NotContains(t, err.Error(), "mongodb://")
			default:
				require.Error(t, err)
				require.Zero(t, calls)
			}
		})
	}
}
