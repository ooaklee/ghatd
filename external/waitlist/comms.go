package waitlist

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/contacter"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// CommsType identifies consent-bearing prerelease submissions in contact history.
const CommsType contacter.CommsType = "waitlist"

// SignupConfig supplies optional host-owned copy for the direct signup adapter.
// It does not configure storage identities, consent or authorization.
type SignupConfig struct {
	// Message is the recorded signup request. Empty uses neutral early-access copy.
	Message string
}

func (c SignupConfig) defaults() SignupConfig {
	if c.Message == "" {
		c.Message = "Email me when early access is available."
	}
	return c
}

// commsID uses a framework-owned domain separator. Branding cannot change identity
// or create duplicate signups. The host database, not this ID, provides isolation.
func commsID(email string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("ghatd:waitlist:"+CampaignID+":"+email)).String()
}

// NewCommsSignupStore keeps older clients on the same signup path as contact
// submissions. Export still reads the existing audience and its delivery state.
func NewCommsSignupStore(service *contacter.Service, audience Store) Store {
	return NewCommsSignupStoreWithConfig(service, audience, SignupConfig{})
}

// NewCommsSignupStoreWithConfig adapts public signup to the same contact service
// using host-owned copy. GHATD owns the stable communication ID scheme.
func NewCommsSignupStoreWithConfig(service *contacter.Service, audience Store, config SignupConfig) Store {
	return &commsSignupStore{Store: audience, service: service, config: config.defaults()}
}

type commsSignupStore struct {
	Store
	service *contacter.Service
	config  SignupConfig
}

func (s *commsSignupStore) Join(ctx context.Context, email string) error {
	_, err := s.service.CreateComms(ctx, &contacter.CreateCommsRequest{
		FullName: "Waitlist subscriber", Email: email, Type: CommsType,
		Message: s.config.Message,
	})
	return err
}

// NewCommsService extends the existing contact integration. All ordinary comms
// keep their existing behavior; waitlist comms also enroll in the prerelease
// audience, retaining its unsubscribe and delivery records.
// Dependencies must be non-nil; invalid dependencies are a programming error.
func NewCommsService(repository *contacter.Repository, audience Store) *contacter.Service {
	service, err := NewCommsServiceWithConfig(repository, audience, CommsConfig{})
	if err != nil {
		panic(err)
	}
	return service
}

// NewCommsServiceWithConfig binds consent metadata for new contacts without
// changing canonical identity or overwriting existing records and annotations.
func NewCommsServiceWithConfig(repository *contacter.Repository, audience Store, config CommsConfig) (*contacter.Service, error) {
	version, err := consentVersion(config.ConsentVersion)
	if err != nil {
		return nil, err
	}
	if repository == nil || audience == nil {
		return nil, errors.New("waitlist contacts require repository and audience")
	}
	types := contacter.DefaultCommsTypeMap()
	types[CommsType] = "Waitlist"
	return contacter.NewService(&commsRepository{Repository: repository, audience: audience, consent: version}, types), nil
}

type commsRepository struct {
	*contacter.Repository
	audience Store
	consent  string
}

func (r *commsRepository) CreateComms(ctx context.Context, comm *contacter.Comms) (*contacter.Comms, error) {
	if comm.Type != CommsType {
		return r.Repository.CreateComms(ctx, comm)
	}
	if _, valid := canonicalEmail(comm.Email); !valid || strings.TrimSpace(comm.Message) == "" {
		return nil, contacter.ErrInvalidCommsPayload
	}
	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, err
	}
	return saveWaitlistCommsWithConsent(ctx, collection, r.audience, comm, r.consent)
}

// saveWaitlistCommsWithConsent writes the initial contact before audience consent;
// a retry repairs partial enrollment without replacing administrator content.
func saveWaitlistCommsWithConsent(ctx context.Context, collection *mongo.Collection, audience Store, comm *contacter.Comms, version string) (*contacter.Comms, error) {
	email, valid := canonicalEmail(comm.Email)
	if !valid || strings.TrimSpace(comm.Message) == "" {
		return nil, contacter.ErrInvalidCommsPayload
	}
	// One durable comm per canonical address, even when a request is retried or
	// races another tab. Insert-only writes preserve admin notes and original dates.
	record := *comm
	record.Email = email
	record.Id = commsID(email)
	record.GenerateNanoId().SetCreatedAtTimeToNow()
	record.Meta = map[string]interface{}{
		"displayed_as": "Waitlist", "subject": "Prerelease early access",
		"campaign": CampaignID, "consent_version": version,
	}
	collection = collection.Clone(options.Collection().SetWriteConcern(writeconcern.Majority()))
	_, err := collection.UpdateOne(ctx, bson.M{"_id": record.Id}, bson.M{"$setOnInsert": record}, options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		err = collection.FindOne(ctx, bson.M{"_id": record.Id}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	}
	if err != nil {
		return nil, err
	}
	// Do not acknowledge signup until both the communication and audience entry
	// are saved. If enrollment fails, a retry repairs it without duplicating either.
	if err := audience.Join(ctx, email); err != nil {
		return nil, err
	}
	// Give the same receipt for new and repeated submissions. Never return an
	// existing record's admin notes, reply, identity or signup date to a visitor.
	return &contacter.Comms{Type: CommsType, Meta: map[string]interface{}{"displayed_as": "Waitlist"}}, nil
}
