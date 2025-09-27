
# **Repository✨**

The [**repository**](./mongo_repository.go) and [**repositoryhelpers**](./helpers) packages provide an extensible foundation for your data access layer, focusing on best practices for observability, testability, and error handling when interacting with  MongoDB.


## **🚀 Getting Started**

The core of the new structure is the **RepositoryHelper** interface, which handles all database access (client/database retrieval) and utility functions (logging, mapping).

> To see various examples of how these packages can be leverated

### **1. Initialise the Repository Helper**

You create a handler with and configuration as required using the options and then use it to create your core repository.

```Go
func main() {  
	// 1. Create a fully configured MongoDB handler  
	mongoHandler, err := repositoryhelpers.NewHandlerWithOptions(  
		os.Getenv("MONGODB_URI"),  
		os.Getenv("DATABASE_NAME"),  
		// Configure a production-ready connection pool  
		repositoryhelpers.WithConnectionPool(200, 10, 15*time.Minute),  
		// Set critical timeouts  
		repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 30*time.Second),  
		// Enable retry and monitoring policies  
		repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),  
		repositoryhelpers.WithMonitoring(  
			repositoryhelpers.NewLoggingHook(log.Default()),  
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

### **2. Inject and Use in Repositories**

In your application's domain repositories (like `UserRepository`), you inject and use a `MongoDbStore` interface (shape as needed) to perform all database interactions, logging, and result mapping.

#### **Example: FindUsers**

This example illustrates how to utilise the core repository's helper methods to interact with the underlying database. The provided methods come preconfigured with structured error and information logging, as well as MapAllToResult for more concise and robust code.

> You can however use the helper methods directly and make your own wrappers.

```Go

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
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
		Field{Key: "operation", Value: "find_users"},  
		Field{Key: "count", Value: len(users)},  
	)

	return users, nil  
}
```
