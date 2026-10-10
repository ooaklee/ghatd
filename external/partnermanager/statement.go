package partnermanager

import (
	"context"
	"github.com/ooaklee/ghatd/external/partnerearnings"
)

// Statement returns the verified actor's own earnings statement, validating
// partner and currency before and rechecking self authority after the owning
// read.
func (m *Manager) Statement(ctx context.Context, actor string, query partnerearnings.StatementQuery) (partnerearnings.Statement, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return partnerearnings.Statement{}, err
	}
	result, err := m.deps.Earnings.GetStatement(ctx, p.ID, query)
	if err != nil {
		return partnerearnings.Statement{}, err
	}
	if result.PartnerID != p.ID || result.Currency != m.deps.Program.Config().Currency {
		return partnerearnings.Statement{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return partnerearnings.Statement{}, err
	}
	return result, nil
}

// AdminStatement requires reporting authority for the selected partner before
// and after the read, verifies the partner record, and validates statement
// partner/currency identity; a mismatch fails with ErrUnavailable.
func (m *Manager) AdminStatement(ctx context.Context, actor, partner string, query partnerearnings.StatementQuery) (partnerearnings.Statement, error) {
	if !validWorkText(partner, 256) {
		return partnerearnings.Statement{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return partnerearnings.Statement{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return partnerearnings.Statement{}, err
	}
	if p.ID != partner {
		return partnerearnings.Statement{}, ErrUnavailable
	}
	result, err := m.deps.Earnings.GetStatement(ctx, p.ID, query)
	if err != nil {
		return partnerearnings.Statement{}, err
	}
	if result.PartnerID != p.ID || result.Currency != m.deps.Program.Config().Currency {
		return partnerearnings.Statement{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return partnerearnings.Statement{}, err
	}
	return result, nil
}
