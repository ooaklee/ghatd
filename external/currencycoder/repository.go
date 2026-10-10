package currencycoder

import (
	"context"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const Collection = "i18n_currencies"

// Repository is the typed catalogue repository contract specialised to Currency
// records.
type Repository interface{ catalogue.Repository[Currency] }

// NewRepository adapts a cataloguestore Store into the Currency repository,
// projecting each record's inline entry.
func NewRepository(store cataloguestore.Store) (Repository, error) {
	return cataloguestore.NewRepository(store, func(value *Currency) *catalogue.Entry { return &value.Entry })
}

// NewMongoRepository opens the currency collection over the host's database and
// adapts it into the Currency repository.
func NewMongoRepository(db *mongo.Database) (Repository, error) {
	store, err := cataloguestore.OpenMongo(db, Collection)
	if err != nil {
		return nil, err
	}
	return NewRepository(store)
}

// EnsureIndexes creates the catalogue indexes on the currency collection,
// delegating to the shared cataloguestore migration helper.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	return cataloguestore.EnsureIndexes(ctx, db, Collection)
}
