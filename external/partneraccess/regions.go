package partneraccess

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partnermanager"
)

const (
	// ReviewedRegionScope records independently administered participation
	// admission after the host's approved region review. It is not country data.
	ReviewedRegionScope = "partners.region.reviewed"
	// RegionPermission is distinct from cohort, customer and operator authority.
	RegionPermission = "partner.region.eligible"
)

// Regions enforces explicit current regional participation admission. The host
// has no trusted country field to infer it from, so a privileged reviewer must
// grant the exact selected customer's scope AND permission through the existing
// audited access-policy management boundary after reviewing the approved rules.
// Editable profile extensions, group membership, phone, locale and GeoIP cannot
// confer admission. This read creates no approval or commercial launch decision.
type Regions struct {
	system string
	policy PolicyService
}

// NewRegions validates the system identifier and borrowed policy port,
// returning ErrUnavailable on invalid input.
func NewRegions(system string, policy PolicyService) (*Regions, error) {
	if !validID(system) || nilPort(policy) {
		return nil, partnermanager.ErrUnavailable
	}
	return &Regions{system: system, policy: policy}, nil
}

// IsPartnerRegionEligible supplies the shared identity adapter's host admission
// port for an already verified owning account ID. Known denial means false;
// unknown/joined/alias failures remain unavailable, never fabricated ineligibility
// or success. Every read uses current grants; no prior approval is cached. This
// is administrative admission, not automated country verification or consent.
func (r *Regions) IsPartnerRegionEligible(ctx context.Context, customerID string) (bool, error) {
	if ctx == nil || !validID(customerID) {
		return false, partnermanager.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r == nil || nilPort(r.policy) {
		return false, partnermanager.ErrUnavailable
	}
	subject := accesspolicy.Subject{System: r.system, Kind: accesspolicy.UserSubject, ID: customerID}
	err := r.policy.Authorize(ctx, subject, []string{ReviewedRegionScope}, []string{RegionPermission})
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if soleDenial(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %w", partnermanager.ErrUnavailable, err)
	}
	return true, nil
}

var _ partnermanager.RegionEligibility = (*Regions)(nil)
