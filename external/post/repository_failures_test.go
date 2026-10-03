package post

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// postStoreProbe supplies concrete command receipts without calling a driver.
// Embedding the port makes unexpected dispatch fail rather than silently pass.
type postStoreProbe struct {
	MongoDbStore
	item     Post
	failure  error
	updated  *mongo.UpdateResult
	deleted  *mongo.DeleteResult
	inserted *mongo.InsertOneResult
	filter   bson.M
	calls    int
}

func (p *postStoreProbe) ExecuteFindOneCommandDecodeResult(_ context.Context, _ *mongo.Collection, filter any, result any, _ string, _ bool, onFailure error) error {
	p.calls++
	p.filter = filter.(bson.M)
	if onFailure != nil {
		panic("native error requested")
	}
	*(result.(*Post)) = p.item
	return p.failure
}
func (p *postStoreProbe) ExecuteUpdateOneCommandResult(_ context.Context, _ *mongo.Collection, filter, update any, _ ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	p.calls++
	p.filter = filter.(bson.M)
	p.item = *copyPost(update.(bson.M)["$set"].(*Post))
	return p.updated, p.failure
}
func (p *postStoreProbe) ExecuteDeleteOneCommandResult(_ context.Context, _ *mongo.Collection, filter any, _ ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error) {
	p.calls++
	p.filter = filter.(bson.M)
	return p.deleted, p.failure
}

func (p *postStoreProbe) ExecuteInsertOneCommand(_ context.Context, _ *mongo.Collection, document any, _ string) (*mongo.InsertOneResult, error) {
	p.calls++
	p.item = *copyPost(document.(*Post))
	return p.inserted, p.failure
}

func TestPostRepositoryInsertSnapshots(t *testing.T) {
	outage := errors.New("private insert diagnostic")
	for _, op := range []string{"create", "raw"} {
		for _, tc := range []struct {
			name string
			want error
		}{{"success", nil}, {"nil receipt", ErrPostUnavailable}, {"outage", outage}, {"unacknowledged", ErrPostUnavailable}, {"wrong inserted ID", ErrPostUnavailable}} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				p := &postStoreProbe{inserted: &mongo.InsertOneResult{InsertedID: "target", Acknowledged: true}}
				if tc.name == "unacknowledged" {
					p.inserted.Acknowledged = false
				}
				if tc.name == "wrong inserted ID" {
					p.inserted.InsertedID = "other"
				}
				if tc.name == "nil receipt" {
					p.inserted = nil
				}
				if tc.name == "outage" {
					p.failure = outage
				}
				r := NewRepository(p)
				r.collection = &mongo.Collection{}
				input := validPostSnapshot()
				input.NanoId = "nano"
				before := copyPost(input)
				var result *Post
				var err error
				if op == "create" {
					result, err = r.CreatePost(context.Background(), input)
				} else {
					result, err = r.CreateRawPosts(context.Background(), input)
				}
				require.Equal(t, tc.want, err)
				require.Equal(t, before, input)
				require.Equal(t, 1, p.calls)
				if err == nil {
					require.NotEmpty(t, result.CreatedAt)
				} else {
					require.Nil(t, result)
				}
			})
		}
	}
}

func TestPostCollectionEntryGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{{"nil context", ErrPostBadRequest}, {"cancelled", context.Canceled}, {"nil repository", ErrPostUnavailable}, {"nil store", ErrPostUnavailable}, {"typed nil store", ErrPostUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			p := &postStoreProbe{}
			r := NewRepository(p)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch tc.name {
			case "nil context":
				ctx = nil
			case "cancelled":
				cancel()
			case "nil repository":
				r = nil
			case "nil store":
				r.Store = nil
			case "typed nil store":
				r.Store = (*postStoreProbe)(nil)
			}
			got, err := r.GetPostCollection(ctx)
			require.Nil(t, got)
			require.Equal(t, tc.want, err)
			require.Zero(t, p.calls)
		})
	}
}

func TestPostRepositoryLookupClassification(t *testing.T) {
	outage := errors.New("private driver diagnostic")
	for _, selector := range []string{"_id", "_nano_id", "_url_friendly_id"} {
		for _, tc := range []struct {
			name    string
			failure error
			want    error
		}{
			{"found", nil, nil}, {"mongo absent", mongo.ErrNoDocuments, ErrResourceNotFound}, {"wrapped absent", fmt.Errorf("lookup: %w", mongo.ErrNoDocuments), ErrResourceNotFound},
			{"domain absent", ErrResourceNotFound, ErrResourceNotFound}, {"outage", outage, outage}, {"same message", errors.New(mongo.ErrNoDocuments.Error()), nil},
			{"joined absent", errors.Join(mongo.ErrNoDocuments), nil}, {"mixed", errors.Join(mongo.ErrNoDocuments, outage), nil}, {"alias", absenceImpostor{}, nil}, {"wrong identity", nil, ErrPostUnavailable},
		} {
			t.Run(selector+"/"+tc.name, func(t *testing.T) {
				p := &postStoreProbe{failure: tc.failure, item: Post{Id: "target", NanoId: "target", UrlFriendlyId: "target"}}
				if tc.name == "wrong identity" {
					p.item = Post{}
				}
				r := NewRepository(p)
				r.collection = &mongo.Collection{}
				var got *Post
				var err error
				switch selector {
				case "_id":
					got, err = r.GetPostById(context.Background(), "target")
				case "_nano_id":
					got, err = r.GetPostByNanoId(context.Background(), "target")
				case "_url_friendly_id":
					got, err = r.GetPostByUrlFriendlyId(context.Background(), "target")
				}
				want := tc.want
				if want == nil && tc.failure != nil {
					want = tc.failure
				}
				require.Equal(t, want, err)
				if err != nil {
					require.Nil(t, got)
				} else {
					require.Equal(t, p.item, *got)
				}
				require.Equal(t, bson.M{selector: "target"}, p.filter)
				require.Equal(t, 1, p.calls)
			})
		}
	}
}

func TestPostRepositoryMutationReceipts(t *testing.T) {
	outage := errors.New("private write diagnostic")
	for _, op := range []string{"update", "soft delete", "hard delete"} {
		for _, tc := range []struct {
			name              string
			matched, modified int64
			want              error
		}{
			{"matched", 1, 1, nil}, {"matched no-op", 1, 0, nil}, {"missing", 0, 0, ErrResourceNotFound},
			{"nil receipt", 0, 0, ErrPostUnavailable}, {"negative", -1, 0, ErrPostUnavailable}, {"too many", 2, 1, ErrPostUnavailable}, {"write failure", 1, 1, outage},
			{"unacknowledged empty", 0, 0, ErrPostUnavailable}, {"unacknowledged positive", 1, 1, ErrPostUnavailable},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				p := &postStoreProbe{updated: &mongo.UpdateResult{MatchedCount: tc.matched, ModifiedCount: tc.modified, Acknowledged: true}, deleted: &mongo.DeleteResult{DeletedCount: tc.matched, Acknowledged: true}}
				if strings.HasPrefix(tc.name, "unacknowledged") {
					p.updated.Acknowledged = false
					p.deleted.Acknowledged = false
				}
				if tc.name == "nil receipt" {
					p.updated = nil
					p.deleted = nil
				}
				if tc.name == "write failure" {
					p.failure = outage
				}
				r := NewRepository(p)
				r.collection = &mongo.Collection{}
				post := validPostSnapshot()
				before := copyPost(post)
				var err error
				switch op {
				case "update":
					_, err = r.UpdatePost(context.Background(), post)
				case "soft delete":
					err = r.SoftDeletePost(context.Background(), post, "actor")
				case "hard delete":
					err = r.DeletePost(context.Background(), post.Id)
				}
				require.Equal(t, tc.want, err)
				require.Equal(t, before, post)
				require.Equal(t, 1, p.calls)
				require.Equal(t, bson.M{"_id": "target"}, p.filter)
			})
		}
	}
	for _, tc := range []struct {
		name   string
		result *mongo.UpdateResult
	}{
		{"upsert", &mongo.UpdateResult{MatchedCount: 1, UpsertedCount: 1, Acknowledged: true}}, {"impossible modification", &mongo.UpdateResult{MatchedCount: 0, ModifiedCount: 1, Acknowledged: true}},
		{"contradictory upsert ID", &mongo.UpdateResult{MatchedCount: 1, UpsertedID: "other", Acknowledged: true}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, ErrPostUnavailable, postUpdateOutcome(tc.result, nil)) })
	}
}

// isolatedPostMongo owns only its random database and disconnects its client.
// The URI must name a disposable test service, never application data.
func isolatedPostMongo(t *testing.T) (*Repository, *mongo.Database, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI for MongoDB integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("ghatd_post_test_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	t.Cleanup(func() {
		clean, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, db.Drop(clean))
		require.NoError(t, client.Disconnect(clean))
	})
	core, err := repository.NewMongoDbRepositoryFromDatabase(db, repository.NewZapRepositoryLogger())
	require.NoError(t, err)
	return NewRepository(core), db, ctx
}

func TestPostMongoAbsenceAndWriteOutcomes(t *testing.T) {
	for _, op := range []string{"id", "nano", "slug", "update", "soft delete", "hard delete"} {
		for _, present := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/present=%v", op, present), func(t *testing.T) {
				r, db, ctx := isolatedPostMongo(t)
				input := validPostSnapshot()
				input.NanoId = "nano"
				before := copyPost(input)
				if present {
					_, err := db.Collection(PostCollection).InsertOne(ctx, input)
					require.NoError(t, err)
				}
				var err error
				switch op {
				case "id":
					_, err = r.GetPostById(ctx, input.Id)
				case "nano":
					_, err = r.GetPostByNanoId(ctx, input.NanoId)
				case "slug":
					_, err = r.GetPostByUrlFriendlyId(ctx, input.UrlFriendlyId)
				case "update":
					_, err = r.UpdatePost(ctx, input)
				case "soft delete":
					err = r.SoftDeletePost(ctx, input, "actor")
				case "hard delete":
					err = r.DeletePost(ctx, input.Id)
				}
				if present {
					require.NoError(t, err)
				} else {
					require.Equal(t, ErrResourceNotFound, err)
				}
				require.Equal(t, before, input)
				count, countErr := db.Collection(PostCollection).CountDocuments(ctx, bson.M{})
				require.NoError(t, countErr)
				want := int64(0)
				if present && op != "hard delete" {
					want = 1
				}
				require.Equal(t, want, count)
				if present && (op == "update" || op == "soft delete") {
					var stored Post
					require.NoError(t, db.Collection(PostCollection).FindOne(ctx, bson.M{"_id": input.Id}).Decode(&stored))
					if op == "update" {
						require.NotEmpty(t, stored.UpdatedAt)
					} else {
						require.NotEmpty(t, stored.DeletedAt)
						require.Equal(t, "actor", stored.DeletedByUserId)
					}
				}
			})
		}
	}
}

func TestPostMongoDecodeFailureIsNotAbsence(t *testing.T) {
	for _, selector := range []string{"id", "nano", "slug"} {
		t.Run(selector, func(t *testing.T) {
			r, db, ctx := isolatedPostMongo(t)
			_, err := db.Collection(PostCollection).InsertOne(ctx, bson.M{"_id": "target", "_nano_id": "nano", "_url_friendly_id": "faq-original", "title": bson.M{"invalid": "private"}})
			require.NoError(t, err)
			switch selector {
			case "id":
				_, err = r.GetPostById(ctx, "target")
			case "nano":
				_, err = r.GetPostByNanoId(ctx, "nano")
			case "slug":
				_, err = r.GetPostByUrlFriendlyId(ctx, "faq-original")
			}
			require.Error(t, err)
			require.False(t, postAbsent(err))
		})
	}
}

func TestPostMongoUnacknowledgedWrites(t *testing.T) {
	for _, op := range []string{"create", "raw", "update", "soft delete", "hard delete"} {
		t.Run(op, func(t *testing.T) {
			_, db, ctx := isolatedPostMongo(t)
			input := validPostSnapshot()
			input.NanoId = "nano"
			before := copyPost(input)
			if op != "create" && op != "raw" {
				_, err := db.Collection(PostCollection).InsertOne(ctx, input)
				require.NoError(t, err)
			}
			unackDB := db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged()))
			core, err := repository.NewMongoDbRepositoryFromDatabase(unackDB, repository.NewZapRepositoryLogger())
			require.NoError(t, err)
			r := NewRepository(core)
			switch op {
			case "create":
				_, err = r.CreatePost(ctx, input)
			case "raw":
				_, err = r.CreateRawPosts(ctx, input)
			case "update":
				_, err = r.UpdatePost(ctx, input)
			case "soft delete":
				err = r.SoftDeletePost(ctx, input, "actor")
			case "hard delete":
				err = r.DeletePost(ctx, input.Id)
			}
			require.Equal(t, ErrPostUnavailable, err)
			require.Equal(t, before, input)
			// An unacknowledged command may already have written. No rollback or
			// definitive stored-state assertion is valid for this result.
		})
	}
}
