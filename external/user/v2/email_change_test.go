package user

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// emailStorageSetupProbe never connects to a live database. It checks wiring
// and setup retry behavior separately from conditional write integration tests.
type emailStorageSetupProbe struct {
	MongoDbStore
	client                   *mongo.Client
	database                 *mongo.Database
	initErr, databaseErr     error
	initCalls, databaseCalls int
	cancel                   context.CancelFunc
	cancelAt                 string
}

func (p *emailStorageSetupProbe) InitialiseClient(context.Context) (*mongo.Client, error) {
	p.initCalls++
	if p.cancelAt == "client" {
		p.cancel()
	}
	return p.client, p.initErr
}
func (p *emailStorageSetupProbe) GetDatabase(context.Context, string) (*mongo.Database, error) {
	p.databaseCalls++
	if p.cancelAt == "database" {
		p.cancel()
	}
	return p.database, p.databaseErr
}

func TestEmailChangeCollectionSetup(t *testing.T) {
	native := errors.New("native setup failure")
	for _, tc := range []struct {
		name             string
		want             error
		inits, databases int
	}{
		{"cache", nil, 1, 1}, {"client failure", native, 3, 0}, {"database failure", native, 3, 3},
		{"nil client", ErrDatabaseError, 1, 0}, {"nil database", ErrDatabaseError, 1, 1},
		{"nil repository", ErrDatabaseError, 0, 0}, {"nil context", ErrDatabaseError, 0, 0}, {"typed nil store", ErrDatabaseError, 0, 0},
		{"already canceled", context.Canceled, 0, 0}, {"cancel client", context.Canceled, 1, 0}, {"cancel database", context.Canceled, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &emailStorageSetupProbe{client: client, database: client.Database("setup_test"), cancel: cancel}
			r := NewRepository(p)
			switch tc.name {
			case "client failure":
				p.initErr = native
			case "database failure":
				p.databaseErr = native
			case "nil client":
				p.client = nil
			case "nil database":
				p.database = nil
			case "nil repository":
				r = nil
			case "nil context":
				ctx = nil
			case "typed nil store":
				r.Store = (*emailStorageSetupProbe)(nil)
			case "already canceled":
				cancel()
			case "cancel client":
				p.cancelAt = "client"
			case "cancel database":
				p.cancelAt = "database"
			}
			collection, err := r.GetUserCollection(ctx)
			require.Equal(t, tc.want, err)
			if tc.want != nil {
				require.Nil(t, collection)
			} else {
				again, err := r.GetUserCollection(ctx)
				require.NoError(t, err)
				require.Same(t, collection, again)
				cancel()
				_, err = r.GetUserCollection(ctx)
				require.Equal(t, context.Canceled, err)
			}
			require.Equal(t, tc.inits, p.initCalls)
			require.Equal(t, tc.databases, p.databaseCalls)
		})
	}
}

func TestEmailChangeIndexConstraint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  bool
	}{
		{"int32", int32(1), true}, {"int64", int64(1), true}, {"double", float64(1), true},
		{"fraction", 1.9, false}, {"descending", -1, false}, {"string", "1", false},
		{"nan", math.NaN(), false}, {"infinity", math.Inf(1), false}, {"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := bson.Marshal(bson.D{{Key: "email", Value: tc.value}})
			require.NoError(t, err)
			require.Equal(t, tc.want, emailKeyPattern(raw))
		})
	}
	for _, kind := range []string{"ready", "hidden", "building", "top-level build marker", "partial", "nonunique", "compound", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			spec := bson.M{"name": "idx_users_email", "unique": true, "key": bson.M{"email": 1}}
			switch kind {
			case "hidden":
				spec["hidden"] = true
			case "building":
				spec = bson.M{"spec": spec, "buildUUID": bson.Binary{Subtype: 4, Data: make([]byte, 16)}}
			case "top-level build marker":
				spec["buildUUID"] = "unfinished"
			case "partial":
				spec["partialFilterExpression"] = bson.M{"status": "ACTIVE"}
			case "nonunique":
				spec["unique"] = false
			case "compound":
				spec["key"] = bson.M{"email": 1, "status": 1}
			}
			raw, err := bson.Marshal(spec)
			require.NoError(t, err)
			if kind == "malformed" {
				raw = []byte{1, 2, 3}
			}
			require.Equal(t, kind == "ready" || kind == "hidden", emailChangeIndexReady(raw))
		})
	}
}

// emailRepositoryProbe implements only the domain ports exercised by these
// cases. Unexpected use of an embedded legacy port fails the test immediately.
type emailRepositoryProbe struct {
	UserRepository
	stored, result    *UniversalUser
	readErr, writeErr error
	reads, writes     int
	command           ChangeUserEmailRequest
	status            string
	cancel            context.CancelFunc
	mutateCommand     bool
}

func (p *emailRepositoryProbe) GetUserByID(context.Context, string) (*UniversalUser, error) {
	p.reads++
	return p.stored, p.readErr
}

func (p *emailRepositoryProbe) ChangeUserEmail(_ context.Context, r *ChangeUserEmailRequest, status string, at time.Time) (*UniversalUser, error) {
	p.writes++
	p.command, p.status = *r, status
	if p.cancel != nil {
		p.cancel()
	}
	if p.mutateCommand {
		r.UserID = "adapter-forged-owner"
	}
	return p.result, p.writeErr
}

func emailChangeFixture() (*Service, *emailRepositoryProbe, *ChangeUserEmailRequest) {
	r := &ChangeUserEmailRequest{UserID: "owner", Email: " New@Example.test ", ExpectedEmail: "old@example.test", ExpectedRevision: 2, ExpectedType: UserConfigTypeDefault, ExpectedStatus: "ACTIVE"}
	p := &emailRepositoryProbe{
		stored: &UniversalUser{ID: "owner", Email: r.ExpectedEmail, EmailRevision: 2, Type: UserConfigTypeDefault, Status: "ACTIVE"},
		result: &UniversalUser{ID: "owner", Email: "new@example.test", EmailRevision: 3, Type: UserConfigTypeDefault, Status: "PROVISIONED", Verification: &VerificationStatus{}},
	}
	return NewService(p, nil, nil, nil, nil, nil, ""), p, r
}

func TestEmailChangeDomain(t *testing.T) {
	native := errors.New("private store failure")
	for _, tc := range []struct {
		name   string
		want   error
		writes int
	}{
		{"success", nil, 1}, {"legacy type", nil, 1}, {"legacy raw request", nil, 1},
		{"configuration without verification requirement", nil, 1}, {"adapter mutates request", nil, 1}, {"late cancellation after commit", nil, 1},
		{"nil request", ErrInvalidUserBody, 0}, {"empty identity", ErrInvalidUserBody, 0}, {"overflow", ErrInvalidUserBody, 0},
		{"negative revision", ErrInvalidUserBody, 0}, {"bad email", ErrInvalidEmail, 0}, {"same email", ErrNoChangesDetected, 0},
		{"read failure", native, 0}, {"nil account", ErrEmailChangeUnavailable, 0}, {"foreign account", ErrEmailChangeUnavailable, 0},
		{"changed email", ErrEmailChangeConflict, 0}, {"changed revision", ErrEmailChangeConflict, 0}, {"changed status", ErrEmailChangeConflict, 0},
		{"changed type", ErrEmailChangeConflict, 0}, {"unknown type", ErrInvalidUserConfigType, 0}, {"disallowed status", ErrUserInvalidStatusTransition, 0},
		{"missing capability", ErrEmailChangeUnavailable, 0}, {"write failure", native, 1}, {"nil receipt", ErrEmailChangeUnavailable, 1},
		{"wrong receipt owner", ErrEmailChangeUnavailable, 1}, {"wrong receipt revision", ErrEmailChangeUnavailable, 1}, {"verified receipt", ErrEmailChangeUnavailable, 1},
		{"old verification timestamp", ErrEmailChangeUnavailable, 1}, {"wrong receipt status", ErrEmailChangeUnavailable, 1},
		{"canceled", context.Canceled, 0}, {"nil context", ErrEmailChangeUnavailable, 0}, {"nil service", ErrEmailChangeUnavailable, 0}, {"typed nil repository", ErrEmailChangeUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, req := emailChangeFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch tc.name {
			case "legacy type":
				p.stored.Type, p.result.Type = "", ""
			case "legacy raw request":
				p.stored.Type, p.result.Type, req.ExpectedType = "", "", ""
			case "configuration without verification requirement":
				s = NewService(p, nil, APIServiceUserConfig(), nil, nil, nil, "")
				req.ExpectedType, p.stored.Type, p.result.Type, p.result.Status = UserConfigTypeAPIService, UserConfigTypeAPIService, UserConfigTypeAPIService, "ACTIVE"
			case "adapter mutates request":
				p.mutateCommand = true
			case "late cancellation after commit":
				p.cancel = cancel
			case "nil request":
				req = nil
			case "empty identity":
				req.UserID = " "
			case "overflow":
				req.ExpectedRevision = math.MaxInt64
			case "negative revision":
				req.ExpectedRevision = -1
			case "bad email":
				req.Email = "Display Name <new@example.test>"
			case "same email":
				req.Email = " OLD@EXAMPLE.TEST "
			case "read failure":
				p.readErr = native
			case "nil account":
				p.stored = nil
			case "foreign account":
				p.stored.ID = "foreign"
			case "changed email":
				p.stored.Email = "changed@example.test"
			case "changed revision":
				p.stored.EmailRevision++
			case "changed status":
				p.stored.Status = "SUSPENDED"
			case "changed type":
				s.WithConfigs(WebAppUserConfig())
				p.stored.Type = UserConfigTypeWebApp
			case "unknown type":
				p.stored.Type = "unknown"
			case "disallowed status":
				p.stored.Status, req.ExpectedStatus = "SUSPENDED", "SUSPENDED"
			case "missing capability":
				s.UserRepository = struct{ UserRepository }{p}
			case "write failure":
				p.writeErr = native
			case "nil receipt":
				p.result = nil
			case "wrong receipt owner":
				p.result.ID = "foreign"
			case "wrong receipt revision":
				p.result.EmailRevision = 2
			case "verified receipt":
				p.result.Verification.EmailVerified = true
			case "old verification timestamp":
				p.result.Verification.EmailVerifiedAt = "old"
			case "wrong receipt status":
				p.result.Status = "ACTIVE"
			case "canceled":
				cancel()
			case "nil context":
				ctx = nil
			case "nil service":
				s = nil
			case "typed nil repository":
				s.UserRepository = (*emailRepositoryProbe)(nil)
			}
			var before ChangeUserEmailRequest
			if req != nil {
				before = *req
			}
			result, err := s.ChangeUserEmail(ctx, req)
			require.Equal(t, tc.want, err)
			require.Equal(t, tc.writes, p.writes)
			if req != nil {
				require.Equal(t, before, *req)
			}
			if tc.want != nil {
				require.Nil(t, result)
				return
			}
			require.Equal(t, "owner", result.ID)
			require.Equal(t, "new@example.test", result.Email)
			require.EqualValues(t, 3, result.EmailRevision)
			require.False(t, result.Verification.EmailVerified)
			require.NotSame(t, p.result, result)
			require.Equal(t, "old@example.test", p.stored.Email, "read snapshot must remain unchanged")
		})
	}
}

func TestProfileUpdateCannotChangeEmail(t *testing.T) {
	for _, mode := range []string{"scalar", "replacement", "empty replacement"} {
		t.Run(mode, func(t *testing.T) {
			s, p, _ := emailChangeFixture()
			req := &UpdateUserRequest{ID: "owner", Email: "new@example.test"}
			if mode != "scalar" {
				copy := *p.stored
				copy.Email = req.Email
				if mode == "empty replacement" {
					copy.Email = ""
				}
				req = &UpdateUserRequest{User: &copy}
			}
			response, err := s.UpdateUser(context.Background(), req)
			require.Nil(t, response)
			require.Equal(t, ErrEmailChangeRequired, err)
			require.Equal(t, "old@example.test", p.stored.Email)
		})
	}
}
