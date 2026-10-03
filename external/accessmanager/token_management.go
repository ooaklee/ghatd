package accessmanager

import (
	"context"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// tokenManagementSession requires trusted, explicitly authenticated session
// metadata and independent caller/owner agreement. Publishing context is not
// token verification: middleware or an in-process adapter must first verify the
// credential and live session. Delegated API credentials never gain account-wide
// credential management, even when their owner is an administrator.
func tokenManagementSession(ctx context.Context, actor, owner string) error {
	if ctx == nil {
		return ErrTokenPolicyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !emailActorMatches(ctx, actor) {
		return ErrUnauthorizedUnableToAttainRequestorID
	}
	session := accesshelpers.AcquireSessionFrom(ctx)
	if session == nil || strings.TrimSpace(session.AccessUUID) == "" {
		return ErrUnauthorizedUnableToAttainRequestorID
	}
	if actor != owner {
		return ErrForbiddenUnableToAction
	}
	return nil
}

// tokenManagementOwner rechecks the live account after session admission. The
// returned value is an independent snapshot; no cached role or issuance-time
// status authorizes a mutation. Policy issuance calls this inside its transaction
// on every retry. Other operations are point-in-time reads, not account locks.
func (s *Service) tokenManagementOwner(ctx context.Context, actor string) (*user.UniversalUser, error) {
	if err := tokenManagementSession(ctx, actor, actor); err != nil {
		return nil, err
	}
	if s == nil || nilAccessDependency(s.UserService) {
		return nil, ErrTokenPolicyUnavailable
	}
	result, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: actor})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result == nil || result.User == nil || result.User.ID != actor || result.User.Status != user.AccountStatusKeyActive {
		return nil, ErrForbiddenUnableToAction
	}
	session := accesshelpers.AcquireSessionFrom(ctx)
	if session == nil || result.User.EmailRevision < 0 || result.User.EmailRevision != session.EmailRevision || !auth.MatchesUserType(session.UserType, result.User) {
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}
	value := *result.User
	value.Roles = append([]string(nil), value.Roles...)
	return &value, nil
}

// tokenListResponse rejects mismatched ownership and malformed pagination before
// projecting copied rows without secrets or digests. A custom adapter must obey
// the same ownership contract as the built-in token domain; empty lists are valid.
func tokenListResponse(result *apitoken.GetAPITokensForResponse, owner string) (*GetSpecificUserAPITokensResponse, error) {
	if result == nil || result.Total < 0 || result.TotalPages < 0 || result.Page < 0 || result.APITokensPerPage < 0 {
		return nil, apitoken.ErrServiceUnavailable
	}
	rows := make([]apitoken.UserAPIToken, len(result.APITokens))
	for i, value := range result.APITokens {
		if strings.TrimSpace(value.ID) == "" || value.CreatedByID != owner {
			return nil, apitoken.ErrServiceUnavailable
		}
		value.Value, value.ValueSHA = "", nil
		rows[i] = value
	}
	return &GetSpecificUserAPITokensResponse{UserAPITokens: rows, Total: result.Total, TotalPages: result.TotalPages, Page: result.Page, ResourcesPerPage: result.APITokensPerPage}, nil
}

// validTokenThresholdResponse applies the same consistency rules to custom
// HTTP service adapters as the live policy port; zero inventory means disabled.
func validTokenThresholdResponse(value *GetUserAPITokenThresholdResponse) bool {
	return value != nil && accesspolicy.ValidateTokenLimits(accesspolicy.TokenLimits{Permanent: value.PermanentUserTokenLimit, Ephemeral: value.EphemeralUserTokenLimit, MinimumTTL: value.EphemeralMinimumAllowedTime, MaximumTTL: value.EphemeralMaximumAllowedTime, TTLIncrement: value.EphemeralMinimumIncrements}) == nil
}
