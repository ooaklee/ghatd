package repository

import (
	"context"
	"log"
	"time"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Example: How to migrate from old to new structure

// NEW WAY - Extensible approach
func ExampleNewWayBasic() (*MongoDbRepository, error) {
	connectionString := "mongodb://localhost:27017"
	database := "myapp"

	// Create extensible handler with default configuration
	mongoHandler, err := repositoryhelpers.NewHandlerWithOptions(
		connectionString,
		database,
	)
	if err != nil {
		return nil, err
	}

	// Create extensible repository with default zap logger
	repo := NewMongoDbRepositoryWithDefaults(mongoHandler, database)

	return repo, nil
}

// NEW WAY - Production configuration
func ExampleNewWayProduction() (*MongoDbRepository, error) {
	connectionString := "mongodb+srv://user:pass@cluster.mongodb.net/production"
	database := "production_db"

	// Create monitoring hooks
	loggingHook := repositoryhelpers.NewLoggingHook(log.Default(), []string{"pass"})
	metricsHook := repositoryhelpers.NewMetricsHook()
	circuitBreaker := repositoryhelpers.NewCircuitBreakerHook(5, 30*time.Second)

	// Create extensible handler with production settings
	mongoHandler, err := repositoryhelpers.NewHandlerWithOptions(
		connectionString,
		database,
		repositoryhelpers.WithConnectionPool(200, 10, 15*time.Minute),
		repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 30*time.Second),
		repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),
		repositoryhelpers.WithReadPreference(readpref.SecondaryPreferred()),
		repositoryhelpers.WithMonitoring(loggingHook, metricsHook, circuitBreaker),
	)
	if err != nil {
		return nil, err
	}

	// Create extensible repository
	repo := NewMongoDbRepositoryWithDefaults(mongoHandler, database)

	return repo, nil
}

// Usage Example: How to use the new utilities
func ExampleUsingNewUtilities() error {
	repo, err := ExampleNewWayBasic()
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Get database
	db, err := repo.GetDatabase(ctx, "")
	if err != nil {
		return err
	}

	collection := db.Collection("users")

	// Find documents
	cursor, err := collection.Find(ctx, map[string]interface{}{})
	if err != nil {
		// Use new structured logging
		repo.LogError(ctx, "Failed to find users", err,
			Field{Key: "collection", Value: "users"},
			Field{Key: "operation", Value: "find"},
		)
		return err
	}
	defer cursor.Close(ctx)

	// Map cursor to results using new utility
	var users []map[string]interface{}
	if err := repo.MapAllInCursorToResult(ctx, cursor, &users, "users"); err != nil {
		return err // Error already logged by the utility
	}

	// Log success with structured fields
	repo.LogInfo(ctx, "Successfully retrieved users", nil,
		Field{Key: "count", Value: len(users)},
		Field{Key: "operation", Value: "find_users"},
	)

	return nil
}

// Migration Example: Updating existing repository methods
type UserRepository struct {
	repo RepositoryHelper // Use interface for testability
}

func NewUserRepository(repo RepositoryHelper) *UserRepository {
	return &UserRepository{repo: repo}
}

// NEW METHOD - After migration
func (r *UserRepository) FindUsers(ctx context.Context) ([]User, error) {
	// New way with structured logging and better error handling
	db, err := r.repo.GetDatabase(ctx, "")
	if err != nil {
		return nil, err // Error already logged by helper
	}

	collection := db.Collection("users")

	cursor, err := collection.Find(ctx, map[string]interface{}{})
	if err != nil {
		r.repo.LogError(ctx, "Failed to find users", err,
			Field{Key: "collection", Value: "users"},
			Field{Key: "operation", Value: "find"},
		)
		return nil, NewRepositoryError(ErrKeyUnableToGenerateCollectionCursor, "failed to create cursor")
	}
	defer cursor.Close(ctx)

	var users []User
	if err := r.repo.MapAllToResult(ctx, cursor, &users, "users"); err != nil {
		return nil, err // Error already logged by helper
	}

	// Log successful operation
	r.repo.LogInfo(ctx, "Successfully retrieved users", nil,
		Field{Key: "count", Value: len(users)},
		Field{Key: "operation", Value: "find_users"},
	)

	return users, nil
}

// Testing Example: Easy to mock with interfaces
type MockRepositoryHelper struct{}

func (m *MockRepositoryHelper) GetClient(ctx context.Context) (*mongo.Client, error) {
	// Return mock client or error for testing
	return nil, nil
}

func (m *MockRepositoryHelper) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	// Return mock database
	return nil, nil
}

func (m *MockRepositoryHelper) MapAllToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error {
	// Mock cursor mapping
	return nil
}

func (m *MockRepositoryHelper) MapOneToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error {
	return nil
}

func (m *MockRepositoryHelper) LogError(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) LogWarn(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) LogInfo(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) LogDebug(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) Error(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) Warn(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) Info(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) Debug(ctx context.Context, message string, err error, fields ...Field) {
}
func (m *MockRepositoryHelper) Health(ctx context.Context) map[string]interface{} { return nil }
func (m *MockRepositoryHelper) Stats() repositoryhelpers.ConnectionStats {
	return repositoryhelpers.ConnectionStats{}
}

func (m *MockRepositoryHelper) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	return 0, nil
}
func (m *MockRepositoryHelper) ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	return nil
}
func (m *MockRepositoryHelper) ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {
	return nil
}
func (m *MockRepositoryHelper) ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {
	return nil
}
func (m *MockRepositoryHelper) ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	return nil
}
func (m *MockRepositoryHelper) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	return nil
}
func (m *MockRepositoryHelper) ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error {
	return nil
}
func (m *MockRepositoryHelper) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	return nil, nil
}
func (m *MockRepositoryHelper) ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error) {
	return nil, nil
}

func (m *MockRepositoryHelper) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	return nil, nil
}
func (m *MockRepositoryHelper) ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error) {
	return nil, nil
}

func ExampleTestingWithMocks() {
	// Easy to create mocks for testing
	mockHelper := &MockRepositoryHelper{}
	userRepo := NewUserRepository(mockHelper)

	// Test with mock
	users, err := userRepo.FindUsers(context.Background())
	_ = users
	_ = err
}

// Configuration Example: Environment-specific setups
func ExampleEnvironmentConfigs() {
	// Development configuration
	devRepo := func() (*MongoDbRepository, error) {
		handler, err := repositoryhelpers.NewHandlerWithOptions(
			"mongodb://localhost:27017",
			"dev_db",
			repositoryhelpers.WithConnectionPool(10, 1, 5*time.Minute),
			repositoryhelpers.WithTimeouts(30*time.Second, 10*time.Second, 60*time.Second),
			repositoryhelpers.WithMonitoring(repositoryhelpers.NewLoggingHook(log.Default(), []string{})),
		)
		if err != nil {
			return nil, err
		}
		return NewMongoDbRepositoryWithDefaults(handler, "dev_db"), nil
	}

	// Production configuration
	prodRepo := func() (*MongoDbRepository, error) {
		handler, err := repositoryhelpers.NewHandlerWithOptions(
			"mongodb+srv://production-cluster",
			"prod_db",
			repositoryhelpers.WithConnectionPool(200, 20, 15*time.Minute),
			repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 30*time.Second),
			repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),
			repositoryhelpers.WithMonitoring(
				repositoryhelpers.NewLoggingHook(log.Default(), []string{}),
				repositoryhelpers.NewMetricsHook(),
				repositoryhelpers.NewCircuitBreakerHook(3, 30*time.Second),
			),
		)
		if err != nil {
			return nil, err
		}
		return NewMongoDbRepositoryWithDefaults(handler, "prod_db"), nil
	}

	// Test configuration
	testRepo := func() (*MongoDbRepository, error) {
		handler, err := repositoryhelpers.NewHandlerWithOptions(
			"mongodb://localhost:27017",
			"test_db",
			repositoryhelpers.WithConnectionPool(5, 1, 1*time.Minute),
		)
		if err != nil {
			return nil, err
		}
		// Use no-op logger for tests
		logger := NewNoOpRepositoryLogger()
		helper := NewMongoRepositoryHelper(handler, logger, "test_db")
		return &MongoDbRepository{helper: helper}, nil
	}

	_, _ = devRepo, prodRepo
	_, _ = testRepo()
}

// User struct for examples
type User struct {
	ID   string `bson:"_id"`
	Name string `bson:"name"`
}

/////////////////////////////////////////
/// Handler Config Examples
/////////////////////////////////////////

// Handler example usage scenarios

// Example 1: Basic usage with default configuration
func ExampleHandlerBasicUsage() (*repositoryhelpers.Handler, error) {
	connectionString := "mongodb://localhost:27017"
	database := "myapp"

	handler, err := repositoryhelpers.NewHandlerWithOptions(
		connectionString,
		database,
	)
	if err != nil {
		return nil, err
	}

	return handler, nil
}

// Example 2: Production configuration with connection pooling and timeouts
func ExampleHandlerProductionConfig() (*repositoryhelpers.Handler, error) {
	connectionString := "mongodb://user:pass@cluster.mongodb.net/myapp?retryWrites=true&w=majority"
	database := "production_db"

	handler, err := repositoryhelpers.NewHandlerWithOptions(
		connectionString,
		database,
		// Connection pool settings for high-load production
		repositoryhelpers.WithConnectionPool(200, 10, 15*time.Minute),
		// Aggressive timeouts for production
		repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 30*time.Second),
		// Enable retry policies
		repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),
		// Use secondary read preference for read operations
		repositoryhelpers.WithReadPreference(readpref.SecondaryPreferred()),
	)
	if err != nil {
		return nil, err
	}

	return handler, nil
}

// Example 3: Development configuration with extensive logging
func ExampleHandlerDevelopmentConfig() (*repositoryhelpers.Handler, error) {
	connectionString := "mongodb://localhost:27017"
	database := "development_db"

	// Create monitoring hooks
	loggingHook := repositoryhelpers.NewLoggingHook(log.Default(), []string{})
	metricsHook := repositoryhelpers.NewMetricsHook()

	handler, err := repositoryhelpers.NewHandlerWithOptions(
		connectionString,
		database,
		// Smaller connection pool for development
		repositoryhelpers.WithConnectionPool(20, 2, 5*time.Minute),
		// More lenient timeouts for debugging
		repositoryhelpers.WithTimeouts(30*time.Second, 10*time.Second, 60*time.Second),
		// Add monitoring for debugging
		repositoryhelpers.WithMonitoring(loggingHook, metricsHook),
	)
	if err != nil {
		return nil, err
	}

	return handler, nil
}

// Example 4: High-availability configuration with circuit breaker
func ExampleHandlerHighAvailabilityConfig() (*repositoryhelpers.Handler, error) {
	connectionString := "mongodb+srv://cluster.mongodb.net/myapp"
	database := "ha_database"

	// Create monitoring hooks
	loggingHook := repositoryhelpers.NewLoggingHook(log.Default(), []string{})
	metricsHook := repositoryhelpers.NewMetricsHook()
	circuitBreaker := repositoryhelpers.NewCircuitBreakerHook(5, 30*time.Second)

	handler, err := repositoryhelpers.NewHandlerWithOptions(
		connectionString,
		database,
		// Large connection pool for HA
		repositoryhelpers.WithConnectionPool(500, 50, 30*time.Minute),
		// Fast failure timeouts
		repositoryhelpers.WithTimeouts(2*time.Second, 1*time.Second, 10*time.Second),
		// Aggressive retry policy
		repositoryhelpers.WithRetryPolicy(true, true, 5*time.Second),
		// Primary preferred for consistency
		repositoryhelpers.WithReadPreference(readpref.PrimaryPreferred()),
		// Full monitoring suite
		repositoryhelpers.WithMonitoring(loggingHook, metricsHook, circuitBreaker),
	)
	if err != nil {
		return nil, err
	}

	return handler, nil
}

// Example 5: Custom configuration for specific use case
func ExampleHandlerCustomConfig() (*repositoryhelpers.Handler, error) {
	// Create fully custom configuration
	config := &repositoryhelpers.Config{
		ConnectionString: "mongodb://special-cluster:27017",
		Database:         "custom_db",
	}

	// Custom timeouts
	connectTimeout := 45 * time.Second
	config.ConnectTimeout = &connectTimeout

	// Custom pool settings
	maxPool := uint64(300)
	minPool := uint64(25)
	config.MaxPoolSize = &maxPool
	config.MinPoolSize = &minPool

	// Custom read preference
	config.ReadPreference = readpref.Secondary()

	// Add custom monitoring
	config.MonitoringHooks = []repositoryhelpers.MonitoringHook{
		repositoryhelpers.NewLoggingHook(log.Default(), []string{}),
		repositoryhelpers.NewMetricsHook(),
	}

	handler, err := repositoryhelpers.NewHandler(config)
	if err != nil {
		return nil, err
	}

	return handler, nil
}

// Example 6: Using the handler in application code
func ExampleHandlerApplicationUsage() {
	handler, err := ExampleHandlerProductionConfig()
	if err != nil {
		log.Fatal(err)
	}
	defer handler.Close(context.Background())

	ctx := context.Background()

	// Get database
	db, err := handler.GetDatabase(ctx, "")
	if err != nil {
		log.Fatal(err)
	}

	// Use database
	collection := db.Collection("users")

	// Example operations...
	_ = collection // Use collection for CRUD operations

	// Health check
	health := handler.Health(ctx)
	log.Printf("MongoDB Health: %+v", health)

	// Get statistics
	stats := handler.Stats()
	log.Printf("Connection Stats: %+v", stats)
}

// Example 7: Multiple database connections
func ExampleHandlerMultipleDatabases() error {
	// Primary database handler
	primaryHandler, err := repositoryhelpers.NewHandlerWithOptions(
		"mongodb://primary-cluster:27017",
		"primary_db",
		repositoryhelpers.WithConnectionPool(100, 10, 10*time.Minute),
	)
	if err != nil {
		return err
	}
	defer primaryHandler.Close(context.Background())

	// Analytics database handler (different cluster)
	analyticsHandler, err := repositoryhelpers.NewHandlerWithOptions(
		"mongodb://analytics-cluster:27017",
		"analytics_db",
		repositoryhelpers.WithConnectionPool(50, 5, 15*time.Minute),
		repositoryhelpers.WithReadPreference(readpref.SecondaryPreferred()),
	)
	if err != nil {
		return err
	}
	defer analyticsHandler.Close(context.Background())

	ctx := context.Background()

	// Use both databases
	primaryDB, _ := primaryHandler.GetDatabase(ctx, "")
	analyticsDB, _ := analyticsHandler.GetDatabase(ctx, "")

	_ = primaryDB   // Primary operations
	_ = analyticsDB // Analytics operations

	return nil
}

// Example 8: Testing configuration
func ExampleHandlerTestingConfig() (*repositoryhelpers.Handler, error) {
	// Minimal configuration for tests
	handler, err := repositoryhelpers.NewHandlerWithOptions(
		"mongodb://localhost:27017",
		"test_db",
		repositoryhelpers.WithConnectionPool(5, 1, 1*time.Minute),
		repositoryhelpers.WithTimeouts(1*time.Second, 500*time.Millisecond, 5*time.Second),
	)
	if err != nil {
		return nil, err
	}

	return handler, nil
}
