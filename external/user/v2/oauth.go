package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"net/mail"
	"strings"
	"time"
)

var (
	// ErrOAuthLinkRequired prevents email-based account takeover.
	ErrOAuthLinkRequired = errors.New("OAuthLinkRequired")
	// ErrOAuthIdentityConflict prevents a provider identity belonging to two accounts.
	ErrOAuthIdentityConflict = errors.New("OAuthIdentityConflict")
	// ErrOAuthRestricted denies provider authentication for restricted accounts.
	ErrOAuthRestricted = errors.New("OAuthRestricted")
	// ErrOAuthUnsupported requires the optional identity persistence capability.
	ErrOAuthUnsupported = errors.New("OAuthUnsupported")
	// ErrOAuthIndexesRequired requires the explicit unique-index migration.
	ErrOAuthIndexesRequired = errors.New("OAuthIndexesRequired")
)

// OAuthIdentity is server-owned metadata excluded from all JSON representations.
type OAuthIdentity struct {
	Provider string    `bson:"provider" json:"-"`
	Issuer   string    `bson:"issuer" json:"-"`
	Subject  string    `bson:"subject" json:"-"`
	Key      string    `bson:"key" json:"-"`
	LinkedAt time.Time `bson:"linked_at" json:"-"`
}

// CanonicalOAuthIdentity binds a key to an unambiguous signed issuer and subject.
func CanonicalOAuthIdentity(identity *OAuthIdentity) (*OAuthIdentity, error) {
	if identity == nil || identity.Subject == "" || len(identity.Subject) > 512 || strings.TrimSpace(identity.Subject) != identity.Subject {
		return nil, ErrValidationFailed
	}
	value := *identity
	if value.Provider == "google" && value.Issuer == "accounts.google.com" {
		value.Issuer = "https://accounts.google.com"
	}
	if !((value.Provider == "google" && value.Issuer == "https://accounts.google.com") || (value.Provider == "apple" && value.Issuer == "https://appleid.apple.com")) {
		return nil, ErrValidationFailed
	}
	payload, _ := json.Marshal([]string{value.Issuer, value.Subject})
	digest := sha256.Sum256(payload)
	value.Key = hex.EncodeToString(digest[:])
	if value.LinkedAt.IsZero() {
		value.LinkedAt = time.Now().UTC()
	}
	return &value, nil
}

// CreateOAuthUserRequest accepts only trusted identity and optional profile data.
type CreateOAuthUserRequest struct {
	Identity                                   OAuthIdentity
	Email, FirstName, LastName, FullName, Type string
}

// CreateOAuthUserResponse distinguishes the winning atomic insert from a retry.
type CreateOAuthUserResponse struct {
	User    *UniversalUser
	Created bool
}

// OAuthRepository is an optional capability; existing UserRepository remains compatible.
type OAuthRepository interface {
	GetUserByOAuthIdentity(context.Context, *OAuthIdentity) (*UniversalUser, error)
	CreateOAuthUser(context.Context, *UniversalUser) (*CreateOAuthUserResponse, error)
	LinkOAuthIdentity(context.Context, string, *OAuthIdentity) (*UniversalUser, error)
	RecordOAuthLogin(context.Context, string, time.Time) (*UniversalUser, error)
}

// GetUserByOAuthIdentity resolves identity without considering provider email.
func (r *Repository) GetUserByOAuthIdentity(ctx context.Context, identity *OAuthIdentity) (*UniversalUser, error) {
	identity, err := CanonicalOAuthIdentity(identity)
	if err != nil {
		return nil, err
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	var user UniversalUser
	err = collection.FindOne(ctx, bson.M{"oauth_identity_keys": identity.Key}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, ErrDatabaseError
	}
	return &user, nil
}

// requireOAuthIndexes fails closed if either account uniqueness constraint is absent.
func (r *Repository) requireOAuthIndexes(ctx context.Context, collection *mongo.Collection) error {
	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		return ErrOAuthIndexesRequired
	}
	defer cursor.Close(ctx)
	var indexes []struct {
		Key     bson.D `bson:"key"`
		Unique  bool   `bson:"unique"`
		Partial bson.M `bson:"partialFilterExpression"`
	}
	if cursor.All(ctx, &indexes) != nil {
		return ErrOAuthIndexesRequired
	}
	email, identity := false, false
	for _, index := range indexes {
		if !index.Unique || len(index.Key) != 1 || len(index.Partial) > 0 {
			continue
		}
		switch index.Key[0].Key {
		case "email":
			email = true
		case "oauth_identity_keys":
			identity = true
		}
	}
	if !email || !identity {
		return ErrOAuthIndexesRequired
	}
	return nil
}

// CreateOAuthUser inserts the verified account and identity in a single document.
func (r *Repository) CreateOAuthUser(ctx context.Context, user *UniversalUser) (*CreateOAuthUserResponse, error) {
	if user == nil || len(user.OAuthIdentities) != 1 || !oauthUserActive(user) {
		return nil, ErrValidationFailed
	}
	identity, err := CanonicalOAuthIdentity(&user.OAuthIdentities[0])
	if err != nil {
		return nil, err
	}
	user.OAuthIdentities = []OAuthIdentity{*identity}
	user.OAuthIdentityKeys = []string{identity.Key}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err = r.requireOAuthIndexes(ctx, collection); err != nil {
		return nil, err
	}
	_, err = collection.InsertOne(ctx, user)
	if err == nil {
		return &CreateOAuthUserResponse{User: user, Created: true}, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return nil, ErrDatabaseError
	}
	winner, lookupErr := r.GetUserByOAuthIdentity(ctx, identity)
	if lookupErr == nil {
		return &CreateOAuthUserResponse{User: winner}, nil
	}
	if !errors.Is(lookupErr, ErrUserNotFound) {
		return nil, lookupErr
	}
	return nil, ErrOAuthLinkRequired
}

// LinkOAuthIdentity atomically links only a verified active account, preserving its profile.
func (r *Repository) LinkOAuthIdentity(ctx context.Context, id string, identity *OAuthIdentity) (*UniversalUser, error) {
	identity, err := CanonicalOAuthIdentity(identity)
	if err != nil {
		return nil, err
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err = r.requireOAuthIndexes(ctx, collection); err != nil {
		return nil, err
	}
	filter := oauthActiveFilter(id)
	filter["oauth_identity_keys"] = bson.M{"$ne": identity.Key}
	update := bson.M{"$addToSet": bson.M{"oauth_identity_keys": identity.Key, "oauth_identities": identity}}
	var user UniversalUser
	err = collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&user)
	if mongo.IsDuplicateKeyError(err) {
		return nil, ErrOAuthIdentityConflict
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		err = collection.FindOne(ctx, oauthActiveFilter(id)).Decode(&user)
		if err == nil {
			for _, key := range user.OAuthIdentityKeys {
				if key == identity.Key {
					return &user, nil
				}
			}
		}
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrDatabaseError
		}
		return nil, ErrOAuthRestricted
	}
	if err != nil {
		return nil, ErrDatabaseError
	}
	return &user, nil
}

// oauthActiveFilter protects writes against concurrent account restriction.
func oauthActiveFilter(id string) bson.M {
	return bson.M{"_id": id, "status": "ACTIVE", "verification.email_verified": true}
}

// oauthUserActive checks status and email verification before tokens are minted.
func oauthUserActive(user *UniversalUser) bool {
	return user != nil && user.Status == "ACTIVE" && user.Verification != nil && user.Verification.EmailVerified
}

// RecordOAuthLogin writes only timestamps while rechecking current security state.
func (r *Repository) RecordOAuthLogin(ctx context.Context, id string, at time.Time) (*UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	stamp := at.UTC().Format(DefaultTimeFormatRFC3339NanoUTC)
	var user UniversalUser
	err = collection.FindOneAndUpdate(ctx, oauthActiveFilter(id), bson.M{"$set": bson.M{"metadata.last_login_at": stamp, "metadata.last_fresh_login_at": stamp, "metadata.updated_at": stamp}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrOAuthRestricted
	}
	if err != nil {
		return nil, ErrDatabaseError
	}
	return &user, nil
}

// GetUserByOAuthIdentity hydrates the existing account configuration.
func (s *Service) GetUserByOAuthIdentity(ctx context.Context, identity *OAuthIdentity) (*UniversalUser, error) {
	repo, ok := s.UserRepository.(OAuthRepository)
	if !ok {
		return nil, ErrOAuthUnsupported
	}
	user, err := repo.GetUserByOAuthIdentity(ctx, identity)
	if err == nil {
		s.setUserDependencies(user)
	}
	return user, err
}

// CreateOAuthUser creates a verified active account without weakening email signup rules.
func (s *Service) CreateOAuthUser(ctx context.Context, req *CreateOAuthUserRequest) (*CreateOAuthUserResponse, error) {
	repo, ok := s.UserRepository.(OAuthRepository)
	if !ok {
		return nil, ErrOAuthUnsupported
	}
	if req == nil {
		return nil, ErrValidationFailed
	}
	identity, err := CanonicalOAuthIdentity(&req.Identity)
	if err != nil {
		return nil, err
	}
	email := normaliseUserEmail(req.Email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 320 {
		return nil, ErrInvalidEmail
	}
	config, err := s.resolveRequestedConfig(req.Type)
	if err != nil {
		return nil, err
	}
	user := NewUniversalUser(config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	user.Email = email
	user.PersonalInfo = &PersonalInfo{FirstName: req.FirstName, LastName: req.LastName, FullName: req.FullName}
	user.OAuthIdentities = []OAuthIdentity{*identity}
	user.OAuthIdentityKeys = []string{identity.Key}
	user.GenerateNewUUID()
	if config.MultipleIdentifiers {
		user.GenerateNewNanoID()
	}
	user.SetInitialState()
	user.Status = "ACTIVE"
	user.VerifyEmail()
	user.SetActivatedAtNow()
	user.SetStatusChangedAtNow()
	if config.DefaultRole != "" {
		user.Roles = append(user.Roles, config.DefaultRole)
	}
	if s.shouldBeAutoAdmin(email) {
		user.Roles = append(user.Roles, UserRoleAdmin)
	}
	user.Standardise()
	if user.Validate() != nil {
		return nil, ErrValidationFailed
	}
	response, err := repo.CreateOAuthUser(ctx, user)
	if err == nil {
		s.setUserDependencies(response.User)
	}
	return response, err
}

// LinkOAuthIdentity uses a server-authenticated target ID and hydrates its unchanged profile.
func (s *Service) LinkOAuthIdentity(ctx context.Context, id string, identity *OAuthIdentity) (*UniversalUser, error) {
	repo, ok := s.UserRepository.(OAuthRepository)
	if !ok {
		return nil, ErrOAuthUnsupported
	}
	user, err := repo.LinkOAuthIdentity(ctx, id, identity)
	if err == nil {
		s.setUserDependencies(user)
	}
	return user, err
}

// RecordOAuthLogin returns the security state observed by the conditional write.
func (s *Service) RecordOAuthLogin(ctx context.Context, id string, at time.Time) (*UniversalUser, error) {
	repo, ok := s.UserRepository.(OAuthRepository)
	if !ok {
		return nil, ErrOAuthUnsupported
	}
	user, err := repo.RecordOAuthLogin(ctx, id, at)
	if err == nil {
		s.setUserDependencies(user)
	}
	return user, err
}
