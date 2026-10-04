package waitlist

import (
	"context"
	"errors"
	"math"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// ErrSequenceState requires operator review of a missing/inconsistent counter or
// unavailable initialization. Enrollment never guesses or reuses an ordinal.
var ErrSequenceState = errors.New("waitlist sequence state is unavailable or inconsistent")

func (s *MongoStore) transaction(ctx context.Context, fn func(context.Context) error) error {
	session, err := s.signups.Database().Client().StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) { return nil, fn(tx) }, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	return err
}

// sequenceState reads one snapshot. Missing counters are acceptable only for a
// never-sequenced audience; deletion or counter drift does not reset allocation.
func (s *MongoStore) sequenceState(ctx context.Context) (int64, bool, error) {
	var last Entry
	err := s.signups.FindOne(ctx, bson.M{"sequence": bson.M{"$gt": 0}}, options.FindOne().SetSort(bson.D{{Key: "sequence", Value: -1}}).SetProjection(bson.M{"sequence": 1})).Decode(&last)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return 0, false, err
	}
	var counter struct {
		Value *int64 `bson:"value"`
	}
	err = s.counters.FindOne(ctx, bson.M{"_id": CampaignID}).Decode(&counter)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if last.Sequence != 0 {
			return 0, false, ErrSequenceState
		}
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if counter.Value == nil || *counter.Value < 0 || *counter.Value != last.Sequence {
		return 0, false, ErrSequenceState
	}
	return *counter.Value, true, nil
}

func (s *MongoStore) initializeSequence(ctx context.Context) error {
	_, err := s.signups.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "sequence", Value: 1}},
		Options: options.Index().SetName("waitlist_signup_sequence").SetUnique(true).SetPartialFilterExpression(bson.M{"sequence": bson.M{"$gt": 0}}),
	})
	if err != nil {
		return err
	}
	// Ensure the counter collection exists before the transaction for older servers.
	if err := s.counters.Database().CreateCollection(ctx, s.counters.Name()); err != nil {
		var command mongo.CommandError
		if !errors.As(err, &command) || command.Code != 48 {
			return err
		}
	}
	err = s.transaction(ctx, func(tx context.Context) error {
		value, exists, err := s.sequenceState(tx)
		if err != nil {
			return err
		}
		if !exists {
			_, err = s.counters.InsertOne(tx, bson.M{"_id": CampaignID, "value": int64(0)})
			return err
		}
		// A write also verifies transactions when the counter already exists.
		_, err = s.counters.UpdateOne(tx, bson.M{"_id": CampaignID}, bson.M{"$set": bson.M{"value": value}})
		return err
	})
	if err == nil {
		s.sequenceReady.Store(true)
	}
	return err
}

func (s *MongoStore) joinSequenced(ctx context.Context, entry Entry) error {
	if !s.sequenceReady.Load() {
		return ErrSequenceState
	}
	return s.transaction(ctx, func(tx context.Context) error {
		err := s.signups.FindOne(tx, bson.M{"_id": entry.ID}).Err()
		if err == nil {
			return nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		current, exists, err := s.sequenceState(tx)
		if err != nil {
			return err
		}
		if !exists || current == math.MaxInt64 {
			return ErrSequenceState
		}
		result, err := s.counters.UpdateOne(tx, bson.M{"_id": CampaignID, "value": current}, bson.M{"$inc": bson.M{"value": int64(1)}})
		if err != nil {
			return err
		}
		if result.MatchedCount != 1 {
			return ErrSequenceState
		}
		saved := entry
		saved.Sequence = current + 1
		_, err = s.signups.InsertOne(tx, saved)
		return err
	})
}
