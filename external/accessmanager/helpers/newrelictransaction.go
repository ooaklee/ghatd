package accessmanagerhelpers

import (
	"context"

	"github.com/newrelic/go-agent/v3/newrelic"
)

// TransactionKey is the key used to hold the transaction is context
const TransactionKey contextKey = "NewRelicTransaction"

// TransitTransactionWith packages both passed context and new relic transaction to  move
// across processes.
func TransitTransactionWith(ctx context.Context, newRelicTransaction *newrelic.Transaction) context.Context {
	return context.WithValue(ctx, TransactionKey, newRelicTransaction)
}

// AcquireTransactionFrom pulls new relic transaction  from context if exists or returns nil
func AcquireTransactionFrom(ctx context.Context) *newrelic.Transaction {

	newRelicTransaction, ok := ctx.Value(TransactionKey).(*newrelic.Transaction)
	if ok && newRelicTransaction != nil {
		return newRelicTransaction
	}

	return nil

}
