package referral

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type rotationAdmissionRepo struct {
	Repository
	calls int
}

func (r *rotationAdmissionRepo) WithLinkTransaction(context.Context, string, string, func(LinkRotationTransaction) error) error {
	r.calls++
	return ErrUnavailable
}

func validRotationFixture() RotateLinkRequest {
	return RotateLinkRequest{Partner: PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true}, ActorID: "owner", ExpectedLinkCode: "original-code", Reason: "replace reviewed link", IdempotencyKey: "original-key"}
}

func TestLinkRotationRejectsMalformedIntentBeforeTransaction(t *testing.T) {
	for _, tc := range []struct {
		name, field, value string
		want               error
	}{
		{"empty_actor", "actor", "", ErrInvalid}, {"padded_actor", "actor", " owner", ErrInvalid},
		{"long_actor", "actor", strings.Repeat("a", 257), ErrInvalid},
		{"empty_partner", "partner", "", ErrInvalid}, {"empty_customer", "customer", "", ErrInvalid},
		{"empty_original_code", "code", "", ErrInvalid}, {"long_code", "code", strings.Repeat("a", 129), ErrInvalid},
		{"padded_code", "code", "original-code ", ErrInvalid},
		{"empty_reason", "reason", "", ErrInvalid}, {"long_reason", "reason", strings.Repeat("a", 1001), ErrInvalid},
		{"empty_key", "key", "", ErrInvalid}, {"long_key", "key", strings.Repeat("a", 257), ErrInvalid},
		{"nil_context", "context", "nil", ErrInvalid}, {"cancelled_context", "context", "cancelled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &rotationAdmissionRepo{}
			s, err := NewService(r, fakeClock{time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}, &seqIDs{}, time.Hour)
			require.NoError(t, err)
			req := validRotationFixture()
			ctx := context.Background()
			switch tc.field {
			case "actor":
				req.ActorID = tc.value
			case "partner":
				req.Partner.PartnerID = tc.value
			case "customer":
				req.Partner.CustomerID = tc.value
			case "code":
				req.ExpectedLinkCode = tc.value
			case "reason":
				req.Reason = tc.value
			case "key":
				req.IdempotencyKey = tc.value
			case "context":
				if tc.value == "nil" {
					ctx = nil
				} else {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
			}
			_, err = s.RotateLink(ctx, req)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, r.calls)
		})
	}
}

func TestLinkRotationMissingAtomicCapabilityNeverFallsBack(t *testing.T) {
	for _, tc := range []struct{ name string }{{"ordinary_repository"}, {"typed_nil_atomic_repository"}} {
		t.Run(tc.name, func(t *testing.T) {
			var repo Repository = newFakeRepo()
			if tc.name == "typed_nil_atomic_repository" {
				var typed *rotationAdmissionRepo
				repo = typed
			}
			s, err := NewService(repo, fakeClock{time.Now()}, &seqIDs{}, time.Hour)
			if tc.name == "typed_nil_atomic_repository" {
				require.ErrorIs(t, err, ErrUnavailable)
				return
			}
			require.NoError(t, err)
			_, err = s.RotateLink(context.Background(), validRotationFixture())
			require.ErrorIs(t, err, ErrUnavailable)
			require.Empty(t, repo.(*fakeRepo).links)
		})
	}
}

func TestLinkRotationReceiptRejectsCorruptIdentityAndFrozenResult(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{"valid", true}, {"wrong_id", false}, {"wrong_actor", false}, {"wrong_customer", false},
		{"wrong_partner", false}, {"wrong_reason", false}, {"wrong_key", false},
		{"mutable_acquisition_flag", false}, {"bad_fingerprint", false},
		{"wrong_result_program", false}, {"wrong_result_owner", false}, {"same_result_code", false},
		{"retired_result", false}, {"retirement_reason_on_active_result", false}, {"zero_created_at", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := normalizedRotation(validRotationFixture())
			fp, err := rotationFingerprint(req)
			require.NoError(t, err)
			at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			id := LinkRotationID(req.Partner.PartnerID, req.ActorID, req.IdempotencyKey)
			v := LinkRotationReceipt{ID: id, Request: req, RequestFingerprint: fp, Link: Link{ID: id + "_result", ProgramID: ProgramID, PartnerID: "partner", Code: "replacement-code", CreatedAt: at}}
			switch tc.name {
			case "wrong_id":
				v.ID = "different"
			case "wrong_actor":
				v.Request.ActorID = "different"
			case "wrong_customer":
				v.Request.Partner.CustomerID = "different"
			case "wrong_partner":
				v.Request.Partner.PartnerID = "different"
			case "wrong_reason":
				v.Request.Reason = "different"
			case "wrong_key":
				v.Request.IdempotencyKey = "different"
			case "mutable_acquisition_flag":
				v.Request.Partner.CanAcquireReferrals = true
			case "bad_fingerprint":
				v.RequestFingerprint = strings.Repeat("0", 64)
			case "wrong_result_program":
				v.Link.ProgramID = "different"
			case "wrong_result_owner":
				v.Link.PartnerID = "different"
			case "same_result_code":
				v.Link.Code = req.ExpectedLinkCode
			case "retired_result":
				v.Link.RetiredAt = &at
			case "retirement_reason_on_active_result":
				v.Link.RetireReason = "different"
			case "zero_created_at":
				v.Link.CreatedAt = time.Time{}
			}
			if tc.valid {
				require.NoError(t, v.Validate())
			} else {
				require.ErrorIs(t, v.Validate(), ErrUnavailable)
			}
		})
	}
}
