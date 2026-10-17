package adminaccess

import (
	"context"
	"encoding/json"
	"github.com/ooaklee/ghatd/external/accesspolicy"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const capabilitiesJSON = `{"enabled":true,"expires_at":null,"scopes":["billing.selected","programme"],"permissions":["billing.migrate","programme.read"]}`

func capabilityPreview(t *testing.T, f *bridgeFixture) string {
	t.Helper()
	w := f.call(f.request("POST", "/users/"+f.actor.ID+"/preview", capabilitiesJSON))
	require.Equal(t, 200, w.Code, w.Body.String())
	f.reviewID = w.Header().Get(ReviewHeader)
	require.True(t, opaqueID.MatchString(f.reviewID))
	return w.Header().Get("ETag")
}
func capabilityApply(f *bridgeFixture, body, etag string) *http.Request {
	r := f.request("PUT", "/users/"+f.actor.ID, body)
	r.Header.Set("If-Match", etag)
	return r
}

func TestCapabilityReviewedBrowserCommands(t *testing.T) {
	for _, scenario := range []string{"happy replay", "not approved", "enabled tamper", "expiry tamper", "scope tamper", "permission tamper", "target tamper", "revision tamper", "reordered canonical", "revoked", "demoted", "email changed", "session changed", "expired", "cancelled", "concurrent", "CAS drift", "unknown write", "noop"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBridgeFixtureKind(t, Capabilities)
			f.start(t)
			etag := capabilityPreview(t, f)
			if scenario == "noop" {
				var initial accesspolicy.Capabilities
				require.NoError(t, json.Unmarshal([]byte(capabilitiesJSON), &initial))
				_, err := f.bridge.config.Manager.(CapabilityManager).ApplyCapabilities(f.ctxWithManagement(t), f.actor.ID, 0, initial)
				require.NoError(t, err)
				// Approve a different proposal but do not dispatch it. A subsequent
				// semantic no-op must invalidate that still-live approval.
				different := strings.Replace(capabilitiesJSON, `"enabled":true`, `"enabled":false`, 1)
				pending := f.call(f.request("POST", "/users/"+f.actor.ID+"/preview", different))
				require.Equal(t, 200, pending.Code, pending.Body.String())
				f.reviewID = pending.Header().Get(ReviewHeader)
				f.approve(t)
				w := f.call(f.request("POST", "/users/"+f.actor.ID+"/preview", capabilitiesJSON))
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Empty(t, w.Header().Get(ReviewHeader))
				require.Contains(t, w.Body.String(), `"changed":false`)
				require.Equal(t, 428, f.call(f.request("POST", "/challenge", "")).Code)
				require.Equal(t, 428, f.call(capabilityApply(f, different, `"1"`)).Code)
				return
			}
			if scenario != "not approved" {
				f.approve(t)
			}
			r := capabilityApply(f, capabilitiesJSON, etag)
			want := 428
			switch scenario {
			case "happy replay":
				want = 200
			case "enabled tamper":
				r = capabilityApply(f, strings.Replace(capabilitiesJSON, `"enabled":true`, `"enabled":false`, 1), etag)
			case "expiry tamper":
				r = capabilityApply(f, strings.Replace(capabilitiesJSON, `"expires_at":null`, `"expires_at":"2030-01-01T00:00:00Z"`, 1), etag)
			case "scope tamper":
				r = capabilityApply(f, strings.Replace(capabilitiesJSON, "billing.selected", "billing.other", 1), etag)
			case "permission tamper":
				r = capabilityApply(f, strings.Replace(capabilitiesJSON, "billing.migrate", "billing.other", 1), etag)
			case "target tamper":
				r.URL.Path = CapabilityBasePath + "/users/other"
			case "revision tamper":
				r.Header.Set("If-Match", `"1"`)
			case "reordered canonical":
				r = capabilityApply(f, strings.Replace(capabilitiesJSON, `["billing.selected","programme"]`, `["programme","billing.selected"]`, 1), etag)
				want = 200
			case "revoked":
				require.NoError(t, f.runtime.Store.DeleteAllTokenExceptedSpecified(f.ctx, f.actor.ID, nil))
				want = 401
			case "demoted":
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.actor.ID}, bson.M{"$set": bson.M{"roles": bson.A{}}})
				require.NoError(t, err)
				want = 403
			case "email changed":
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.actor.ID}, bson.M{"$inc": bson.M{"email_revision": 1}})
				require.NoError(t, err)
				want = 401
			case "session changed":
				tokens, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.actor, time.Now())
				require.NoError(t, err)
				require.NoError(t, f.runtime.Store.CreateAuth(f.ctx, f.actor.ID, tokens))
				r.Header.Set("Cookie", "access="+tokens.AccessToken)
			case "expired":
				require.NoError(t, f.redis.HSet(f.bridge.config.Store.prefix+f.page, "expires", 1).Err())
			case "cancelled":
				require.Equal(t, 200, f.call(f.request("POST", "/cancel", "")).Code)
			case "CAS drift":
				_, err := f.bridge.config.Manager.(CapabilityManager).ApplyCapabilities(f.ctxWithManagement(t), f.actor.ID, 0, accesspolicy.Capabilities{Enabled: true, Scopes: []string{"concurrent"}})
				require.NoError(t, err)
				want = 412
			case "unknown write":
				f.bridge.config.Manager = &uncertainCapabilityManager{PolicyManager: f.bridge.config.Manager, CapabilityManager: f.bridge.config.Manager.(CapabilityManager)}
				want = 503
			case "concurrent":
				var wg sync.WaitGroup
				results := make(chan int, 8)
				for range 8 {
					wg.Go(func() { results <- f.call(capabilityApply(f, capabilitiesJSON, etag)).Code })
				}
				wg.Wait()
				close(results)
				success := 0
				for status := range results {
					if status == 200 {
						success++
					} else {
						require.Equal(t, 428, status)
					}
				}
				require.Equal(t, 1, success)
				return
			}
			w := f.call(r)
			require.Equal(t, want, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), f.tokens.AccessToken)
			if want == 200 || scenario == "CAS drift" || scenario == "unknown write" {
				require.Equal(t, 428, f.call(capabilityApply(f, capabilitiesJSON, etag)).Code)
			}
		})
	}
}

// ctxWithManagement uses the real cookie adaptation and verified middleware;
// it never manufactures actor/session claims for manager calls.
func (f *bridgeFixture) ctxWithManagement(t *testing.T) context.Context {
	t.Helper()
	var ctx context.Context
	h := f.bridge.browserSession(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() }))
	h.ServeHTTP(httptest.NewRecorder(), f.request("POST", "/session", ""))
	require.NotNil(t, ctx)
	return ctx
}

type uncertainCapabilityManager struct {
	PolicyManager
	CapabilityManager
}

func (m *uncertainCapabilityManager) ApplyCapabilities(ctx context.Context, target string, rev int64, patch accesspolicy.Capabilities) (accesspolicy.Grant, error) {
	_, err := m.CapabilityManager.ApplyCapabilities(ctx, target, rev, patch)
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	return accesspolicy.Grant{}, accesspolicy.ErrConfiguration
}

func TestCapabilityAndLegacyPurposeIsolation(t *testing.T) {
	for _, scenario := range []string{"cap review in token reader", "token review in cap reader", "old binding compatibility", "shared challenge throttle", "shared context throttle"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBridgeFixtureKind(t, Capabilities)
			c := f.bridge.config
			c.ReviewKind = TokenLimits
			token, err := New(c)
			require.NoError(t, err)
			require.NoError(t, token.Attach(f.routeRegistry))
			f.start(t)
			capPage := f.page
			capabilityPreview(t, f)
			capReview := f.reviewID
			capBinding, err := f.bridge.identity(f.ctxWithManagement(t))
			require.NoError(t, err)
			tokenBinding, err := token.identity(f.ctxWithManagement(t))
			require.NoError(t, err)
			require.NotEqual(t, capBinding, tokenBinding)
			switch scenario {
			case "cap review in token reader":
				f.bridge = token
				require.Equal(t, 428, f.call(f.request("POST", "/challenge", "")).Code)
			case "token review in cap reader":
				f.bridge = token
				f.page = ""
				f.reviewID = ""
				f.start(t)
				f.preview(t)
				f.bridge = &Bridge{config: c}
				f.bridge.config.ReviewKind = Capabilities
				require.Equal(t, 428, f.call(f.request("POST", "/challenge", "")).Code)
			case "old binding compatibility":
				raw, _ := json.Marshal([]any{"token-allowance", c.System, c.Environment, c.Origin, f.actor.ID, f.tokens.AccessUUID, f.actor.Email, f.actor.EmailRevision})
				require.Equal(t, digest(string(raw)), tokenBinding)
				// Simulate the deployed token reader ignoring new JSON fields. Its old
				// binding still cannot read the capability payload, even with zero limits.
				_, err = f.bridge.config.Store.run(f.ctx, capPage, tokenBinding, "read", capReview, "", "", 0)
				require.ErrorIs(t, err, ErrReview)
			case "shared challenge throttle":
				require.Equal(t, 200, f.call(f.request("POST", "/challenge", "")).Code)
				f.bridge = token
				f.page = ""
				f.reviewID = ""
				f.start(t)
				f.preview(t)
				require.Equal(t, 429, f.call(f.request("POST", "/challenge", "")).Code)
			case "shared context throttle":
				for i := 0; i < 9; i++ {
					binding := capBinding
					if i%2 == 0 {
						binding = tokenBinding
					}
					_, err = f.bridge.config.Store.Create(f.ctx, binding, f.actor.ID)
					require.NoError(t, err)
				}
				_, err = f.bridge.config.Store.Create(f.ctx, tokenBinding, f.actor.ID)
				require.ErrorIs(t, err, ErrCooldown)
			}
		})
	}
}

func TestNativeCapabilityReadPreviewAndPreservation(t *testing.T) {
	for _, variant := range []string{"absent grant read and preview", "existing grant preserves accounting"} {
		t.Run(variant, func(t *testing.T) {
			f := newBridgeFixtureKind(t, Capabilities)
			f.start(t)
			if variant == "existing grant preserves accounting" {
				_, err := f.bridge.config.Manager.Apply(f.ctxWithManagement(t), f.actor.ID, 0, accesspolicy.TokenLimits{Permanent: 2})
				require.NoError(t, err)
				// Seed a nonempty accounting limit through its owning policy service.
				ctx := f.ctxWithManagement(t)
				before, err := f.bridge.config.Manager.(CapabilityManager).ReviewCapabilities(ctx, f.actor.ID)
				require.NoError(t, err)
				before.Limits = map[string]accesspolicy.Limit{"requests": {Maximum: 12, WindowSeconds: 60}}
				_, err = f.owningPolicy.ReplaceGrant(ctx, *before, before.Revision)
				require.NoError(t, err)
				require.EqualValues(t, 2, before.Tokens.Permanent)
			}
			f.mutationMu.Lock()
			f.mutations = nil
			f.mutationMu.Unlock()
			w := f.call(f.request("POST", "/users/"+f.actor.ID+"/review", ""))
			require.Equal(t, 200, w.Code, w.Body.String())
			etag := capabilityPreview(t, f)
			f.mutationMu.Lock()
			mutations := append([]string{}, f.mutations...)
			f.mutationMu.Unlock()
			require.Empty(t, mutations, "read/preview must not prepare indexes, inventory or write Mongo")
			ctx := f.ctxWithManagement(t)
			before, err := f.bridge.config.Manager.(CapabilityManager).ReviewCapabilities(ctx, f.actor.ID)
			require.NoError(t, err)
			f.approve(t)
			require.Equal(t, 200, f.call(capabilityApply(f, capabilitiesJSON, etag)).Code)
			after, err := f.bridge.config.Manager.(CapabilityManager).ReviewCapabilities(ctx, f.actor.ID)
			require.NoError(t, err)
			if before != nil {
				require.Equal(t, before.Tokens, after.Tokens)
				require.Equal(t, before.Limits, after.Limits)
			} else {
				require.Zero(t, after.Tokens)
				require.Empty(t, after.Limits)
			}
		})
	}
}
