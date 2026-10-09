package currencycoder

import (
	"context"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const Collection = "i18n_currencies"

type Repository interface{ catalogue.Repository[Currency] }

func NewRepository(store cataloguestore.Store) (Repository, error) {
	return cataloguestore.NewRepository(store, func(value *Currency) *catalogue.Entry { return &value.Entry })
}
func NewMongoRepository(db *mongo.Database) (Repository, error) {
	store, err := cataloguestore.OpenMongo(db, Collection)
	if err != nil {
		return nil, err
	}
	return NewRepository(store)
}
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	return cataloguestore.EnsureIndexes(ctx, db, Collection)
}
