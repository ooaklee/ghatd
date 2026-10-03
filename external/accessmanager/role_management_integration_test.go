package accessmanager_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// interleavedRoleRepository changes real persistence immediately before the
// narrow command, proving that the command—not a probe—resolves the race.
type interleavedRoleRepository struct {
	*user.Repository
	before func()
}

func (r *interleavedRoleRepository) SetAccountRoles(ctx context.Context, c *user.SetAccountRolesRequest) (*user.UniversalUser, error) {
	r.before()
	return r.Repository.SetAccountRoles(ctx, c)
}

func TestAdministratorRoleHTTPIntegration(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, name := range []string{"success", "self target", "no-op", "missing target", "demoted after middleware", "suspended after middleware", "revoked after middleware", "type after middleware", "revision after middleware", "target roles race", "target status race", "target email race", "unrelated update"} {
			t.Run(stringMode(remove)+"/"+name, func(t *testing.T) {
				f := newConnectionFixture(t)
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN", "ADMIN"}}})
				require.NoError(t, err)
				// Credentials predate promotion; current roles, not the token flag, govern.
				target, _ := f.newAccount(t, "role-target@example.test", "role-target")
				id := target.ID
				if name == "self target" {
					id = f.account.ID
				}
				roles := bson.A{"USER"}
				if remove {
					roles = bson.A{"USER", "ADMIN", "ADMIN"}
				}
				if name == "no-op" {
					roles = bson.A{"USER", "ADMIN"}
					if remove {
						roles = bson.A{"USER"}
					}
				}
				if name != "self target" {
					_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"roles": roles}})
					require.NoError(t, err)
				}
				a := &adminStatusAudit{}
				manager := (&usermanager.Service{UserService: f.users, AuditService: a}).WithAdministratorAuthorizer(f.service)
				h := user.NewHandler(f.users, validator.NewValidator()).WithRoleManager(manager)
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
						case "type after middleware":
							fields = bson.M{"type": "api_service"}
						case "revision after middleware":
							fields = bson.M{"email_revision": f.account.EmailRevision + 1}
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
				f.users.UserRepository = &interleavedRoleRepository{Repository: f.users.UserRepository.(*user.Repository), before: func() {
					var fields bson.M
					switch name {
					case "target roles race":
						fields = bson.M{"roles": bson.A{"OTHER"}}
					case "target status race":
						fields = bson.M{"status": "SUSPENDED"}
					case "target email race":
						fields = bson.M{"email": "changed@example.test"}
					case "unrelated update":
						fields = bson.M{"personal_info.first_name": "Concurrent", "verification.phone_verified_at": "new-phone"}
					}
					if fields != nil {
						_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": id}, bson.M{"$set": fields})
						require.NoError(t, err)
					}
				}}
				urlID := id
				if name == "missing target" {
					urlID = "missing"
				}
				method := http.MethodPost
				if remove {
					method = http.MethodDelete
				}
				r := httptest.NewRequest(method, "https://app.example/api/v2/users/"+urlID+"/roles", strings.NewReader(`{"role":"ADMIN","id":"forged","actor_id":"forged"}`))
				r.Header.Set("Content-Type", "application/json")
				r.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
				r.AddCookie(&http.Cookie{Name: "refresh", Value: f.tokens.RefreshToken})
				w := httptest.NewRecorder()
				routes.GetRouter().ServeHTTP(w, r)
				ok := name == "success" || name == "self target" || name == "no-op" || name == "unrelated update"
				if ok {
					require.Equal(t, 200, w.Code, w.Body.String())
					audits := 1
					if name == "no-op" || name == "self target" && !remove {
						audits = 0
					}
					require.Len(t, a.events, audits)
					if audits > 0 {
						require.Equal(t, f.account.ID, a.events[0].ActorId)
						require.Equal(t, id, a.events[0].TargetId)
					}
				} else {
					require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
					require.Empty(t, a.events)
					if strings.HasSuffix(name, "race") {
						require.Equal(t, 409, w.Code)
						require.Contains(t, w.Body.String(), "USV2-044")
					}
					if name == "missing target" {
						require.Equal(t, 404, w.Code)
					}
				}
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				stored, err := f.users.GetUserByID(f.ctx, &user.GetUserByIDRequest{ID: id})
				require.NoError(t, err)
				if ok {
					require.Equal(t, !remove, stored.User.IsAdmin())
				}
				if name == "unrelated update" {
					require.Equal(t, "Concurrent", stored.User.PersonalInfo.FirstName)
					require.Equal(t, "new-phone", stored.User.Verification.PhoneVerifiedAt)
				}
				if name == "self target" && remove {
					// The original signed administrator session must now fail live authority.
					w = httptest.NewRecorder()
					r = httptest.NewRequest(http.MethodPost, "https://app.example/api/v2/users/"+id+"/roles", strings.NewReader(`{"role":"ADMIN"}`))
					r.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
					r.AddCookie(&http.Cookie{Name: "refresh", Value: f.tokens.RefreshToken})
					routes.GetRouter().ServeHTTP(w, r)
					require.GreaterOrEqual(t, w.Code, 400)
					require.Len(t, a.events, 1)
				}
			})
		}
	}
}

// stringMode provides stable operation labels for the shared add/remove matrix.
func stringMode(remove bool) string {
	if remove {
		return "remove"
	}
	return "add"
}
