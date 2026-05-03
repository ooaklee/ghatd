package group_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/router"
)

func TestAttachRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		withAdminOnly       bool
		withAuthenticated   bool
		requestMethod       string
		requestPath         string
		expectStatus        int
		expectHandlerCalled bool
	}{
		{
			name:                "Success - POST to create group with admin middleware",
			withAdminOnly:       true,
			requestMethod:       http.MethodPost,
			requestPath:         "/api/v1/groups",
			expectStatus:        http.StatusOK,
			expectHandlerCalled: true,
		},
		{
			name:                "Success - GET groups with admin middleware",
			withAdminOnly:       true,
			requestMethod:       http.MethodGet,
			requestPath:         "/api/v1/groups",
			expectStatus:        http.StatusOK,
			expectHandlerCalled: true,
		},
		{
			name:                "Success - GET group by ID with admin middleware",
			withAdminOnly:       true,
			requestMethod:       http.MethodGet,
			requestPath:         "/api/v1/groups/test-group-id",
			expectStatus:        http.StatusOK,
			expectHandlerCalled: true,
		},
		{
			name:                "Success - PATCH update group with admin middleware",
			withAdminOnly:       true,
			requestMethod:       http.MethodPatch,
			requestPath:         "/api/v1/groups/test-group-id",
			expectStatus:        http.StatusOK,
			expectHandlerCalled: true,
		},
		{
			name:                "Success - DELETE group with admin middleware",
			withAdminOnly:       true,
			requestMethod:       http.MethodDelete,
			requestPath:         "/api/v1/groups/test-group-id",
			expectStatus:        http.StatusOK,
			expectHandlerCalled: true,
		},
		{
			name:                "Success - GET group config with admin middleware",
			withAdminOnly:       true,
			requestMethod:       http.MethodGet,
			requestPath:         "/api/v1/groups/configs",
			expectStatus:        http.StatusOK,
			expectHandlerCalled: true,
		},
		{
			name:                "Success - GET group stats with admin middleware",
			withAdminOnly:       true,
			requestMethod:       http.MethodGet,
			requestPath:         "/api/v1/groups/stats",
			expectStatus:        http.StatusOK,
			expectHandlerCalled: true,
		},
		{
			name:                "No authenticated-only route attached",
			withAuthenticated:   true,
			requestMethod:       http.MethodGet,
			requestPath:         "/api/v1/group/my-groups",
			expectStatus:        http.StatusNotFound,
			expectHandlerCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var handlerCalled bool

			adminMiddleware := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					next.ServeHTTP(w, r)
				})
			}

			authMiddleware := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					next.ServeHTTP(w, r)
				})
			}

			mockHandler := &mockGroupHandler{
				callTracker: &handlerCalled,
			}

			r := router.NewRouter(nil, nil)
			group.AttachRoutes(&group.AttachRoutesRequest{
				Router:                  r,
				Handler:                 mockHandler,
				AdminOnlyMiddleware:     adminMiddleware,
				AuthenticatedMiddleware: authMiddleware,
			})

			req := httptest.NewRequest(tt.requestMethod, tt.requestPath, nil)
			rec := httptest.NewRecorder()

			r.GetRouter().ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			if tt.expectHandlerCalled {
				assert.True(t, handlerCalled, "expected handler to be called")
			}
		})
	}
}

// mockGroupHandler implements group.GroupHandler for testing route registration
type mockGroupHandler struct {
	callTracker *bool
}

func (m *mockGroupHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupByID(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupLineage(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupDescendants(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupByNanoID(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroups(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupsByUserID(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupsAwaitingAnswerForInvitationsByMemberID(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) InviteUser(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) UninviteUser(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) RejectInvite(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupMembers(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) UpdateOwner(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) RepairInvalidMembers(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) ArchiveGroup(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) RestoreGroup(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupStats(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupsStats(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) GetGroupsConfig(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) ValidateGroupName(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) EnableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) DisableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) EnableGroupAutoInviteByEmailDomain(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockGroupHandler) DisableGroupAutoInviteByEmailDomain(w http.ResponseWriter, r *http.Request) {
	*m.callTracker = true
	w.WriteHeader(http.StatusOK)
}
