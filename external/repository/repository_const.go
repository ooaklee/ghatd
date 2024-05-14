package repository

const (
	// MongoRegexStringFormat holds format string for case insensitive regex mapping
	// in mongo queries
	MongoRegexStringFormat = ".*%s.*"
)

const (
	logWarn  = "WARN"
	logError = "ERROR"
)

// RepositoryCollection the collection
type RepositoryCollection string

const (

	// AuditCollection collection name for audit events
	AuditCollection RepositoryCollection = "audit"

	// ApiTokenCollection collection name for api tokens
	ApiTokenCollection RepositoryCollection = "apitokens"

	// UserCollection collection name for users
	UserCollection RepositoryCollection = "users"
)

const (
	// ErrKeyResourceNotFound returned when resources cannot be found
	ErrKeyResourceNotFound = "ResourceNotFound"

	// ErrKeyUnableToInitialiseDBClient returned when unable to get back
	// a valid client to run request.
	ErrKeyUnableToInitialiseDBClient = "UnableToInitialiseDBClient"

	// ErrKeyUnableToCountDocuments returned when error occurs while counting documents
	ErrKeyUnableToCountDocuments = "UnableToCountDocuments"

	// ErrKeyUnableToGenerateCollectionCursor returned when error occurs while creating
	// collection cursor
	ErrKeyUnableToGenerateCollectionCursor = "UnableToGenerateCollectionCursor"

	// ErrKeyUnableToDecodeQueriedDocuments returned when pulled documents
	// from mongo DB cannot be unmarshalled into specified struct.
	ErrKeyUnableToDecodeQueriedDocuments = "UnableToDecodeQueriedDocuments"

	// MongoAggregationKeySample holds the key used to get sample in mongo aggregation
	MongoAggregationKeySample = "$sample"

	// MongoAggregationKeySampleOptionSize holds the key used when specifying sample size
	MongoAggregationKeySampleOptionSize = "size"
)
