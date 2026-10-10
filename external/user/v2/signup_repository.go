package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// GetSignupAttribution loads a customer's stored attribution for the program,
// returning ErrUserNotFound when the account or matching attribution is absent
// and ErrSignupEvidenceUnavailable on ownership mismatch, with the stored
// creation time restored to UTC.
func (r *Repository) GetSignupAttribution(ctx context.Context, program, customer string) (SignupAttribution, error) {
	if err := r.checkSignupQuery(ctx, program, customer); err != nil {
		return SignupAttribution{}, err
	}
	u, err := r.GetUserByID(ctx, customer)
	if err != nil {
		return SignupAttribution{}, err
	}
	if u == nil || u.SignupAttribution == nil || u.SignupAttribution.ProgramID != program {
		return SignupAttribution{}, ErrUserNotFound
	}
	if u.SignupAttribution.CustomerID != customer {
		return SignupAttribution{}, ErrSignupEvidenceUnavailable
	}
	return restoredSignupTime(*u.SignupAttribution)
}

// PendingSignupAttributions returns up to limit pending attributions for a
// program from the beginning of the feed; it forwards to
// PendingSignupAttributionsAfter with an empty cursor.
func (r *Repository) PendingSignupAttributions(ctx context.Context, program string, limit int) ([]SignupAttribution, error) {
	return r.PendingSignupAttributionsAfter(ctx, program, "", limit)
}

// PendingSignupAttributionsAfter returns up to 200 pending attributions with
// IDs strictly after afterCustomer, ordered by ID and projected to attribution
// fields only. Malformed cursors, nil cursors or ownership mismatches yield
// signup evidence errors.
func (r *Repository) PendingSignupAttributionsAfter(ctx context.Context, program, afterCustomer string, limit int) ([]SignupAttribution, error) {
	if afterCustomer != strings.TrimSpace(afterCustomer) || len(afterCustomer) > 256 || strings.ContainsAny(afterCustomer, "\r\n\x00") {
		return nil, ErrSignupEvidenceInvalid
	}
	if limit < 1 || limit > 200 {
		return nil, ErrSignupEvidenceInvalid
	}
	if err := r.checkSignupQuery(ctx, program, "feed"); err != nil {
		return nil, err
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"signup_attribution.program_id": program, "signup_attribution.state": "pending"}
	if afterCustomer != "" {
		filter["_id"] = bson.M{"$gt": afterCustomer}
	}
	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)).SetProjection(bson.M{"_id": 1, "signup_attribution": 1}))
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrSignupEvidenceUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = cursor.Close(cleanup)
	}()
	var users []UniversalUser
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	out := make([]SignupAttribution, 0, len(users))
	for _, u := range users {
		if u.SignupAttribution == nil || u.SignupAttribution.CustomerID != u.ID || u.SignupAttribution.ProgramID != program {
			return nil, ErrSignupEvidenceUnavailable
		}
		fact, err := restoredSignupTime(*u.SignupAttribution)
		if err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	return out, nil
}

// ConsumeSignupAttribution atomically moves a pending attribution to consumed
// with the given receipt. When the conditional update finds no pending
// document, it re-reads and treats an identical prior consumption as success,
// reporting ErrSignupEvidenceConflict for any other state.
func (r *Repository) ConsumeSignupAttribution(ctx context.Context, program, customer string, c SignupConsumption) error {
	if err := r.checkSignupQuery(ctx, program, customer); err != nil {
		return err
	}
	store, ok := r.Store.(atomicUserMongoStore)
	if !ok {
		return ErrSignupEvidenceUnavailable
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return err
	}
	var after UniversalUser
	err = store.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, bson.M{"_id": customer, "signup_attribution.program_id": program, "signup_attribution.state": "pending"}, bson.M{"$set": bson.M{"signup_attribution.state": "consumed", "signup_attribution.consumption": c}}, &after, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if onlySignupAbsence(err) {
		current, loadErr := r.GetSignupAttribution(ctx, program, customer)
		if loadErr != nil {
			return loadErr
		}
		if current.State == "consumed" && current.Consumption != nil && current.Consumption.ReceiptID == c.ReceiptID && current.Consumption.Outcome == c.Outcome && current.Consumption.ActorID == c.ActorID {
			return nil
		}
		return ErrSignupEvidenceConflict
	}
	if err != nil {
		return err
	}
	if after.SignupAttribution == nil || after.SignupAttribution.CustomerID != customer || after.SignupAttribution.Consumption == nil || after.SignupAttribution.Consumption.ReceiptID != c.ReceiptID {
		return ErrSignupEvidenceUnavailable
	}
	return nil
}

// checkSignupQuery validates the context, bounded program and customer
// identifiers, and repository readiness before any signup-evidence storage
// access.
func (r *Repository) checkSignupQuery(ctx context.Context, program, customer string) error {
	if ctx == nil || strings.TrimSpace(program) == "" || len(program) > 128 || strings.TrimSpace(customer) == "" || len(customer) > 256 {
		return ErrSignupEvidenceInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || nilUserDependency(r.Store) {
		return ErrSignupEvidenceUnavailable
	}
	return nil
}

// cloneSignupAttribution returns a copy with a duplicated Consumption pointer
// so callers cannot mutate the original nested receipt.
func cloneSignupAttribution(v SignupAttribution) SignupAttribution {
	if v.Consumption != nil {
		c := *v.Consumption
		v.Consumption = &c
	}
	return v
}

// restoredSignupTime verifies the stored UTC timestamp string round-trips and
// matches the typed CreatedAt to millisecond precision, then returns the
// attribution with a canonical UTC time; mismatches yield
// ErrSignupEvidenceUnavailable.
func restoredSignupTime(v SignupAttribution) (SignupAttribution, error) {
	at, err := time.Parse(time.RFC3339Nano, v.CreatedAtUTC)
	if err != nil || at.IsZero() || v.CreatedAtUTC != at.UTC().Format(time.RFC3339Nano) || !at.Truncate(time.Millisecond).Equal(v.CreatedAt.Truncate(time.Millisecond)) {
		return SignupAttribution{}, ErrSignupEvidenceUnavailable
	}
	v.CreatedAt = at.UTC()
	return cloneSignupAttribution(v), nil
}

// A joined absence and operational failure is an outage, not an empty result.
func onlySignupAbsence(err error) bool {
	for err != nil {
		if err == mongo.ErrNoDocuments {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
