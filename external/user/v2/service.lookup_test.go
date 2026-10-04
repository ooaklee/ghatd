package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// lookupRepositoryProbe exposes only the existing list repository operation.
// Any accidental total-count or single-user query panics through the nil port.
type lookupRepositoryProbe struct {
	UserRepository
	requests []GetUsersRequest
	read     func(context.Context, *GetUsersRequest, int) ([]UniversalUser, error)
}

// GetUsers captures request values before a simulated adapter can mutate them.
func (p *lookupRepositoryProbe) GetUsers(ctx context.Context, req *GetUsersRequest) ([]UniversalUser, error) {
	snapshot := *req
	snapshot.IDsFilter = slices.Clone(req.IDsFilter)
	p.requests = append(p.requests, snapshot)
	if p.read != nil {
		return p.read(ctx, req, len(p.requests)-1)
	}
	users := make([]UniversalUser, 0, len(req.IDsFilter))
	for _, id := range req.IDsFilter {
		users = append(users, UniversalUser{ID: id})
	}
	return users, nil
}

func TestGetUsersByIDsSelectors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ids, want []string
	}{
		{name: "nil"},
		{name: "empty", ids: []string{}},
		{name: "only whitespace", ids: []string{"", " \t"}},
		{name: "sorted deduplicated selectors", ids: []string{" b ", "a", "b", ""}, want: []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &lookupRepositoryProbe{}
			service := NewService(repo, nil, nil, nil, nil, nil, "")
			original := slices.Clone(tc.ids)
			got, err := service.GetUsersByIDs(context.Background(), &GetUsersByIDsRequest{IDs: tc.ids})
			require.NoError(t, err)
			require.NotNil(t, got.Users)
			require.Len(t, got.Users, len(tc.want))
			require.Equal(t, original, tc.ids)
			if len(tc.want) == 0 {
				require.Empty(t, repo.requests, "never query all users for an empty selector")
			} else {
				require.Equal(t, []GetUsersRequest{{IDsFilter: tc.want, Page: 1, PerPage: len(tc.want)}}, repo.requests)
			}
		})
	}
}

func TestGetUsersByIDsGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil request", ErrInvalidUserID}, {"nil service", ErrDatabaseError},
		{"nil context", ErrDatabaseError}, {"nil repository", ErrDatabaseError},
		{"typed nil repository", ErrDatabaseError}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &lookupRepositoryProbe{}
			service := NewService(repo, nil, nil, nil, nil, nil, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := &GetUsersByIDsRequest{IDs: []string{"user"}}
			switch tc.name {
			case "nil request":
				req = nil
			case "nil service":
				service = nil
			case "nil context":
				ctx = nil
			case "nil repository":
				service.UserRepository = nil
			case "typed nil repository":
				service.UserRepository = (*lookupRepositoryProbe)(nil)
			case "canceled":
				cancel()
			}
			got, err := service.GetUsersByIDs(ctx, req)
			require.ErrorIs(t, err, tc.want)
			require.NotNil(t, got.Users)
			require.Empty(t, got.Users)
			require.Empty(t, repo.requests)
		})
	}
}

func TestGetUsersByIDsBatchesAndNativeFailures(t *testing.T) {
	firstErr, secondErr := errors.New("first adapter failure"), errors.New("second adapter failure")
	for _, tc := range []struct {
		name                 string
		count                int
		failures             map[int]error
		cancelAt             int
		wantUsers, wantCalls int
	}{
		{"one", 1, nil, -1, 1, 1},
		{"boundary", 100, nil, -1, 100, 1},
		{"three batches", 201, nil, -1, 201, 3},
		{"partial middle failure", 201, map[int]error{1: firstErr}, -1, 101, 3},
		{"multiple native failures", 201, map[int]error{0: firstErr, 2: secondErr}, -1, 100, 3},
		{"canceled first batch", 201, nil, 0, 0, 1},
		{"canceled later batch", 201, nil, 1, 100, 2},
		{"failure and cancellation retained", 201, map[int]error{0: firstErr, 1: secondErr}, 1, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ids := make([]string, tc.count)
			for i := range ids {
				ids[i] = fmt.Sprintf("user-%03d", i)
			}
			repo := &lookupRepositoryProbe{read: func(_ context.Context, req *GetUsersRequest, batch int) ([]UniversalUser, error) {
				if batch == tc.cancelAt {
					cancel()
				}
				users := make([]UniversalUser, len(req.IDsFilter))
				for i, id := range req.IDsFilter {
					users[i].ID = id
				}
				if err := tc.failures[batch]; err != nil {
					return users, fmt.Errorf("wrapped: %w", err)
				}
				return users, nil
			}}
			got, err := NewService(repo, nil, nil, nil, nil, nil, "").GetUsersByIDs(ctx, &GetUsersByIDsRequest{IDs: ids})
			require.Len(t, got.Users, tc.wantUsers)
			require.Len(t, repo.requests, tc.wantCalls)
			for batch, failure := range tc.failures {
				if batch < tc.wantCalls {
					require.ErrorIs(t, err, failure)
				}
			}
			if tc.cancelAt >= 0 {
				require.ErrorIs(t, err, context.Canceled)
			}
			if len(tc.failures) == 0 && tc.cancelAt < 0 {
				require.NoError(t, err)
			}
			for i, req := range repo.requests {
				want := ids[i*100 : min((i+1)*100, len(ids))]
				require.Equal(t, GetUsersRequest{Page: 1, PerPage: len(want), IDsFilter: want}, req)
				for _, id := range want {
					_, exists := got.Users[id]
					require.Equal(t, tc.failures[i] == nil && i != tc.cancelAt, exists)
				}
			}
		})
	}
}

func TestGetUsersByIDsExactReceiptsAndDetachment(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rows          []UniversalUser
		mutateRequest bool
		want          int
	}{
		{name: "missing records"},
		{name: "padded record", rows: []UniversalUser{{ID: " user "}}},
		{name: "unrequested record", rows: []UniversalUser{{ID: "other"}}},
		{name: "only exact records", rows: []UniversalUser{{ID: "user"}, {ID: "other"}}, want: 1},
		{name: "adapter changes selectors", rows: []UniversalUser{{ID: "user"}, {ID: "other"}}, mutateRequest: true, want: 1},
		{name: "detached model", rows: []UniversalUser{{ID: "user", Roles: []string{"ADMIN"}, PersonalInfo: &PersonalInfo{FullName: "Original"}}}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &lookupRepositoryProbe{read: func(_ context.Context, req *GetUsersRequest, _ int) ([]UniversalUser, error) {
				if tc.mutateRequest {
					req.IDsFilter[0] = "other"
				}
				return tc.rows, nil
			}}
			req := &GetUsersByIDsRequest{IDs: []string{"user"}}
			got, err := NewService(repo, nil, nil, nil, nil, nil, "").GetUsersByIDs(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, got.Users, tc.want)
			require.Equal(t, []string{"user"}, req.IDs)
			if tc.want > 0 {
				value := got.Users["user"]
				require.NotNil(t, value)
				require.NotSame(t, &tc.rows[0], value)
				require.NotNil(t, value.config)
				require.Nil(t, tc.rows[0].config, "do not hydrate cached repository models in place")
				if tc.rows[0].PersonalInfo != nil {
					value.PersonalInfo.FullName = "Changed"
					value.Roles[0] = "Changed"
					require.Equal(t, "Original", tc.rows[0].PersonalInfo.FullName)
					require.Equal(t, "ADMIN", tc.rows[0].Roles[0])
				}
			}
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			require.JSONEq(t, `{}`, string(raw), "lookup results are not a transport projection")
		})
	}
}
