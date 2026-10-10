package telenumcoder

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/ooaklee/ghatd/external/catalogue"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

//go:embed seeds.json
var seedData []byte

// Seeds decodes the embedded country seed data into Country rows, returning any
// JSON unmarshalling error.
func Seeds() ([]Country, error) {
	var rows []Country
	err := json.Unmarshal(seedData, &rows)
	return rows, err
}

// Migrate is package-owned and insertion-only; reruns preserve administrator edits.
func Migrate(ctx context.Context, db *mongo.Database) error {
	if err := EnsureIndexes(ctx, db); err != nil {
		return err
	}
	repo, err := NewMongoRepository(db)
	if err != nil {
		return err
	}
	service, err := NewService(repo, catalogue.RealClock{})
	if err != nil {
		return err
	}
	return service.Migrate(ctx)
}

// Migrate seeds the catalogue with the package's embedded country rows via the
// insertion-only Seed path, preserving existing administrator edits on reruns.
func (s *Service) Migrate(ctx context.Context) error {
	rows, err := Seeds()
	if err != nil {
		return err
	}
	return s.Seed(ctx, rows)
}
