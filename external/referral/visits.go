package referral

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	VisitEligible  = "eligible"
	VisitDuplicate = "duplicate"
	VisitKnownBot  = "known_bot"
)

// VisitIdentity is authenticated by EvidenceSigner, not decoded from a browser
// observation. Only a link-scoped keyed digest is retained in analytics storage.
type VisitIdentity struct {
	LinkID    string    `json:"-"`
	Digest    string    `json:"-"`
	ExpiresAt time.Time `json:"-"`
}

// VisitReceipt freezes one eligible observation for a bounded signed nonce.
// It is internal analytics evidence, not a person or financial entitlement.
type VisitReceipt struct {
	LinkID, Digest, MeasuredClickID string
	ExpiresAt                       time.Time
}

// VisitRequest carries server-bound consent/classification and authenticated
// visit identity. Hosts apply consent/rate/bot policy before invoking it.
type VisitRequest struct {
	Code      string        `json:"-"`
	Identity  VisitIdentity `json:"-"`
	Consented bool          `json:"-"`
	KnownBot  bool          `json:"-"`
}

type VisitObservation struct {
	Click    Click
	Eligible Click // frozen first observation, including on duplicates
}

func analyticsID(id string, max int) bool {
	if id == "" || len(id) > max || strings.TrimSpace(id) != id || !utf8.ValidString(id) {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ObserveVisit records a consented request and atomically freezes the first
// eligible observation for its signed identity. Reloads/concurrent retries do
// not create new measured visits. Known bots never seed signup evidence. Raw
// nonces, IPs and user-agent strings are not stored by this measurement path.
// An optional analytics failure must be handled separately from navigation or
// signup/financial acceptance by the manager/host.
func (s *Service) ObserveVisit(ctx context.Context, req VisitRequest) (VisitObservation, error) {
	if err := s.ready(ctx); err != nil {
		return VisitObservation{}, err
	}
	if !req.Consented {
		return VisitObservation{}, nil
	}
	if s.analytics == nil {
		return VisitObservation{}, ErrUnavailable
	}
	link, err := s.repo.GetLinkByCode(ctx, req.Code)
	if err != nil {
		return VisitObservation{}, err
	}
	if link.RetiredAt != nil {
		return VisitObservation{}, ErrCodeRetired
	}
	if link.ProgramID != ProgramID || !analyticsID(link.ID, 128) || !analyticsID(link.Code, 128) || link.CreatedAt.IsZero() {
		return VisitObservation{}, ErrUnavailable
	}
	if req.KnownBot {
		id := s.ids.NewID()
		if !analyticsID(id, 252) {
			return VisitObservation{}, ErrInvalid
		}
		click := Click{ID: "clk_" + id, LinkID: link.ID, Code: link.Code, OccurredAt: s.clock.Now().UTC(), Classification: VisitKnownBot}
		if click.OccurredAt.IsZero() || click.OccurredAt.Before(link.CreatedAt) {
			return VisitObservation{}, ErrUnavailable
		}
		if err := s.repo.WithVisitTransaction(ctx, link.ID, func(tx Repository) error {
			if nilReferralDependency(tx) {
				return ErrUnavailable
			}
			return s.recordObservation(ctx, tx, click)
		}); err != nil {
			return VisitObservation{}, err
		}
		return VisitObservation{Click: click}, nil
	}
	identity := req.Identity
	now := s.clock.Now().UTC()
	if identity.LinkID != link.ID || !validCorrectionFingerprint(identity.Digest) || identity.ExpiresAt.Sub(now) > 24*time.Hour {
		return VisitObservation{}, ErrInvalid
	}
	if !identity.ExpiresAt.After(now) {
		return VisitObservation{}, ErrDenied
	}
	var out VisitObservation
	err = s.repo.WithVisitTransaction(ctx, link.ID, func(tx Repository) error {
		out = VisitObservation{}
		if nilReferralDependency(tx) {
			return ErrUnavailable
		}
		at := s.clock.Now().UTC()
		if at.IsZero() || at.Before(link.CreatedAt) {
			return ErrUnavailable
		}
		if !identity.ExpiresAt.After(at) {
			return ErrDenied
		}
		id := s.ids.NewID()
		if !analyticsID(id, 252) {
			return ErrInvalid
		}
		c := Click{ID: "clk_" + id, LinkID: link.ID, Code: link.Code, OccurredAt: at, VisitDigest: identity.Digest}
		receipt, err := tx.GetVisitReceipt(ctx, link.ID, identity.Digest)
		if singleReferralCause(err, ErrNotFound) {
			c.Classification, c.MeasuredClickID = VisitEligible, c.ID
			receipt = VisitReceipt{LinkID: link.ID, Digest: identity.Digest, MeasuredClickID: c.ID, ExpiresAt: identity.ExpiresAt}
			if err := tx.InsertVisitReceipt(ctx, receipt); err != nil {
				return err
			}
			out.Eligible = c
		} else {
			if err != nil {
				return err
			}
			if receipt.LinkID != link.ID || receipt.Digest != identity.Digest || !receipt.ExpiresAt.Equal(identity.ExpiresAt) || !analyticsID(receipt.MeasuredClickID, 256) {
				return ErrUnavailable
			}
			first, err := tx.GetClick(ctx, link.ID, receipt.MeasuredClickID)
			if err != nil {
				return err
			}
			if first.Classification != VisitEligible || first.ID != first.MeasuredClickID || first.LinkID != link.ID || first.Code != link.Code || first.VisitDigest != identity.Digest || first.OccurredAt.IsZero() || first.OccurredAt.After(at) {
				return ErrUnavailable
			}
			c.Classification, c.MeasuredClickID = VisitDuplicate, first.ID
			out.Eligible = first
		}
		if err := s.recordObservation(ctx, tx, c); err != nil {
			return err
		}
		out.Click = c
		return nil
	})
	if err != nil {
		return VisitObservation{}, err
	}
	return out, nil
}
