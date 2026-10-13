package billinglifecycle

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type expiryMisreportRepository struct {
	CompletionRepository
	CheckoutExpiryRepository
	change string
}

func (r expiryMisreportRepository) ExpireCheckout(ctx context.Context, o CheckoutExpiryObservation) (CompletedExecution, error) {
	v, err := r.CheckoutExpiryRepository.ExpireCheckout(ctx, o)
	if err != nil {
		return v, err
	}
	switch r.change {
	case "missing_expiry":
		v.Expiry = nil
	case "changed_fingerprint":
		v.Expiry.Fingerprint = "wrong"
	case "changed_revision":
		v.Job.Revision++
	case "future_completion":
		v.Receipt.CompletedAt = v.Receipt.CompletedAt.Add(time.Hour)
	case "changed_completion_reference":
		v.Job.LastCompletionID = "wrong"
	}
	return v, nil
}

func TestCheckoutExpiryEncryptedRejectsAdapterMisreport(t *testing.T) {
	for _, change := range []string{"missing_expiry", "changed_fingerprint", "changed_revision", "future_completion", "changed_completion_reference"} {
		t.Run(change, func(t *testing.T) {
			f, _, o := nativeExpiryObservation(t)
			r := expiryMisreportRepository{f.repo, f.repo, change}
			c, err := NewCheckoutCompletion(f.execution, r)
			require.NoError(t, err)
			out, err := c.completeExpiry(f.ctx, r, o)
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			require.Zero(t, out)
			// The adapter's response cannot alter already committed native proof.
			recovered, err := c.InspectLast(f.ctx, o.Job.Source)
			require.NoError(t, err)
			require.NotNil(t, recovered.Expiry)
			require.Equal(t, o.Expiry.Fingerprint, recovered.Expiry.Fingerprint)
		})
	}
}

// This is the schema-1 completion payload from the base revision, retained as
// a compatibility reader. Its field order also verifies captured bytes.
type schemaOneCompletionPayload struct {
	OriginalStatus                                   *statusPreparationPayload
	Schema                                           int
	JobRevision                                      int64
	Job                                              scheduleJobPayload
	NativeCaptureID, NativeFingerprint               string
	NativeRequestedAt, NativeObservedAt, CompletedAt time.Time
}

func TestCheckoutExpiryEncryptedCodecCompatibility(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "schema_one_captured"
		if expired {
			name = "explicit_expiry_variant"
		}
		t.Run(name, func(t *testing.T) {
			status, payment := "complete", "paid"
			if expired {
				status, payment = "expired", "unpaid"
			}
			f := nativeCheckoutHTTPFixture(t, status, payment, expired, false)
			c, err := NewCheckoutCompletion(f.execution, f.repo)
			require.NoError(t, err)
			_, err = c.runDisposition(f.ctx, JobLease(f.job))
			require.NoError(t, err)
			out, err := c.InspectLast(f.ctx, f.job.Source)
			require.NoError(t, err)
			row, err := encodeCompletion(out)
			require.NoError(t, err)
			var old schemaOneCompletionPayload
			err = strictScheduleDecode(row.Data, &old)
			if expired {
				require.Error(t, err, "old strict completion reader must reject new expiry field")
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, old.Schema)
				before, err := json.Marshal(old)
				require.NoError(t, err)
				require.Equal(t, string(before), string(row.Data), "captured schema-1 bytes stay identical")
			}
			jobRow, err := jobRecord(out.Job)
			require.NoError(t, err)
			var oldJob scheduleJobPayload
			require.NoError(t, strictScheduleDecode(jobRow.Data, &oldJob))
			require.Equal(t, 1, oldJob.Schema)
			decoded, err := readJob(jobRow)
			require.NoError(t, err)
			require.True(t, sameExecutionJob(out.Job, decoded))
			// Both the immutable terminal source and malformed new variants must
			// fail closed under each decoder, never become a capture.
			if expired {
				var current completionPayload
				require.NoError(t, strictScheduleDecode(row.Data, &current))
				current.Schema = 1
				row.Data, err = json.Marshal(current)
				require.NoError(t, err)
				_, err = decodeCompletion(row, CompletionKey{out.Job.Source, out.Receipt.NativeCaptureID})
				require.ErrorIs(t, err, recordstore.ErrUnavailable)
				current.Schema = 2
				current.Expiry = nil
				row.Data, err = json.Marshal(current)
				require.NoError(t, err)
				_, err = decodeCompletion(row, CompletionKey{out.Job.Source, out.Receipt.NativeCaptureID})
				require.ErrorIs(t, err, recordstore.ErrUnavailable)
			}
		})
	}
}
