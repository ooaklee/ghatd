package apitoken

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Inventory counts every stored credential for an owner, including expired and
// revoked credentials. Inventory is not authorization or active usage. Deletion
// explicitly frees a slot; merely fetching a list never does.
type Inventory struct {
	// Permanent counts records with no expiry (including legacy empty/null fields).
	Permanent int64
	// Ephemeral counts all remaining records, even malformed expiry metadata.
	Ephemeral int64
}

// CountTokenInventoryFenced acquires the repository's owner-wide write fence
// before exact counts. The caller must insert in that same transaction/client
// and must not publish the secret until commit. This composes with a system
// grant fence: each system compares total owner inventory to its own current
// allowance, not to the minimum of every system's allowances. Missing support
// or preparation fails closed; an unfenced count is never substituted.
func (s *Service) CountTokenInventoryFenced(ctx context.Context, ownerID string) (Inventory, error) {
	if err := s.checkContext(ctx); err != nil {
		return Inventory{}, err
	}
	if ownerID == "" {
		return Inventory{}, ErrRequiredUserIDMissing
	}
	fencer, ok := s.ApitokenRespository.(interface {
		FenceInventory(context.Context, string) error
	})
	if !ok {
		return Inventory{}, ErrInventoryUnavailable
	}
	if err := fencer.FenceInventory(ctx, ownerID); err != nil {
		return Inventory{}, err
	}
	return s.CountTokenInventory(ctx, ownerID)
}

// CountTokenInventory performs exact, unpaginated counts using the caller's
// context. For admission it must run under the same transaction/client and
// owner fence as the following insert; alone, these reads are not atomic.
func (s *Service) CountTokenInventory(ctx context.Context, ownerID string) (Inventory, error) {
	if err := s.checkContext(ctx); err != nil {
		return Inventory{}, err
	}
	if ownerID == "" {
		return Inventory{}, ErrRequiredUserIDMissing
	}
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	permanent, err := s.ApitokenRespository.GetTotalApiTokens(ctx, ownerID, "", "", "", "", "", false, true)
	if err != nil {
		return Inventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	if permanent < 0 {
		return Inventory{}, ErrInventoryUnavailable
	}
	ephemeral, err := s.ApitokenRespository.GetTotalApiTokens(ctx, ownerID, "", "", "", "", "", true, false)
	if err != nil {
		return Inventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	if ephemeral < 0 {
		return Inventory{}, ErrInventoryUnavailable
	}
	return Inventory{Permanent: permanent, Ephemeral: ephemeral}, nil
}

// validCredentialPart applies one bounded grammar to issued prefixes and parsed
// credential fragments. It rejects ambiguous separators and invisible characters.
func validCredentialPart(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsAny(value, ".,") &&
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) < 0
}
