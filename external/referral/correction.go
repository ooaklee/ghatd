package referral

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"
)

const CorrectionProspective = "prospective"

// CorrectionRequest is privileged owning-service input. All source and terms
// fields come from the manager's current owning services. Reviewed fingerprints
// are preconditions, never proof of caller permission. V1 moves no past money.
type CorrectionRequest struct {
	Partner                                                             PartnerState
	ReferredCustomer, SignupID                                          string
	SignupCreatedAt                                                     time.Time
	ActorID                                                             string `json:"-"`
	Reason, Mode, IdempotencyKey                                        string
	ExpectedRevision                                                    int64
	ExpectedReferralID, ExpectedSnapshotFingerprint, PreviewFingerprint string
	Terms                                                               TermsSnapshot
}

// CorrectionReceipt freezes original request evidence for actor/key recovery.
// The parent Referral excludes this from JSON. Adapters explicitly persist it
// inside the encrypted record, separately from customer transport decoding.
type CorrectionReceipt struct {
	RequestFingerprint, PreviewFingerprint, SnapshotFingerprint string
	PartnerCustomer, SignupID                                   string
	SignupCreatedAt                                             time.Time
	ExpectedRevision                                            int64
	ExpectedReferralID, Mode                                    string
}

// AttributionSnapshot is one complete owning transaction's current head,
// retained history and payment bindings. All existing bindings are immutable
// exclusions, including allocations whose economic time is in the future.
type AttributionSnapshot struct {
	ReferredCustomer string
	Head             Referral
	History          []Referral
	Bindings         []PaymentAttribution
	Fingerprint      string
}

func correctionText(v string, n int) bool { return v != "" && v == strings.TrimSpace(v) && len(v) <= n }
func correctionDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func correctionID(actor, customer, key string) string {
	fp, _ := correctionDigest([]string{ProgramID, customer, actor, key})
	return "ref_correction_" + fp
}
func validCorrectionFingerprint(v string) bool {
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == sha256.Size && v == strings.ToLower(v)
}

// GetAttributionSnapshot reads all ownership and payment evidence using the
// same customer guard as a correction or first payment binding. No partial
// snapshot is returned on failure; complete history reads can grow with age.
func (s *Service) GetAttributionSnapshot(ctx context.Context, customer string) (AttributionSnapshot, error) {
	if err := s.ready(ctx); err != nil {
		return AttributionSnapshot{}, err
	}
	if !correctionText(customer, 256) {
		return AttributionSnapshot{}, ErrInvalid
	}
	var out AttributionSnapshot
	err := s.repo.WithAttributionTransaction(ctx, ProgramID, customer, func(tx Repository) error {
		out = AttributionSnapshot{}
		if nilReferralDependency(tx) {
			return ErrUnavailable
		}
		var err error
		out, err = attributionSnapshot(ctx, tx, customer)
		return err
	})
	if err != nil {
		return AttributionSnapshot{}, err
	}
	return out, nil
}

func attributionSnapshot(ctx context.Context, tx Repository, customer string) (AttributionSnapshot, error) {
	head, err := tx.GetReferralByCustomer(ctx, ProgramID, customer)
	if err != nil && !singleReferralCause(err, ErrNotFound) {
		return AttributionSnapshot{}, err
	}
	if err != nil {
		head = Referral{}
	}
	history, err := tx.ListReferralHistory(ctx, ProgramID, customer)
	if err != nil {
		return AttributionSnapshot{}, err
	}
	bindings, err := tx.ListPaymentAttributionsByCustomer(ctx, ProgramID, customer)
	if err != nil {
		return AttributionSnapshot{}, err
	}
	return validatedAttributionSnapshot(customer, head, history, bindings)
}

// validatedAttributionSnapshot is shared by customer correction reads and the
// complete partner reporting read. Its canonical fingerprint remains identical.
func validatedAttributionSnapshot(customer string, head Referral, history []Referral, bindings []PaymentAttribution) (AttributionSnapshot, error) {
	bindings = append([]PaymentAttribution(nil), bindings...)
	history, err := validatedOwnershipHistory(customer, head, history)
	if err != nil || (len(history) == 0 && len(bindings) != 0) {
		return AttributionSnapshot{}, ErrUnavailable
	}
	byID := map[string]Referral{}
	for _, row := range history {
		byID[row.ID] = row
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].PaymentID < bindings[j].PaymentID })
	seenBindingIDs := map[string]bool{}
	for i, b := range bindings {
		owner, exists := byID[b.ReferralID]
		if !exists || b.ID == "" || seenBindingIDs[b.ID] || b.ProgramID != ProgramID || b.ReferredCustomer != customer || b.PaymentID == "" || b.PartnerID != owner.PartnerID || b.EffectiveAt.IsZero() || b.BoundAt.IsZero() || b.EffectiveAt.Before(owner.LockedAt) || !reflect.DeepEqual(b.Terms, owner.TermsSnapshot) || (i > 0 && bindings[i-1].PaymentID == b.PaymentID) {
			return AttributionSnapshot{}, ErrUnavailable
		}
		seenBindingIDs[b.ID] = true
	}
	// Empty lists have one canonical representation, independent of adapter nils.
	if history == nil {
		history = []Referral{}
	}
	if bindings == nil {
		bindings = []PaymentAttribution{}
	}
	out := AttributionSnapshot{ReferredCustomer: customer, Head: head, History: history, Bindings: bindings}
	receipts := make([]*CorrectionReceipt, len(history))
	for i, row := range history {
		receipts[i] = row.Correction
	}
	out.Fingerprint, err = correctionDigest(struct {
		Snapshot AttributionSnapshot
		Receipts []*CorrectionReceipt
	}{out, receipts})
	if err != nil {
		return AttributionSnapshot{}, err
	}
	return out, nil
}

// FindCorrection recovers only an immutable receipt belonging to the selected
// actor/customer/key. The manager must check current authority before and after
// this read. A later head, policy or pause cannot change the original receipt.
func (s *Service) FindCorrection(ctx context.Context, actor, customer, key string) (Referral, error) {
	if err := s.ready(ctx); err != nil {
		return Referral{}, err
	}
	if !correctionText(actor, 256) || !correctionText(customer, 256) || !correctionText(key, 256) {
		return Referral{}, ErrInvalid
	}
	var out Referral
	err := s.repo.WithAttributionTransaction(ctx, ProgramID, customer, func(tx Repository) error {
		out = Referral{}
		if nilReferralDependency(tx) {
			return ErrUnavailable
		}
		var err error
		out, err = findCorrection(ctx, tx, actor, customer, key)
		return err
	})
	if err != nil {
		return Referral{}, err
	}
	return out, nil
}
func findCorrection(ctx context.Context, tx Repository, actor, customer, key string) (Referral, error) {
	rows, err := tx.ListReferralHistory(ctx, ProgramID, customer)
	if err != nil {
		return Referral{}, err
	}
	for _, row := range rows {
		if row.ID != correctionID(actor, customer, key) {
			continue
		}
		if row.ProgramID != ProgramID || row.ReferredCustomer != customer || row.CorrectionBy != actor || row.Correction == nil || row.Correction.Mode != CorrectionProspective || !validCorrectionFingerprint(row.Correction.RequestFingerprint) {
			return Referral{}, ErrUnavailable
		}
		c := row.Correction
		reconstructed := CorrectionRequest{Partner: PartnerState{PartnerID: row.PartnerID, CustomerID: c.PartnerCustomer}, ReferredCustomer: customer, SignupID: c.SignupID, SignupCreatedAt: c.SignupCreatedAt, ActorID: actor, Reason: row.CorrectionReason, Mode: c.Mode, IdempotencyKey: key, ExpectedRevision: c.ExpectedRevision, ExpectedReferralID: c.ExpectedReferralID, ExpectedSnapshotFingerprint: c.SnapshotFingerprint, PreviewFingerprint: c.PreviewFingerprint, Terms: row.TermsSnapshot}
		fp, err := correctionRequestFingerprint(reconstructed)
		if err != nil || fp != c.RequestFingerprint || row.Revision != c.ExpectedRevision+1 || row.CorrectionOf != c.ExpectedReferralID || row.SignupID != c.SignupID {
			return Referral{}, ErrUnavailable
		}
		return row, nil
	}
	return Referral{}, ErrNotFound
}

func correctionRequestFingerprint(req CorrectionRequest) (string, error) {
	// Current acquisition permission is not part of immutable replay input.
	req.Partner.CanAcquireReferrals = false
	req.SignupCreatedAt = req.SignupCreatedAt.UTC()
	req.Terms = cloneTerms(req.Terms)
	return correctionDigest(struct {
		Program, Actor string
		Request        CorrectionRequest
	}{ProgramID, req.ActorID, req})
}

// AssignAttribution applies a reviewed prospective revision and records its
// stable receipt atomically. It never rewrites any existing payment or earning.
// A committed request replays before mutable acquisition/head/time checks.
func (s *Service) AssignAttribution(ctx context.Context, req CorrectionRequest) (Referral, error) {
	if err := s.ready(ctx); err != nil {
		return Referral{}, err
	}
	if !correctionText(req.ActorID, 256) || !correctionText(req.Reason, 1000) {
		return Referral{}, ErrDenied
	}
	if !correctionText(req.ReferredCustomer, 256) || !correctionText(req.Partner.PartnerID, 256) || !correctionText(req.Partner.CustomerID, 256) || !correctionText(req.SignupID, 256) || req.SignupCreatedAt.IsZero() || !correctionText(req.IdempotencyKey, 256) || req.Mode != CorrectionProspective || req.ExpectedRevision < 0 || req.ExpectedRevision == math.MaxInt64 || (req.ExpectedRevision == 0) != (req.ExpectedReferralID == "") || !validTerms(req.Terms) || !validCorrectionFingerprint(req.ExpectedSnapshotFingerprint) || !validCorrectionFingerprint(req.PreviewFingerprint) {
		return Referral{}, ErrInvalid
	}
	fingerprint, err := correctionRequestFingerprint(req)
	if err != nil {
		return Referral{}, err
	}
	var out Referral
	err = s.repo.WithAttributionTransaction(ctx, ProgramID, req.ReferredCustomer, func(tx Repository) error {
		out = Referral{}
		if nilReferralDependency(tx) {
			return ErrUnavailable
		}
		old, err := findCorrection(ctx, tx, req.ActorID, req.ReferredCustomer, req.IdempotencyKey)
		if err == nil {
			if old.Correction.RequestFingerprint != fingerprint {
				return ErrStaleWrite
			}
			out = old
			return nil
		}
		if !singleReferralCause(err, ErrNotFound) {
			return err
		}
		if !req.Partner.CanAcquireReferrals {
			return ErrDenied
		}
		if req.ReferredCustomer == req.Partner.CustomerID {
			return ErrSelfReferral
		}
		state, err := attributionSnapshot(ctx, tx, req.ReferredCustomer)
		if err != nil {
			return err
		}
		prior := state.Head
		if state.Fingerprint != req.ExpectedSnapshotFingerprint || prior.ID != req.ExpectedReferralID || prior.Revision != req.ExpectedRevision {
			return ErrStaleWrite
		}
		if prior.ID != "" && prior.SignupID != req.SignupID {
			return ErrDenied
		}
		now := s.clock.Now().UTC()
		if now.IsZero() || now.Before(req.SignupCreatedAt) || (!prior.LockedAt.IsZero() && now.Before(prior.LockedAt)) {
			return ErrInvalid
		}
		r := Referral{ID: correctionID(req.ActorID, req.ReferredCustomer, req.IdempotencyKey), Revision: req.ExpectedRevision + 1, ProgramID: ProgramID, PartnerID: req.Partner.PartnerID, ReferredCustomer: req.ReferredCustomer, SignupID: req.SignupID, EvidenceDigest: prior.EvidenceDigest, SourceKind: "admin", LockedAt: now, PostedAt: now, TermsSnapshot: cloneTerms(req.Terms), CorrectionOf: prior.ID, PriorPartnerID: prior.PartnerID, CorrectionBy: req.ActorID, CorrectionReason: req.Reason, Correction: &CorrectionReceipt{RequestFingerprint: fingerprint, PreviewFingerprint: req.PreviewFingerprint, SnapshotFingerprint: req.ExpectedSnapshotFingerprint, PartnerCustomer: req.Partner.CustomerID, SignupID: req.SignupID, SignupCreatedAt: req.SignupCreatedAt.UTC(), ExpectedRevision: req.ExpectedRevision, ExpectedReferralID: req.ExpectedReferralID, Mode: req.Mode}}
		if err := tx.InsertReferral(ctx, r, req.ExpectedRevision); err != nil {
			return err
		}
		out = r
		return nil
	})
	if err != nil {
		return Referral{}, err
	}
	return out, nil
}
