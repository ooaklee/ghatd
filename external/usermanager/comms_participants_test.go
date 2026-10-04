package usermanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// participantLookupProbe can return a faulty provider receipt without involving
// persistence. Unimplemented user operations are intentionally never invoked.
type participantLookupProbe struct {
	UserService
	response      *userv2.GetUsersResponse
	err           error
	mutateRequest bool
	requests      [][]string
}

func (p *participantLookupProbe) GetUsers(_ context.Context, req *userv2.GetUsersRequest) (*userv2.GetUsersResponse, error) {
	p.requests = append(p.requests, append([]string(nil), req.IDsFilter...))
	if p.mutateRequest {
		req.IDsFilter[0] = "unrequested"
	}
	return p.response, p.err
}

func (p *participantLookupProbe) GetUsersByIDs(ctx context.Context, req *userv2.GetUsersByIDsRequest) (*userv2.GetUsersByIDsResponse, error) {
	service := userv2.NewService(&enrichmentLookupRepository{list: p.GetUsers}, nil, nil, nil, nil, nil, "")
	return service.GetUsersByIDs(ctx, req)
}

func TestCommsParticipantProjection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actors []string
		users  []userv2.UniversalUser
		want   []CommsParticipant
	}{
		{name: "empty", want: []CommsParticipant{}},
		{name: "missing user", actors: []string{"missing"}, want: []CommsParticipant{}},
		{name: "private name projection", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author", NanoID: "nano", Email: "private@example.test", Roles: []string{"ADMIN"}, PersonalInfo: &userv2.PersonalInfo{FullName: "Full Name", FirstName: "Other", LastName: "Person"}}}, want: []CommsParticipant{{ID: "author", NanoID: "nano", FullName: "Full Name"}}},
		{name: "structured fallback", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author", PersonalInfo: &userv2.PersonalInfo{FirstName: "First", LastName: "Last"}}}, want: []CommsParticipant{{ID: "author", FullName: "First Last"}}},
		{name: "email never substitutes for name", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author", Email: "private@example.test", Roles: []string{"ADMIN"}}}, want: []CommsParticipant{{ID: "author"}}},
		{name: "empty profile", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author", PersonalInfo: &userv2.PersonalInfo{}}}, want: []CommsParticipant{{ID: "author"}}},
		{name: "duplicate and sorted actors", actors: []string{"z", "a", "z"}, users: []userv2.UniversalUser{{ID: "z"}, {ID: "a"}, {ID: "z"}}, want: []CommsParticipant{{ID: "a"}, {ID: "z"}}},
		{name: "unexpected voter not included", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author"}, {ID: "unrequested"}}, want: []CommsParticipant{{ID: "author"}}},
		{name: "padded author not normalized", actors: []string{" author "}, users: []userv2.UniversalUser{{ID: "author"}}, want: []CommsParticipant{}},
		{name: "padded receipt not normalized", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: " author "}}, want: []CommsParticipant{}},
		{name: "empty identities", actors: []string{"", " "}, users: []userv2.UniversalUser{{ID: ""}}, want: []CommsParticipant{}},
		{name: "identifier boundary", actors: []string{strings.Repeat("i", 128)}, users: []userv2.UniversalUser{{ID: strings.Repeat("i", 128), NanoID: strings.Repeat("n", 128)}}, want: []CommsParticipant{{ID: strings.Repeat("i", 128), NanoID: strings.Repeat("n", 128)}}},
		{name: "oversized author", actors: []string{strings.Repeat("i", 129)}, users: []userv2.UniversalUser{{ID: strings.Repeat("i", 129)}}, want: []CommsParticipant{}},
		{name: "oversized nano id", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author", NanoID: strings.Repeat("n", 129)}}, want: []CommsParticipant{}},
		{name: "name byte boundary", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author", PersonalInfo: &userv2.PersonalInfo{FullName: strings.Repeat("é", 128)}}}, want: []CommsParticipant{{ID: "author", FullName: strings.Repeat("é", 128)}}},
		{name: "oversized name omitted not truncated", actors: []string{"author"}, users: []userv2.UniversalUser{{ID: "author", PersonalInfo: &userv2.PersonalInfo{FullName: strings.Repeat("é", 129)}}}, want: []CommsParticipant{{ID: "author"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := &participantLookupProbe{response: &userv2.GetUsersResponse{Users: tc.users}}
			s := &Service{UserService: port}
			got := s.enrichCommsParticipants(context.Background(), tc.actors)
			require.Equal(t, tc.want, got)
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "private@example.test")
			require.NotContains(t, string(raw), "ADMIN")
			if len(got) == 0 {
				require.Equal(t, "[]", string(raw))
			}
			var wire []map[string]any
			require.NoError(t, json.Unmarshal(raw, &wire))
			for _, row := range wire {
				require.Len(t, row, 3)
				for _, key := range []string{"id", "nano_id", "full_name"} {
					require.Contains(t, row, key)
				}
			}
		})
	}
}

func TestCommsParticipantBatchingAndPartialFailure(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		count, failBatch, want int
	}{
		{"zero", 0, -1, 0}, {"one", 1, -1, 1}, {"full batch", 100, -1, 100},
		{"viewer plus 100 authors", 101, -1, 101}, {"first batch unavailable", 101, 0, 1},
		{"second batch unavailable", 101, 1, 100}, {"all unavailable", 100, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := &enrichmentUserServiceStub{users: map[string]userv2.UniversalUser{}, failCalls: map[int]error{tc.failBatch: errors.New("private provider diagnostic")}}
			ids := make([]string, tc.count)
			for i := range ids {
				ids[i] = fmt.Sprintf("user-%03d", i)
				port.users[ids[i]] = userv2.UniversalUser{ID: ids[i]}
			}
			ctx, logs := observedEnrichmentContext()
			got := (&Service{UserService: port}).enrichCommsParticipants(ctx, ids)
			require.Len(t, got, tc.want)
			require.Len(t, port.getUsersCalls, (tc.count+99)/100)
			for _, batch := range port.getUsersCalls {
				require.LessOrEqual(t, len(batch), 100)
			}
			for i := 1; i < len(got); i++ {
				require.Less(t, got[i-1].ID, got[i].ID)
			}
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private provider diagnostic")
			}
		})
	}
}

func TestEnrichmentLoaderFailureAndIdentityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
	}{
		{"nil service", 0}, {"nil users", 0}, {"typed nil users", 0}, {"nil response", 0},
		{"failed lookup", 0}, {"unexpected users", 1}, {"padded returned id", 0},
		{"mutated request", 1}, {"canceled", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := &participantLookupProbe{response: &userv2.GetUsersResponse{Users: []userv2.UniversalUser{{ID: "author"}, {ID: "unrequested"}}}}
			s := &Service{UserService: port}
			ctx, logs := observedEnrichmentContext()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			switch tc.name {
			case "nil service":
				s = nil
			case "nil users":
				s.UserService = nil
			case "typed nil users":
				s.UserService = (*participantLookupProbe)(nil)
			case "nil response":
				port.response = nil
			case "failed lookup":
				port.err = errors.New("sensitive dependency payload")
			case "padded returned id":
				port.response.Users = []userv2.UniversalUser{{ID: " author "}}
			case "mutated request":
				port.mutateRequest = true
			case "canceled":
				cancel()
			}
			ids := []string{"author"}
			got := s.loadUsersForEnrichment(ctx, ids, "enrichment-test")
			require.Len(t, got, tc.want)
			if tc.want > 0 {
				require.Contains(t, got, "author")
				require.NotContains(t, got, "unrequested")
			}
			require.Equal(t, []string{"author"}, ids)
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry.ContextMap()), "sensitive dependency payload")
			}
		})
	}
}
