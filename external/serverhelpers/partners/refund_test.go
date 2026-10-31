package partners

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named boundary cases reject unbounded commands before
// composition and withhold success/private diagnostics after uncertain writes.
func TestPartnersRefundRecoveryCommand(t *testing.T) {
	cases := []struct{ name string }{{"help"}, {"missing_source"}, {"missing_fingerprint"}, {"missing_file"}, {"zero_timeout"}, {"unbounded_timeout"}, {"unexpected_argument"}, {"resolved"}, {"wrong_receipt"}, {"cancelled_after_commit"}, {"uncertain_private"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cmd := NewRefundRecoveryCommand(func(ctx context.Context, opts RefundRecoveryOptions) (billing.RevenueObservation, error) {
				calls++
				require.NoError(t, opts.Validate())
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), time.Minute)
				v := billing.RevenueObservation{ID: "resolution", ResolutionOf: opts.ObservationID, AcceptedAt: time.Now(), FactIDs: []string{"private-fact"}, SourceFingerprint: "private-hash", ResolutionBy: "private-actor"}
				switch tc.name {
				case "wrong_receipt":
					v.ResolutionOf = "other-source"
				case "cancelled_after_commit":
					cancel()
				case "uncertain_private":
					return v, errors.Join(recordstore.ErrUncertain, errors.New("private receipt_url and mongodb://credentials"))
				}
				return v, nil
			})
			args := []string{"--observation-id=source", "--expected-fingerprint=original", "--original-snapshot-file=private.json"}
			switch tc.name {
			case "help":
				args = []string{"--help"}
			case "missing_source":
				args = args[1:]
			case "missing_fingerprint":
				args = []string{args[0], args[2]}
			case "missing_file":
				args = args[:2]
			case "zero_timeout":
				args = append(args, "--timeout=0s")
			case "unbounded_timeout":
				args = append(args, "--timeout=6m")
			case "unexpected_argument":
				args = append(args, "untrusted")
			}
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(ctx)
			switch tc.name {
			case "help":
				require.NoError(t, err)
				require.Zero(t, calls)
			case "resolved":
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				require.Equal(t, "resolved=true facts=1 original_history=preserved\n", output.String())
			case "wrong_receipt", "cancelled_after_commit", "uncertain_private":
				require.Error(t, err)
				require.Equal(t, 1, calls)
				require.Empty(t, output.String())
				require.NotContains(t, err.Error(), "private")
				if tc.name == "uncertain_private" {
					require.ErrorIs(t, err, recordstore.ErrUncertain)
					// main logs the outer command error with %v. Formatting must
					// use its safe Error text, never traverse the private cause.
					require.Equal(t, err.Error(), fmt.Sprint(err))
					var stderr bytes.Buffer
					log.New(&stderr, "", 0).Printf("ghatd/error-executing-command-tree: %v", err)
					require.Equal(t, "ghatd/error-executing-command-tree: "+err.Error()+"\n", stderr.String())
					require.NotContains(t, stderr.String(), "mongodb://")
					require.NotContains(t, stderr.String(), "private")
				}
			default:
				require.Error(t, err)
				require.Zero(t, calls)
			}
		})
	}
}
