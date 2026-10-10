package telenumcoder

import (
	"context"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const Collection = "i18n_phonecodes"

// Repository is the country catalogue repository surface, derived from the
// generic catalogue repository over Country.
type Repository interface{ catalogue.Repository[Country] }

// NewRepository adapts a catalogue store into a Country repository by
// projecting each value's embedded Entry as the catalogue record.
func NewRepository(store cataloguestore.Store) (Repository, error) {
	return cataloguestore.NewRepository(store, func(value *Country) *catalogue.Entry { return &value.Entry })
}

// NewMongoRepository opens the country catalogue store on the given Mongo
// database's Collection and adapts it into a Repository.
func NewMongoRepository(db *mongo.Database) (Repository, error) {
	store, err := cataloguestore.OpenMongo(db, Collection)
	if err != nil {
		return nil, err
	}
	return NewRepository(store)
}

// EnsureIndexes creates the catalogue indexes for the country collection on the
// given Mongo database.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	return cataloguestore.EnsureIndexes(ctx, db, Collection)
}
