package partnermanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// AttributionChange selects targets and a reason. Verified ActorID is bound by
// the host, never decoded from a browser. SignupID resolves an immutable owning
// creation fact; an existing account/profile is not historical signup proof.
type AttributionChange struct {
	ActorID                                             string `json:"-"`
	ReferredCustomer, PartnerID, SignupID, Reason, Mode string
}

// ApplyAttributionRequest extends AttributionChange with optimistic revision,
// referred-identity, snapshot and preview fingerprints, and the durable replay
// idempotency key.
type ApplyAttributionRequest struct {
	AttributionChange
	ExpectedRevision                                                            int64
	ExpectedReferralID, SnapshotFingerprint, PreviewFingerprint, IdempotencyKey string
}

// AttributionPreview is an operator read model. Existing bindings, including
// future-effective allocations, are excluded from reassignment. Historical
// commission/payout delta is exactly zero by contract; future amounts depend on
// actual verified eligible revenue and are not forecasts. This raw owning view
// contains private identity/policy references and needs an explicit host DTO.
type AttributionPreview struct {
	Mode, ReferredCustomer, PartnerID, SignupID, Reason        string
	Original                                                   referral.Referral
	ProposedTerms                                              referral.TermsSnapshot
	History                                                    []referral.Referral
	UnchangedBindings                                          []referral.PaymentAttribution
	HistoricalCommissionDeltaMinor, HistoricalPayoutDeltaMinor int64
	SnapshotFingerprint, Fingerprint                           string
	AsOf                                                       time.Time
}

// attributionSources holds the owning principals, partner and immutable signup
// fact collected during preview; the embedded signup struct is the frozen
// creation evidence.
type attributionSources struct {
	Referred Principal
	Owner    Principal
	Partner  partnerprogram.Partner
	Signup   struct {
		ID, CustomerID, Evidence string
		CreatedAt                time.Time
		NewAccount, Individual   bool
	}
}

// attributionHash hashes the JSON encoding of v with SHA-256, failing with
// ErrInvalid when the value cannot be marshaled.
func attributionHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// validAttributionChange requires bounded referred customer, partner, signup
// and reason text, and only the prospective correction mode.
func validAttributionChange(r AttributionChange) bool {
	return validWorkText(r.ReferredCustomer, 256) && validWorkText(r.PartnerID, 256) && validWorkText(r.SignupID, 256) && validWorkText(r.Reason, 1000) && r.Mode == referral.CorrectionProspective
}

// PreviewAttribution resolves current authority, eligibility and approved
// prospective terms. A typed owning absence is distinct from an outage.
func (m *Manager) PreviewAttribution(ctx context.Context, req AttributionChange) (AttributionPreview, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityAttribution, req.ReferredCustomer); err != nil {
		return AttributionPreview{}, err
	}
	if !validAttributionChange(req) {
		return AttributionPreview{}, ErrInvalid
	}
	if !m.deps.Controls.Attribution {
		return AttributionPreview{}, ErrDenied
	}
	out, _, err := m.previewAttribution(ctx, req)
	if err != nil {
		return AttributionPreview{}, err
	}
	if err := m.authorize(ctx, req.ActorID, CapabilityAttribution, req.ReferredCustomer); err != nil {
		return AttributionPreview{}, err
	}
	return out, nil
}

// previewAttribution resolves the signup fact, referred and owner principals,
// partner admission, the current attribution snapshot and approved terms,
// cross-checking every owning record's scope. It shortens proposed terms to
// honor any earlier promised recurrence end, then fingerprints the change,
// sources, snapshot and terms; AsOf and the actor are deliberately excluded
// from the economic fingerprint.
func (m *Manager) previewAttribution(ctx context.Context, req AttributionChange) (AttributionPreview, attributionSources, error) {
	var sources attributionSources
	at := m.deps.Clock.Now().UTC()
	if at.IsZero() {
		return AttributionPreview{}, sources, ErrUnavailable
	}
	fact, err := m.deps.Identity.GetSignupFact(ctx, req.SignupID)
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	if fact.ID != req.SignupID || fact.CustomerID != req.ReferredCustomer || !fact.NewAccount || !fact.Individual || fact.CreatedAt.IsZero() || fact.CreatedAt.After(at) {
		return AttributionPreview{}, sources, ErrDenied
	}
	sources.Signup.ID = fact.ID
	sources.Signup.CustomerID = fact.CustomerID
	sources.Signup.Evidence = fact.AttributionEvidence
	sources.Signup.CreatedAt = fact.CreatedAt.UTC()
	sources.Signup.NewAccount = fact.NewAccount
	sources.Signup.Individual = fact.Individual
	sources.Referred, err = m.deps.Identity.GetPartnerPrincipal(ctx, req.ReferredCustomer)
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	// These are current target checks, not assertions about historical region or
	// email verification at creation. Historical individual status is the fact.
	if sources.Referred.ID != req.ReferredCustomer || !sources.Referred.Active || !sources.Referred.Individual {
		return AttributionPreview{}, sources, ErrDenied
	}
	sources.Partner, err = m.deps.Program.GetPartner(ctx, req.PartnerID)
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	p := sources.Partner
	if p.ID != req.PartnerID || p.ProgramID != partnerprogram.ProgramID || !validWorkText(p.CustomerID, 256) {
		return AttributionPreview{}, sources, ErrUnavailable
	}
	if p.CustomerID == req.ReferredCustomer {
		return AttributionPreview{}, sources, referral.ErrSelfReferral
	}
	if !p.CanAcquireReferrals {
		return AttributionPreview{}, sources, ErrDenied
	}
	sources.Owner, err = m.deps.Identity.GetPartnerPrincipal(ctx, p.CustomerID)
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	if sources.Owner.ID != p.CustomerID || !sources.Owner.Active || !sources.Owner.EmailVerified || !sources.Owner.Individual || !sources.Owner.RegionEligible {
		return AttributionPreview{}, sources, ErrDenied
	}
	if err := m.requireAcquisition(ctx, p.CustomerID); err != nil {
		return AttributionPreview{}, sources, err
	}
	state, err := m.deps.Referral.GetAttributionSnapshot(ctx, req.ReferredCustomer)
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	if state.ReferredCustomer != req.ReferredCustomer || state.Fingerprint == "" {
		return AttributionPreview{}, sources, ErrUnavailable
	}
	if state.Head.ID != "" && (state.Head.ReferredCustomer != req.ReferredCustomer || state.Head.ProgramID != referral.ProgramID) {
		return AttributionPreview{}, sources, ErrUnavailable
	}
	for _, row := range state.History {
		if row.ReferredCustomer != req.ReferredCustomer || row.ProgramID != referral.ProgramID {
			return AttributionPreview{}, sources, ErrUnavailable
		}
	}
	for _, binding := range state.Bindings {
		if binding.ReferredCustomer != req.ReferredCustomer || binding.ProgramID != referral.ProgramID {
			return AttributionPreview{}, sources, ErrUnavailable
		}
	}
	if state.Head.ID != "" && state.Head.SignupID != fact.ID {
		return AttributionPreview{}, sources, ErrDenied
	}
	groups, err := m.deps.Groups.PartnerGroupIDs(ctx, p.CustomerID)
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	terms, err := m.deps.Program.ResolveReferralTerms(ctx, p, groups, at, fact.CreatedAt)
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	cfg := m.deps.Program.Config()
	if terms.ProgramID != partnerprogram.ProgramID || terms.PartnerID != p.ID || terms.Currency != cfg.Currency || terms.CurrencyExponent != cfg.CurrencyExponent {
		return AttributionPreview{}, sources, ErrUnavailable
	}
	frozen := snapshot(terms)
	// A later owner/policy cannot extend an end condition already promised to
	// this referral, including when the new policy would otherwise be unlimited.
	for _, prior := range state.History {
		until := prior.TermsSnapshot.RecurrenceEndsAt
		if until != nil && (frozen.RecurrenceEndsAt == nil || until.Before(*frozen.RecurrenceEndsAt)) {
			v := until.UTC()
			frozen.RecurrenceEndsAt = &v
		}
	}
	out := AttributionPreview{Mode: req.Mode, ReferredCustomer: req.ReferredCustomer, PartnerID: req.PartnerID, SignupID: req.SignupID, Reason: req.Reason, Original: state.Head, ProposedTerms: frozen, History: state.History, UnchangedBindings: state.Bindings, SnapshotFingerprint: state.Fingerprint, AsOf: at}
	// AsOf and current actor are not economic content. Actor ownership is bound
	// separately in the durable receipt key and checked on every manager call.
	out.Fingerprint, err = attributionHash(struct {
		Change   AttributionChange
		Sources  attributionSources
		Snapshot string
		Terms    referral.TermsSnapshot
	}{req, sources, state.Fingerprint, frozen})
	if err != nil {
		return AttributionPreview{}, sources, err
	}
	return out, sources, nil
}

// ApplyAttribution recovers an exact original receipt before consulting today's
// source/policy or admission switch. Fresh requests recompute their preview and
// the referral owner checks its reviewed snapshot atomically with the append.
func (m *Manager) ApplyAttribution(ctx context.Context, req ApplyAttributionRequest) (referral.Referral, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityAttribution, req.ReferredCustomer); err != nil {
		return referral.Referral{}, err
	}
	if !validAttributionChange(req.AttributionChange) || !validWorkText(req.IdempotencyKey, 256) || req.PreviewFingerprint == "" || req.SnapshotFingerprint == "" || req.ExpectedRevision < 0 {
		return referral.Referral{}, ErrInvalid
	}
	old, err := m.deps.Referral.FindCorrection(ctx, req.ActorID, req.ReferredCustomer, req.IdempotencyKey)
	if err == nil {
		c := old.Correction
		if c == nil || old.CorrectionBy != req.ActorID || old.ProgramID != referral.ProgramID || old.ReferredCustomer != req.ReferredCustomer {
			return referral.Referral{}, ErrUnavailable
		}
		if old.PartnerID != req.PartnerID || old.CorrectionReason != req.Reason || c.SignupID != req.SignupID || c.Mode != req.Mode || c.ExpectedRevision != req.ExpectedRevision || c.ExpectedReferralID != req.ExpectedReferralID || c.PreviewFingerprint != req.PreviewFingerprint || c.SnapshotFingerprint != req.SnapshotFingerprint {
			return referral.Referral{}, referral.ErrStaleWrite
		}
		if err := m.authorize(ctx, req.ActorID, CapabilityAttribution, req.ReferredCustomer); err != nil {
			return referral.Referral{}, err
		}
		return old, nil
	}
	if !singleManagerAbsence(err, referral.ErrNotFound) {
		return referral.Referral{}, err
	}
	if !m.deps.Controls.Attribution {
		return referral.Referral{}, ErrDenied
	}
	preview, sources, err := m.previewAttribution(ctx, req.AttributionChange)
	if err != nil {
		return referral.Referral{}, err
	}
	if preview.Fingerprint != req.PreviewFingerprint || preview.SnapshotFingerprint != req.SnapshotFingerprint || preview.Original.ID != req.ExpectedReferralID || preview.Original.Revision != req.ExpectedRevision {
		return referral.Referral{}, referral.ErrStaleWrite
	}
	if err := m.authorize(ctx, req.ActorID, CapabilityAttribution, req.ReferredCustomer); err != nil {
		return referral.Referral{}, err
	}
	out, err := m.deps.Referral.AssignAttribution(ctx, referral.CorrectionRequest{Partner: referralState(sources.Partner), ReferredCustomer: req.ReferredCustomer, SignupID: req.SignupID, SignupCreatedAt: sources.Signup.CreatedAt, ActorID: req.ActorID, Reason: req.Reason, Mode: req.Mode, IdempotencyKey: req.IdempotencyKey, ExpectedRevision: req.ExpectedRevision, ExpectedReferralID: req.ExpectedReferralID, ExpectedSnapshotFingerprint: req.SnapshotFingerprint, PreviewFingerprint: req.PreviewFingerprint, Terms: preview.ProposedTerms})
	if err != nil {
		return referral.Referral{}, err
	}
	if out.ProgramID != referral.ProgramID || out.ReferredCustomer != req.ReferredCustomer || out.PartnerID != req.PartnerID || out.CorrectionBy != req.ActorID || out.Correction == nil || out.Correction.PreviewFingerprint != req.PreviewFingerprint {
		return referral.Referral{}, ErrUnavailable
	}
	if err := m.authorize(ctx, req.ActorID, CapabilityAttribution, req.ReferredCustomer); err != nil {
		return referral.Referral{}, err
	}
	return out, nil
}
