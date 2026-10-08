package billing

import "context"

const (
	SubscriptionStatusCaptured   = "captured"
	SubscriptionStatusPending    = "pending"
	SubscriptionStatusSuperseded = "superseded"
)

// SubscriptionStatusOriginalSnapshot joins exact capture presence and, only
// when absent, the current head/receipt in one owning snapshot. Absence is
// conclusive only on a successful read; errors never grant supersession.
type SubscriptionStatusOriginalSnapshot struct {
	CaptureFound bool                       `json:"-"`
	Capture      SubscriptionStatus         `json:"-"`
	CurrentFound bool                       `json:"-"`
	Current      SubscriptionStatusSnapshot `json:"-"`
}

// SubscriptionStatusResolutionRepository is an optional private read capability.
// Existing status repositories remain compatible. It creates no capture/head
// and must not mix separately sampled absence and current head observations.
type SubscriptionStatusResolutionRepository interface {
	ReadSubscriptionStatusOriginal(context.Context, RevenueScope, string, string) (SubscriptionStatusOriginalSnapshot, error)
}

// SubscriptionStatusResolution describes the fate of one retained original.
// Superseded requires conclusive receipt absence and a strictly newer valid
// native head in the same snapshot. It is not current permission or a host
// execution grant; hosts must fence their own pointer changes independently.
type SubscriptionStatusResolution struct {
	Preparation SubscriptionStatusPreparation `json:"-"`
	State       string                        `json:"-"`
	Receipt     *SubscriptionStatus           `json:"-"`
	Current     *SubscriptionStatus           `json:"-"`
}

func sameStatusOriginal(a, b SubscriptionStatusPreparation) bool {
	at, bt := a.RequestedAt, b.RequestedAt
	a.RequestedAt, b.RequestedAt = at.UTC(), bt.UTC()
	return a == b
}
func sameStatusOwner(a, b SubscriptionStatusPreparation) bool {
	return a.Scope == b.Scope && a.SubscriptionID == b.SubscriptionID && a.PrincipalID == b.PrincipalID && a.ProviderCustomerID == b.ProviderCustomerID
}

// Validate checks typed consistency and monotonic head conditions only. Native
// owning provenance and one-snapshot absence must be established by the service.
func (r SubscriptionStatusResolution) Validate() error {
	p := r.Preparation
	if p.Validate() != nil {
		return ErrRevenueInvalid
	}
	switch r.State {
	case SubscriptionStatusCaptured:
		if r.Current != nil || r.Receipt == nil || r.Receipt.Validate() != nil || !sameStatusOriginal(r.Receipt.Preparation, p) {
			return ErrRevenueConflict
		}
	case SubscriptionStatusPending, SubscriptionStatusSuperseded:
		if r.Receipt != nil {
			return ErrRevenueConflict
		}
		if r.Current == nil {
			if r.State != SubscriptionStatusPending || p.ExpectedRevision != 0 {
				return ErrRevenueConflict
			}
			return nil
		}
		v := *r.Current
		if v.Validate() != nil || !sameStatusOwner(v.Preparation, p) {
			return ErrRevenueConflict
		}
		if r.State == SubscriptionStatusPending {
			if v.Revision != p.ExpectedRevision || v.Fingerprint != p.ExpectedFingerprint {
				return ErrRevenueConflict
			}
		} else if v.Revision <= p.ExpectedRevision {
			return ErrRevenueConflict
		}
	default:
		return ErrRevenueInvalid
	}
	return nil
}

// ResolveSubscriptionStatus reads native original recovery without provider I/O
// or writes. Exact receipts take precedence over later heads. An uncaptured
// original can be superseded only by a strictly newer authenticated head; native
// revision CAS then prevents that old original ever creating a new capture.
func (s *RevenueService) ResolveSubscriptionStatus(ctx context.Context, p SubscriptionStatusPreparation) (SubscriptionStatusResolution, error) {
	if err := s.ValidateSubscriptionStatusPreparation(ctx, p); err != nil {
		return SubscriptionStatusResolution{}, err
	}
	repo, ok := s.repo.(SubscriptionStatusResolutionRepository)
	if !ok || revenueNil(repo) {
		return SubscriptionStatusResolution{}, ErrRevenueUnavailable
	}
	snapshot, err := repo.ReadSubscriptionStatusOriginal(ctx, p.Scope, p.SubscriptionID, p.CaptureID)
	if err != nil {
		return SubscriptionStatusResolution{}, err
	}
	if err = ctx.Err(); err != nil {
		return SubscriptionStatusResolution{}, err
	}
	out := SubscriptionStatusResolution{Preparation: p}
	if snapshot.CaptureFound {
		if snapshot.CurrentFound || snapshot.Current != (SubscriptionStatusSnapshot{}) {
			return SubscriptionStatusResolution{}, ErrRevenueUnavailable
		}
		out.State = SubscriptionStatusCaptured
		receipt := snapshot.Capture
		out.Receipt = &receipt
	} else {
		if snapshot.Capture != (SubscriptionStatus{}) {
			return SubscriptionStatusResolution{}, ErrRevenueUnavailable
		}
		out.State = SubscriptionStatusPending
		if snapshot.CurrentFound {
			if err = s.verifyStatus(ctx, snapshot.Current); err != nil {
				return SubscriptionStatusResolution{}, err
			}
			current := snapshot.Current.Current
			out.Current = &current
			if current.Revision > p.ExpectedRevision {
				out.State = SubscriptionStatusSuperseded
			}
		} else if snapshot.Current != (SubscriptionStatusSnapshot{}) || p.ExpectedRevision != 0 {
			return SubscriptionStatusResolution{}, ErrRevenueUnavailable
		}
	}
	if err = out.Validate(); err != nil {
		return SubscriptionStatusResolution{}, err
	}
	if err = ctx.Err(); err != nil {
		return SubscriptionStatusResolution{}, err
	}
	return out, nil
}
