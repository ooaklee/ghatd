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

// ErrOAuthConnectionConflict reports that the account no longer matches the
// email, revision or provider-identity snapshot captured before verification.
var ErrOAuthConnectionConflict = errors.New("OAuthConnectionConflict")

// OAuthConnectionsRepository is separate from OAuthRepository: adopting account
// management is optional and cannot disable a host's existing provider login.
type OAuthConnectionsRepository interface {
	// DisconnectOAuthProvider atomically verifies the full account snapshot,
	// removes only the selected provider, applies the verified email and advances
	// the email revision. A conflict must leave all fields unchanged.
	DisconnectOAuthProvider(context.Context, *DisconnectOAuthProviderRequest) (*UniversalUser, error)
	// LinkOAuthIdentityAtRevision attaches the identity only at the expected
	// account revision, preserving cross-account identity uniqueness.
	LinkOAuthIdentityAtRevision(context.Context, string, *OAuthIdentity, int64) (*UniversalUser, error)
}

// OAuthIdentitySnapshot records a provider identity key and link time, allowing
// verification to detect unlink-and-relink changes that retain the email revision.
type OAuthIdentitySnapshot struct {
	Key      string    `json:"key"`
	LinkedAt time.Time `json:"linked_at"`
}

// DisconnectOAuthProviderRequest describes a conditional provider removal.
// ExpectedEmail, EmailRevision and all selected-provider Identities must match the
// persisted account. The caller must independently verify ownership of VerifiedEmail
// before invoking the repository; the repository does not verify mailbox access.
type DisconnectOAuthProviderRequest struct {
	UserID, Provider, ExpectedEmail, VerifiedEmail string
	EmailRevision                                  int64
	Identities                                     []OAuthIdentitySnapshot
	// AllowedRelayFallbackProviders is a server-derived allowlist of enabled
	// providers, excluding the one being removed. It must be recomputed at
	// confirmation, never accepted from clients or persisted in emailed proofs.
	// A relay email requires at least one of these providers to remain linked
	// in the same atomic write. An empty allowlist fails closed for relay emails.
	AllowedRelayFallbackProviders []string `json:"-"`
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
	allowed := bson.A{}
	if IsApplePrivateRelayEmail(email) {
		for _, provider := range req.AllowedRelayFallbackProviders {
			if provider != req.Provider && (provider == "google" || provider == "apple") {
				allowed = append(allowed, provider)
			}
		}
		if len(allowed) == 0 {
			return nil, ErrOAuthReplacementEmailRequired
		}
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
	if len(allowed) > 0 {
		usable := bson.M{"$filter": bson.M{"input": "$oauth_identities", "as": "identity", "cond": bson.M{"$in": bson.A{"$$identity.provider", allowed}}}}
		filter["$expr"] = bson.M{"$and": bson.A{filter["$expr"], bson.M{"$gt": bson.A{bson.M{"$size": usable}, 0}}}}
	}
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

// SupportsOAuthConnections reports whether the repository supports conditional
// provider management. A false result does not disable existing provider login.
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

// DisconnectOAuthProvider applies the verified account change through the
// optional repository capability and restores user dependencies on success.
// It returns ErrOAuthUnsupported when that capability is absent.
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

// GetEmailRevision exposes the persisted sign-in-method revision so newly
// issued tokens can be bound to the current account state.
func (u *UniversalUser) GetEmailRevision() int64 { return u.EmailRevision }
