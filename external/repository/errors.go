package repository

import "errors"

var (
	ErrResourceNotFound                 = errors.New(ErrKeyResourceNotFound)
	ErrUnableToCountDocuments           = errors.New(ErrKeyUnableToCountDocuments)
	ErrUnableToDecodeQueriedDocuments   = errors.New(ErrKeyUnableToDecodeQueriedDocuments)
	ErrUnableToGenerateCollectionCursor = errors.New(ErrKeyUnableToGenerateCollectionCursor)
	ErrUnableToInitialiseDBClient       = errors.New(ErrKeyUnableToInitialiseDBClient)
)
