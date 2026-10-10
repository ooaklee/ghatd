package internationalisationmanagerhelper

import (
	"context"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/currencycoder"
	"github.com/ooaklee/ghatd/external/globalflagger"
	i18n "github.com/ooaklee/ghatd/external/internationalisationmanager"
	"github.com/ooaklee/ghatd/external/telenumcoder"
	"github.com/ooaklee/ghatd/external/timezonecoder"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Config selects explicit native setup; disabled setup ignores Reachability.
// Reachability is trusted host configuration, never a public request value.
type Config struct {
	Enabled      bool
	Reachability telenumcoder.HTTPConfig
}

// InitialiseNative explicitly ensures indexes, seeds missing definitions in
// flags/currency/phone/timezone order and composes owning services over a borrowed
// database. This is seed-writing setup, not a passive constructor or implicit
// migration registration. Existing records, revisions and audit values survive.
//
// Disabled setup returns (nil, nil) without validating unused dependencies.
// Enabled setup validates context/database/reachability before any storage write.
// Failure returns no service; committed partial seed progress may remain and a
// subsequent explicit call can resume. No connection, worker, provider lookup or
// message send is started. Hosts own database lifetime, routes and live security.
func InitialiseNative(ctx context.Context, db *mongo.Database, cfg Config) (*i18n.Service, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if ctx == nil || db == nil {
		return nil, catalogue.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var options []telenumcoder.Option
	if cfg.Reachability.Endpoint != "" {
		provider, err := telenumcoder.NewHTTPReachability(cfg.Reachability)
		if err != nil {
			return nil, err
		}
		options = append(options, telenumcoder.WithReachability(provider))
	}
	for _, migrate := range []func(context.Context, *mongo.Database) error{globalflagger.Migrate, currencycoder.Migrate, telenumcoder.Migrate, timezonecoder.Migrate} {
		if err := migrate(ctx, db); err != nil {
			return nil, err
		}
	}
	currencyRepository, err := currencycoder.NewMongoRepository(db)
	if err != nil {
		return nil, err
	}
	currencies, err := currencycoder.NewService(currencyRepository, nil)
	if err != nil {
		return nil, err
	}
	phoneRepository, err := telenumcoder.NewMongoRepository(db)
	if err != nil {
		return nil, err
	}
	phones, err := telenumcoder.NewService(phoneRepository, nil, options...)
	if err != nil {
		return nil, err
	}
	flagRepository, err := globalflagger.NewMongoRepositoryFromDatabase(db)
	if err != nil {
		return nil, err
	}
	flags := globalflagger.NewService(flagRepository, nil)
	timezoneRepository, err := timezonecoder.NewMongoRepository(db)
	if err != nil {
		return nil, err
	}
	timezones, err := timezonecoder.NewService(timezoneRepository, nil)
	if err != nil {
		return nil, err
	}
	return i18n.NewService(currencies, phones, flags, timezones, nil)
}
