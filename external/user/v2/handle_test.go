package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver"
)

func TestNormalizeHandle(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"canonical", "calm-fox", "calm-fox"}, {"display form", " @Calm_Fox9 ", "calm_fox9"},
		{"minimum", "abc", "abc"}, {"maximum", strings.Repeat("a", 30), strings.Repeat("a", 30)},
		{"short", "ab", ""}, {"long", strings.Repeat("a", 31), ""}, {"number first", "1abc", ""},
		{"double at", "@@abc", ""}, {"space", "calm fox", ""}, {"unicode", "cаlm", ""},
		{"double separator", "calm-_fox", ""}, {"trailing separator", "calm-", ""},
		{"empty", "", ""}, {"control", "calm\nfox", ""}, {"dot", "calm.fox", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeHandle(tc.input)
			if tc.want == "" {
				require.ErrorIs(t, err, ErrInvalidHandle)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestGeneratedHandleBounds(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"words", "At Peace Great White Shark", true}, {"long", "Compassionate Southern Rockhopper Penguin", true},
		{"punctuation", "Calm---Fox's", true}, {"empty", "", false}, {"digits", "123", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := generatedHandle(tc.input)
			if !tc.valid {
				require.ErrorIs(t, err, ErrInvalidHandle)
				return
			}
			require.NoError(t, err)
			for i := 0; i < handleAttempts; i++ {
				candidate := handleCandidate(base, i)
				normalized, err := NormalizeHandle(candidate)
				require.NoError(t, err)
				require.Equal(t, candidate, normalized)
			}
		})
	}
}

// handleStub owns each test's optional capability and live account fixture.
type handleStub struct {
	UserRepository
	current   *UniversalUser
	readyErr  error
	available func(string) (bool, error)
	set       func(*UpdateUserHandleRequest) (*UserHandle, error)
}

func (r *handleStub) GetUserByID(context.Context, string) (*UniversalUser, error) {
	return r.current, nil
}
func (r *handleStub) RequireHandleStorage(context.Context) error { return r.readyErr }
func (r *handleStub) HandleAvailable(_ context.Context, name, _ string) (bool, error) {
	return r.available(name)
}
func (r *handleStub) SetUserHandle(_ context.Context, req *UpdateUserHandleRequest, _ time.Time) (*UserHandle, error) {
	return r.set(req)
}

func TestHandleCreationRetryBoundaries(t *testing.T) {
	outage := errors.New("private storage outage")
	for _, tc := range []struct {
		name            string
		failure         error
		failures, calls int
		enabled         bool
		want            error
	}{
		{"legacy", nil, 0, 1, false, nil}, {"first candidate", nil, 0, 1, true, nil},
		{"one collision", ErrHandleTaken, 1, 2, true, nil}, {"exhaustion", ErrHandleTaken, 100, 100, true, ErrHandleExhausted},
		{"outage", outage, 1, 1, true, outage}, {"email conflict", ErrEmailAlreadyExists, 1, 1, true, ErrEmailAlreadyExists},
		{"wrapped collision not retried", fmt.Errorf("ambiguous: %w", ErrHandleTaken), 1, 1, true, nil},
		{"joined collision not retried", errors.Join(ErrHandleTaken, outage), 1, 1, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &handleStub{}
			service := NewService(repo, nil, DefaultUserConfig(), nil, nil, nil, "").WithHandleGenerator(func() string { return "Calm Fox" })
			account := &UniversalUser{ID: "owner"}
			calls := 0
			_, err := createWithHandle(context.Background(), service, &UserConfig{GenerateHandle: tc.enabled}, account, func() (*UniversalUser, error) {
				calls++
				if calls <= tc.failures {
					return nil, tc.failure
				}
				return account, nil
			})
			want := tc.want
			if tc.failure != nil && tc.failure != ErrHandleTaken && want == nil {
				want = tc.failure
			}
			require.Equal(t, want, err)
			require.Equal(t, tc.calls, calls)
			if tc.enabled {
				require.Equal(t, handleCandidate("calm-fox", calls-1), account.Handle)
				require.EqualValues(t, 1, account.HandleMetadata.Revision)
				require.Zero(t, account.HandleMetadata.ChangeCount)
			} else {
				require.Empty(t, account.Handle)
				require.Nil(t, account.HandleMetadata)
			}
		})
	}
}

func TestHandleServiceAvailability(t *testing.T) {
	outage := errors.New("private query failure")
	for _, tc := range []struct {
		name  string
		want  error
		calls int
	}{
		{"available", nil, 1}, {"suggestion", nil, 2}, {"outage", outage, 1}, {"exhausted", nil, 10},
		{"restricted", ErrUnauthorisedAccess, 0}, {"missing index", ErrHandleIndexesRequired, 0},
		{"cancelled", context.Canceled, 0}, {"wrong owner", ErrUnauthorisedAccess, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			repo := &handleStub{current: &UniversalUser{ID: "owner", Status: "ACTIVE"}}
			repo.available = func(candidate string) (bool, error) {
				calls++
				if tc.name == "outage" {
					return false, outage
				}
				if tc.name == "exhausted" {
					return false, nil
				}
				return tc.name != "suggestion" || candidate == "calm-fox-1", nil
			}
			if tc.name == "restricted" {
				repo.current.Status = "SUSPENDED"
			}
			if tc.name == "wrong owner" {
				repo.current.ID = "other"
			}
			if tc.name == "missing index" {
				repo.readyErr = ErrHandleIndexesRequired
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.name == "cancelled" {
				cancel()
			}
			s := NewService(repo, nil, DefaultUserConfig(), nil, nil, nil, "")
			response, err := s.ValidateUserHandle(ctx, &ValidateUserHandleRequest{UserID: "owner", Handle: "@Calm-Fox"})
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.calls, calls)
			if err == nil {
				require.Equal(t, "calm-fox", response.Handle)
				require.Equal(t, tc.name == "available", response.Available)
				if tc.name == "suggestion" {
					require.Equal(t, "calm-fox-1", response.Suggestion)
				} else {
					require.Empty(t, response.Suggestion)
				}
			}
		})
	}
}

func TestHandleMetadataProjection(t *testing.T) {
	stamp := "2026-01-01T00:00:00Z"
	for _, tc := range []struct {
		name    string
		invalid bool
	}{{"legacy", false}, {"assigned", false}, {"invalid metadata", true}} {
		t.Run(tc.name, func(t *testing.T) {
			account := &UniversalUser{ID: "owner"}
			if tc.name != "legacy" {
				account.Handle = "calm-fox"
				account.HandleMetadata = &HandleMetadata{Revision: 1, CreatedAt: stamp, UpdatedAt: stamp}
			}
			if tc.invalid {
				account.HandleMetadata.Revision = -1
			}
			view, err := handleView(account)
			if tc.invalid {
				require.ErrorIs(t, err, ErrHandleConflict)
				return
			}
			require.NoError(t, err)
			encoded, err := json.Marshal(account)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "handle_metadata")
			require.NotContains(t, string(encoded), "revision")
			require.Equal(t, account.Handle, account.GetAsProfile().Handle)
			require.Equal(t, account.Handle, account.GetAsMicroProfile().Handle)
			if account.HandleMetadata != nil {
				view.Metadata.Revision = 9
				require.EqualValues(t, 1, account.HandleMetadata.Revision)
			}
		})
	}
}

func TestHandleDuplicateClassification(t *testing.T) {
	raw := func(key string) bson.Raw {
		data, err := bson.Marshal(bson.M{"keyPattern": bson.M{key: 1}})
		require.NoError(t, err)
		return data
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"command handle", mongo.CommandError{Code: 11000, Raw: raw("handle")}, true},
		{"native command wrapper", mongo.CommandError{Code: 11000, Raw: raw("handle"), Wrapped: driver.Error{Code: 11000, Raw: []byte(raw("handle"))}}, true},
		{"native command with extra cause", mongo.CommandError{Code: 11000, Raw: raw("handle"), Wrapped: driver.Error{Code: 11000, Raw: []byte(raw("handle")), Wrapped: errors.New("outage")}}, false},
		{"native command with different response", mongo.CommandError{Code: 11000, Raw: raw("handle"), Wrapped: driver.Error{Code: 11000, Raw: []byte(raw("email"))}}, false},
		{"missing raw", mongo.CommandError{Code: 11000}, false},
		{"insert handle", mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000, Raw: raw("handle")}}}, true},
		{"email", mongo.CommandError{Code: 11000, Raw: raw("email")}, false},
		{"message only", mongo.CommandError{Code: 11000, Message: "index: idx_users_handle"}, false},
		{"other code", mongo.CommandError{Code: 112, Raw: raw("handle")}, false},
		{"labelled", mongo.CommandError{Code: 11000, Raw: raw("handle"), Labels: []string{"TransientTransactionError"}}, false},
		{"write concern", mongo.WriteException{WriteConcernError: &mongo.WriteConcernError{Code: 64}, WriteErrors: mongo.WriteErrors{{Code: 11000, Raw: raw("handle")}}}, false},
		{"joined", errors.Join(ErrHandleTaken, errors.New("outage")), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, handleDuplicate(tc.err)) })
	}
}
