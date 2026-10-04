package user

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// userLookupBatchSize bounds each repository query, independently of the number
// of references supplied by an already-authorized consumer.
const userLookupBatchSize = 100

// GetUsersByIDs resolves user references without listing/counting the collection.
// It normalizes selectors, deduplicates and sorts them, then queries sequential
// batches of at most 100 IDs through the existing repository port. Empty input
// never becomes an unfiltered query; absent users are simply omitted.
//
// Successful batches remain available alongside joined native errors from failed
// batches. Cancellation stops further queries and is returned with any earlier
// failures; no records from a canceled or failed query are accepted. Consumers
// choose whether partial data is useful. This method does not log errors, retry,
// authorize requests, or replace strict identity checks in authentication flows.
//
// Returned identities must match a requested ID exactly. Models are detached
// before dependency hydration, like email lookups; arbitrary nested extension
// values and startup collaborators remain read-only shared values. Consumers
// must project only the fields their own response contract permits.
func (s *Service) GetUsersByIDs(ctx context.Context, req *GetUsersByIDsRequest) (*GetUsersByIDsResponse, error) {
	result := &GetUsersByIDsResponse{Users: make(map[string]*UniversalUser)}
	if req == nil {
		return result, ErrInvalidUserID
	}
	if ctx == nil || s == nil || nilUserDependency(s.UserRepository) {
		return result, ErrDatabaseError
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ids := normalizeUserLookupIDs(req.IDs)
	var failures []error
	for start := 0; start < len(ids); start += userLookupBatchSize {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(failures, err)...)
		}
		batch := ids[start:min(start+userLookupBatchSize, len(ids))]
		users, err := s.UserRepository.GetUsers(ctx, &GetUsersRequest{
			IDsFilter: append([]string(nil), batch...),
			Page:      1,
			PerPage:   len(batch),
		})
		if err != nil {
			failures = append(failures, err)
		}
		if canceled := ctx.Err(); canceled != nil {
			return result, errors.Join(append(failures, canceled)...)
		}
		if err != nil {
			continue
		}
		requested := make(map[string]struct{}, len(batch))
		for _, id := range batch {
			requested[id] = struct{}{}
		}
		for i := range users {
			if _, ok := requested[users[i].ID]; !ok {
				continue
			}
			result.Users[users[i].ID] = s.setUserDependencies(copyUserForUpdate(&users[i]))
		}
	}
	return result, errors.Join(failures...)
}

// normalizeUserLookupIDs returns a deterministic selector set without modifying
// caller memory. Persisted identities need strict validation before this step.
func normalizeUserLookupIDs(ids []string) []string {
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			unique[id] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}
