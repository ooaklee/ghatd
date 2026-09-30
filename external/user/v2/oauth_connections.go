package user

import (
	"context"
	"errors"
	"net/mail"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrOAuthConnectionConflict = errors.New("OAuthConnectionConflict")

// OAuthConnectionsRepository is separate from OAuthRepository: adopting account
// management is optional and cannot disable a host's existing provider login.
type OAuthConnectionsRepository interface {
	DisconnectOAuthProvider(context.Context, *DisconnectOAuthProviderRequest) (*UniversalUser, error)
	LinkOAuthIdentityAtRevision(context.Context, string, *OAuthIdentity, int64) (*UniversalUser, error)
}

type OAuthIdentitySnapshot struct {
	Key      string    `json:"key"`
	LinkedAt time.Time `json:"linked_at"`
}

type DisconnectOAuthProviderRequest struct {
	UserID, Provider, ExpectedEmail, VerifiedEmail string
	EmailRevision                                  int64
	Identities                                     []OAuthIdentitySnapshot
}

// DisconnectOAuthProvider verifies the entire selected-provider snapshot and
// replaces the email and identities in one Mongo write. Unique-email failure
// rolls back the entire operation; a later link or email change fails closed.
func (r *Repository) DisconnectOAuthProvider(ctx context.Context, req *DisconnectOAuthProviderRequest) (*UniversalUser, error) {
	if req == nil || req.UserID == "" || (req.Provider != "google" && req.Provider != "apple") || len(req.Identities) == 0 {
		return nil, ErrValidationFailed
	}
	email := normaliseUserEmail(req.VerifiedEmail)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		return nil, ErrInvalidEmail
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err = r.requireOAuthIndexes(ctx, collection); err != nil {
		return nil, err
	}
	filter := oauthActiveFilter(req.UserID)
	filter["email"] = req.ExpectedEmail
	if req.EmailRevision == 0 {
		filter["email_revision"] = bson.M{"$in": bson.A{nil, int64(0)}}
	} else {
		filter["email_revision"] = req.EmailRevision
	}
	snapshots := bson.A{}
	for _, identity := range req.Identities {
		if identity.Key == "" || identity.LinkedAt.IsZero() {
			return nil, ErrValidationFailed
		}
		snapshots = append(snapshots, bson.M{"$elemMatch": bson.M{"key": identity.Key, "provider": req.Provider, "linked_at": identity.LinkedAt}})
	}
	filter["oauth_identities"] = bson.M{"$all": snapshots}
	selected := bson.M{"$filter": bson.M{"input": "$oauth_identities", "as": "identity", "cond": bson.M{"$eq": bson.A{"$$identity.provider", req.Provider}}}}
	filter["$expr"] = bson.M{"$eq": bson.A{bson.M{"$size": selected}, len(req.Identities)}}
	remaining := bson.M{"$filter": bson.M{"input": "$oauth_identities", "as": "identity", "cond": bson.M{"$ne": bson.A{"$$identity.provider", req.Provider}}}}
	stamp := time.Now().UTC().Format(DefaultTimeFormatRFC3339NanoUTC)
	// Pipeline values originating from an email must be literal, even if the
	// address begins with '$'. Empty arrays must be removed for the sparse index.
	update := mongo.Pipeline{
		bson.D{{Key: "$set", Value: bson.M{
			"oauth_identities": remaining, "email": bson.M{"$literal": email},
			"verification.email_verified": true, "verification.email_verified_at": stamp,
			"had_oauth_identity": true, "email_revision": bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$email_revision", 0}}, 1}},
			"metadata.updated_at": stamp,
		}}},
		bson.D{{Key: "$set", Value: bson.M{"oauth_identity_keys": bson.M{"$map": bson.M{"input": "$oauth_identities", "as": "identity", "in": "$$identity.key"}}}}},
		bson.D{{Key: "$set", Value: bson.M{
			"oauth_identity_keys": bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{bson.M{"$size": "$oauth_identity_keys"}, 0}}, "$$REMOVE", "$oauth_identity_keys"}},
			"oauth_identities":    bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{bson.M{"$size": "$oauth_identities"}, 0}}, "$$REMOVE", "$oauth_identities"}},
		}}},
	}
	var account UniversalUser
	err = collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&account)
	if mongo.IsDuplicateKeyError(err) {
		return nil, ErrEmailAlreadyExists
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrOAuthConnectionConflict
	}
	if err != nil {
		return nil, ErrDatabaseError
	}
	return &account, nil
}

func (s *Service) SupportsOAuthConnections() bool {
	_, ok := s.UserRepository.(OAuthConnectionsRepository)
	return ok
}

// LinkOAuthIdentityAtRevision prevents a callback started before disconnect
// from restoring a provider after the user's sign-in methods have changed.
func (s *Service) LinkOAuthIdentityAtRevision(ctx context.Context, id string, identity *OAuthIdentity, revision int64) (*UniversalUser, error) {
	repo, ok := s.UserRepository.(OAuthConnectionsRepository)
	if !ok {
		return nil, ErrOAuthUnsupported
	}
	account, err := repo.LinkOAuthIdentityAtRevision(ctx, id, identity, revision)
	if err == nil {
		s.setUserDependencies(account)
	}
	return account, err
}
func (s *Service) DisconnectOAuthProvider(ctx context.Context, req *DisconnectOAuthProviderRequest) (*UniversalUser, error) {
	repo, ok := s.UserRepository.(OAuthConnectionsRepository)
	if !ok {
		return nil, ErrOAuthUnsupported
	}
	account, err := repo.DisconnectOAuthProvider(ctx, req)
	if err == nil {
		s.setUserDependencies(account)
	}
	return account, err
}

// GetEmailRevision binds newly issued tokens to the current sign-in methods.
func (u *UniversalUser) GetEmailRevision() int64 { return u.EmailRevision }
