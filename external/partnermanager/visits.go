package partnermanager

import (
	"context"
	"errors"
	"time"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// PrepareVisitRequest is server-bound input after host consent/rate/bot policy.
// Prior tokens come from secure cookies; browser JSON supplies no admission or
// analytics identity. The host handles safe same-origin navigation separately.
type PrepareVisitRequest struct {
	Code          string `json:"-"`
	PriorEvidence string `json:"-"`
	PriorVisit    string `json:"-"`
	Consented     bool   `json:"-"`
	KnownBot      bool   `json:"-"`
}

// PreparedVisit separates the bounded measurement cookie from longer signup
// attribution. Hosts set HttpOnly/Secure/SameSite cookies to these absolute
// expiries, clear empty values, and never expose token payloads to browser JSON.
// Measurement is an observation result, not a source-completeness assertion.
type PreparedVisit struct {
	Evidence          string    `json:"-"`
	VisitCookie       string    `json:"-"`
	EvidenceExpiresAt time.Time `json:"-"`
	VisitExpiresAt    time.Time `json:"-"`
	Measurement       string    `json:"measurement"`
}

// PrepareVisit prepares measurement and attribution for a consented visit on a
// live acquiring link. Non-consent returns a non-error result; disabled
// attribution or ineligible/retired links deny. Known bots record a bot
// observation only; otherwise the prior visit cookie is verified or reissued,
// the visit is observed, and evidence tokens (measured or plain) with absolute
// expiries are returned. Measurement failures degrade to "unavailable" while
// cancellation aborts.
func (m *Manager) PrepareVisit(ctx context.Context, req PrepareVisitRequest) (PreparedVisit, error) {
	if ctx == nil {
		return PreparedVisit{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return PreparedVisit{}, err
	}
	if !req.Consented {
		return PreparedVisit{Measurement: "not_consented"}, nil
	}
	if !m.deps.Controls.Attribution {
		return PreparedVisit{}, ErrDenied
	}
	l, err := m.deps.Referral.GetLinkByCode(ctx, referral.NormalizeCode(req.Code))
	if err != nil {
		return PreparedVisit{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, l.PartnerID)
	if err != nil {
		return PreparedVisit{}, err
	}
	if p.ID != l.PartnerID || p.ProgramID != partnerprogram.ProgramID || l.ProgramID != referral.ProgramID || !p.CanAcquireReferrals || l.RetiredAt != nil {
		return PreparedVisit{}, ErrDenied
	}
	if err := m.requireAcquisition(ctx, p.CustomerID); err != nil {
		return PreparedVisit{}, err
	}
	out := PreparedVisit{}
	if !req.KnownBot {
		if old, err := m.deps.Evidence.VerifyCurrent(req.PriorEvidence); err == nil && old.Code == l.Code && old.LinkID == l.ID {
			out.Evidence, out.EvidenceExpiresAt = req.PriorEvidence, old.ExpiresAt
		}
	}
	if !m.deps.Evidence.VisitMeasurementEnabled() {
		out.Measurement = "disabled"
	} else if req.KnownBot {
		_, err := m.deps.Referral.ObserveVisit(ctx, referral.VisitRequest{Code: l.Code, Consented: true, KnownBot: true})
		if err != nil {
			if canceled := cancellationError(ctx, err); canceled != nil {
				return PreparedVisit{}, canceled
			}
			out.Measurement = "unavailable"
		} else {
			out.Measurement = "known_bot"
		}
	} else {
		visitToken := req.PriorVisit
		identity, err := m.deps.Evidence.VerifyVisit(visitToken, l)
		if err != nil {
			visitToken, identity, err = m.deps.Evidence.IssueVisit(l)
		}
		if err != nil {
			out.Measurement = "unavailable"
		} else {
			out.VisitCookie, out.VisitExpiresAt = visitToken, identity.ExpiresAt
			observed, err := m.deps.Referral.ObserveVisit(ctx, referral.VisitRequest{Code: l.Code, Identity: identity, Consented: true})
			if err != nil {
				if canceled := cancellationError(ctx, err); canceled != nil {
					return PreparedVisit{}, canceled
				}
				out.Measurement = "unavailable"
			} else {
				out.Measurement = observed.Click.Classification
				if out.Evidence == "" {
					token, evidence, err := m.deps.Evidence.IssueMeasured(l, observed.Eligible)
					if err != nil {
						out.Measurement = "unavailable"
					} else {
						out.Evidence, out.EvidenceExpiresAt = token, evidence.ExpiresAt
					}
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return PreparedVisit{}, err
	}
	if req.KnownBot {
		return out, nil
	}
	if out.Evidence == "" {
		token, evidence, err := m.deps.Evidence.Issue(l)
		if err != nil {
			return PreparedVisit{}, err
		}
		out.Evidence, out.EvidenceExpiresAt = token, evidence.ExpiresAt
	}
	if err := ctx.Err(); err != nil {
		return PreparedVisit{}, err
	}
	return out, nil
}

// cancellationError returns the context or wrapped cancellation/deadline error
// when the failure is cancellation-related, otherwise nil.
func cancellationError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
