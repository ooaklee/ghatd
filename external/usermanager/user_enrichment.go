package usermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// loadUsersForEnrichment delegates reference resolution to the owning user
// domain. UMS chooses a best-effort fallback only for optional decoration, never
// authentication/authorization. Each consumer still owns its privacy projection
// and validates persisted references before selector normalization. Lookup
// diagnostics remain private; this layer logs only a fixed fallback reason.
func (s *Service) loadUsersForEnrichment(ctx context.Context, userIDs []string, operation string) map[string]*userv2.UniversalUser {
	users := make(map[string]*userv2.UniversalUser)
	if ctx == nil || ctx.Err() != nil || len(userIDs) == 0 || s == nil || nilProfilePort(s.UserService) {
		return users
	}
	response, err := s.UserService.GetUsersByIDs(ctx, &userv2.GetUsersByIDsRequest{IDs: append([]string(nil), userIDs...)})
	if err != nil || response == nil {
		logger.AcquireOperationFrom(ctx, "external/usermanager", operation).Debug(
			"user-enrichment-unavailable", zap.String("reason", "lookup-incomplete"),
		)
	}
	if response != nil && response.Users != nil {
		return response.Users
	}
	return users
}
