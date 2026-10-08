package partnermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/partnerprogram"
)

// AdminPartnerStatus reads one partner's current status and revision under
// Policy at that PartnerID. It does not grant policy authority at CustomerID.
// Hosts must project only status, revision, reason, timestamps and admission
// flags; the private owner record also contains unrelated identity/payout data.
func (m *Manager) AdminPartnerStatus(ctx context.Context, actor, partnerID string) (partnerprogram.Partner, error) {
	if !validWorkText(partnerID, 256) {
		return partnerprogram.Partner{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityPolicy, partnerID); err != nil {
		return partnerprogram.Partner{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partnerID)
	if err != nil {
		return partnerprogram.Partner{}, err
	}
	if !selectedPartnerValid(p, partnerID) {
		return partnerprogram.Partner{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilityPolicy, partnerID); err != nil {
		return partnerprogram.Partner{}, err
	}
	return p, nil
}

func selectedPartnerValid(p partnerprogram.Partner, id string) bool {
	return p.ID == id && p.ProgramID == partnerprogram.ProgramID && validWorkText(p.CustomerID, 256) && p.Revision > 0
}

// ClaimPreparationDestination is the current owning destination needed for
// explicit on-behalf withdrawal review. It is not proof of email ownership or
// an external transfer, and a subsequent edit requires a fresh version.
type ClaimPreparationDestination struct {
	ID, Method, Email string
	Version           int64
}

// ClaimPreparation is a transient selected-partner view for a new withdrawal.
// IdentityEligible reports current active/verified/individual admission without
// identity data. Pauses are data, not read failures. Only AvailableMinor is
// exposed from the financial aggregate; other reporting needs its own grant.
type ClaimPreparation struct {
	Currency          string
	CurrencyExponent  int
	MinimumMinor      int64
	ClaimsEnabled     bool
	CanRequestPayouts bool
	IdentityEligible  bool
	Destination       *ClaimPreparationDestination
	AvailableMinor    int64
}

// AdminClaimPreparation uses CreateClaimOnBehalf at exactly PartnerID, with
// current authorization before and after successful owner reads. Only sole
// canonical destination absence becomes nil; dependency errors remain errors.
// These separate reads are not an atomic admission snapshot. Every command
// checks its own current admission, destination and funds; original-key retry
// must remain available independently of this new-withdrawal preparation read.
func (m *Manager) AdminClaimPreparation(ctx context.Context, actor, partnerID string) (ClaimPreparation, error) {
	if !validWorkText(partnerID, 256) {
		return ClaimPreparation{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityCreateClaimOnBehalf, partnerID); err != nil {
		return ClaimPreparation{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partnerID)
	if err != nil {
		return ClaimPreparation{}, err
	}
	if !selectedPartnerValid(p, partnerID) {
		return ClaimPreparation{}, ErrUnavailable
	}
	principal, err := m.deps.Identity.GetPartnerPrincipal(ctx, p.CustomerID)
	if err != nil {
		return ClaimPreparation{}, err
	}
	if principal.ID != p.CustomerID {
		return ClaimPreparation{}, ErrUnavailable
	}
	destination, err := m.deps.Program.GetPayoutDestination(ctx, p.CustomerID)
	var selected *ClaimPreparationDestination
	if err != nil {
		if !singleManagerAbsence(err, partnerprogram.ErrNotFound) {
			return ClaimPreparation{}, err
		}
	} else {
		if destination.CustomerID != p.CustomerID || !validWorkText(destination.ID, 256) || destination.Version < 1 || destination.Method != partnerprogram.DestinationMethodPayPal || !validWorkText(destination.Email, 254) {
			return ClaimPreparation{}, ErrUnavailable
		}
		selected = &ClaimPreparationDestination{ID: destination.ID, Method: destination.Method, Email: destination.Email, Version: destination.Version}
	}
	balances, err := m.deps.Earnings.Balances(ctx, partnerID)
	if err != nil {
		return ClaimPreparation{}, err
	}
	cfg := m.deps.Program.Config()
	if balances.AvailableMinor < 0 || len(cfg.Currency) != 3 || cfg.Currency[0] < 'A' || cfg.Currency[0] > 'Z' || cfg.Currency[1] < 'A' || cfg.Currency[1] > 'Z' || cfg.Currency[2] < 'A' || cfg.Currency[2] > 'Z' || cfg.CurrencyExponent < 0 || cfg.CurrencyExponent > 3 || m.deps.Claims.MinimumMinor < 0 {
		return ClaimPreparation{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilityCreateClaimOnBehalf, partnerID); err != nil {
		return ClaimPreparation{}, err
	}
	return ClaimPreparation{Currency: cfg.Currency, CurrencyExponent: cfg.CurrencyExponent, MinimumMinor: m.deps.Claims.MinimumMinor, ClaimsEnabled: m.deps.Controls.Claims, CanRequestPayouts: p.CanRequestPayouts, IdentityEligible: principal.Active && principal.EmailVerified && principal.Individual, Destination: selected, AvailableMinor: balances.AvailableMinor}, nil
}

// IndividualPolicyHistory contains only the selected customer's individual
// stream. Revision is its maximum published revision, including future/expired
// versions, for publication CAS; it is not the currently effective policy.
// Hosts must explicitly project safe policy fields rather than owner audit IDs.
type IndividualPolicyHistory struct {
	Versions []partnerprogram.PolicyVersion
	Revision int64
}

// AdminIndividualPolicyVersions requires Policy at exactly CustomerID, not
// PartnerID or the empty program-list target. It filters complete owning
// history, copies mutable fields and rechecks current authority before return.
// Conclusive empty history has an empty slice and first-publication revision 0.
func (m *Manager) AdminIndividualPolicyVersions(ctx context.Context, actor, customerID string) (IndividualPolicyHistory, error) {
	if !validWorkText(customerID, 256) {
		return IndividualPolicyHistory{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityPolicy, customerID); err != nil {
		return IndividualPolicyHistory{}, err
	}
	rows, err := m.deps.Program.ListPolicyVersions(ctx)
	if err != nil {
		return IndividualPolicyHistory{}, err
	}
	out := IndividualPolicyHistory{Versions: []partnerprogram.PolicyVersion{}}
	for _, row := range rows {
		if row.ProgramID != partnerprogram.ProgramID {
			return IndividualPolicyHistory{}, ErrUnavailable
		}
		if row.Scope != "individual" || row.PartnerCustomer != customerID {
			continue
		}
		if !validWorkText(row.ID, 256) || row.Revision < 1 || row.GroupID != "" {
			return IndividualPolicyHistory{}, ErrUnavailable
		}
		if row.Revision > out.Revision {
			out.Revision = row.Revision
		}
		if row.EligiblePlanIDs != nil {
			row.EligiblePlanIDs = append([]string{}, row.EligiblePlanIDs...)
		}
		if row.EffectiveTo != nil {
			v := *row.EffectiveTo
			row.EffectiveTo = &v
		}
		if row.RecurringMonths != nil {
			v := *row.RecurringMonths
			row.RecurringMonths = &v
		}
		out.Versions = append(out.Versions, row)
	}
	if err := m.authorize(ctx, actor, CapabilityPolicy, customerID); err != nil {
		return IndividualPolicyHistory{}, err
	}
	return out, nil
}
