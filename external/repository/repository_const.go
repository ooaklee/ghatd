package repository

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
)
