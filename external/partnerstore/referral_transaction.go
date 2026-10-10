package partnerstore

import (
	"context"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// partitionStore keeps owning reads/writes on the existing transaction and
// restricts nested writes to its selected domain partition guard.
type partitionStore struct {
	tx        recordstore.Tx
	partition string
}

// Read runs fn with the underlying transaction, validating context and callback
// presence first; it opens no new transaction of its own.
func (s partitionStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	if err := validStoreContext(ctx); err != nil {
		return err
	}
	if fn == nil || nilStoreDependency(s.tx) {
		return recordstore.ErrInvalid
	}
	return fn(s.tx)
}

// Transact forwards to Read only when the requested partition key matches the
// store's selected partition; any other key is rejected as ErrInvalid rather
// than opening a new transaction.
func (s partitionStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	if key != s.partition {
		return recordstore.ErrInvalid
	}
	return s.Read(ctx, fn)
}

// WithAttributionTransaction uses the same customer guard as ownership CAS.
// Payment identity remains globally unique, including competing customers.
func (r *ReferralRepository) WithAttributionTransaction(ctx context.Context, program, customer string, fn func(referral.Repository) error) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ErrUnavailable
	}
	if program != referral.ProgramID || customer == "" || len(customer) > 256 || fn == nil {
		return referral.ErrInvalid
	}
	if _, nested := r.store.(partitionStore); nested {
		return referral.ErrInvalid
	}
	partition := referralPartition(customer)
	return referralError(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		return fn(&ReferralRepository{store: partitionStore{tx: tx, partition: partition}})
	}))
}
