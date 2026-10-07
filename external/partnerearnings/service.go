package partnerearnings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Service owns commission accrual/maturity, refund reversals, claim lifecycle
// and manual payment recording. It computes every business rule and delegates
// all datastore I/O to the Repository port; it never reads another domain's
// store and never issues datastore queries itself.
type Service struct {
	repo      Repository
	clock     Clock
	ids       IDGenerator
	programID string
	currency  string
}

// NewService validates wiring; missing financial configuration fails closed.
func NewService(repo Repository, clock Clock, ids IDGenerator, config Config) (*Service, error) {
	if isNilInterface(repo) {
		return nil, errWrap(ErrUnavailable, "repository required")
	}
	if isNilInterface(ids) {
		return nil, errWrap(ErrUnavailable, "id generator required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	if isNilInterface(clock) {
		return nil, errWrap(ErrUnavailable, "clock required")
	}
	return &Service{repo: repo, clock: clock, ids: ids, programID: config.ProgramID, currency: config.Currency}, nil
}

// Config exposes the effective configuration (read-only copy).
func (s *Service) Config() Config { return Config{ProgramID: s.programID, Currency: s.currency} }

// ProgramID returns the owning program.
func (s *Service) ProgramID() string { return s.programID }

// Currency returns the single approved currency.
func (s *Service) Currency() string { return s.currency }

// CommissionMinor returns the exact commission for amountMinor at the given
// rate, rounded half-up, in minor units. It never overflows int64.
func CommissionMinor(amountMinor int64, rateBasisPoints int) (int64, error) {
	if amountMinor < 0 {
		return 0, errWrap(ErrInvalid, "amount must be non-negative")
	}
	if rateBasisPoints < MinRateBasisPoints || rateBasisPoints > MaxRateBasisPoints {
		return 0, errWrap(ErrInvalid, "rate out of bounds")
	}
	if amountMinor == 0 || rateBasisPoints == 0 {
		return 0, nil
	}
	num := new(big.Int).Mul(big.NewInt(amountMinor), big.NewInt(int64(rateBasisPoints)))
	return bigToInt64(roundHalfUp(num, BasisPointsPerUnit))
}

// roundHalfUp returns floor((2*num + den) / (2*den)), exact half-up rounding.
func roundHalfUp(num *big.Int, den int64) *big.Int {
	two := big.NewInt(2)
	d := big.NewInt(den)
	numerator := new(big.Int).Mul(num, two)
	numerator.Add(numerator, d)
	denominator := new(big.Int).Mul(d, two)
	return new(big.Int).Quo(numerator, denominator)
}

// roundedRatio returns roundHalfUp(commission * refunded / paid) without
// overflow, used for cumulative refund reversal.
func roundedRatio(commission, refunded, paid int64) (int64, error) {
	num := new(big.Int).Mul(big.NewInt(commission), big.NewInt(refunded))
	return bigToInt64(roundHalfUp(num, paid))
}

func bigToInt64(v *big.Int) (int64, error) {
	if !v.IsInt64() {
		return 0, errWrap(ErrInvalid, "amount overflows minor units")
	}
	return v.Int64(), nil
}

// negBigInt returns -v without the signed overflow of negating MinInt64 in
// int64 space first (big.NewInt(-MinInt64) overflows before big.Int sees it).
func negBigInt(v int64) *big.Int {
	return new(big.Int).Neg(big.NewInt(v))
}

// ---- Accrual ----

// Accrue books one commission for a verified payment. It is idempotent by
// PaymentID: an exact replay returns the original entry, a replay with any
// changed frozen field (amount, rate, hold, provider-effective timestamp,
// referral, terms, policy or source metadata) conflicts. A hold-free commission
// is matured in the same transaction.
func (s *Service) Accrue(ctx context.Context, req AccrualRequest) (Entry, error) {
	if err := s.checkContext(ctx); err != nil {
		return Entry{}, err
	}
	if err := validateAccrual(s.currency, req); err != nil {
		return Entry{}, err
	}
	fp := accrualFingerprint(s.programID, req)
	var out Entry
	err := s.repo.WithTransaction(ctx, s.programID, req.PartnerID, s.currency, func(tx Repository) error {
		out = Entry{}
		if isNilInterface(tx) {
			return ErrUnavailable
		}
		existing, err := tx.EntryBySource(ctx, s.programID, req.PartnerID, EntryAccrued, req.PaymentID)
		switch {
		case err == nil:
			if existing.Fingerprint != fp {
				return errWrap(ErrConflict, "payment already accrued with different terms")
			}
			out = existing
			source, err := tx.GetMaturitySource(ctx, s.programID, maturityID(s.programID, s.currency, req.PartnerID, req.PaymentID))
			if strictFinancialAbsence(err) {
				return ErrUnavailable
			}
			if err != nil {
				return err
			}
			entries, err := tx.ListEntries(ctx, s.programID, req.PartnerID)
			if err != nil {
				return err
			}
			return s.verifyMaturitySource(source, entries)
		case !strictFinancialAbsence(err):
			return err
		}

		commission, err := CommissionMinor(req.PaymentMinor, req.RateBasisPoints)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		availableAt := req.OccurredAt.Add(req.HoldDuration)

		accrued := Entry{
			Kind: EntryAccrued, SourceEventID: req.PaymentID,
			AmountMinor: commission, Currency: s.currency,
			OccurredAt: req.OccurredAt, CreatedAt: now,
			PaymentMinor: req.PaymentMinor, CommissionMinor: commission,
			ReferralID: req.ReferralID, PlanID: req.PlanID, TermsVersion: req.TermsVersion,
			PolicyID: req.PolicyID, RateBasisPoints: req.RateBasisPoints,
			HoldDuration: req.HoldDuration, AvailableAt: &availableAt,
			Note: req.SourceKind, Fingerprint: fp,
		}
		if err := s.append(ctx, tx, req.PartnerID, &accrued); err != nil {
			return err
		}
		out = accrued
		source := sourceFromAccrual(accrued)
		if err := source.Validate(); err != nil {
			return err
		}
		if err := tx.InsertMaturitySource(ctx, source); err != nil {
			return err
		}

		if req.HoldDuration <= 0 {
			matured := Entry{
				Kind: EntryMatured, SourceEventID: req.PaymentID,
				AmountMinor: commission, Currency: s.currency,
				OccurredAt: now, CreatedAt: now,
			}
			if err := s.append(ctx, tx, req.PartnerID, &matured); err != nil {
				return err
			}
			if err := s.completeMaturitySource(ctx, tx, accrued, matured); err != nil {
				return err
			}
		}
		return s.validateLedgerRepresentable(ctx, tx, req.PartnerID)
	})
	if err != nil {
		return Entry{}, err
	}
	return out, nil
}

// Mature journals the explicit maturity movement for every pending accrual
// whose hold has elapsed. It is idempotent per payment.
func (s *Service) Mature(ctx context.Context, partnerID string) ([]Entry, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(partnerID) == "" {
		return nil, errWrap(ErrInvalid, "partner required")
	}
	now := s.clock.Now()
	var matured []Entry
	err := s.repo.WithTransaction(ctx, s.programID, partnerID, s.currency, func(tx Repository) error {
		matured = matured[:0]
		if isNilInterface(tx) {
			return ErrUnavailable
		}
		entries, err := tx.ListEntries(ctx, s.programID, partnerID)
		if err != nil {
			return err
		}
		maturedIDs := map[string]bool{}
		for _, e := range entries {
			if e.Kind == EntryMatured {
				maturedIDs[e.SourceEventID] = true
			}
		}
		for _, e := range entries {
			if e.Kind != EntryAccrued {
				continue
			}
			source, err := tx.GetMaturitySource(ctx, s.programID, maturityID(s.programID, s.currency, partnerID, e.SourceEventID))
			if strictFinancialAbsence(err) {
				return ErrUnavailable
			}
			if err != nil {
				return err
			}
			if err := s.verifyMaturitySource(source, entries); err != nil {
				return err
			}
			if maturedIDs[e.SourceEventID] {
				continue
			}
			if e.AvailableAt == nil || e.AvailableAt.After(now) {
				continue
			}
			m := Entry{
				Kind: EntryMatured, SourceEventID: e.SourceEventID,
				AmountMinor: e.AmountMinor, Currency: s.currency,
				OccurredAt: now, CreatedAt: now,
			}
			if err := s.append(ctx, tx, partnerID, &m); err != nil {
				return err
			}
			if err := s.completeMaturitySource(ctx, tx, e, m); err != nil {
				return err
			}
			matured = append(matured, m)
			maturedIDs[e.SourceEventID] = true
		}
		return s.validateLedgerRepresentable(ctx, tx, partnerID)
	})
	if err != nil {
		return nil, err
	}
	return matured, nil
}

// ---- Reversal ----

// Reverse books one refund against a payment. CumulativeRefundedMinor is the
// total eligible refund in payment units after this refund, so split refunds
// produce the same cumulative reversal. A refund received before its payment is
// unresolved and must be replayed once the accrual exists. Reversing a matured
// credit atomically releases reservations of affected requested claims and
// flags affected processing/needs_review claims for review.
//
// Reverse is idempotent by RefundID: an exact replay returns the original
// reversal, a replay with a changed payload conflicts, and a refund whose
// reversal delta rounds to zero (or that reports lower cumulative evidence than
// already recorded) still journals a zero-debit observation so the event stays
// durably anchored without reversing extra money.
func (s *Service) Reverse(ctx context.Context, req ReversalRequest) ([]Entry, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateReversal(s.currency, req); err != nil {
		return nil, err
	}
	fp := reversalFingerprint(s.programID, req)
	var out []Entry
	err := s.repo.WithTransaction(ctx, s.programID, req.PartnerID, s.currency, func(tx Repository) error {
		out = out[:0]

		if existing, err := tx.EntryBySource(ctx, s.programID, req.PartnerID, EntryReversed, req.RefundID); err == nil {
			if existing.Fingerprint != fp {
				return errWrap(ErrConflict, "refund already processed with different payload")
			}
			out = append(out, existing)
			return nil
		} else if !strictFinancialAbsence(err) {
			return err
		}

		accrual, err := tx.EntryBySource(ctx, s.programID, req.PartnerID, EntryAccrued, req.PaymentID)
		if strictFinancialAbsence(err) {
			return errWrap(ErrUnresolved, "refund received before payment is accrued")
		} else if err != nil {
			return err
		}
		if accrual.Currency != s.currency {
			return errWrap(ErrCurrencyMismatch, "payment currency does not match program currency")
		}
		if req.CumulativeRefundedMinor > accrual.PaymentMinor {
			return errWrap(ErrInvalid, "cumulative refund exceeds original payment")
		}

		entries, err := tx.ListEntries(ctx, s.programID, req.PartnerID)
		if err != nil {
			return err
		}
		var priorReversal, priorLost big.Int
		var priorCumulative int64
		for _, e := range entries {
			if e.Kind == EntryDisputeLost && e.SourceRef == req.PaymentID {
				priorLost.Add(&priorLost, negBigInt(e.AmountMinor))
			}
			if e.Kind != EntryReversed || e.SourceRef != req.PaymentID {
				continue
			}
			priorReversal.Add(&priorReversal, negBigInt(e.AmountMinor))
			if e.CumulativeRefundedMinor > priorCumulative {
				priorCumulative = e.CumulativeRefundedMinor
			}
		}

		cumulativeReversal, err := roundedRatio(accrual.CommissionMinor, req.CumulativeRefundedMinor, accrual.PaymentMinor)
		if err != nil {
			return err
		}
		if cumulativeReversal > accrual.CommissionMinor {
			cumulativeReversal = accrual.CommissionMinor
		}
		delta := new(big.Int).Sub(big.NewInt(cumulativeReversal), &priorReversal)
		delta.Sub(delta, &priorLost)
		effectiveDelta := int64(0)
		if delta.Sign() > 0 {
			if effectiveDelta, err = bigToInt64(delta); err != nil {
				return err
			}
		}

		now := s.clock.Now()
		rev := Entry{
			Kind: EntryReversed, SourceEventID: req.RefundID, SourceRef: req.PaymentID,
			AmountMinor: -effectiveDelta, Currency: s.currency,
			OccurredAt: req.OccurredAt, CreatedAt: now,
			RefundedMinor:           req.CumulativeRefundedMinor - priorCumulative,
			CumulativeRefundedMinor: req.CumulativeRefundedMinor,
			Fingerprint:             fp,
		}
		if err := s.append(ctx, tx, req.PartnerID, &rev); err != nil {
			return err
		}
		out = append(out, rev)

		if effectiveDelta > 0 {
			if err := s.reduceDisputeHolds(ctx, tx, req.PartnerID, req.PaymentID, req.RefundID, effectiveDelta, req.OccurredAt); err != nil {
				return err
			}
		}
		// Recompute reservations of claims backed by the reversed payment.
		if err := s.recomputeBacking(ctx, tx, req.PartnerID, req.PaymentID); err != nil {
			return err
		}
		return s.validateLedgerRepresentable(ctx, tx, req.PartnerID)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// recomputeBacking releases reservations of requested claims allocated from the
// given payment and flags processing/needs_review claims for review.
func (s *Service) recomputeBacking(ctx context.Context, tx Repository, partnerID, paymentID string) error {
	entries, err := tx.ListEntries(ctx, s.programID, partnerID)
	if err != nil {
		return err
	}
	claimed := map[string]bool{}
	for _, e := range entries {
		if e.Kind == EntryAllocated && e.SourceRef == paymentID {
			claimed[e.SourceEventID] = true
		}
	}
	now := s.clock.Now()
	for claimID := range claimed {
		claim, err := tx.GetClaim(ctx, s.programID, claimID)
		if err != nil {
			return err
		}
		switch claim.State {
		case ClaimRequested:
			claim.State = ClaimCancelled
			claim.Reason = "reversed"
			claim.UpdatedAt = now
			claim.Revision++
			if err := s.releaseReservation(ctx, tx, claim, ""); err != nil {
				return err
			}
		case ClaimProcessing, ClaimNeedsReview:
			if claim.ReviewReason == "" {
				claim.ReviewReason = "reversed"
				claim.UpdatedAt = now
				claim.Revision++
				if _, err := tx.ReplaceClaim(ctx, claim, claim.Revision-1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// releasesFor returns the allocation-released entries for one claim, stamped
// with the actor responsible for the release (empty for system reversals).
func (s *Service) releasesFor(ctx context.Context, tx Repository, claim Claim, actorID string) ([]Entry, error) {
	entries, err := tx.ListEntries(ctx, s.programID, claim.PartnerID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	var out []Entry
	for _, e := range entries {
		if e.Kind != EntryAllocated || e.SourceEventID != claim.ID {
			continue
		}
		out = append(out, Entry{
			Kind: EntryAllocationReleased, SourceEventID: claim.ID, SourceRef: e.SourceRef,
			AmountMinor: -e.AmountMinor, Currency: s.currency,
			OccurredAt: now, CreatedAt: now, ActorID: actorID,
		})
	}
	return out, nil
}

// ---- Claims ----

// RequestClaim reserves matured funds for a payout, allocating oldest credit
// first. It is idempotent by (ActorID, UseCase, Partner, Currency, Key); the
// same request replays the original claim and a changed payload conflicts.
func (s *Service) RequestClaim(ctx context.Context, req ClaimRequest) (Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return Claim{}, err
	}
	if err := validateClaimRequest(s.currency, req); err != nil {
		return Claim{}, err
	}
	fp := claimFingerprint(s.programID, req)

	var out Claim
	err := s.repo.WithTransaction(ctx, s.programID, req.PartnerID, s.currency, func(tx Repository) error {
		key := ReceiptKey{
			ProgramID: s.programID, PartnerID: req.PartnerID, ActorID: req.ActorID,
			UseCase: UseCaseClaim, Currency: s.currency, Key: req.IdempotencyKey,
		}
		receipt, err := tx.GetReceipt(ctx, key)
		switch {
		case err == nil:
			if receipt.Fingerprint != fp {
				return errWrap(ErrConflict, "claim idempotency key reused with different payload")
			}
			claim, err := tx.GetClaim(ctx, s.programID, receipt.ClaimID)
			if err != nil {
				return err
			}
			out = claim
			return nil
		case !strictFinancialAbsence(err):
			return err
		}

		entries, err := tx.ListEntries(ctx, s.programID, req.PartnerID)
		if err != nil {
			return err
		}
		claims, err := tx.ListClaims(ctx, s.programID, req.PartnerID, nil, 0, "")
		if err != nil {
			return err
		}
		balances, credits, err := derive(entries, claims)
		if err != nil {
			return err
		}
		if req.AmountMinor > balances.AvailableMinor {
			return errWrap(ErrInsufficient, "claim exceeds available matured funds")
		}

		now := s.clock.Now()
		claim := Claim{
			ID: s.ids.NewID(), ProgramID: s.programID, PartnerID: req.PartnerID,
			AmountMinor: req.AmountMinor, Currency: s.currency, State: ClaimRequested,
			DestinationID: req.DestinationID, DestinationSnapshot: copyMap(req.DestinationSnapshot),
			RequestedAt: now, UpdatedAt: now, RequestedBy: req.ActorID, RequestedReason: req.Reason, Revision: 1,
		}
		if err := tx.InsertClaim(ctx, claim); err != nil {
			return err
		}

		for _, part := range allocate(credits, req.AmountMinor) {
			a := Entry{
				Kind: EntryAllocated, SourceEventID: claim.ID, SourceRef: part.paymentID,
				AmountMinor: part.amount, Currency: s.currency,
				OccurredAt: now, CreatedAt: now, ActorID: req.ActorID,
			}
			if err := s.append(ctx, tx, req.PartnerID, &a); err != nil {
				return err
			}
		}

		r := Receipt{
			ProgramID: s.programID, PartnerID: req.PartnerID, ActorID: req.ActorID,
			UseCase: UseCaseClaim, Currency: s.currency, Key: req.IdempotencyKey,
			Fingerprint: fp, ClaimID: claim.ID, CreatedAt: now,
		}
		if err := tx.PutReceipt(ctx, r); err != nil {
			return err
		}
		out = claim
		return s.validateLedgerRepresentable(ctx, tx, req.PartnerID)
	})
	if err != nil {
		return Claim{}, err
	}
	return out, nil
}

// GetClaim returns one claim.
func (s *Service) GetClaim(ctx context.Context, id string) (Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return Claim{}, err
	}
	return s.repo.GetClaim(ctx, s.programID, id)
}

// ListClaims pages claims. Empty states means all states; empty partnerID means
// all partners (administrator queue). limit <= 0 means no limit.
func (s *Service) ListClaims(ctx context.Context, partnerID string, states []string, limit int, afterID string) ([]Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	return s.repo.ListClaims(ctx, s.programID, partnerID, states, limit, afterID)
}

// DecideClaim moves a claim through the operator queue. NewState may never be
// paid. Rejecting or cancelling a processing/needs_review claim requires
// ConfirmedUnsent; taking a claim already owned by another actor conflicts.
func (s *Service) DecideClaim(ctx context.Context, req ClaimDecision) (Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return Claim{}, err
	}
	if err := validateActor(req.ActorID); err != nil {
		return Claim{}, err
	}
	if err := validateExpectedRevision(req.ExpectedRevision); err != nil {
		return Claim{}, err
	}
	if _, ok := cleanPlain(req.ClaimID, maxIDLength); !ok {
		return Claim{}, errWrap(ErrInvalid, "claim id must be plain and non-empty")
	}
	pre, err := s.repo.GetClaim(ctx, s.programID, req.ClaimID)
	if err != nil {
		return Claim{}, err
	}
	var out Claim
	err = s.repo.WithTransaction(ctx, s.programID, pre.PartnerID, s.currency, func(tx Repository) error {
		out = Claim{}
		claim, err := tx.GetClaim(ctx, s.programID, req.ClaimID)
		if err != nil {
			return err
		}
		if claim.Revision != req.ExpectedRevision {
			return errWrap(ErrStaleWrite, "claim revision changed")
		}
		updated, err := s.applyDecision(claim, req)
		if err != nil {
			return err
		}
		if updated.needsRelease {
			if err := s.releaseReservation(ctx, tx, updated.claim, req.ActorID); err != nil {
				return err
			}
		} else if _, err := tx.ReplaceClaim(ctx, updated.claim, updated.claim.Revision-1); err != nil {
			return err
		}
		out = updated.claim
		return s.validateLedgerRepresentable(ctx, tx, pre.PartnerID)
	})
	if err != nil {
		return Claim{}, err
	}
	return out, nil
}

type decisionResult struct {
	claim        Claim
	needsRelease bool
}

func (s *Service) applyDecision(claim Claim, req ClaimDecision) (decisionResult, error) {
	now := s.clock.Now()
	next := claim
	next.UpdatedAt = now
	next.UpdatedBy = req.ActorID

	release := func(reason string) (decisionResult, error) {
		if req.NewState == ClaimProcessing || req.NewState == ClaimNeedsReview {
			return decisionResult{}, errWrap(ErrInvalidState, "not a release transition")
		}
		next.State = req.NewState
		next.Reason = reason
		next.Revision++
		return decisionResult{claim: next, needsRelease: true}, nil
	}

	switch claim.State {
	case ClaimRequested:
		switch req.NewState {
		case ClaimProcessing:
			if claim.ProcessingActor != "" && claim.ProcessingActor != req.ActorID {
				return decisionResult{}, errWrap(ErrConflict, "claim already owned by another actor")
			}
			next.ProcessingActor = req.ActorID
			next.State = ClaimProcessing
			next.Revision++
			return decisionResult{claim: next}, nil
		case ClaimNeedsReview:
			if strings.TrimSpace(req.Reason) == "" {
				return decisionResult{}, errWrap(ErrInvalid, "review reason required")
			}
			next.State = ClaimNeedsReview
			next.ReviewReason = req.Reason
			next.Revision++
			return decisionResult{claim: next}, nil
		case ClaimRejected, ClaimCancelled:
			if strings.TrimSpace(req.Reason) == "" {
				return decisionResult{}, errWrap(ErrInvalid, "reason required")
			}
			return release(req.Reason)
		case ClaimPaid:
			return decisionResult{}, errWrap(ErrInvalidState, "claims may only be paid via RecordPayment")
		default:
			return decisionResult{}, errWrap(ErrInvalidState, "invalid target state")
		}

	case ClaimProcessing, ClaimNeedsReview:
		switch req.NewState {
		case ClaimProcessing:
			if claim.ProcessingActor != "" && claim.ProcessingActor != req.ActorID {
				return decisionResult{}, errWrap(ErrConflict, "claim already owned by another actor")
			}
			if claim.State == ClaimProcessing {
				return decisionResult{}, errWrap(ErrInvalidState, "claim already processing")
			}
			next.State = ClaimProcessing
			if next.ProcessingActor == "" {
				next.ProcessingActor = req.ActorID
			}
			next.Revision++
			return decisionResult{claim: next}, nil
		case ClaimNeedsReview:
			if strings.TrimSpace(req.Reason) == "" {
				return decisionResult{}, errWrap(ErrInvalid, "review reason required")
			}
			next.State = ClaimNeedsReview
			next.ReviewReason = req.Reason
			next.Revision++
			return decisionResult{claim: next}, nil
		case ClaimRejected, ClaimCancelled:
			if !req.ConfirmedUnsent {
				return decisionResult{}, errWrap(ErrDenied, "transfer may be in flight; confirmed-unsent required")
			}
			if strings.TrimSpace(req.Reason) == "" {
				return decisionResult{}, errWrap(ErrInvalid, "reason required")
			}
			return release(req.Reason)
		case ClaimPaid:
			return decisionResult{}, errWrap(ErrInvalidState, "claims may only be paid via RecordPayment")
		default:
			return decisionResult{}, errWrap(ErrInvalidState, "invalid target state")
		}

	default:
		return decisionResult{}, errWrap(ErrInvalidState, "claim is terminal")
	}
}

// releaseReservation journals the allocation release and applies the terminal
// state for a reject/cancel decision. It is called inside the decision
// transaction, so the reservation is released exactly once.
func (s *Service) releaseReservation(ctx context.Context, tx Repository, claim Claim, actorID string) error {
	releases, err := s.releasesFor(ctx, tx, claim, actorID)
	if err != nil {
		return err
	}
	for i := range releases {
		if err := s.append(ctx, tx, claim.PartnerID, &releases[i]); err != nil {
			return err
		}
	}
	if actorID != "" {
		claim.UpdatedBy = actorID
	}
	if _, err := tx.ReplaceClaim(ctx, claim, claim.Revision-1); err != nil {
		return err
	}
	return nil
}

// RecordPayment settles a claim with a privileged manual payout. A full debit is
// permitted only for a proven complete, exact-amount, exact-currency transfer;
// partial, unknown or mismatched attempts produce durable evidence and send the
// claim to review without a debit or reservation release. ActorID and the
// recorded time are server-bound. Idempotent by (ActorID, UseCase, Partner,
// Currency, Key) with a fingerprint binding the claim identity, its frozen
// amount/currency and the observed payload.
func (s *Service) RecordPayment(ctx context.Context, req RecordPaymentRequest) (Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return Claim{}, err
	}
	if err := validateActor(req.ActorID); err != nil {
		return Claim{}, err
	}
	if err := validateExpectedRevision(req.ExpectedRevision); err != nil {
		return Claim{}, err
	}
	if _, ok := cleanPlain(req.ClaimID, maxIDLength); !ok {
		return Claim{}, errWrap(ErrInvalid, "claim id must be plain and non-empty")
	}
	if _, ok := cleanPlain(req.IdempotencyKey, maxIdempotencyKeyLen); !ok {
		return Claim{}, errWrap(ErrInvalid, "idempotency key must be plain and non-empty")
	}
	method, ref, err := validatePaymentPayload(s.clock.Now(), req.Method, req.Reference, req.PaidAt)
	if err != nil {
		return Claim{}, err
	}
	if err := validatePaymentEvidence(req.AmountMinor, req.Currency, req.State); err != nil {
		return Claim{}, err
	}

	pre, err := s.repo.GetClaim(ctx, s.programID, req.ClaimID)
	if err != nil {
		return Claim{}, err
	}
	partnerID := pre.PartnerID

	var out Claim
	err = s.repo.WithTransaction(ctx, s.programID, partnerID, s.currency, func(tx Repository) error {
		out = Claim{}
		claim, err := tx.GetClaim(ctx, s.programID, req.ClaimID)
		if err != nil {
			return err
		}
		fp := paymentFingerprint(s.programID, claim.ID, claim.AmountMinor, claim.Currency, method, ref, req.PaidAt, req.AmountMinor, req.Currency, req.State, req.ExpectedRevision)
		key := ReceiptKey{
			ProgramID: s.programID, PartnerID: partnerID, ActorID: req.ActorID,
			UseCase: UseCasePayment, Currency: s.currency, Key: req.IdempotencyKey,
		}
		receipt, err := tx.GetReceipt(ctx, key)
		switch {
		case err == nil:
			if receipt.Fingerprint != fp {
				return errWrap(ErrConflict, "payment idempotency key reused with different payload")
			}
			original, err := tx.GetClaim(ctx, s.programID, receipt.ClaimID)
			if err != nil {
				return err
			}
			out = original
			return nil
		case !strictFinancialAbsence(err):
			return err
		}

		if claim.Revision != req.ExpectedRevision {
			return errWrap(ErrStaleWrite, "claim revision changed")
		}

		if claim.State != ClaimProcessing && claim.State != ClaimNeedsReview {
			return errWrap(ErrInvalidState, "claim must be processing before payment")
		}
		if claim.Payment != nil {
			return errWrap(ErrConflict, "claim already paid")
		}
		if claim.ProcessingActor == "" {
			return errWrap(ErrInvalidState, "claim has no assigned processing actor")
		}
		if claim.ProcessingActor != req.ActorID {
			return errWrap(ErrConflict, "claim assigned to another processing actor")
		}

		now := s.clock.Now()
		full := req.State == PaymentStateFull && req.AmountMinor == claim.AmountMinor && req.Currency == claim.Currency
		observedState := req.State
		if req.State == PaymentStateFull && !full {
			observedState = PaymentStateMismatched
		}

		if full {
			paid := Entry{
				Kind: EntryPaid, SourceEventID: claim.ID,
				AmountMinor: -claim.AmountMinor, Currency: s.currency,
				OccurredAt: req.PaidAt, CreatedAt: now, ActorID: req.ActorID,
			}
			if err := s.append(ctx, tx, claim.PartnerID, &paid); err != nil {
				return err
			}
			// Settle the claim's allocations (audit of consumed backing).
			entries, err := tx.ListEntries(ctx, s.programID, claim.PartnerID)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if e.Kind != EntryAllocated || e.SourceEventID != claim.ID {
					continue
				}
				settled := Entry{
					Kind: EntryAllocationSettled, SourceEventID: claim.ID, SourceRef: e.SourceRef,
					AmountMinor: -e.AmountMinor, Currency: s.currency,
					OccurredAt: now, CreatedAt: now, ActorID: req.ActorID,
				}
				if err := s.append(ctx, tx, claim.PartnerID, &settled); err != nil {
					return err
				}
			}

			claim.State = ClaimPaid
			claim.Payment = &ManualPayment{
				Method: method, Reference: ref, PaidAt: req.PaidAt,
				RecordedBy: req.ActorID, RecordedAt: now,
				AmountMinor: req.AmountMinor, Currency: req.Currency, State: req.State,
			}
			claim.UpdatedAt = now
			claim.UpdatedBy = req.ActorID
			claim.Revision++
			if _, err := tx.ReplaceClaim(ctx, claim, claim.Revision-1); err != nil {
				return err
			}
		} else {
			// Durable evidence of a non-settling attempt: no debit, reservation
			// preserved, claim routed to review.
			observed := Entry{
				Kind: EntryPaymentObserved, SourceEventID: claim.ID,
				AmountMinor: 0, Currency: s.currency,
				OccurredAt: req.PaidAt, CreatedAt: now, ActorID: req.ActorID,
				Fingerprint: fp, Note: paymentReviewReason(observedState),
			}
			if err := s.append(ctx, tx, claim.PartnerID, &observed); err != nil {
				return err
			}
			claim.PaymentObservations = append(claim.PaymentObservations, PaymentObservation{
				AmountMinor: req.AmountMinor, Currency: req.Currency, State: observedState,
				Method: method, Reference: ref, PaidAt: req.PaidAt,
				RecordedBy: req.ActorID, RecordedAt: now,
			})
			claim.State = ClaimNeedsReview
			claim.ReviewReason = paymentReviewReason(observedState)
			claim.UpdatedAt = now
			claim.UpdatedBy = req.ActorID
			claim.Revision++
			if _, err := tx.ReplaceClaim(ctx, claim, claim.Revision-1); err != nil {
				return err
			}
		}

		r := Receipt{
			ProgramID: s.programID, PartnerID: claim.PartnerID, ActorID: req.ActorID,
			UseCase: UseCasePayment, Currency: s.currency, Key: req.IdempotencyKey,
			Fingerprint: fp, ClaimID: claim.ID, CreatedAt: now,
		}
		if err := tx.PutReceipt(ctx, r); err != nil {
			return err
		}
		out = claim
		return s.validateLedgerRepresentable(ctx, tx, claim.PartnerID)
	})
	if err != nil {
		return Claim{}, err
	}
	return out, nil
}

// AmendPayment corrects recorded payment details with a required reason. It
// appends an amendment and never creates a second debit. IdempotencyKey is the
// replay identity so a retry never appends a duplicate audit record;
// ExpectedRevision is a precondition on the claim revision.
func (s *Service) AmendPayment(ctx context.Context, req AmendPaymentRequest) (Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return Claim{}, err
	}
	if err := validateActor(req.ActorID); err != nil {
		return Claim{}, err
	}
	if err := validateExpectedRevision(req.ExpectedRevision); err != nil {
		return Claim{}, err
	}
	if _, ok := cleanPlain(req.ClaimID, maxIDLength); !ok {
		return Claim{}, errWrap(ErrInvalid, "claim id must be plain and non-empty")
	}
	if _, ok := cleanPlain(req.IdempotencyKey, maxIdempotencyKeyLen); !ok {
		return Claim{}, errWrap(ErrInvalid, "idempotency key must be plain and non-empty")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return Claim{}, errWrap(ErrInvalid, "amendment reason required")
	}
	if len(req.Reason) > maxReasonLength {
		return Claim{}, errWrap(ErrInvalid, "amendment reason too long")
	}
	method, ref, err := validatePaymentPayload(s.clock.Now(), req.Method, req.Reference, req.PaidAt)
	if err != nil {
		return Claim{}, err
	}
	pre, err := s.repo.GetClaim(ctx, s.programID, req.ClaimID)
	if err != nil {
		return Claim{}, err
	}
	var out Claim
	err = s.repo.WithTransaction(ctx, s.programID, pre.PartnerID, s.currency, func(tx Repository) error {
		out = Claim{}
		claim, err := tx.GetClaim(ctx, s.programID, req.ClaimID)
		if err != nil {
			return err
		}
		if claim.State != ClaimPaid || claim.Payment == nil {
			return errWrap(ErrInvalidState, "claim must be paid before amendment")
		}
		now := s.clock.Now()
		fp := amendmentFingerprint(s.programID, claim.ID, method, ref, req.PaidAt, strings.TrimSpace(req.Reason), req.IdempotencyKey, req.ExpectedRevision)
		key := ReceiptKey{
			ProgramID: s.programID, PartnerID: claim.PartnerID, ActorID: req.ActorID,
			UseCase: UseCaseAmendment, Currency: s.currency, Key: req.IdempotencyKey,
		}
		receipt, err := tx.GetReceipt(ctx, key)
		switch {
		case err == nil:
			if receipt.Fingerprint != fp {
				return errWrap(ErrConflict, "amendment idempotency key reused with different payload")
			}
			original, err := tx.GetClaim(ctx, s.programID, receipt.ClaimID)
			if err != nil {
				return err
			}
			out = original
			return nil
		case !strictFinancialAbsence(err):
			return err
		}

		if claim.Revision != req.ExpectedRevision {
			return errWrap(ErrStaleWrite, "claim revision changed")
		}

		claim.PaymentAmendments = append(claim.PaymentAmendments, PaymentAmendment{
			Method: method, Reference: ref, PaidAt: req.PaidAt,
			Reason: strings.TrimSpace(req.Reason), By: req.ActorID, At: now,
			IdempotencyKey: req.IdempotencyKey,
		})
		claim.UpdatedAt = now
		claim.UpdatedBy = req.ActorID
		claim.Revision++
		if _, err := tx.ReplaceClaim(ctx, claim, claim.Revision-1); err != nil {
			return err
		}
		r := Receipt{
			ProgramID: s.programID, PartnerID: claim.PartnerID, ActorID: req.ActorID,
			UseCase: UseCaseAmendment, Currency: s.currency, Key: req.IdempotencyKey,
			Fingerprint: fp, ClaimID: claim.ID, CreatedAt: now,
		}
		if err := tx.PutReceipt(ctx, r); err != nil {
			return err
		}
		out = claim
		return s.validateLedgerRepresentable(ctx, tx, pre.PartnerID)
	})
	if err != nil {
		return Claim{}, err
	}
	return out, nil
}

// ---- Reads ----

// Balances derives the partner's current balance from a single transaction-
// consistent snapshot of the journal and claim states, so a concurrent payout
// cannot produce an incoherent available balance. Pending accruals are reported
// separately from the net matured balance.
func (s *Service) Balances(ctx context.Context, partnerID string) (Balances, error) {
	if err := s.checkContext(ctx); err != nil {
		return Balances{}, err
	}
	if strings.TrimSpace(partnerID) == "" {
		return Balances{}, errWrap(ErrInvalid, "partner required")
	}
	var b Balances
	err := s.repo.WithTransaction(ctx, s.programID, partnerID, s.currency, func(tx Repository) error {
		entries, err := tx.ListEntries(ctx, s.programID, partnerID)
		if err != nil {
			return err
		}
		claims, err := tx.ListClaims(ctx, s.programID, partnerID, nil, 0, "")
		if err != nil {
			return err
		}
		b, _, err = derive(entries, claims)
		return err
	})
	if err != nil {
		return Balances{}, err
	}
	return b, nil
}

// ListJournal returns the partner's append-only journal in sequence order.
func (s *Service) ListJournal(ctx context.Context, partnerID string) ([]Entry, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	return s.repo.ListEntries(ctx, s.programID, partnerID)
}

// ---- Internal helpers ----

func (s *Service) checkContext(ctx context.Context) error {
	if s == nil || s.repo == nil || s.ids == nil || ctx == nil {
		return ErrUnavailable
	}
	return ctx.Err()
}

// append assigns the stable sequence and id, then appends one journal entry.
func (s *Service) append(ctx context.Context, tx Repository, partnerID string, e *Entry) error {
	seq, err := tx.NextSequence(ctx, s.programID, partnerID)
	if err != nil {
		return err
	}
	e.Sequence = seq
	e.ID = s.ids.NewID()
	e.ProgramID = s.programID
	e.PartnerID = partnerID
	if e.Currency == "" {
		e.Currency = s.currency
	}
	return tx.AppendEntry(ctx, *e)
}

// validateLedgerRepresentable re-derives the entire final transaction state and
// fails closed if any required int64 balance or counter can no longer be
// represented. It is called at the end of every mutation, after all appends and
// claim updates, so a ledger that would be unreadable on the next balance read
// rolls back instead of committing.
func (s *Service) validateLedgerRepresentable(ctx context.Context, tx Repository, partnerID string) error {
	entries, err := tx.ListEntries(ctx, s.programID, partnerID)
	if err != nil {
		return err
	}
	claims, err := tx.ListClaims(ctx, s.programID, partnerID, nil, 0, "")
	if err != nil {
		return err
	}
	_, _, err = derive(entries, claims)
	return err
}

func errWrap(err error, format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{err}, args...)...)
}

// isNilInterface reports whether i is nil or holds a nil pointer/map/slice/func
// (a typed nil). Constructors must reject typed nils, not dereference them.
func isNilInterface(i any) bool {
	if i == nil {
		return true
	}
	v := reflect.ValueOf(i)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	}
	return false
}

// derive computes balances and per-payment matured credit availability from the
// ordered journal and current claims. It is pure and fully deterministic.
func derive(entries []Entry, claims []Claim) (Balances, []creditAllocation, error) {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Sequence < sorted[j].Sequence })

	type pstate struct {
		accrued, matured, maturedRev, pendingRev big.Int
		allocated, released                      big.Int
		disputeHold, disputeLost                 big.Int
		hasMatured                               bool
		maturedSeq                               int64
	}
	payments := map[string]*pstate{}
	get := func(id string) *pstate {
		if p, ok := payments[id]; ok {
			return p
		}
		p := &pstate{}
		payments[id] = p
		return p
	}

	var paidOut, returnedTotal big.Int
	for _, e := range sorted {
		switch e.Kind {
		case EntryAccrued:
			p := get(e.SourceEventID)
			p.accrued.Add(&p.accrued, big.NewInt(e.AmountMinor))
		case EntryMatured:
			p := get(e.SourceEventID)
			p.matured.Add(&p.matured, big.NewInt(e.AmountMinor))
			p.hasMatured = true
			p.maturedSeq = e.Sequence
		case EntryReversed:
			p := get(e.SourceRef)
			amount := negBigInt(e.AmountMinor)
			if p.hasMatured && e.Sequence > p.maturedSeq {
				p.maturedRev.Add(&p.maturedRev, amount)
			} else {
				p.pendingRev.Add(&p.pendingRev, amount)
			}
		case EntryAllocated:
			p := get(e.SourceRef)
			p.allocated.Add(&p.allocated, big.NewInt(e.AmountMinor))
		case EntryAllocationReleased:
			p := get(e.SourceRef)
			p.released.Add(&p.released, big.NewInt(e.AmountMinor))
		case EntryPaid:
			paidOut.Add(&paidOut, negBigInt(e.AmountMinor))
		case EntryReturned:
			// A returned transfer restores settled debit (credit to matched).
			returnedTotal.Add(&returnedTotal, big.NewInt(e.AmountMinor))
		case EntryDisputeHold:
			p := get(e.SourceRef)
			p.disputeHold.Add(&p.disputeHold, big.NewInt(e.AmountMinor))
		case EntryDisputeWon, EntryDisputeReleased:
			p := get(e.SourceRef)
			p.disputeHold.Sub(&p.disputeHold, big.NewInt(e.AmountMinor))
		case EntryDisputeLost:
			p := get(e.SourceRef)
			p.disputeLost.Add(&p.disputeLost, negBigInt(e.AmountMinor))
		}
	}

	var pending, matched big.Int
	credits := make([]creditAllocation, 0, len(payments))
	for id, p := range payments {
		var reversed big.Int
		reversed.Add(&p.maturedRev, &p.pendingRev)
		var pv, mv, av big.Int
		if p.hasMatured {
			mv.Sub(&p.matured, &reversed)
			mv.Sub(&mv, &p.disputeLost)
			matched.Add(&matched, &mv)
			av.Set(&mv)
			av.Sub(&av, &p.allocated)
			av.Sub(&av, &p.released)
			av.Sub(&av, &p.disputeHold)
		} else {
			pv.Sub(&p.accrued, &reversed)
			pv.Sub(&pv, &p.disputeLost)
			pending.Add(&pending, &pv)
		}
		avInt, err := bigToInt64(&av)
		if err != nil {
			return Balances{}, nil, err
		}
		credits = append(credits, creditAllocation{
			paymentID: id,
			available: avInt,
			order:     p.maturedSeq,
		})
	}
	matched.Sub(&matched, &paidOut)
	matched.Add(&matched, &returnedTotal)

	var maturedDisputeHold, pendingDisputeHold big.Int
	for _, p := range payments {
		if p.hasMatured {
			maturedDisputeHold.Add(&maturedDisputeHold, &p.disputeHold)
		} else {
			pendingDisputeHold.Add(&pendingDisputeHold, &p.disputeHold)
		}
	}

	var reserved, reviewHold big.Int
	for _, c := range claims {
		switch c.State {
		case ClaimRequested, ClaimProcessing:
			reserved.Add(&reserved, big.NewInt(c.AmountMinor))
		case ClaimNeedsReview:
			reviewHold.Add(&reviewHold, big.NewInt(c.AmountMinor))
		}
	}

	var available big.Int
	available.Sub(&matched, &reserved)
	available.Sub(&available, &reviewHold)
	available.Sub(&available, &maturedDisputeHold)
	if available.Sign() < 0 {
		available.SetInt64(0)
	}
	var debt big.Int
	if m := new(big.Int).Neg(&matched); m.Sign() > 0 {
		debt.Set(m)
	}

	toInt64 := func(v *big.Int) (int64, error) { return bigToInt64(v) }
	pendingI, err := toInt64(&pending)
	if err != nil {
		return Balances{}, nil, err
	}
	matchedI, err := toInt64(&matched)
	if err != nil {
		return Balances{}, nil, err
	}
	reservedI, err := toInt64(&reserved)
	if err != nil {
		return Balances{}, nil, err
	}
	reviewHoldI, err := toInt64(&reviewHold)
	if err != nil {
		return Balances{}, nil, err
	}
	pendingDisputeI, err := toInt64(&pendingDisputeHold)
	if err != nil {
		return Balances{}, nil, err
	}
	disputeHoldI, err := toInt64(&maturedDisputeHold)
	if err != nil {
		return Balances{}, nil, err
	}
	availableI, err := toInt64(&available)
	if err != nil {
		return Balances{}, nil, err
	}
	debtI, err := toInt64(&debt)
	if err != nil {
		return Balances{}, nil, err
	}
	paidOutI, err := toInt64(&paidOut)
	if err != nil {
		return Balances{}, nil, err
	}

	sort.Slice(credits, func(i, j int) bool { return credits[i].order < credits[j].order })
	return Balances{
		PendingMinor:     pendingI,
		MatchedMinor:     matchedI,
		ReservedMinor:    reservedI,
		ReviewHoldMinor:  reviewHoldI,
		DisputeHoldMinor: disputeHoldI, PendingDisputeHoldMinor: pendingDisputeI,
		AvailableMinor: availableI,
		DebtMinor:      debtI,
		PaidOutMinor:   paidOutI,
	}, credits, nil
}

type creditAllocation struct {
	paymentID string
	available int64
	order     int64
}

type allocationPart struct {
	paymentID string
	amount    int64
}

// allocate distributes amount across the oldest available matured credits.
func allocate(credits []creditAllocation, amount int64) []allocationPart {
	var parts []allocationPart
	remaining := amount
	for _, c := range credits {
		if c.available <= 0 || remaining <= 0 {
			continue
		}
		take := c.available
		if take > remaining {
			take = remaining
		}
		parts = append(parts, allocationPart{paymentID: c.paymentID, amount: take})
		remaining -= take
	}
	return parts
}

// ---- Validation ----

func validateAccrual(currency string, req AccrualRequest) error {
	if _, ok := cleanPlain(req.PartnerID, maxIDLength); !ok {
		return errWrap(ErrInvalid, "partner required")
	}
	if _, ok := cleanPlain(req.PaymentID, maxIDLength); !ok {
		return errWrap(ErrInvalid, "payment required")
	}
	if req.PaymentMinor < 0 {
		return errWrap(ErrInvalid, "payment amount must be non-negative")
	}
	if req.RateBasisPoints < MinRateBasisPoints || req.RateBasisPoints > MaxRateBasisPoints {
		return errWrap(ErrInvalid, "rate out of bounds")
	}
	if req.HoldDuration < 0 {
		return errWrap(ErrInvalid, "hold must be non-negative")
	}
	if req.HoldDuration > maxHoldDuration {
		return errWrap(ErrInvalid, "hold exceeds maximum duration")
	}
	if req.OccurredAt.IsZero() {
		return errWrap(ErrInvalid, "occurred-at required")
	}
	if req.Currency != currency {
		return errWrap(ErrCurrencyMismatch, "source currency does not match program currency")
	}
	if !boundedOptional(req.ReferralID, maxIDLength) {
		return errWrap(ErrInvalid, "referral id too long")
	}
	if req.PlanID != "" {
		if cleaned, ok := cleanPlain(req.PlanID, maxIDLength); !ok || cleaned != req.PlanID {
			return errWrap(ErrInvalid, "plan id must be canonical and bounded")
		}
	}
	if !boundedOptional(req.TermsVersion, maxTermsLength) {
		return errWrap(ErrInvalid, "terms version too long")
	}
	if !boundedOptional(req.PolicyID, maxPolicyLength) {
		return errWrap(ErrInvalid, "policy id too long")
	}
	if !boundedOptional(req.SourceKind, maxSourceKindLength) {
		return errWrap(ErrInvalid, "source kind too long")
	}
	return nil
}

// boundedOptional reports whether an optional provenance string is within the
// allowed length (empty is permitted, control characters are rejected).
func boundedOptional(s string, maxLen int) bool {
	if s == "" {
		return true
	}
	_, ok := cleanPlain(s, maxLen)
	return ok
}

func validateReversal(currency string, req ReversalRequest) error {
	if strings.TrimSpace(req.PartnerID) == "" || strings.TrimSpace(req.RefundID) == "" || strings.TrimSpace(req.PaymentID) == "" {
		return errWrap(ErrInvalid, "partner, refund and payment required")
	}
	if req.CumulativeRefundedMinor < 0 {
		return errWrap(ErrInvalid, "cumulative refund must be non-negative")
	}
	if req.OccurredAt.IsZero() {
		return errWrap(ErrInvalid, "occurred-at required")
	}
	if req.Currency != currency {
		return errWrap(ErrCurrencyMismatch, "source currency does not match program currency")
	}
	return nil
}

func validateClaimRequest(currency string, req ClaimRequest) error {
	if !boundedOptional(req.Reason, maxReasonLength) {
		return ErrInvalid
	}
	for _, value := range []string{req.PartnerID, req.ActorID, req.DestinationID} {
		if _, ok := cleanPlain(value, maxIDLength); !ok {
			return errWrap(ErrInvalid, "claim identity must be plain and bounded")
		}
	}
	if len(req.DestinationSnapshot) > 32 {
		return errWrap(ErrInvalid, "destination snapshot too large")
	}
	snapshotBytes := 0
	for key, value := range req.DestinationSnapshot {
		if _, ok := cleanPlain(key, 128); !ok || !boundedOptional(value, 1024) {
			return errWrap(ErrInvalid, "destination snapshot must be plain and bounded")
		}
		snapshotBytes += len(key) + len(value)
	}
	if snapshotBytes > 4096 {
		return errWrap(ErrInvalid, "destination snapshot too large")
	}
	if strings.TrimSpace(req.PartnerID) == "" {
		return errWrap(ErrInvalid, "partner required")
	}
	if strings.TrimSpace(req.ActorID) == "" {
		return errWrap(ErrInvalid, "actor required")
	}
	if req.AmountMinor <= 0 {
		return errWrap(ErrInvalid, "claim amount must be positive")
	}
	if req.Currency != currency {
		return errWrap(ErrCurrencyMismatch, "claim currency does not match program currency")
	}
	if strings.TrimSpace(req.DestinationID) == "" {
		return errWrap(ErrInvalid, "destination required")
	}
	key, ok := cleanPlain(req.IdempotencyKey, maxIdempotencyKeyLen)
	if !ok || key == "" {
		return errWrap(ErrInvalid, "idempotency key required")
	}
	return nil
}

// validateActor requires a non-empty, plain, bounded verified actor identity.
func validateActor(actorID string) error {
	if _, ok := cleanPlain(actorID, maxIDLength); !ok {
		return errWrap(ErrInvalid, "actor required")
	}
	return nil
}

// validateExpectedRevision requires a positive claim revision precondition. The
// revision is a caller-observed optimistic-concurrency guard, never a zero
// bypass; zero and negative values fail closed.
func validateExpectedRevision(rev int64) error {
	if rev <= 0 {
		return errWrap(ErrInvalid, "expected revision must be positive")
	}
	return nil
}

// validatePaymentPayload bounds and normalizes the manual payment metadata.
func validatePaymentPayload(now time.Time, method, reference string, paidAt time.Time) (string, string, error) {
	m, ok := cleanPlain(method, maxMethodLength)
	if !ok || m == "" {
		return "", "", errWrap(ErrInvalid, "payment method must be plain and non-empty")
	}
	r, ok := cleanPlain(reference, maxReferenceLength)
	if !ok || r == "" {
		return "", "", errWrap(ErrInvalid, "payment reference must be plain and non-empty")
	}
	if paidAt.IsZero() {
		return "", "", errWrap(ErrInvalid, "paid-at required")
	}
	if paidAt.After(now) {
		return "", "", errWrap(ErrInvalid, "paid-at must not be in the future")
	}
	return m, r, nil
}

// validatePaymentEvidence requires the observed transfer amount, currency and
// state, the mandatory proof of a manual payout. Full settlement is only
// permitted for a proven exact amount/currency; anything else is evidence of a
// non-settling attempt.
func validatePaymentEvidence(amountMinor int64, currency, state string) error {
	if amountMinor < 0 || (state != PaymentStateUnknown && amountMinor == 0) {
		return errWrap(ErrInvalid, "observed transfer amount must be positive")
	}
	if !(state == PaymentStateUnknown && currency == "") && (!isUpperAlpha(currency) || len(currency) != 3) {
		return errWrap(ErrInvalid, "observed transfer currency must be a 3-letter code")
	}
	switch state {
	case PaymentStateFull, PaymentStatePartial, PaymentStateUnknown, PaymentStateMismatched:
		return nil
	default:
		return errWrap(ErrInvalid, "unknown payment state")
	}
}

// paymentReviewReason is the durable reason recorded when a non-settling
// payment observation sends a claim to review.
func paymentReviewReason(state string) string {
	switch state {
	case PaymentStatePartial:
		return "partial payment recorded; reservation preserved"
	case PaymentStateUnknown:
		return "payment outcome unconfirmed; reservation preserved"
	case PaymentStateMismatched:
		return "payment amount or currency mismatch; reservation preserved"
	default:
		return "payment needs review"
	}
}

func cleanPlain(s string, maxLen int) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" || len(t) > maxLen || !utf8.ValidString(t) {
		return "", false
	}
	for _, r := range t {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return t, true
}

func isUpperAlpha(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return len(s) > 0
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// claimFingerprint is the canonical fingerprint of a claim request payload.
func claimFingerprint(programID string, req ClaimRequest) string {
	return fingerprint(
		programID, req.PartnerID, req.ActorID, req.Currency,
		strconv.FormatInt(req.AmountMinor, 10), req.DestinationID,
		canonicalSnapshot(req.DestinationSnapshot), req.Reason,
	)
}

// paymentFingerprint is the canonical fingerprint of a manual payment payload,
// binding the claim's identity, frozen amount/currency and the observed
// transfer evidence, so the same idempotency key cannot replay a different
// claim or a different observed payload.
func paymentFingerprint(programID, claimID string, amountMinor int64, currency, method, reference string, paidAt time.Time, observedMinor int64, observedCurrency, state string, expectedRevision int64) string {
	return fingerprint(
		programID, "payment", claimID,
		strconv.FormatInt(amountMinor, 10), currency,
		method, reference, paidAt.UTC().Format(time.RFC3339Nano),
		strconv.FormatInt(observedMinor, 10), observedCurrency, state, strconv.FormatInt(expectedRevision, 10),
	)
}

// amendmentFingerprint binds an amendment's replay identity to the claim and the
// corrected payload, so a retry under the same key never appends a duplicate.
func amendmentFingerprint(programID, claimID, method, reference string, paidAt time.Time, reason, key string, expectedRevision int64) string {
	return fingerprint(
		programID, "amendment", claimID,
		method, reference, paidAt.UTC().Format(time.RFC3339Nano), reason, key, strconv.FormatInt(expectedRevision, 10),
	)
}

// accrualFingerprint is the canonical fingerprint of every frozen accrual field:
// payment amount, rate, hold, provider-effective timestamp, referral, terms,
// policy and source metadata. A replay under the same economic ID (PaymentID)
// conflicts unless all of these match.
func accrualFingerprint(programID string, req AccrualRequest) string {
	return fingerprint(
		programID, "accrual",
		strings.TrimSpace(req.PartnerID),
		strings.TrimSpace(req.PaymentID),
		strconv.FormatInt(req.PaymentMinor, 10),
		strconv.Itoa(req.RateBasisPoints),
		strconv.FormatInt(int64(req.HoldDuration), 10),
		req.Currency,
		req.OccurredAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(req.ReferralID),
		strings.TrimSpace(req.TermsVersion),
		strings.TrimSpace(req.PolicyID),
		strings.TrimSpace(req.SourceKind),
		req.PlanID,
	)
}

// reversalFingerprint is the canonical fingerprint of a refund event's frozen
// fields, used to deduplicate and detect changed-payload replays by RefundID.
func reversalFingerprint(programID string, req ReversalRequest) string {
	return fingerprint(
		programID, "reversal",
		strings.TrimSpace(req.PartnerID),
		strings.TrimSpace(req.RefundID),
		strings.TrimSpace(req.PaymentID),
		strconv.FormatInt(req.CumulativeRefundedMinor, 10),
		req.Currency,
		req.OccurredAt.UTC().Format(time.RFC3339Nano),
	)
}

func canonicalSnapshot(m map[string]string) string {
	// encoding/json sorts string map keys and escapes delimiters, so structurally
	// different destination snapshots cannot alias the same replay evidence.
	body, _ := json.Marshal(m)
	return string(body)
}

// fingerprint builds a length-prefixed SHA-256 over the parts so that arbitrary
// values cannot alias another identity.
func fingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(strconv.Itoa(len(p))))
		h.Write([]byte{':'})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
