package accessmanager_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// adminStatusAudit captures manager attribution without adding an external sink.
type adminStatusAudit struct {
	usermanager.AuditService
	events []audit.LogAuditEventRequest
}

func (a *adminStatusAudit) LogAuditEvent(_ context.Context, r *audit.LogAuditEventRequest) error {
	a.events = append(a.events, *r)
	return nil
}

// interleavedStatusRepository changes persisted state between domain validation
// and the real atomic command; it does not synthesize storage receipts.
type interleavedStatusRepository struct {
	*user.Repository
	before func()
}

func (r *interleavedStatusRepository) SetAccountStatus(ctx context.Context, c *user.SetAccountStatusRequest) (*user.UniversalUser, error) {
	r.before()
	return r.Repository.SetAccountStatus(ctx, c)
}

func TestAdministratorStatusHTTPIntegration(t *testing.T) {
	for _, name := range []string{"success", "EMAIL_CHANGE", "bulk", "missing target", "body ID ignored", "demoted after middleware", "suspended after middleware", "revoked after middleware", "revision after middleware", "type after middleware", "target email race", "target status race", "verification race", "unrelated update"} {
		t.Run(name, func(t *testing.T) {
			f := newConnectionFixture(t)
			_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}}})
			require.NoError(t, err)
			// The original token predates promotion, proving current roles win over
			// historical token flags. Target and caller are distinct real accounts.
			target, _ := f.newAccount(t, "target@example.test", "target-subject")
			a := &adminStatusAudit{}
			manager := (&usermanager.Service{UserService: f.users, AuditService: a}).WithAdministratorAuthorizer(f.service)
			h := user.NewHandler(f.users, validator.NewValidator()).WithStatusManager(manager)
			suite, err := middleware.NewSuite(&middleware.NewSuiteRequest{Service: f.service, EphemeralStore: f.ephemeral, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh"})
			require.NoError(t, err)
			routes := router.NewRouter(nil, nil)
			user.AttachRoutes(&user.AttachRoutesRequest{Router: routes, Handler: h, AdminOnlyMiddleware: func(next http.Handler) http.Handler {
				return suite.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var fields bson.M
					switch name {
					case "demoted after middleware":
						fields = bson.M{"roles": bson.A{"USER"}}
					case "suspended after middleware":
						fields = bson.M{"status": "SUSPENDED"}
					case "revision after middleware":
						fields = bson.M{"email_revision": f.account.EmailRevision + 1}
					case "type after middleware":
						fields = bson.M{"type": "api_service"}
					case "revoked after middleware":
						_, err := f.ephemeral.DeleteAuth(f.ctx, toolbox.CombinedUuidFormat(f.account.ID, f.tokens.AccessUUID))
						require.NoError(t, err)
					}
					if fields != nil {
						_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": fields})
						require.NoError(t, err)
					}
					next.ServeHTTP(w, r)
				}))
			}})
			require.NoError(t, routes.ValidateRoutePolicies())
			f.users.UserRepository = &interleavedStatusRepository{Repository: f.users.UserRepository.(*user.Repository), before: func() {
				var fields bson.M
				switch name {
				case "target email race":
					fields = bson.M{"email": "changed@example.test"}
				case "target status race":
					fields = bson.M{"status": "DEACTIVATED"}
				case "verification race":
					fields = bson.M{"verification.email_verified_at": "new-verification"}
				case "unrelated update":
					fields = bson.M{"roles": bson.A{"ADMIN"}, "personal_info.first_name": "Concurrent"}
				}
				if fields != nil {
					_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": target.ID}, bson.M{"$set": fields})
					require.NoError(t, err)
				}
			}}
			desired := "SUSPENDED"
			if name == "EMAIL_CHANGE" || name == "verification race" {
				desired = "EMAIL_CHANGE"
			}
			id := target.ID
			if name == "missing target" {
				id = "missing"
			}
			body, err := json.Marshal(map[string]string{"desired_status": desired, "id": f.account.ID, "actor_id": target.ID})
			require.NoError(t, err)
			path, method := "/api/v2/users/"+id+"/status", http.MethodPatch
			if name == "bulk" {
				method = http.MethodPost
				path = "/api/v2/users/bulk/status"
				body, err = json.Marshal(map[string]any{"ids": []string{target.ID, "missing"}, "desired_status": desired})
				require.NoError(t, err)
			}
			r := httptest.NewRequest(method, "https://app.example"+path, strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
			r.AddCookie(&http.Cookie{Name: "refresh", Value: f.tokens.RefreshToken})
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			ok := name == "success" || name == "EMAIL_CHANGE" || name == "body ID ignored" || name == "unrelated update" || name == "bulk"
			if ok {
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Len(t, a.events, 1)
				require.Equal(t, f.account.ID, a.events[0].ActorId)
				require.Equal(t, target.ID, a.events[0].TargetId)
			} else {
				require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
				require.Empty(t, a.events)
				if strings.Contains(name, "race") {
					require.Equal(t, 409, w.Code)
					require.Contains(t, w.Body.String(), "USV2-042")
				}
				if name == "missing target" {
					require.Equal(t, 404, w.Code)
				}
			}
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			stored, err := f.users.GetUserByID(f.ctx, &user.GetUserByIDRequest{ID: target.ID})
			require.NoError(t, err)
			if ok {
				expected := "SUSPENDED"
				if desired == "EMAIL_CHANGE" {
					expected = "PROVISIONED"
					require.False(t, stored.User.Verification.EmailVerified)
				}
				require.Equal(t, expected, stored.User.Status)
			} else if strings.HasSuffix(name, "after middleware") {
				require.Equal(t, "ACTIVE", stored.User.Status)
			}
			if name == "unrelated update" {
				require.True(t, stored.User.IsAdmin())
				require.Equal(t, "Concurrent", stored.User.PersonalInfo.FirstName)
			}
		})
	}
}
