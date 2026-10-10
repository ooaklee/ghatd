package revenuestore

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindLifecycleSourceEpoch         = "billing_lifecycle_source_epoch"
	kindLifecyclePreparationProgress = "billing_lifecycle_preparation_progress"
)

// lifecycleSourceEpoch is the persisted source-write epoch DTO for one scope;
// Schema pins the only recognized encoding. Its record revision, not the
// struct, carries the epoch counter.
type lifecycleSourceEpoch struct {
	Scope  billing.RevenueScope
	Schema int
}

// Explicit encrypted DTO: the domain state deliberately has no public JSON.
type lifecyclePreparationProgress struct {
	Scope                                                  billing.RevenueScope
	Schema                                                 int
	Epoch, Sweeps, Scanned, Selected, CustomerlessPayments int64
	Phase                                                  int
	AfterID                                                string
	PreparedAt                                             time.Time
}

// preparationBound binds one native transaction to a single revenue scope for
// the lifecycle preparation callback; it is valid only inside
// WithLifecyclePreparation.
type preparationBound struct {
	tx    recordstore.Tx
	scope billing.RevenueScope
}

// readLifecycleSourceEpoch returns the scope's epoch as the record revision. A
// genuine single-record absence yields 0 with no error; corrupt or mismatched
// rows become ErrRevenueUnavailable.
func readLifecycleSourceEpoch(ctx context.Context, tx recordstore.Tx, scope billing.RevenueScope) (int64, error) {
	id, part := checkoutScopeKey(scope), lifecycleSourcePartition(scope)
	v, row, err := get[lifecycleSourceEpoch](ctx, tx, kindLifecycleSourceEpoch, id, part)
	if singleCause(err, billing.ErrRevenueNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !discoveryMetadata(row, kindLifecycleSourceEpoch, id, part, false) || v.Scope != scope || v.Schema != 1 {
		return 0, billing.ErrRevenueUnavailable
	}
	return row.Revision, nil
}

// replaceLifecycleSourceEpoch advances the epoch from expected to expected+1
// using Insert when absent and CAS Replace otherwise. Negative, MaxInt64 or
// stale expectations fail without writing.
func replaceLifecycleSourceEpoch(ctx context.Context, tx recordstore.Tx, scope billing.RevenueScope, expected int64) error {
	if expected < 0 || expected == math.MaxInt64 {
		return billing.ErrRevenueUnavailable
	}
	row, err := recordstore.NewRecord(kindLifecycleSourceEpoch, checkoutScopeKey(scope), lifecycleSourcePartition(scope), expected+1, lifecycleSourceEpoch{scope, 1})
	if err != nil {
		return mapped(err)
	}
	if expected == 0 {
		return mapped(tx.Insert(ctx, row))
	}
	return mapped(tx.Replace(ctx, row, expected))
}

// EVERY relevant current source write touches this row in its original native
// transaction. Replay receipts and projection-only preparation do not touch it.
func touchLifecycleSourceEpoch(ctx context.Context, tx recordstore.Tx, scope billing.RevenueScope) error {
	revision, err := readLifecycleSourceEpoch(ctx, tx, scope)
	if err != nil {
		return err
	}
	return replaceLifecycleSourceEpoch(ctx, tx, scope, revision)
}

// WithLifecyclePreparation runs fn inside one scope-named transaction after
// validating scope/cursor shape. Store failures are classified; unexpected
// errors join ErrRevenueUnavailable while conflict, uncertainty, invalid and
// cancellation pass through, and ctx.Err() is returned on success.
func (r *Repository) WithLifecyclePreparation(ctx context.Context, scope billing.RevenueScope, fn func(billing.LifecyclePreparationTx) error) error {
	if ctx == nil || fn == nil {
		return billing.ErrRevenueInvalid
	}
	if _, err := (billing.LifecycleDiscoveryQuery{Scope: scope, Kind: billing.LifecycleCheckoutSources, Limit: 1}).AfterID(); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return billing.ErrRevenueUnavailable
	}
	err := r.store.Transact(ctx, "billing-lifecycle-preparation-v1:"+checkoutScopeKey(scope), func(tx recordstore.Tx) error { return fn(&preparationBound{tx, scope}) })
	if err != nil {
		classified := mapped(err)
		if errors.Is(classified, billing.ErrRevenueConflict) || errors.Is(classified, billing.ErrRevenueUncertain) || errors.Is(classified, billing.ErrRevenueInvalid) || errors.Is(classified, context.Canceled) || errors.Is(classified, context.DeadlineExceeded) {
			return classified
		}
		return errors.Join(billing.ErrRevenueUnavailable, classified)
	}
	return ctx.Err()
}

// State reads preparation progress in the bound transaction. First absence is
// conclusive only when the discovery marker is also absent; a Complete phase
// additionally requires a joined marker whose PreparedAt matches exactly,
// otherwise unavailable/corruption errors are returned.
func (b *preparationBound) State(ctx context.Context) (billing.LifecyclePreparationState, error) {
	id, part := checkoutScopeKey(b.scope), lifecycleSourcePartition(b.scope)
	p, row, err := get[lifecyclePreparationProgress](ctx, b.tx, kindLifecyclePreparationProgress, id, part)
	if err != nil {
		if singleCause(err, billing.ErrRevenueNotFound) {
			_, _, markerErr := get[lifecycleDiscoveryPreparation](ctx, b.tx, kindLifecycleDiscoveryPreparation, id, part)
			if !singleCause(markerErr, billing.ErrRevenueNotFound) {
				return billing.LifecyclePreparationState{}, errOrUnavailable(markerErr)
			}
		}
		return billing.LifecyclePreparationState{}, err
	}
	if !discoveryMetadata(row, kindLifecyclePreparationProgress, id, part, false) || p.Scope != b.scope || p.Schema != 1 {
		return billing.LifecyclePreparationState{}, billing.ErrRevenueUnavailable
	}
	v := billing.LifecyclePreparationState{Scope: p.Scope, Revision: row.Revision, Epoch: p.Epoch, Sweeps: p.Sweeps, Scanned: p.Scanned, Selected: p.Selected, CustomerlessPayments: p.CustomerlessPayments, Phase: p.Phase, AfterID: p.AfterID, PreparedAt: p.PreparedAt}
	ready, marker, err := get[lifecycleDiscoveryPreparation](ctx, b.tx, kindLifecycleDiscoveryPreparation, id, part)
	if v.Phase == billing.LifecyclePreparationComplete {
		if err != nil || !discoveryMetadata(marker, kindLifecycleDiscoveryPreparation, id, part, true) || ready.Scope != b.scope || ready.Schema != 1 || !ready.PreparedAt.Equal(v.PreparedAt) {
			return billing.LifecyclePreparationState{}, discoveryJoinedError(errOrUnavailable(err))
		}
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LifecyclePreparationState{}, errOrUnavailable(err)
	}
	return v, nil
}

// errOrUnavailable converts a nil error into ErrRevenueUnavailable, preserving
// any non-nil error unchanged; used where missing joined evidence must read as
// unavailability, not success.
func errOrUnavailable(err error) error {
	if err == nil {
		return billing.ErrRevenueUnavailable
	}
	return err
}

// Epoch reads the bound scope's lifecycle source epoch, returning 0 on first
// absence and ErrRevenueUnavailable on corrupt rows.
func (b *preparationBound) Epoch(ctx context.Context) (int64, error) {
	return readLifecycleSourceEpoch(ctx, b.tx, b.scope)
}

// Save writes the next preparation state at revision expected+1 via Insert or
// CAS Replace. Mismatched scope or non-contiguous revisions are invalid; stale
// expectations fail at the store.
func (b *preparationBound) Save(ctx context.Context, v billing.LifecyclePreparationState, expected int64) error {
	if v.Scope != b.scope || v.Revision != expected+1 {
		return billing.ErrRevenueInvalid
	}
	p := lifecyclePreparationProgress{v.Scope, 1, v.Epoch, v.Sweeps, v.Scanned, v.Selected, v.CustomerlessPayments, v.Phase, v.AfterID, v.PreparedAt}
	row, err := recordstore.NewRecord(kindLifecyclePreparationProgress, checkoutScopeKey(b.scope), lifecycleSourcePartition(b.scope), v.Revision, p)
	if err != nil {
		return mapped(err)
	}
	if expected == 0 {
		return mapped(b.tx.Insert(ctx, row))
	}
	return mapped(b.tx.Replace(ctx, row, expected))
}

// Complete atomically advances the source epoch, saves the completed state at
// expected+1 and inserts the discovery marker with the same PreparedAt. Non-
// complete phases, zero PreparedAt or an epoch other than current+1 conflict.
func (b *preparationBound) Complete(ctx context.Context, v billing.LifecyclePreparationState, expected int64) error {
	epoch, err := b.Epoch(ctx)
	if err != nil {
		return err
	}
	if v.Phase != billing.LifecyclePreparationComplete || v.PreparedAt.IsZero() || v.Epoch != epoch+1 {
		return billing.ErrRevenueConflict
	}
	if err := replaceLifecycleSourceEpoch(ctx, b.tx, b.scope, epoch); err != nil {
		return err
	}
	if err := b.Save(ctx, v, expected); err != nil {
		return err
	}
	return insert(ctx, b.tx, kindLifecycleDiscoveryPreparation, checkoutScopeKey(b.scope), lifecycleSourcePartition(b.scope), lifecycleDiscoveryPreparation{b.scope, 1, v.PreparedAt})
}

// Retain admits one candidate in the bound scope. Acknowledged checkout intents
// replay exactly (identical retained rows return nil; differences conflict) and
// otherwise retain the original; subscription candidates retain
// subscription/fact/anchor evidence without an intent.
func (b *preparationBound) Retain(ctx context.Context, c billing.LifecycleDiscoveryCandidate) error {
	if c.Scope != b.scope {
		return billing.ErrRevenueConflict
	}
	if c.Intent.ID != "" {
		id := billing.LifecycleDiscoverySourceID(b.scope, billing.LifecycleCheckoutSources, c.Intent.ID)
		old, row, err := get[lifecycleCheckoutSource](ctx, b.tx, kindLifecycleCheckoutSource, id, lifecycleSourcePartition(b.scope))
		want := lifecycleCheckoutSource{b.scope, c.Intent.ID, c.Intent.Fingerprint, c.Intent.SessionID}
		if err == nil {
			if !discoveryMetadata(row, kindLifecycleCheckoutSource, id, lifecycleSourcePartition(b.scope), true) || old != want {
				return billing.ErrRevenueConflict
			}
			return nil
		}
		if !singleCause(err, billing.ErrRevenueNotFound) {
			return err
		}
		return retainLifecycleCheckout(ctx, b.tx, c.Intent, c.Intent.SessionID)
	}
	return retainLifecycleSubscription(ctx, b.tx, lifecycleSubscriptionSource{Scope: b.scope, SubscriptionID: c.SubscriptionID, PrincipalID: c.PrincipalID, CustomerID: c.CustomerID, FactID: c.Fact.ID, FactFingerprint: c.Fact.Fingerprint, AnchorIntentID: c.Anchor.IntentID, AnchorFingerprint: c.Anchor.Fingerprint})
}

// Sources reads ONE bounded legacy partition page per phase. Native global
// history is scanned once per explicit upgrade sweep, never per discovered item.
// The original cursor advances even for other scopes, without exposing them.
func (b *preparationBound) Sources(ctx context.Context, phase int, after string, limit int) ([]billing.LifecyclePreparationRow, error) {
	if phase < 0 || phase > 5 || limit < 1 || limit > 200 {
		return nil, billing.ErrRevenueInvalid
	}
	kinds := []string{kindCheckoutAck, kindFact, kindCheckoutLifecycleAnchor, kindCheckoutPrincipal, kindLifecycleCheckoutSource, kindLifecycleSubscriptionSource}
	kind, part := kinds[phase], checkoutPartition
	state := ""
	if phase == 1 {
		part, state = partition, billing.RevenuePayment
	} else if phase >= 4 {
		part = lifecycleSourcePartition(b.scope)
	}
	rows, err := b.tx.Find(ctx, recordstore.Query{Kind: kind, Partition: part, State: state, AfterID: after, Limit: limit})
	if err != nil {
		return nil, err
	}
	if len(rows) > limit {
		return nil, billing.ErrRevenueUnavailable
	}
	out := make([]billing.LifecyclePreparationRow, 0, len(rows))
	for _, row := range rows {
		if row.ID <= after || row.Kind != kind || row.Partition != part || row.Revision < 1 || (phase != 5 && row.Revision != 1) || row.ExpiresAt != nil {
			return nil, billing.ErrRevenueUnavailable
		}
		after = row.ID
		result := billing.LifecyclePreparationRow{ID: row.ID}
		if phase >= 4 {
			var c billing.LifecycleDiscoveryCandidate
			var err error
			if phase == 4 {
				c, err = joinDiscoveryCheckout(ctx, b.tx, row, b.scope)
			} else {
				c, err = joinDiscoverySubscription(ctx, b.tx, row, b.scope)
			}
			if err != nil {
				return nil, err
			}
			result.Candidate = &c
			out = append(out, result)
			continue
		}
		var source lifecycleSubscriptionSource
		switch phase {
		case 0:
			intent, err := discoveryIntent(ctx, b.tx, row.ID)
			if err != nil {
				return nil, err
			}
			result.OriginalIntent = &intent
			if intent.Scope == b.scope && intent.Request.Mode == paymentprovider.CheckoutModeSubscription {
				c := billing.LifecycleDiscoveryCandidate{ID: billing.LifecycleDiscoverySourceID(b.scope, billing.LifecycleCheckoutSources, intent.ID), Revision: 1, Scope: b.scope, PrincipalID: intent.Request.UserID, Intent: intent}
				result.Candidate = &c
			}
		case 1:
			fact, err := decodeFact(row)
			if err != nil {
				return nil, err
			}
			if row.State != billing.RevenuePayment || fact.Kind != billing.RevenuePayment {
				return nil, billing.ErrRevenueUnavailable
			}
			result.OriginalFact = &fact
			if fact.Scope == b.scope && fact.ProviderCustomerID != "" {
				source = lifecycleSubscriptionSource{Scope: b.scope, SubscriptionID: fact.SubscriptionID, PrincipalID: fact.PrincipalID, CustomerID: fact.ProviderCustomerID, FactID: fact.ID, FactFingerprint: fact.Fingerprint}
			}
		case 2:
			var stored persistedCheckoutLifecycle
			if row.Decode(&stored) != nil {
				return nil, billing.ErrRevenueUnavailable
			}
			a := stored.anchor()
			if a.Validate() != nil || !discoveryMetadata(row, kind, associationKey(anchorScope(a), a.Evidence.SubscriptionID, ""), part, true) {
				return nil, billing.ErrRevenueUnavailable
			}
			if anchorScope(a) == b.scope {
				source = lifecycleSubscriptionSource{Scope: b.scope, SubscriptionID: a.Evidence.SubscriptionID, PrincipalID: a.PrincipalID, CustomerID: a.Evidence.CustomerID, AnchorIntentID: a.IntentID, AnchorFingerprint: a.Fingerprint}
			}
		case 3:
			var paid billing.CheckoutAssociation
			if row.Decode(&paid) != nil || !discoveryMetadata(row, kind, associationKey(paid.Scope, paid.SubscriptionID, ""), part, true) {
				return nil, billing.ErrRevenueUnavailable
			}
			if paid.Scope == b.scope {
				source = lifecycleSubscriptionSource{Scope: b.scope, SubscriptionID: paid.SubscriptionID, PrincipalID: paid.PrincipalID, CustomerID: paid.CustomerID}
			}
		}
		if source.Scope == b.scope {
			// The first native anchor always joins even when scanning a paid source.
			a, err := (&checkoutBound{tx: b.tx}).GetCheckoutLifecycleAnchor(ctx, b.scope, source.SubscriptionID)
			if err == nil {
				source.AnchorIntentID, source.AnchorFingerprint = a.IntentID, a.Fingerprint
			} else if !singleCause(err, billing.ErrRevenueNotFound) {
				return nil, err
			}
			projection, err := recordstore.NewRecord(kindLifecycleSubscriptionSource, lifecycleSubscriptionKey(b.scope, source.SubscriptionID), lifecycleSourcePartition(b.scope), 1, source)
			if err != nil {
				return nil, err
			}
			c, err := joinDiscoverySubscription(ctx, b.tx, projection, b.scope)
			if err != nil {
				return nil, err
			}
			result.Candidate = &c
		}
		out = append(out, result)
	}
	return out, nil
}

var _ billing.LifecyclePreparationRepository = (*Repository)(nil)
var _ billing.LifecyclePreparationTx = (*preparationBound)(nil)
