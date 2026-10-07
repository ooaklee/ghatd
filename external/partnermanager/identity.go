package partnermanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/user/v2"
)

// UserIdentityService is the identity owner's service capability. The adapter
// does not query collections, match email addresses or read browser bodies.
type UserIdentityService interface {
	GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error)
	GetSignupAttribution(context.Context, string) (user.SignupAttribution, error)
}

// RegionEligibility resolves the host's approved participation rules from
// verified owning account information. It must not infer consent from GeoIP.
type RegionEligibility interface {
	IsPartnerRegionEligible(context.Context, string) (bool, error)
}

type UserIdentityConfig struct {
	ProgramID              string
	IndividualAccountTypes []string
	ActiveAccountStatuses  []string
}

// UserIdentityAdapter projects current eligibility and immutable creation facts
// from user/v2. A missing historical capture never becomes a new signup fact.
// Within a program, SignupFact.ID is the owning new-account CustomerID: there is
// one atomic creation capture per account, separate from any login or seat event.
type UserIdentityAdapter struct {
	owner  UserIdentityService
	region RegionEligibility
	config UserIdentityConfig
}

func NewUserIdentityAdapter(owner UserIdentityService, region RegionEligibility, config UserIdentityConfig) (*UserIdentityAdapter, error) {
	if nilManagerDependency(owner) || nilManagerDependency(region) {
		return nil, ErrUnavailable
	}
	if !identityValue(config.ProgramID, 128) || !identityChoices(config.IndividualAccountTypes) || !identityChoices(config.ActiveAccountStatuses) {
		return nil, ErrInvalid
	}
	config.IndividualAccountTypes = append([]string(nil), config.IndividualAccountTypes...)
	config.ActiveAccountStatuses = append([]string(nil), config.ActiveAccountStatuses...)
	return &UserIdentityAdapter{owner: owner, region: region, config: config}, nil
}
func identityValue(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= limit
}
func identityChoices(values []string) bool {
	if len(values) < 1 || len(values) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !identityValue(value, 128) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}
func identityContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func identityContext(ctx context.Context, id string) error {
	if ctx == nil || !identityValue(id, 256) {
		return ErrInvalid
	}
	return ctx.Err()
}
func (a *UserIdentityAdapter) GetPartnerPrincipal(ctx context.Context, id string) (Principal, error) {
	if err := identityContext(ctx, id); err != nil {
		return Principal{}, err
	}
	response, err := a.owner.GetUserByID(ctx, &user.GetUserByIDRequest{ID: id})
	if err != nil {
		return Principal{}, err
	}
	if response == nil || response.User == nil || response.User.ID != id {
		return Principal{}, ErrUnavailable
	}
	account := response.User
	principal := Principal{ID: id, Active: identityContains(a.config.ActiveAccountStatuses, account.Status), Individual: identityContains(a.config.IndividualAccountTypes, account.Type), EmailVerified: account.Verification != nil && account.Verification.EmailVerified}
	if !principal.Active || !principal.Individual || !principal.EmailVerified {
		return principal, nil
	}
	eligible, err := a.region.IsPartnerRegionEligible(ctx, id)
	if err != nil {
		return Principal{}, err
	}
	principal.RegionEligible = eligible
	return principal, nil
}
func (a *UserIdentityAdapter) GetSignupFact(ctx context.Context, id string) (SignupFact, error) {
	if err := identityContext(ctx, id); err != nil {
		return SignupFact{}, err
	}
	capture, err := a.owner.GetSignupAttribution(ctx, id)
	if err != nil {
		return SignupFact{}, err
	}
	if capture.CustomerID != id || capture.ProgramID != a.config.ProgramID || capture.CreatedAt.IsZero() || len(capture.Evidence) > 2048 {
		return SignupFact{}, ErrUnavailable
	}
	// No current user-profile field may change account type or creation evidence
	// after this capture. An empty token remains empty, never invented attribution.
	return SignupFact{ID: id, CustomerID: id, CreatedAt: capture.CreatedAt, NewAccount: true, Individual: capture.Individual, AttributionEvidence: capture.Evidence}, nil
}

var _ Identity = (*UserIdentityAdapter)(nil)
