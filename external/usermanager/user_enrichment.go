package usermanager

import (
	"context"
	"sort"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// userEnrichmentBatchSize limits the number of user IDs sent in a single
// GetUsers request during enrichment to avoid oversized queries.
const userEnrichmentBatchSize = 100

// loadUsersForEnrichment resolves persisted user references for optional
// response enrichment. It deduplicates and sorts IDs before issuing bounded,
// sequential GetUsers requests.
//
// Missing users are expected and are omitted from the returned map. A failed
// batch produces one DEBUG fallback event and does not discard users resolved
// by other batches. This helper must not be used for authentication or
// authorization, where a missing user or lookup failure must remain fatal.
func (s *Service) loadUsersForEnrichment(ctx context.Context, userIDs []string, operation string) map[string]*userv2.UniversalUser {
	ids := normaliseUserEnrichmentIDs(userIDs)
	usersByID := make(map[string]*userv2.UniversalUser, len(ids))
	if len(ids) == 0 || s.UserService == nil {
		return usersByID
	}

	logger := logger.AcquireOperationFrom(ctx, "external/usermanager", operation)
	for start := 0; start < len(ids); start += userEnrichmentBatchSize {
		end := min(start+userEnrichmentBatchSize, len(ids))
		batch := ids[start:end]
		response, err := s.UserService.GetUsers(ctx, &userv2.GetUsersRequest{
			IDsFilter: batch,
			Page:      1,
			PerPage:   len(batch),
		})
		if err != nil {
			logger.Debug(
				"user-enrichment-batch-unavailable",
				zap.Int("user-count", len(batch)),
				zap.Error(err),
			)
			continue
		}
		if response == nil {
			logger.Debug(
				"user-enrichment-batch-unavailable",
				zap.Int("user-count", len(batch)),
				zap.String("reason", "empty-response"),
			)
			continue
		}

		requested := make(map[string]struct{}, len(batch))
		for _, userID := range batch {
			requested[userID] = struct{}{}
		}
		for i := range response.Users {
			user := &response.Users[i]
			userID := strings.TrimSpace(user.ID)
			if _, ok := requested[userID]; ok {
				usersByID[userID] = user
			}
		}
	}

	return usersByID
}

// normaliseUserEnrichmentIDs deduplicates, trims whitespace, removes empty
// values, and sorts the given user IDs. Callers receive a deterministic,
// compact input for batched enrichment lookups.
func normaliseUserEnrichmentIDs(userIDs []string) []string {
	unique := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID != "" {
			unique[userID] = struct{}{}
		}
	}

	ids := make([]string, 0, len(unique))
	for userID := range unique {
		ids = append(ids, userID)
	}
	sort.Strings(ids)
	return ids
}
