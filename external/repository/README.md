
# **Repository✨**

The [**repository**](./mongo_repository.go) and [**repositoryhelpers**](./helpers) packages provide an extensible foundation for your data access layer, focusing on best practices for observability, testability, and error handling when interacting with  MongoDB.


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
