
# **Repository✨**

The [**repository**](./mongo_repository.go) and [**repositoryhelpers**](./helpers) packages provide an extensible foundation for your data access layer, focusing on best practices for observability, testability, and error handling when interacting with  MongoDB.

## Transaction-safe operations

`repositoryhelpers.WithTopology(replicaSet, directConnection)` merges explicit
topology into `Config.ConnectionString` before `BuildClientOptions`. Other URI
options survive; a nonempty replica set or `true` direct connection overrides
that URI option, while blank/false leaves existing selections unchanged. The
helper adds the slash required before a Mongo query and performs no I/O. URI
parse errors are left untouched for driver validation. It mutates the configured
URI; do not log credential-bearing connection strings. Hosts own environment
parsing and the decision to enable direct connection. A nil config is inert.

Reuse the host's `MongoDbRepository`. When an integration receives an existing
`*mongo.Database`, `NewMongoDbRepositoryFromDatabase(db, logger)` adapts it without
creating or taking ownership of another client. Its manager cannot reconnect,
and closing the borrowed adapter leaves the original client open. Its database
getter rejects a different database name. Connection statistics are unavailable
for that borrowed adapter; zero counters are not measured pool activity.

`WithMongoTransaction(ctx, db, callback)` supplies the same database client's
session with snapshot reads, primary selection and majority writes. Existing
caller sessions are rejected, not silently replaced. Return database errors and
use the callback context and same client for every operation. The driver may
retry the callback: do not call providers, send email or publish a result inside
it. Do not manually commit, abort or end its session. An explicitly aborted
callback cannot return success through this helper.

Cancellation observed before commit rejects the callback result. Cancellation
during commit still requires reconciliation of an uncertain outcome.

Use a bounded caller context. The driver owns transient retries and uncertain
commit resolution, which may outlive cancellation. Session cleanup uses a
detached, five-second context. An error does **not** prove that an uncertain
commit never happened; retain idempotency keys and reconcile unknown outcomes.
This helper is not a hard wall-clock deadline or a resource-authorization check.

- `ExecuteReplaceOneCommandResult` retains `MatchedCount` and upsert results.
- `ExecuteUpdateOneCommandResult` retains matched/modified counts.
- `ExecuteFindOneAndUpdateCommandDecodeResult` atomically updates and decodes
  one selected document image using the caller's collection codec registry.
- `ExecuteDeleteOneCommandResult` retains `DeletedCount`.
- `ExecuteCountDocuments`, `ExecuteFindCommand` and cursor mappers preserve native
  error causes underneath their existing repository error codes.
- `EnsureMongoCollection` accepts an existing namespace without altering it.
- `EnsureMongoIndexes` creates explicit domain-owned definitions, never dropping
  or silently repairing conflicting indexes.
- `ProbeMongoTransactions` inserts, reads and deletes one random probe atomically
  in a precreated collection. It verifies actual transaction support at startup
  and leaves no committed document. It requires read/write/delete privileges.
  Choose a collection that permits an `_id`-only probe document; strict domain
  validators or unrelated unique constraints may require a separate probe collection.

Inspect result counts in the owning domain: a zero-match CAS is not a driver
error, and a successful no-op need not increment `ModifiedCount`. The older
error-only mutation helpers remain source-compatible; they cannot report CAS
success. No methods were added to the existing `CommonOperations` interface.
Map application errors after transaction retry decisions whenever possible.

### Atomic update and selected document image

Use `ExecuteFindOneAndUpdateCommandDecodeResult` when a domain must return the
revision written by that operation. A separate update followed by a read can
observe a later concurrent writer. Pass a non-nil pointer as the destination and
include the expected revision and resource constraints in the filter:

```go
var updated Record
err := repo.ExecuteFindOneAndUpdateCommandDecodeResult(
    ctx, collection,
    bson.M{"_id": recordID, "revision": expectedRevision},
    bson.M{"$set": changes, "$inc": bson.M{"revision": 1}},
    &updated,
    options.FindOneAndUpdate().SetReturnDocument(options.After),
)
```

`Record`, `changes` and revision policy belong to the calling domain. The helper
passes options unchanged: by default MongoDB returns the **before** image, and
upsert is off. `SetReturnDocument(options.After)` returns the **after** image.
Projection, sort, update pipelines and explicit upsert retain their driver
semantics. See the [MongoDB compound-operation documentation](https://www.mongodb.com/docs/drivers/go/current/fundamentals/crud/compound-operations/).

Native errors, duplicate-key information and transaction labels are preserved.
`mongo.ErrNoDocuments` can mean a missing match or stale revision; the domain
decides its HTTP/error-map meaning. With an explicitly enabled **before-image
upsert**, the same error can accompany a successful insert because no previous
document existed. Do not interpret every error as proof that no write occurred.

Writes without an acknowledged receipt return `ErrUnacknowledgedMongoWrite`,
not success or an authoritative `mongo.ErrNoDocuments`. Their destination is
left untouched: neither the image nor absence from an unacknowledged result is
reliable. Other native driver failures still retain precedence and identity.
Use acknowledged writes for conditional mutations and reconcile uncertain
outcomes; this sentinel does not establish rollback or make a retry safe.
The helper logs an absent acknowledged image as `no_document_image`, not as
proof that no mutation occurred: a before-image upsert may have inserted a row.

Nil/non-pointer destinations are rejected before driver work, but a valid pointer
can still fail BSON decoding **after the write**. Discard partially decoded data.
Return errors from transaction callbacks so the transaction owner can abort or
retry; outside transactions, reconcile ambiguous writes instead of retrying them
blindly. The helper does not invent an idempotency key or start a transaction.
It uses the caller's session without substituting another client. No method was
added to `CommonOperations` or `RepositoryHelper`; custom adapters can opt into
this capability through a narrow interface without changing legacy adapters.

The repository does not own schemas, revision fields, indexes, grant policy or
retention. Explicit collections/databases must belong to the intended host
client. The driver rejects foreign sessions; the helper does not infer tenant
authorization from a database or collection handle.

### Logging and compatibility

Automatic CRUD, decode, setup and transaction logs contain only fixed `operation`
and `outcome` fields. They omit filters, pipelines, document/replacement bytes,
collection/object names and raw driver errors. Duplicate-key errors may contain
private values; even a custom logger receives no raw error. This deliberately
changes log content, and missing-document lookups now use debug-level telemetry.
Update log consumers rather than restoring sensitive payload fields.

Errors returned to trusted callers retain their original causes for
`errors.Is`/`errors.As`, duplicate-key classification and transaction labels.
Do not expose their raw messages in HTTP responses. Explicit `Log*` calls,
connection-manager hooks and host-configured Mongo command monitors are separate
surfaces: their callers must also enforce appropriate redaction.

`MapAllInCursorToResult` consumes/closes its cursor; `MapOneInCursorToResult`
closes it with bounded detached cleanup and distinguishes iteration failure from
absence. Code that consumes cursors directly remains responsible for closing
them. See [the storage boundary decision](../../docs/adr/adr026-shared-storage-primitives.md).


## **🚀 Getting Started**

The core of the new structure is the **RepositoryHelper** interface, which handles all database access (client/database retrieval) and utility functions (logging, mapping).

See [`mongo_examples.go`](./mongo_examples.go) for additional executable
examples of the repository and monitoring APIs.

### **1. Use the Mongo runtime helper**

For a GHATD host application, `NewMongoRuntime` keeps the common bootstrap in
one place: URI generation, handler creation, optional warmup, cleanup, and the
core Mongo repository used by `starter/v0`.

```Go
mongoRuntime, err := repository.NewMongoRuntime(context.Background(), &repository.NewMongoRuntimeRequest{
	URIConfig: repositoryhelpers.MongoURIConfig{
		Username: os.Getenv("MONGO_DB_USERNAME"),
		Password: os.Getenv("MONGO_DB_PASSWORD"),
		Host:     os.Getenv("MONGO_DB_HOST"),
		AppName:  os.Getenv("MONGO_DB_APP_NAME"),
		Atlas:    os.Getenv("MONGO_DB_ATLAS") == "true",
	},
	Database: os.Getenv("MONGO_DB_NAME"),
	Options: []repositoryhelpers.ConfigOption{
		repositoryhelpers.WithConnectionPool(200, 10, 15*time.Minute),
		repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 120*time.Second),
		repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),
	},
})
if err != nil {
	log.Fatal(err)
}
defer mongoRuntime.Close(context.Background())

coreRepository := mongoRuntime.CoreRepository
```

### **2. Build a MongoDB URI**

Use the URI helpers when a project collects MongoDB settings as separate
environment variables. They keep Atlas and non-Atlas URI generation in one
well-tested place, including URL encoding for credentials and `appName`.

```Go
mongoURI, err := repositoryhelpers.GenerateMongoURI(repositoryhelpers.MongoURIConfig{
	Username: os.Getenv("MONGO_DB_USERNAME"),
	Password: os.Getenv("MONGO_DB_PASSWORD"),
	Host:     os.Getenv("MONGO_DB_HOST"),
	AppName:  os.Getenv("MONGO_DB_APP_NAME"),
	Atlas:    os.Getenv("MONGO_DB_ATLAS") == "true",
})
if err != nil {
	log.Fatal(err)
}
```

For lower-level composition, use `GenerateGenericMongoURI` or
`GenerateAtlasMongoURI` directly.

MongoDB migrations use the same URI helpers but intentionally create an
isolated client and lifecycle through the shared
[`external/migrator/mongo`](../migrator/mongo/README.md) command. They do not
reuse an application's `MongoRuntime` or repository client. See
[Managing MongoDB Migrations](../../docs/how-to/manage-mongodb-migrations.md)
for host registration, configuration, deployment, and rollback guidance.

### **3. Initialise the Repository Helper**

Create a handler with the required configuration options, then use it to create
the core repository.

```Go
func main() {  
	// 1. Create a fully configured MongoDB handler  
	mongoHandler, err := repositoryhelpers.NewHandlerWithOptions(  
		mongoURI,  
		os.Getenv("DATABASE_NAME"),  
		// Configure a production-ready connection pool  
		repositoryhelpers.WithConnectionPool(200, 10, 15*time.Minute),  
		// Set critical timeouts  
		repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 30*time.Second),  
		// Enable retry and monitoring policies  
		repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),  
		repositoryhelpers.WithMonitoring(  
			repositoryhelpers.NewLoggingHook(log.Default(), []string{}),
			repositoryhelpers.NewMetricsHook(),  
		),  
	)  
	if err != nil {  
		log.Fatal(err)  
	}  
	defer mongoHandler.Close(context.Background())

	// 2. Create the core repository, using the initiated handler  
	coreRepository := repository.NewMongoDbRepositoryWithDefaults(  
		mongoHandler,  
		os.Getenv("DATABASE_NAME"),  
	)

	// 3. Inject the domain repositories  
	userRepo := NewUserRepository(coreRepository)  
}
```

### **4. Inject and Use in Repositories**

In your application's domain repositories (like `UserRepository`), you inject and use a `MongoDbStore` interface (shape as needed) to perform all database interactions, logging, and result mapping.

#### **Example: FindUsers**

This example illustrates how to utilise the core repository's helper methods to interact with the underlying database. The provided methods come preconfigured with structured error and information logging, as well as MapAllToResult for more concise and robust code.

> You can however use the helper methods directly and make your own wrappers.

```Go

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	LogInfo(ctx context.Context, message string, err error, fields ...repository.Field)
}

// UserRepository represents the datastore to hold resource data
type UserRepository struct {
	Store MongoDbStore
}

// NewUserRepository ....

func (r *UserRepository) FindUsers(ctx context.Context) ([]User, error) {  

    // Initatilises the client (if needed)
    _, err := r.Store.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

    // Get the database instance 
	db, err := r.Store.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	collection := db.Collection("users")

    var users []User  
    findOptions := options.Find()

    // Use Find wrapper method
    cursor, err := r.Store.ExecuteFindCommand(ctx, collection, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}

    // Map result to slice 
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &users, "users"); err != nil {
		return nil, err
	}
 
	// Log success with metrics  
	r.Store.LogInfo(ctx, "Successfully retrieved users", nil,  
		repository.Field{Key: "operation", Value: "find_users"},
		repository.Field{Key: "count", Value: len(users)},
	)

	return users, nil  
}
```
