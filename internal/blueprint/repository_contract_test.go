package blueprint

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// invokeBlueprintStorage exercises real repository methods, not just their guard.
func invokeBlueprintStorage(r *Repository, ctx context.Context, op string) error {
	var err error
	switch op {
	case "collection":
		_, err = r.GetBlueprintCollection(ctx)
	case "create":
		_, err = r.CreateBlueprint(ctx, &Blueprint{ID: "bp-1"})
	case "get":
		_, err = r.GetBlueprintByID(ctx, "bp-1")
	case "name":
		_, err = r.GetBlueprintByNameAndKind(ctx, "Example", "demo")
	case "list":
		_, err = r.GetBlueprints(ctx, nil)
	case "count":
		_, err = r.GetTotalBlueprints(ctx, nil)
	case "update":
		_, err = r.UpdateBlueprint(ctx, &Blueprint{ID: "bp-1"})
	case "delete":
		err = r.DeleteBlueprintByID(ctx, "bp-1")
	default:
		panic("unknown storage operation")
	}
	return err
}

func TestBlueprintStorageEntry(t *testing.T) {
	for _, op := range []string{"collection", "create", "get", "name", "list", "count", "update", "delete"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"nil context", ErrBlueprintInvalidPayload}, {"nil repository", ErrBlueprintUnavailable},
			{"nil store", ErrBlueprintUnavailable}, {"typed nil store", ErrBlueprintUnavailable}, {"cancelled", context.Canceled},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				store := newMockMongoStore(t)
				r := NewRepository(store)
				ctx := context.Background()
				switch tc.name {
				case "nil context":
					ctx = nil
				case "nil repository":
					r = nil
				case "nil store":
					r.Store = nil
				case "typed nil store":
					r.Store = (*mockMongoStore)(nil)
				case "cancelled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				require.ErrorIs(t, invokeBlueprintStorage(r, ctx, op), tc.want)
				require.Empty(t, store.operations)
			})
		}
	}
}

func TestBlueprintStorageInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil create", ErrBlueprintInvalidPayload}, {"nil update", ErrBlueprintInvalidPayload},
		{"empty create ID", ErrBlueprintIDIsRequired}, {"empty update ID", ErrBlueprintIDIsRequired},
		{"empty get ID", ErrBlueprintIDIsRequired}, {"empty delete ID", ErrBlueprintIDIsRequired},
		{"empty name", ErrBlueprintNameIsRequired}, {"empty kind", ErrBlueprintKindIsRequired},
		{"overflow page", ErrBlueprintInvalidQueryParam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockMongoStore(t)
			r := NewRepository(store)
			ctx := context.Background()
			var err error
			switch tc.name {
			case "nil create":
				_, err = r.CreateBlueprint(ctx, nil)
			case "nil update":
				_, err = r.UpdateBlueprint(ctx, nil)
			case "empty create ID":
				_, err = r.CreateBlueprint(ctx, &Blueprint{ID: " "})
			case "empty update ID":
				_, err = r.UpdateBlueprint(ctx, &Blueprint{})
			case "empty get ID":
				_, err = r.GetBlueprintByID(ctx, " ")
			case "empty delete ID":
				err = r.DeleteBlueprintByID(ctx, "")
			case "empty name":
				_, err = r.GetBlueprintByNameAndKind(ctx, " ", "demo")
			case "empty kind":
				_, err = r.GetBlueprintByNameAndKind(ctx, "Example", " ")
			case "overflow page":
				_, err = r.GetBlueprints(ctx, &GetBlueprintsRequest{Page: math.MaxInt64, PageSize: 2})
			}
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, store.operations)
		})
	}
}

func TestBlueprintCollectionBoundary(t *testing.T) {
	failure := errors.New("private setup failure")
	for _, tc := range []struct {
		name  string
		want  error
		calls []string
	}{
		{"nil client", ErrBlueprintUnavailable, []string{"initialize"}},
		{"nil database", ErrBlueprintUnavailable, []string{"initialize", "database"}},
		{"cancel after init", context.Canceled, []string{"initialize"}},
		{"cancel after database", context.Canceled, []string{"initialize", "database"}},
		{"cancel with init failure", failure, []string{"initialize"}},
		{"cancel with database failure", failure, []string{"initialize", "database"}},
		{"cancel cached", context.Canceled, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := newMockMongoStore(t)
			r := NewRepository(store)
			switch tc.name {
			case "nil client":
				store.nilClient = true
			case "nil database":
				store.nilDatabase = true
			case "cancel after init":
				store.afterInit = cancel
			case "cancel after database":
				store.afterDatabase = cancel
			case "cancel with init failure":
				store.afterInit = cancel
				store.initialiseClientErrs = []error{failure}
			case "cancel with database failure":
				store.afterDatabase = cancel
				store.getDatabaseErrs = []error{failure}
			case "cancel cached":
				_, err := r.GetBlueprintCollection(ctx)
				require.NoError(t, err)
				store.operations = nil
				cancel()
			}
			_, err := r.GetBlueprintCollection(ctx)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.calls, store.operations)
		})
	}
}

func TestBlueprintCollectionConcurrentCache(t *testing.T) {
	for _, workers := range []int{2, 8} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			store := newMockMongoStore(t)
			r := NewRepository(store)
			results := make(chan *mongo.Collection, workers)
			failures := make(chan error, workers)
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					c, err := r.GetBlueprintCollection(context.Background())
					results <- c
					failures <- err
				}()
			}
			wg.Wait()
			close(results)
			close(failures)
			for err := range failures {
				require.NoError(t, err)
			}
			for c := range results {
				require.Same(t, r.collection, c)
			}
			require.Equal(t, 1, store.initialiseClientCalls)
			require.Equal(t, 1, store.getDatabaseCalls)
		})
	}
}

func TestBlueprintWriteReceipts(t *testing.T) {
	for _, op := range []string{"create", "update", "delete"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"acknowledged", nil}, {"cancel after acknowledgement", nil}, {"nil receipt", ErrBlueprintUnavailable},
			{"unacknowledged", ErrBlueprintUnavailable}, {"native error", nil}, {"zero count", ErrBlueprintResourceNotFound},
			{"negative count", ErrBlueprintUnavailable}, {"multiple count", ErrBlueprintUnavailable},
			{"no-op", nil}, {"negative modified", ErrBlueprintUnavailable}, {"excess modified", ErrBlueprintUnavailable},
			{"upsert count", ErrBlueprintUnavailable}, {"negative upsert count", ErrBlueprintUnavailable}, {"upsert ID", ErrBlueprintUnavailable},
			{"foreign receipt ID", ErrBlueprintUnavailable}, {"non-string receipt ID", ErrBlueprintUnavailable}, {"mutated document ID", ErrBlueprintUnavailable},
		} {
			if op == "create" && (tc.name == "zero count" || tc.name == "negative count" || tc.name == "multiple count" || tc.name == "no-op" || tc.name == "negative modified" || tc.name == "excess modified" || tc.name == "upsert count" || tc.name == "negative upsert count" || tc.name == "upsert ID") {
				continue
			}
			if op != "create" && (tc.name == "foreign receipt ID" || tc.name == "non-string receipt ID") {
				continue
			}
			if op == "delete" && (tc.name == "no-op" || tc.name == "negative modified" || tc.name == "excess modified" || tc.name == "upsert count" || tc.name == "negative upsert count" || tc.name == "upsert ID" || tc.name == "mutated document ID") {
				continue
			}
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				store := newMockMongoStore(t)
				r := NewRepository(store)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				failure := fmt.Errorf("private write: %w", ErrBlueprintDatabaseError)
				calls := 0
				input := &Blueprint{ID: "bp-1", Name: "private name", CreatedByUserID: "owner"}
				before := *input
				store.insertFunc = func(document *Blueprint) (*mongo.InsertOneResult, error) {
					calls++
					require.NotSame(t, input, document)
					res := &mongo.InsertOneResult{Acknowledged: true, InsertedID: "bp-1"}
					switch tc.name {
					case "nil receipt":
						return nil, nil
					case "unacknowledged":
						res.Acknowledged = false
					case "native error":
						return res, failure
					case "cancel after acknowledgement":
						cancel()
					case "foreign receipt ID":
						res.InsertedID = "other"
					case "non-string receipt ID":
						res.InsertedID = []string{"bp-1"}
					case "mutated document ID":
						document.ID = "other"
						res.InsertedID = "other"
					}
					return res, nil
				}
				store.updateFunc = func(filter, update bson.M) (*mongo.UpdateResult, error) {
					calls++
					require.Equal(t, bson.M{"_id": "bp-1"}, filter)
					document := update["$set"].(*Blueprint)
					require.NotSame(t, input, document)
					res := &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1}
					switch tc.name {
					case "nil receipt":
						return nil, nil
					case "unacknowledged":
						res.Acknowledged = false
					case "native error":
						return res, failure
					case "cancel after acknowledgement":
						cancel()
					case "zero count":
						res.MatchedCount = 0
						res.ModifiedCount = 0
					case "negative count":
						res.MatchedCount = -1
					case "multiple count":
						res.MatchedCount = 2
					case "no-op":
						res.ModifiedCount = 0
					case "negative modified":
						res.ModifiedCount = -1
					case "excess modified":
						res.ModifiedCount = 2
					case "upsert count":
						res.UpsertedCount = 1
					case "negative upsert count":
						res.UpsertedCount = -1
					case "upsert ID":
						res.UpsertedID = "other"
					case "mutated document ID":
						document.ID = "other"
					}
					return res, nil
				}
				store.deleteFunc = func(filter bson.M) (*mongo.DeleteResult, error) {
					calls++
					require.Equal(t, bson.M{"_id": "bp-1"}, filter)
					res := &mongo.DeleteResult{Acknowledged: true, DeletedCount: 1}
					switch tc.name {
					case "nil receipt":
						return nil, nil
					case "unacknowledged":
						res.Acknowledged = false
					case "native error":
						return res, failure
					case "cancel after acknowledgement":
						cancel()
					case "zero count":
						res.DeletedCount = 0
					case "negative count":
						res.DeletedCount = -1
					case "multiple count":
						res.DeletedCount = 2
					}
					return res, nil
				}
				var got *Blueprint
				var err error
				switch op {
				case "create":
					got, err = r.CreateBlueprint(ctx, input)
				case "update":
					got, err = r.UpdateBlueprint(ctx, input)
				case "delete":
					err = r.DeleteBlueprintByID(ctx, input.ID)
				}
				if tc.name == "native error" {
					require.Same(t, failure, err)
				} else if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
				} else {
					require.NoError(t, err)
					if op != "delete" {
						require.Equal(t, "bp-1", got.ID)
					}
				}
				require.Equal(t, 1, calls)
				require.Equal(t, before, *input)
				require.Equal(t, BlueprintCollection, store.lastCollectionName)
				if op == "create" {
					require.Equal(t, 1, store.insertOneCalls)
				}
			})
		}
	}
}

type blueprintFalseAbsence struct{}

func (*blueprintFalseAbsence) Error() string { return "private alias" }
func (*blueprintFalseAbsence) Is(error) bool { return true }

type blueprintCycleError struct{}

func (e *blueprintCycleError) Error() string { return "private cycle" }
func (e *blueprintCycleError) Unwrap() error { return e }

func TestBlueprintReadOutcomes(t *testing.T) {
	for _, op := range []string{"get", "name"} {
		for _, tc := range []struct {
			name          string
			failure, want error
		}{
			{"selected", nil, nil}, {"mongo absence", mongo.ErrNoDocuments, ErrBlueprintResourceNotFound},
			{"wrapped absence", fmt.Errorf("private: %w", mongo.ErrNoDocuments), ErrBlueprintResourceNotFound},
			{"domain absence", ErrBlueprintResourceNotFound, ErrBlueprintResourceNotFound},
			{"mixed", errors.Join(mongo.ErrNoDocuments, errors.New("private")), nil},
			{"single join", errors.Join(mongo.ErrNoDocuments), nil}, {"alias", &blueprintFalseAbsence{}, nil},
			{"cycle", &blueprintCycleError{}, nil}, {"typed nil", (*blueprintFalseAbsence)(nil), nil},
			{"outage", errors.New("private outage"), nil}, {"empty ID", nil, ErrBlueprintUnavailable},
			{"wrong selection", nil, ErrBlueprintUnavailable}, {"cancelled result", nil, context.Canceled},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				store := newMockMongoStore(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				store.findOneFunc = func(filter bson.M, result *Blueprint) error {
					if op == "get" {
						require.Equal(t, bson.M{"_id": "bp-1"}, filter)
					} else {
						require.Equal(t, bson.M{"name": "Example", "kind": "demo"}, filter)
					}
					*result = Blueprint{ID: "bp-1", Name: "Example", Kind: "demo"}
					switch tc.name {
					case "empty ID":
						result.ID = ""
					case "wrong selection":
						result.ID = "other"
						result.Name = "other"
					case "cancelled result":
						cancel()
					}
					return tc.failure
				}
				var got *Blueprint
				var err error
				r := NewRepository(store)
				if op == "get" {
					got, err = r.GetBlueprintByID(ctx, "bp-1")
				} else {
					got, err = r.GetBlueprintByNameAndKind(ctx, " Example ", " Demo ")
				}
				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
					require.Nil(t, got)
				} else if tc.failure != nil {
					require.Same(t, tc.failure, err)
					require.Nil(t, got)
				} else {
					require.NoError(t, err)
					require.Equal(t, "bp-1", got.ID)
				}
				require.Equal(t, []string{"initialize", "database", "find one"}, store.operations)
			})
		}
	}
}

func TestBlueprintListAndCountFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil cursor", ErrBlueprintUnavailable}, {"find failure", nil}, {"map failure", nil},
		{"cancel after find", context.Canceled}, {"cancel after map", context.Canceled},
		{"count failure", nil}, {"negative count", ErrBlueprintUnavailable}, {"cancel after count", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockMongoStore(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("private read error")
			cursor, err := mongo.NewCursorFromDocuments([]interface{}{bson.M{"_id": "bp-1"}}, nil, nil)
			require.NoError(t, err)
			t.Cleanup(func() { closeBlueprintCursor(cursor) })
			store.findFunc = func([]options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
				if tc.name == "nil cursor" {
					return nil, nil
				}
				if tc.name == "find failure" {
					return cursor, failure
				}
				if tc.name == "cancel after find" {
					cancel()
				}
				return cursor, nil
			}
			store.mapFunc = func(*mongo.Cursor, *[]Blueprint) error {
				if tc.name == "map failure" {
					return failure
				}
				if tc.name == "cancel after map" {
					cancel()
				}
				return nil
			}
			store.countFunc = func() (int64, error) {
				if tc.name == "count failure" {
					return 0, failure
				}
				if tc.name == "cancel after count" {
					cancel()
				}
				return -1, nil
			}
			r := NewRepository(store)
			isCount := tc.name == "count failure" || tc.name == "negative count" || tc.name == "cancel after count"
			if isCount {
				_, err = r.GetTotalBlueprints(ctx, nil)
			} else {
				_, err = r.GetBlueprints(ctx, nil)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.Same(t, failure, err)
			}
			if !isCount && tc.name != "nil cursor" {
				require.False(t, cursor.Next(context.Background()), "repository must close owned cursor")
			}
			if tc.name == "nil cursor" || tc.name == "cancel after find" || tc.name == "find failure" {
				require.NotContains(t, store.operations, "map")
			}
		})
	}
}

func TestBlueprintPaginationAndQuerySnapshots(t *testing.T) {
	for _, tc := range []struct {
		name        string
		req         *GetBlueprintsRequest
		limit, skip int64
	}{
		{"nil", nil, 0, 0}, {"defaults", &GetBlueprintsRequest{}, 0, 0},
		{"negative", &GetBlueprintsRequest{Page: -1, PageSize: -1}, 0, 0},
		{"second page", &GetBlueprintsRequest{Query: "a.b*", Page: 2, PageSize: 5}, 5, 5},
		{"maximum safe", &GetBlueprintsRequest{Page: math.MaxInt64, PageSize: 1}, 1, math.MaxInt64 - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockMongoStore(t)
			if tc.req != nil {
				store.afterInit = func() { tc.req.Page = 1; tc.req.PageSize = 99; tc.req.Query = "changed" }
			}
			store.findFunc = func(opts []options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
				var decoded options.FindOptions
				for _, builder := range opts {
					for _, apply := range builder.List() {
						require.NoError(t, apply(&decoded))
					}
				}
				if tc.limit == 0 {
					require.Nil(t, decoded.Limit)
					require.Nil(t, decoded.Skip)
				} else {
					require.Equal(t, tc.limit, *decoded.Limit)
					require.Equal(t, tc.skip, *decoded.Skip)
				}
				if tc.name == "second page" {
					require.Equal(t, []bson.M{{"name": bson.M{"$regex": `a\.b\*`, "$options": "i"}}, {"kind": bson.M{"$regex": `a\.b\*`, "$options": "i"}}, {"description": bson.M{"$regex": `a\.b\*`, "$options": "i"}}}, store.lastFilter.(bson.M)["$or"])
				}
				return mongo.NewCursorFromDocuments(nil, nil, nil)
			}
			got, err := NewRepository(store).GetBlueprints(context.Background(), tc.req)
			require.NoError(t, err)
			require.Len(t, got, 1)
		})
	}
}

func TestBlueprintCountQuerySnapshots(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprint(populated), func(t *testing.T) {
			store := newMockMongoStore(t)
			var req *GetBlueprintsRequest
			want := bson.M{"_id": bson.M{"$exists": true}}
			if populated {
				req = &GetBlueprintsRequest{Kind: " Demo ", Status: " Active "}
				want["kind"], want["status"] = "demo", "active"
				store.afterInit = func() { req.Kind, req.Status = "changed", "changed" }
			}
			total, err := NewRepository(store).GetTotalBlueprints(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, int64(7), total)
			require.Equal(t, want, store.lastFilter)
			require.Equal(t, []string{"initialize", "database", "count"}, store.operations)
		})
	}
}

func TestBlueprintSetupFailureLogPrivacy(t *testing.T) {
	for _, phase := range []string{"initialize", "database"} {
		t.Run(phase, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(context.Background(), zap.New(core))
			store := newMockMongoStore(t)
			failure := errors.New("private connection credentials")
			if phase == "initialize" {
				store.initialiseClientErrs = []error{failure, failure}
			} else {
				store.getDatabaseErrs = []error{failure, failure}
			}
			_, err := NewRepository(store).WithCollectionInitMaxAttemptsLimit(2).GetBlueprintCollection(ctx)
			require.ErrorIs(t, err, failure)
			require.NotErrorIs(t, err, ErrBlueprintDatabaseError)
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private")
				require.NotContains(t, entry.Message, "private")
			}
			require.NotEmpty(t, logs.All())
		})
	}
}

func TestBlueprintStorageHTTPBoundary(t *testing.T) {
	for _, op := range []string{"create", "get", "list"} {
		for _, tc := range []struct {
			name    string
			failure error
			status  int
			code    string
		}{
			{"mapped wrapped", fmt.Errorf("private diagnostic: %w", ErrBlueprintResourceNotFound), 404, "BLP0-005"},
			{"mixed", errors.Join(mongo.ErrNoDocuments, errors.New("private diagnostic")), 500, ""},
			{"outage", errors.New("private diagnostic"), 500, ""},
			{"invalid result", nil, 503, "BLP0-013"},
			{"host override", fmt.Errorf("private diagnostic: %w", ErrBlueprintDatabaseError), 502, "HOST-DATABASE"},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				store := newMockMongoStore(t)
				store.insertFunc = func(*Blueprint) (*mongo.InsertOneResult, error) { return nil, tc.failure }
				store.findOneFunc = func(bson.M, *Blueprint) error { return tc.failure }
				store.findFunc = func([]options.Lister[options.FindOptions]) (*mongo.Cursor, error) { return nil, tc.failure }
				core, logs := observer.New(zap.DebugLevel)
				ctx := logger.TransitWith(blueprintActorContext(context.Background(), "private actor"), zap.New(core))
				r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"private name","kind":"private kind"}`)).WithContext(ctx)
				r = mux.SetURLVars(r, map[string]string{BlueprintURIVariableID: "private target"})
				h := NewHandler(NewService(NewRepository(store)), validator.NewValidator(), reply.ErrorManifest{ErrBlueprintDatabaseError: {StatusCode: 502, Code: "HOST-DATABASE"}})
				w := httptest.NewRecorder()
				switch op {
				case "create":
					h.CreateBlueprint(w, r)
				case "get":
					h.GetBlueprintByID(w, r)
				case "list":
					h.GetBlueprints(w, r)
				}
				require.Equal(t, tc.status, w.Code, w.Body.String())
				if tc.code != "" {
					require.Contains(t, w.Body.String(), tc.code)
				}
				require.NotContains(t, w.Body.String(), "private")
				for _, entry := range logs.All() {
					require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private")
					require.NotContains(t, entry.Message, "private")
				}
			})
		}
	}
}
