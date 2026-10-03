package apitoken

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// unreachableRepository panics through its nil embedded interface if a guard
// dispatches to persistence despite invalid service wiring or cancellation.
type unreachableRepository struct{ ApitokenRespository }

// invalidMongoStore exercises malformed adapter success without a live server.
type invalidMongoStore struct{ MongoDbStore }

func (*invalidMongoStore) InitialiseClient(context.Context) (*mongo.Client, error) { return nil, nil }
func (*invalidMongoStore) GetDatabase(context.Context, string) (*mongo.Database, error) {
	return nil, nil
}
func (*invalidMongoStore) ExecuteUpdateOneCommandResult(context.Context, *mongo.Collection, any, any, ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	return nil, nil
}
func (*invalidMongoStore) ExecuteDeleteOneCommandResult(context.Context, *mongo.Collection, any, ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error) {
	return nil, nil
}
func (*invalidMongoStore) ExecuteInsertOneCommand(context.Context, *mongo.Collection, any, string) (*mongo.InsertOneResult, error) {
	return nil, nil
}

func TestRepositoryInvalidDependenciesAndResults(t *testing.T) {
	for _, operation := range []string{"collection", "touch", "status", "delete", "create"} {
		t.Run(operation, func(t *testing.T) {
			for _, variant := range []string{"nil repository", "nil store", "nil context", "canceled", "nil result"} {
				t.Run(variant, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					t.Cleanup(cancel)
					repo := NewRepository(&invalidMongoStore{})
					if operation != "collection" {
						repo.collection = &mongo.Collection{}
					}
					want := ErrServiceUnavailable
					switch variant {
					case "nil repository":
						repo = nil
					case "nil store":
						repo.Store = nil
					case "nil context":
						ctx = nil
						if operation == "status" || operation == "delete" {
							want = ErrResourceNotFound
						}
					case "canceled":
						cancel()
						want = context.Canceled
					}
					var err error
					require.NotPanics(t, func() {
						switch operation {
						case "collection":
							_, err = repo.GetApiTokenCollection(ctx)
						case "touch":
							err = repo.TouchAPIToken(ctx, "token", "owner", make([]byte, 32), time.Now())
						case "status":
							err = repo.SetAPITokenStatusFor(ctx, "owner", "token", UserTokenStatusKeyActive)
						case "delete":
							err = repo.DeleteAPITokenFor(ctx, "owner", "token")
						case "create":
							_, err = repo.CreateUserAPIToken(ctx, &UserAPIToken{CreatedByID: "owner"})
						}
					})
					require.ErrorIs(t, err, want)
				})
			}
		})
	}
}

func TestServiceBoundaryGuards(t *testing.T) {
	for _, operation := range []string{"create", "verify", "touch", "activate", "revoke", "delete", "delete owner", "count", "list", "owner list", "read", "inventory", "fenced inventory"} {
		t.Run(operation, func(t *testing.T) {
			for _, variant := range []string{"nil service", "nil repository", "nil context", "canceled"} {
				t.Run(variant, func(t *testing.T) {
					s := NewService(&unreachableRepository{})
					ctx, cancel := context.WithCancel(context.Background())
					t.Cleanup(cancel)
					want := ErrServiceUnavailable
					switch variant {
					case "nil service":
						s = nil
					case "nil repository":
						s.ApitokenRespository = nil
					case "nil context":
						ctx = nil
					case "canceled":
						cancel()
						want = context.Canceled
					}
					digest := sha256.Sum256([]byte("test-secret"))
					request := httptest.NewRequest("GET", "/", nil)
					request.Header.Set(common.SystemWideXApiToken, "prefix.test-secret")
					var err error
					require.NotPanics(t, func() {
						switch operation {
						case "create":
							_, err = s.CreateAPIToken(ctx, &CreateAPITokenRequest{UserID: "owner", UserNanoId: "prefix"})
						case "verify":
							_, err = s.ExtractValidateUserAPITokenMetadata(ctx, request)
						case "touch":
							err = s.UpdateAPITokenLastUsedAt(ctx, &UpdateAPITokenLastUsedAtRequest{TokenID: "token", ClientID: "owner", APITokenEncoded: digest[:]})
						case "activate":
							err = s.ActivateAPIToken(ctx, &ActivateAPITokenRequest{UserID: "owner", ID: "token"})
						case "revoke":
							err = s.RevokeAPIToken(ctx, &RevokeAPITokenRequest{UserID: "owner", ID: "token"})
						case "delete":
							err = s.DeleteAPIToken(ctx, &DeleteAPITokenRequest{UserID: "owner", APITokenID: "token"})
						case "delete owner":
							err = s.DeleteApiTokensByOwnerId(ctx, "owner")
						case "count":
							_, err = s.GetTotalApiTokens(ctx, &GetTotalApiTokensRequest{UserId: "owner"})
						case "list":
							_, err = s.GetAPITokens(ctx, &GetAPITokensRequest{CreatedByID: "owner"})
						case "owner list":
							_, err = s.GetAPITokensFor(ctx, &GetAPITokensForRequest{ID: "owner"})
						case "read":
							_, err = s.GetAPIToken(ctx, &GetAPITokenRequest{ID: "token"})
						case "inventory":
							_, err = s.CountTokenInventory(ctx, "owner")
						case "fenced inventory":
							_, err = s.CountTokenInventoryFenced(ctx, "owner")
						}
					})
					require.ErrorIs(t, err, want)
				})
			}
		})
	}
}

// snapshotRepository simulates a custom adapter returning shared or malformed
// snapshots. Only the operations exercised by these boundary tables are wired.
type snapshotRepository struct {
	ApitokenRespository
	variant   string
	stored    *UserAPIToken
	cancel    context.CancelFunc
	count     int64
	listCalls int
}

func (r *snapshotRepository) CreateUserAPIToken(_ context.Context, token *UserAPIToken) (*UserAPIToken, error) {
	token.Generate().GenerateNewUUID()
	r.stored = token
	switch r.variant {
	case "nil":
		return nil, nil
	case "id":
		token.ID = ""
	case "owner":
		token.CreatedByID = "different"
	case "prefix":
		token.CreatedByNanoId = "different"
	case "secret":
		token.Value = "invalid.secret"
	case "digest":
		token.ValueSHA = nil
	case "status":
		token.Status = UserTokenStatusKeyRevoked
	case "expiry":
		token.TtlExpiresAt = ""
	case "cancel":
		r.cancel()
	}
	return token, nil
}

func (r *snapshotRepository) GetAPITokenByID(context.Context, string) (*UserAPIToken, error) {
	if r.variant == "cancel" {
		r.cancel()
	}
	return r.stored, nil
}

func (r *snapshotRepository) GetTotalApiTokens(context.Context, string, string, string, string, string, string, bool, bool) (int64, error) {
	if r.variant == "cancel count" {
		r.cancel()
	}
	return r.count, nil
}

func (r *snapshotRepository) GetAPITokens(context.Context, *GetAPITokensRequest) ([]UserAPIToken, error) {
	r.listCalls++
	if r.variant == "cancel" {
		r.cancel()
	}
	return []UserAPIToken{*r.stored}, nil
}

func TestCreationValidatesStoreResult(t *testing.T) {
	for _, variant := range []string{"valid", "nil", "id", "owner", "prefix", "secret", "digest", "status", "expiry", "cancel"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			repo := &snapshotRepository{variant: variant, cancel: cancel}
			got, err := NewService(repo).CreateAPIToken(ctx, &CreateAPITokenRequest{UserID: "owner", UserNanoId: "prefix", TokenTtl: 60})
			if variant != "valid" {
				want := ErrServiceUnavailable
				if variant == "cancel" {
					want = context.Canceled
				}
				require.ErrorIs(t, err, want)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "prefix."+repo.stored.Value, got.APIToken.Value)
			require.False(t, strings.Contains(repo.stored.Value, "."))
			payload, err := json.Marshal(got)
			require.NoError(t, err)
			require.Contains(t, string(payload), `"value":"prefix.`)
			require.NotContains(t, string(payload), "value_sha")
			got.APIToken.ValueSHA[0] ^= 0xff
			require.NotEqual(t, repo.stored.ValueSHA, got.APIToken.ValueSHA)
		})
	}
}

func TestManagementSnapshotsAreDetachedAndRedacted(t *testing.T) {
	for _, operation := range []string{"read", "list", "owner list"} {
		t.Run(operation, func(t *testing.T) {
			for _, variant := range []string{"valid", "cancel"} {
				t.Run(variant, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					t.Cleanup(cancel)
					repo := &snapshotRepository{variant: variant, cancel: cancel, count: 1, stored: &UserAPIToken{ID: "token", Value: "private-secret", ValueSHA: []byte("digest"), LastUsedAt: "2026-01-01T00:00:00Z"}}
					svc := NewService(repo)
					var got UserAPIToken
					var err error
					switch operation {
					case "read":
						result, failure := svc.GetAPIToken(ctx, &GetAPITokenRequest{ID: "token"})
						err = failure
						if result != nil {
							got = result.APIToken
						}
					case "list":
						req := &GetAPITokensRequest{CreatedByID: "owner"}
						before := *req
						result, failure := svc.GetAPITokens(ctx, req)
						err = failure
						require.Equal(t, before, *req)
						if result != nil {
							got = result.APITokens[0]
						}
					case "owner list":
						req := &GetAPITokensForRequest{ID: "owner"}
						before := *req
						result, failure := svc.GetAPITokensFor(ctx, req)
						err = failure
						require.Equal(t, before, *req)
						if result != nil {
							got = result.APITokens[0]
						}
					}
					if variant == "cancel" {
						require.ErrorIs(t, err, context.Canceled)
						require.Zero(t, got)
						return
					}
					require.NoError(t, err)
					require.Empty(t, got.Value)
					require.NotEmpty(t, got.HumanReadableLastUsedAt)
					require.Empty(t, repo.stored.HumanReadableLastUsedAt)
					require.Equal(t, "private-secret", repo.stored.Value)
					payload, err := json.Marshal(got)
					require.NoError(t, err)
					require.NotContains(t, string(payload), "value_sha")
					require.NotContains(t, string(payload), `"value"`)
					got.ValueSHA[0] ^= 0xff
					require.Equal(t, []byte("digest"), repo.stored.ValueSHA)
				})
			}
		})
	}
}

func TestInvalidManagementResults(t *testing.T) {
	for _, operation := range []string{"read", "count", "list", "owner list"} {
		t.Run(operation, func(t *testing.T) {
			for _, variant := range []string{"invalid", "cancel count"} {
				t.Run(variant, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					repo := &snapshotRepository{count: -1, variant: variant, cancel: cancel}
					svc := NewService(repo)
					var err error
					switch operation {
					case "read":
						if variant == "cancel count" {
							repo.stored = &UserAPIToken{ID: "wrong"}
						}
						_, err = svc.GetAPIToken(ctx, &GetAPITokenRequest{ID: "token"})
					case "count":
						_, err = svc.GetTotalApiTokens(ctx, &GetTotalApiTokensRequest{UserId: "owner"})
					case "list":
						_, err = svc.GetAPITokens(ctx, &GetAPITokensRequest{CreatedByID: "owner"})
					case "owner list":
						_, err = svc.GetAPITokensFor(ctx, &GetAPITokensForRequest{ID: "owner"})
					}
					want := ErrServiceUnavailable
					if variant == "cancel count" && operation != "read" {
						want = context.Canceled
					}
					require.ErrorIs(t, err, want)
					require.Zero(t, repo.listCalls)
				})
			}
		})
	}
}
