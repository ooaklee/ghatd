package partnerstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named isolated encrypted-native snapshots test complete
// sets beyond a list page, binding visibility, callback reset and late failures.
func TestMongoCompleteRelationshipEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, failKind string
		retry          bool
		customers      int
	}{
		{"complete_set_exceeds_one_relationship_page", "", false, 101},
		{"read_callback_reentry_resets_bindings", "", true, 2},
		{"late_binding_failure_discards_loaded_history", kindPaymentBinding, false, 2},
		{"late_history_failure_discards_memberships", kindReferralRevision, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, time.Hour)
			require.NoError(t, err)
			for n := 0; n < tc.customers; n++ {
				customer := fmt.Sprintf("complete-customer-%03d", n)
				first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "initial reviewed assignment", Terms: frozenTerms()}))
				require.NoError(t, err)
				_, err = s.BindPayment(ctx, customer, "original-payment-"+customer, clock.Now())
				require.NoError(t, err)
				clock.now = clock.now.Add(time.Minute)
				_, err = s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "other", CustomerID: "other-owner", CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "prospective reassignment", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: frozenTerms()}))
				require.NoError(t, err)
				_, err = s.BindPayment(ctx, customer, "later-payment-"+customer, clock.Now())
				require.NoError(t, err)
			}
			reader := &analyticsReadStore{Store: store, retry: tc.retry, failKind: tc.failKind}
			r, err = NewReferralRepository(reader)
			require.NoError(t, err)
			s, err = referral.NewService(r, clock, randomIDs{}, time.Hour)
			require.NoError(t, err)
			out, err := s.GetRelationshipEvidence(ctx, "original")
			require.Equal(t, 1, reader.reads, "complete evidence uses one owning read")
			if tc.failKind != "" {
				require.ErrorIs(t, err, referral.ErrUnavailable)
				require.Zero(t, out)
				return
			}
			require.NoError(t, err)
			require.Len(t, out.Items, tc.customers)
			for _, item := range out.Items {
				require.False(t, item.Relationship.Current)
				require.Len(t, item.Relationship.Periods, 1)
				require.Len(t, item.Attribution.History, 2)
				require.Len(t, item.Attribution.Bindings, 2, "other owner's binding is private evidence, not silently lost")
				perCustomer, err := s.GetAttributionSnapshot(ctx, item.Relationship.ReferredCustomer)
				require.NoError(t, err)
				require.Equal(t, perCustomer.Fingerprint, item.Attribution.Fingerprint)
			}
		})
	}
}

// Standalone audit exception: the persisted read/write overlap is the behavior
// under test. A gate after membership selection pins the native snapshot while
// one real correction and binding commit before the older read continues.
func TestMongoCompleteEvidencePinsBindingsAndOwnership(t *testing.T) {
	_, _, store, _, clock, ctx := mongoEarnings(t)
	r, err := NewReferralRepository(store)
	require.NoError(t, err)
	s, err := referral.NewService(r, clock, randomIDs{}, time.Hour)
	require.NoError(t, err)
	first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "initial assignment", Terms: frozenTerms()}))
	require.NoError(t, err)
	_, err = s.BindPayment(ctx, "customer", "original-payment", clock.Now())
	require.NoError(t, err)
	gate := &relationshipSnapshotStore{Store: store, selected: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	reader, err := NewReferralRepository(gate)
	require.NoError(t, err)
	type result struct {
		rows []referral.RelationshipEvidenceRow
		err  error
	}
	done := make(chan result, 1)
	go func() {
		rows, err := reader.ReadRelationshipEvidence(ctx, referral.ProgramID, "original")
		done <- result{rows, err}
	}()
	select {
	case <-gate.selected:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	clock.now = clock.now.Add(time.Minute)
	_, err = s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "other", CustomerID: "other-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "prospective reassignment", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: frozenTerms()}))
	require.NoError(t, err)
	_, err = s.BindPayment(ctx, "customer", "new-owner-payment", clock.Now())
	require.NoError(t, err)
	release.Do(func() { close(gate.release) })
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Len(t, got.rows, 1)
		require.Equal(t, first.ID, got.rows[0].Relationship.Head.ID)
		require.Len(t, got.rows[0].Relationship.History, 1)
		require.Len(t, got.rows[0].Bindings, 1)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	confirm, err := s.GetRelationshipEvidence(context.Background(), "original")
	require.NoError(t, err)
	require.Len(t, confirm.Items[0].Attribution.History, 2)
	require.Len(t, confirm.Items[0].Attribution.Bindings, 2)
	require.False(t, confirm.Items[0].Relationship.Current)
}

// Standalone audit exception: one persisted exact-capacity set grows by one
// binding. Rebuilding two 10,000-row encrypted fixtures would duplicate costly
// preparation; the before/after bounded native transition is the behavior.
func TestMongoCompleteEvidenceExactCapacityThenOverflow(t *testing.T) {
	_, _, store, _, clock, ctx := mongoEarnings(t)
	r, err := NewReferralRepository(store)
	require.NoError(t, err)
	s, err := referral.NewService(r, clock, randomIDs{}, time.Hour)
	require.NoError(t, err)
	first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "initial assignment", Terms: frozenTerms()}))
	require.NoError(t, err)
	// Membership + head + one revision consume three of the shared budget.
	require.NoError(t, store.Transact(ctx, referralPartition("customer"), func(tx recordstore.Tx) error {
		for i := 0; i < referral.RelationshipEvidenceCapacity-3; i++ {
			payment := fmt.Sprintf("payment-%05d", i)
			digest := sha256.Sum256([]byte(referral.ProgramID + ":" + payment))
			binding := referral.PaymentAttribution{ID: "binding_" + hex.EncodeToString(digest[:]), ProgramID: referral.ProgramID, PaymentID: payment, ReferredCustomer: "customer", ReferralID: first.ID, PartnerID: "original", EffectiveAt: clock.Now(), BoundAt: clock.Now(), Terms: first.TermsSnapshot}
			row, err := recordstore.NewRecord(kindPaymentBinding, identity(referral.ProgramID, payment), referralProgramPartition(), 1, binding)
			if err != nil {
				return err
			}
			row.State = "customer"
			if err := tx.Insert(ctx, row); err != nil {
				return err
			}
		}
		return nil
	}))
	exact, err := s.GetRelationshipEvidence(ctx, "original")
	require.NoError(t, err)
	require.Len(t, exact.Items, 1)
	require.Len(t, exact.Items[0].Attribution.Bindings, referral.RelationshipEvidenceCapacity-3)
	conversion, err := s.GetConversionEvidence(ctx, "original", referral.AnalyticsQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, conversion.Relationships.Items[0].Attribution.Bindings, referral.RelationshipEvidenceCapacity-3)
	_, err = s.IssueLink(ctx, referral.PartnerState{PartnerID: "original", CustomerID: "owner", CanAcquireReferrals: true})
	require.NoError(t, err)
	tooLarge, err := s.GetConversionEvidence(ctx, "original", referral.AnalyticsQuery{Limit: 1})
	require.ErrorIs(t, err, referral.ErrCapacity, "a retained link shares the conversion evidence budget")
	require.Zero(t, tooLarge)
	_, err = s.BindPayment(ctx, "customer", "one-beyond-combined-capacity", clock.Now())
	require.NoError(t, err)
	overflow, err := s.GetRelationshipEvidence(ctx, "original")
	require.ErrorIs(t, err, referral.ErrCapacity)
	require.Zero(t, overflow)
	current, err := r.GetReferralByCustomer(ctx, referral.ProgramID, "customer")
	require.NoError(t, err)
	require.Equal(t, first.ID, current.ID, "a failed report cannot change owning attribution")
}
