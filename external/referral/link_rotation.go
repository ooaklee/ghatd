package referral

import (
	"context"
	"reflect"
	"time"
)

// RotateLinkRequest selects the original link and preserves the caller's
// immutable retry intent. The manager binds Partner and ActorID from verified
// owning services; transports supply only the code, reason and request key.
type RotateLinkRequest struct {
	Partner          PartnerState
	ActorID          string `json:"-"`
	ExpectedLinkCode string
	Reason           string
	IdempotencyKey   string
}

// LinkRotationReceipt is private immutable recovery evidence. It preserves the
// originally issued result even after a later rotation retires that result.
// Customer transports must project Link only, never this receipt or request.
type LinkRotationReceipt struct {
	ID                 string
	Request            RotateLinkRequest
	RequestFingerprint string
	Link               Link
}

// LinkRotationTransaction binds link retirement, issuance and receipt storage
// to one partner guard. The callback may retry and performs no external effects.
type LinkRotationTransaction interface {
	GetLinkByCode(context.Context, string) (Link, error)
	RetireLink(context.Context, string, time.Time, string, string) error
	InsertLink(context.Context, Link) error
	GetLinkRotation(context.Context, string, string, string) (LinkRotationReceipt, error)
	InsertLinkRotation(context.Context, LinkRotationReceipt) error
}

// LinkRotationRepository is an optional atomic capability. An adapter without
// it cannot safely rotate; there is no separate retire/issue fallback.
type LinkRotationRepository interface {
	WithLinkTransaction(context.Context, string, string, func(LinkRotationTransaction) error) error
}

func normalizedRotation(req RotateLinkRequest) RotateLinkRequest {
	req.Partner.CanAcquireReferrals = false
	return req
}

func rotationFingerprint(req RotateLinkRequest) (string, error) {
	// ActorID is intentionally excluded from transport JSON, so encode it
	// explicitly in private request identity rather than relying on that tag.
	return correctionDigest(struct {
		Program, Actor string
		Request        RotateLinkRequest
	}{ProgramID, req.ActorID, normalizedRotation(req)})
}

// LinkRotationID identifies one actor/key within its owning partner partition.
// Different payloads under that identity are rejected rather than rotating twice.
func LinkRotationID(partner, actor, key string) string {
	fp, _ := correctionDigest([]string{ProgramID, partner, actor, key})
	return "lnk_rotation_" + fp
}

func validRotationRequest(req RotateLinkRequest) bool {
	return correctionText(req.Partner.PartnerID, 256) && correctionText(req.Partner.CustomerID, 256) &&
		correctionText(req.ActorID, 256) && correctionText(req.ExpectedLinkCode, 128) &&
		correctionText(req.Reason, 1000) && correctionText(req.IdempotencyKey, 256)
}

// Validate checks the private receipt's identity, original intent and frozen
// result. Adapters must also verify its canonical storage kind/partition/revision.
func (v LinkRotationReceipt) Validate() error {
	if !validRotationRequest(v.Request) || v.Request.Partner.CanAcquireReferrals ||
		v.ID != LinkRotationID(v.Request.Partner.PartnerID, v.Request.ActorID, v.Request.IdempotencyKey) ||
		!validCorrectionFingerprint(v.RequestFingerprint) {
		return ErrUnavailable
	}
	fp, err := rotationFingerprint(v.Request)
	l := v.Link
	if err != nil || fp != v.RequestFingerprint || l.ID != v.ID+"_result" ||
		l.ProgramID != ProgramID || l.PartnerID != v.Request.Partner.PartnerID ||
		!correctionText(l.Code, 128) || l.Code == v.Request.ExpectedLinkCode || l.CreatedAt.IsZero() ||
		l.RetiredAt != nil || l.RetireReason != "" || l.RetiredBy != "" {
		return ErrUnavailable
	}
	return nil
}

func recoveredRotation(ctx context.Context, tx LinkRotationTransaction, req RotateLinkRequest, fp string) (Link, error) {
	v, err := tx.GetLinkRotation(ctx, req.Partner.PartnerID, req.ActorID, req.IdempotencyKey)
	if err != nil {
		return Link{}, err
	}
	if v.Validate() != nil || v.Request.Partner.PartnerID != req.Partner.PartnerID ||
		v.Request.ActorID != req.ActorID || v.Request.IdempotencyKey != req.IdempotencyKey {
		return Link{}, ErrUnavailable
	}
	if v.RequestFingerprint != fp {
		return Link{}, ErrStaleWrite
	}
	// The immutable result remains the original result. Verify current storage
	// still retains both issued links, allowing later retirement of the result.
	current, err := tx.GetLinkByCode(ctx, v.Link.Code)
	if err != nil {
		if singleReferralCause(err, ErrNotFound) {
			return Link{}, ErrUnavailable
		}
		return Link{}, err
	}
	current.RetiredAt, current.RetireReason, current.RetiredBy = nil, "", ""
	if !reflect.DeepEqual(current, v.Link) {
		return Link{}, ErrUnavailable
	}
	original, err := tx.GetLinkByCode(ctx, req.ExpectedLinkCode)
	if err != nil {
		if singleReferralCause(err, ErrNotFound) {
			return Link{}, ErrUnavailable
		}
		return Link{}, err
	}
	if original.ProgramID != ProgramID || original.PartnerID != req.Partner.PartnerID ||
		original.RetiredAt == nil || !original.RetiredAt.Equal(v.Link.CreatedAt) ||
		original.RetiredBy != req.ActorID || original.RetireReason != req.Reason {
		return Link{}, ErrUnavailable
	}
	return v.Link, nil
}

// RotateLink atomically replaces the selected original link and records its
// actor/key receipt. Replay precedes current acquisition/head checks; callers
// must still check current identity and scoped permission before every call.
func (s *Service) RotateLink(ctx context.Context, req RotateLinkRequest) (Link, error) {
	if err := s.ready(ctx); err != nil {
		return Link{}, err
	}
	if !validRotationRequest(req) {
		return Link{}, ErrInvalid
	}
	repo, ok := s.repo.(LinkRotationRepository)
	if !ok || nilReferralDependency(repo) {
		return Link{}, ErrUnavailable
	}
	fp, err := rotationFingerprint(req)
	if err != nil {
		return Link{}, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		candidate := Link{ID: LinkRotationID(req.Partner.PartnerID, req.ActorID, req.IdempotencyKey) + "_result", ProgramID: ProgramID, PartnerID: req.Partner.PartnerID, Code: s.newCode(), CreatedAt: s.clock.Now().UTC()}
		var out Link
		err = repo.WithLinkTransaction(ctx, ProgramID, req.Partner.PartnerID, func(tx LinkRotationTransaction) error {
			out = Link{}
			if nilReferralDependency(tx) {
				return ErrUnavailable
			}
			prior, err := recoveredRotation(ctx, tx, req, fp)
			if err == nil {
				out = prior
				return nil
			}
			if !singleReferralCause(err, ErrNotFound) {
				return err
			}
			if !req.Partner.CanAcquireReferrals {
				return ErrDenied
			}
			original, err := tx.GetLinkByCode(ctx, req.ExpectedLinkCode)
			if err != nil {
				return err
			}
			if original.ProgramID != ProgramID || original.PartnerID != req.Partner.PartnerID || original.RetiredAt != nil {
				return ErrStaleWrite
			}
			if candidate.CreatedAt.IsZero() || original.CreatedAt.IsZero() || candidate.CreatedAt.Before(original.CreatedAt) {
				return ErrInvalid
			}
			if err := tx.RetireLink(ctx, original.ID, candidate.CreatedAt, req.Reason, req.ActorID); err != nil {
				return err
			}
			if err := tx.InsertLink(ctx, candidate); err != nil {
				return err
			}
			receipt := LinkRotationReceipt{ID: LinkRotationID(req.Partner.PartnerID, req.ActorID, req.IdempotencyKey), Request: normalizedRotation(req), RequestFingerprint: fp, Link: candidate}
			if err := tx.InsertLinkRotation(ctx, receipt); err != nil {
				return err
			}
			out = candidate
			return nil
		})
		if singleReferralCause(err, ErrCodeTaken) {
			continue
		}
		if err != nil {
			return Link{}, err
		}
		return out, nil
	}
	return Link{}, ErrCodeTaken
}
