package partnermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/partnerearnings"
)

// MaturityFeed is the earnings owner's verified private source capability. A
// worker obtains it from the same Earnings dependency used for MaturePartner;
// a separate feed could acknowledge a different ledger. Config binds one
// program/currency, and missing source evidence is an error, never caught-up proof.
type MaturityFeed interface {
	Config() partnerearnings.Config
	GetMaturitySource(context.Context, string) (partnerearnings.MaturitySource, error)
	PendingMaturitySourcesAfter(context.Context, string, int) ([]partnerearnings.MaturitySource, error)
}

func maturityCandidate(source partnerearnings.MaturitySource, program, currency string) (WorkCandidate, error) {
	if source.Validate() != nil || source.ProgramID != program || source.Currency != currency {
		return WorkCandidate{}, ErrUnavailable
	}
	return WorkCandidate{SourceID: source.ID, SourceFingerprint: source.Fingerprint, DueAt: source.AvailableAt.UTC()}, nil
}

func (w *Worker) readMaturity(ctx context.Context, item WorkItem) (partnerearnings.MaturitySource, error) {
	if item.Kind != WorkMaturity || !w.queue.validateLease(item) {
		return partnerearnings.MaturitySource{}, ErrWorkConflict
	}
	if err := w.authorize(ctx, WorkMaturity, ""); err != nil {
		return partnerearnings.MaturitySource{}, err
	}
	source, err := w.maturity.GetMaturitySource(ctx, item.SourceID)
	if err != nil {
		return partnerearnings.MaturitySource{}, err
	}
	candidate, err := maturityCandidate(source, item.ProgramID, w.maturity.Config().Currency)
	if err != nil || candidate.SourceID != item.SourceID || candidate.SourceFingerprint != item.SourceFingerprint || !candidate.DueAt.Equal(item.InitialDueAt) {
		return partnerearnings.MaturitySource{}, ErrWorkConflict
	}
	if err := w.authorize(ctx, WorkMaturity, source.PartnerID); err != nil {
		return partnerearnings.MaturitySource{}, err
	}
	return source, nil
}

// processMaturity returns a retry target only after the financial owner has
// verified immutable source evidence and current partner authority. A lost
// financial/decision/completion acknowledgement is recovered from the original
// retained receipt. Stored decision authorship never substitutes for authority.
func (w *Worker) processMaturity(ctx context.Context, item WorkItem) (string, error) {
	source, err := w.readMaturity(ctx, item)
	if err != nil {
		return "", err
	}
	partner := source.PartnerID
	// Lease eligibility and queue decision times share this scheduling clock.
	// The earnings owner independently determines what it can mature; its
	// subsequently verified receipt, not this clock, establishes acceptance.
	at := w.queue.clock.Now().UTC()
	if at.IsZero() || item.InitialDueAt.After(at) || item.CreatedAt.After(at) {
		return partner, partnerearnings.ErrUnresolved
	}
	var decision WorkDecision
	if item.Decision != nil {
		decision = *item.Decision
		want, digestErr := workDecisionDigest(item, decision.ActorID, decision.Outcome, decision.AcceptanceID, decision.ReasonCode)
		if digestErr != nil || decision.Fingerprint != want || decision.ID != "decision_"+want || !validWorkText(decision.ActorID, 256) || decision.RecordedAt.Before(item.CreatedAt) || decision.RecordedAt.Before(source.MaturedAt) || decision.RecordedAt.After(at) || source.State != partnerearnings.MaturityCompleted || decision.Outcome != WorkAccepted || decision.ReasonCode != "financial_matured" || decision.AcceptanceID != source.MaturedEntryID {
			return partner, ErrWorkConflict
		}
	} else {
		if source.State == partnerearnings.MaturityPending {
			if _, err := w.manager.MaturePartner(ctx, w.config.ActorID, partner); err != nil {
				return partner, err
			}
			// Another worker may have matured this allocation already. The
			// mutation result can be empty; only owning snapshot proof counts.
			if err := w.authorize(ctx, WorkMaturity, partner); err != nil {
				return partner, err
			}
			accepted, err := w.readMaturity(ctx, item)
			if err != nil {
				return partner, err
			}
			if accepted.PartnerID != partner {
				return partner, ErrWorkConflict
			}
			source = accepted
		}
		if source.State != partnerearnings.MaturityCompleted {
			return partner, partnerearnings.ErrUnresolved
		}
		if source.MaturedAt.After(w.queue.clock.Now().UTC()) {
			// A skewed scheduling clock must catch up to owning receipt time;
			// it cannot record financial acceptance before that receipt exists.
			return partner, partnerearnings.ErrUnresolved
		}
		if err := w.authorize(ctx, WorkMaturity, partner); err != nil {
			return partner, err
		}
		decision, err = w.queue.decide(ctx, item, w.config.ActorID, WorkAccepted, source.MaturedEntryID, "financial_matured", source.MaturedAt)
		if err != nil {
			return partner, err
		}
	}
	if err := w.authorize(ctx, WorkMaturity, partner); err != nil {
		return partner, err
	}
	return partner, w.queue.Complete(ctx, item, decision.ID)
}

var _ MaturityFeed = (*partnerearnings.Service)(nil)
