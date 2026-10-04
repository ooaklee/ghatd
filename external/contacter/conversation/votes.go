package commsconversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// VotesCollection stores mutable actor-specific feedback outside contact history.
const VotesCollection = "comms_entry_votes"

var (
	errVoteInvalid     = errors.New("commsconversation/invalid-vote")
	errVoteUnavailable = errors.New("commsconversation/vote-unavailable")
)

// AdministratorVerifier is the shared live session/account authority port.
type AdministratorVerifier interface {
	// AuthorizeAdministrator rechecks the session and current account authority.
	AuthorizeAdministrator(context.Context) (string, error)
	// AdministratorErrorMaps returns only the safe public authority error manifests.
	AdministratorErrorMaps() []reply.ErrorManifest
}

// Voting uses the already managed database and shared contact/user repositories.
// Votes are independent mutable feedback, never an edit to immutable history.
type Voting struct {
	// Database is the existing managed database; Voting never closes its client.
	Database *mongo.Database
	// Contacts resolves real contacts and immutable entry membership.
	Contacts *contacter.Repository
	// Users optionally supplies bounded participant labels, never email addresses.
	Users *user.Service
	// Authority verifies the actor on every read/write, beyond route admission.
	Authority AdministratorVerifier
}

// VoteSummary exposes aggregate feedback and the verified viewer's current vote.
type VoteSummary struct {
	// CommsID identifies the parent contact; it is not the requesting actor.
	CommsID string `json:"comms_id"`
	// EntryID is empty for the original contact, otherwise an immutable entry ID.
	EntryID string `json:"entry_id"`
	// Up counts positive (1) votes.
	Up int `json:"up"`
	// Down counts negative (0) votes.
	Down int `json:"down"`
	// ViewerVote is nil when absent; zero is a real negative vote, not unknown.
	ViewerVote *int `json:"viewer_vote"`
}

// Participant is the minimal author label returned by private conversation views.
type Participant struct {
	// ID is the known actor identity, used to match conversation authors.
	ID string `json:"id"`
	// NanoID is the public short identifier when available.
	NanoID string `json:"nano_id"`
	// FullName is bounded display text, never trusted markup.
	FullName string `json:"full_name"`
}

// VotePage is a bounded private projection; it never replaces immutable history.
type VotePage struct {
	// CommsID identifies the selected contact.
	CommsID string `json:"comms_id"`
	// ViewerActorID is server-bound from verified live authority.
	ViewerActorID string `json:"viewer_actor_id"`
	// ByEntry contains the original contact and up to 100 requested entry IDs.
	ByEntry map[string]VoteSummary `json:"by_entry"`
	// Participants contains only known authors/viewer, without email or roles.
	Participants []Participant `json:"participants"`
}

func voteErrors() reply.ErrorManifest {
	return reply.ErrorManifest{
		errVoteInvalid:     {Title: "Provide a valid conversation vote.", StatusCode: 400, Code: "HOST_COMMS_VOTE_INVALID"},
		errVoteUnavailable: {Title: "Conversation voting could not be confirmed.", StatusCode: 503, Code: "HOST_COMMS_VOTE_UNAVAILABLE"},
	}
}

// AttachVoting registers private reads and explicitly owner-bound set/remove
// actions. It keeps public contact and shared conversation projections unchanged.
func AttachVoting(r *router.Router, service *Voting, adminOnly mux.MiddlewareFunc) error {
	if r == nil || service == nil || service.Database == nil || service.Contacts == nil || service.Authority == nil || adminOnly == nil {
		return errVoteUnavailable
	}
	group := r.NewRouteGroup("/api/v1/ums", router.AdminSession, adminOnly)
	group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation/votes", Operation: "commsconversation.ReadVotes", Methods: []string{http.MethodGet, http.MethodOptions}}, service.read)
	for _, path := range []string{"/comms/{id}/vote", "/comms/{id}/conversation/{entryId}/vote"} {
		group.Handle(router.RouteDefinition{Path: path, Operation: "commsconversation.SetVote", Methods: []string{http.MethodPost, http.MethodOptions}}, requireOwner(http.HandlerFunc(service.write)).ServeHTTP)
		group.Handle(router.RouteDefinition{Path: path, Operation: "commsconversation.RemoveVote", Methods: []string{http.MethodDelete, http.MethodOptions}}, requireOwner(http.HandlerFunc(service.write)).ServeHTTP)
	}
	return nil
}
func validVoteTarget(id, entry string) bool {
	if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return false
	}
	if entry == "" {
		return true
	}
	if len(entry) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(entry)
	return err == nil && hex.EncodeToString(decoded) == entry
}
func voteID(actor, comms, entry string) string {
	value, _ := json.Marshal([]string{actor, comms, entry})
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
func (s *Voting) authorize(ctx context.Context) (string, error) {
	if s == nil || s.Authority == nil || s.Database == nil || s.Contacts == nil {
		return "", errVoteUnavailable
	}
	actor, err := s.Authority.AuthorizeAdministrator(ctx)
	if err != nil {
		return "", err
	}
	if actor == "" {
		return "", errVoteUnavailable
	}
	return actor, nil
}
func (s *Voting) target(ctx context.Context, comms, entry string) (*contacter.CommsEntry, error) {
	if !validVoteTarget(comms, entry) {
		return nil, errVoteInvalid
	}
	rows, err := s.Contacts.GetCommsByIds(ctx, []string{comms})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 || rows[0].Id != comms {
		return nil, contacter.ErrCommsNotFound
	}
	if entry != "" {
		return s.Contacts.FindCommsEntry(ctx, comms, entry)
	}
	return nil, nil
}

// summaries reads a bounded set with two queries, independent of entry count.
// A malformed stored vote is unavailable, rather than a fabricated downvote.
func (s *Voting) summaries(ctx context.Context, actor, comms string, ids []string) (map[string]VoteSummary, error) {
	result := map[string]VoteSummary{}
	ownIDs := make([]string, 0, len(ids))
	targets := map[string]string{}
	for _, id := range ids {
		result[id] = VoteSummary{CommsID: comms, EntryID: id}
		digest := voteID(actor, comms, id)
		ownIDs = append(ownIDs, digest)
		targets[digest] = id
	}
	collection := s.Database.Collection(VotesCollection)
	cursor, err := collection.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"comms_id": comms, "entry_id": bson.M{"$in": ids}}}},
		{{Key: "$group", Value: bson.M{"_id": bson.M{"entry_id": "$entry_id", "vote": "$vote"}, "count": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return nil, err
	}
	var counts []struct {
		Key struct {
			EntryID string `bson:"entry_id"`
			Vote    *int   `bson:"vote"`
		} `bson:"_id"`
		Count int `bson:"count"`
	}
	if err = cursor.All(ctx, &counts); err != nil {
		return nil, err
	}
	for _, count := range counts {
		summary, found := result[count.Key.EntryID]
		if !found || count.Key.Vote == nil || count.Count < 0 {
			return nil, errVoteUnavailable
		}
		switch *count.Key.Vote {
		case 1:
			summary.Up = count.Count
		case 0:
			summary.Down = count.Count
		default:
			return nil, errVoteUnavailable
		}
		result[count.Key.EntryID] = summary
	}
	cursor, err = collection.Find(ctx, bson.M{"_id": bson.M{"$in": ownIDs}}, options.Find().SetProjection(bson.M{"_id": 1, "entry_id": 1, "comms_id": 1, "actor_id": 1, "vote": 1}))
	if err != nil {
		return nil, err
	}
	var own []struct {
		ID      string `bson:"_id"`
		EntryID string `bson:"entry_id"`
		CommsID string `bson:"comms_id"`
		ActorID string `bson:"actor_id"`
		Vote    *int   `bson:"vote"`
	}
	if err = cursor.All(ctx, &own); err != nil {
		return nil, err
	}
	for _, vote := range own {
		target, found := targets[vote.ID]
		if !found || vote.EntryID != target || vote.CommsID != comms || vote.ActorID != actor || vote.Vote == nil || (*vote.Vote != 0 && *vote.Vote != 1) {
			return nil, errVoteUnavailable
		}
		summary := result[target]
		summary.ViewerVote = vote.Vote
		result[target] = summary
	}
	return result, nil
}
func (s *Voting) participants(ctx context.Context, actors map[string]bool) []Participant {
	result := []Participant{}
	if s.Users == nil {
		return result
	}
	ids := make([]string, 0, len(actors))
	for id := range actors {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return result
	}
	// The shared service caps pages at 100; at most 101 known authors are requested.
	sort.Strings(ids)
	for offset := 0; offset < len(ids); offset += 100 {
		stop := offset + 100
		if stop > len(ids) {
			stop = len(ids)
		}
		found, err := s.Users.GetUsers(ctx, &user.GetUsersRequest{Page: 1, PerPage: 100, IDsFilter: ids[offset:stop]})
		if err != nil || found == nil {
			continue
		}
		for _, person := range found.Users {
			if !actors[person.ID] {
				continue
			}
			v := Participant{ID: person.ID, NanoID: person.NanoID}
			if p := person.PersonalInfo; p != nil {
				v.FullName = p.FullName
				if v.FullName == "" {
					v.FullName = strings.TrimSpace(p.FirstName + " " + p.LastName)
				}
			}
			if len(v.ID) > 128 || len(v.NanoID) > 128 {
				continue
			}
			if len(v.FullName) > 256 {
				v.FullName = ""
			}
			result = append(result, v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
func (s *Voting) respond(w http.ResponseWriter, r *http.Request, data any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	maps := errormanifest.NewComposer().Add(contacter.ContacterErrorMap).Add(voteErrors())
	if s != nil && s.Authority != nil {
		maps.Add(s.Authority.AdministratorErrorMaps()...)
	}
	if err != nil {
		_ = errormanifest.WriteHTTPError(w, err, maps.Build(), reply.WithContext(r.Context()))
		return
	}
	_ = reply.NewReplier(maps.Build()).NewHTTPDataResponse(w, 200, data, reply.WithContext(r.Context()))
}
func (s *Voting) read(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	actor, err := s.authorize(ctx)
	comms := mux.Vars(r)["id"]
	ids := r.URL.Query()["entry_id"]
	if len(ids) > 100 {
		err = errVoteInvalid
	}
	page := VotePage{CommsID: comms, ViewerActorID: actor, ByEntry: map[string]VoteSummary{}, Participants: []Participant{}}
	actors := map[string]bool{actor: true}
	seen := map[string]bool{}
	if err == nil {
		_, err = s.target(ctx, comms, "")
	}
	for _, id := range ids {
		if err != nil {
			break
		}
		if id == "" || seen[id] || !validVoteTarget(comms, id) {
			err = errVoteInvalid
			break
		}
		seen[id] = true
	}
	if err == nil && len(ids) > 0 {
		// Query only native immutable entry identities belonging to this contact.
		// Unknown targets fail the complete read; never synthesize a zero vote.
		cursor, queryErr := s.Database.Collection(contacter.CommsEntriesCollection).Find(ctx, bson.M{"comms_id": comms, "_id": bson.M{"$in": ids}}, options.Find().SetProjection(bson.M{"_id": 1, "actor_id": 1}))
		err = queryErr
		if err == nil {
			var entries []struct {
				ID      string `bson:"_id"`
				ActorID string `bson:"actor_id"`
			}
			err = cursor.All(ctx, &entries)
			if err == nil && len(entries) != len(ids) {
				err = contacter.ErrCommsNotFound
			}
			if err == nil {
				for _, entry := range entries {
					actors[entry.ActorID] = true
				}
			}
		}
	}
	if err == nil {
		page.ByEntry, err = s.summaries(ctx, actor, comms, append([]string{""}, ids...))
	}
	if err == nil {
		page.Participants = s.participants(ctx, actors)
	}
	s.respond(w, r, page, err)
}
func (s *Voting) write(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	actor, err := s.authorize(ctx)
	comms, entry := mux.Vars(r)["id"], mux.Vars(r)["entryId"]
	if err == nil {
		_, err = s.target(ctx, comms, entry)
	}
	var input struct {
		Vote *int `json:"vote"`
	}
	if err == nil && r.Method == http.MethodPost && r.Body == nil {
		err = errVoteInvalid
	}
	if err == nil && r.Method == http.MethodPost {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		var extra any
		if decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF || input.Vote == nil || (*input.Vote != 0 && *input.Vote != 1) {
			err = errVoteInvalid
		}
	}
	if err == nil {
		collection := s.Database.Collection(VotesCollection).Clone(options.Collection().SetWriteConcern(writeconcern.Majority()))
		selector := bson.M{"_id": voteID(actor, comms, entry)}
		if r.Method == http.MethodDelete {
			var result *mongo.DeleteResult
			result, err = collection.DeleteOne(ctx, selector)
			if err == nil && (result == nil || !result.Acknowledged) {
				err = errVoteUnavailable
			}
		} else {
			var result *mongo.UpdateResult
			result, err = collection.UpdateOne(ctx, selector, bson.M{"$set": bson.M{"vote": *input.Vote, "updated_at": time.Now().UTC()}, "$setOnInsert": bson.M{"comms_id": comms, "entry_id": entry, "actor_id": actor}}, options.UpdateOne().SetUpsert(true))
			if err == nil && (result == nil || !result.Acknowledged) {
				err = errVoteUnavailable
			}
		}
	}
	page := VotePage{CommsID: comms, ViewerActorID: actor, ByEntry: map[string]VoteSummary{}, Participants: []Participant{}}
	if err == nil {
		page.ByEntry, err = s.summaries(ctx, actor, comms, []string{entry})
	}
	s.respond(w, r, page, err)
}

// EnsureVotingIndexes is an explicit host migration; reads never create indexes.
func EnsureVotingIndexes(ctx context.Context, db *mongo.Database) error {
	if ctx == nil || db == nil {
		return errVoteUnavailable
	}
	_, err := db.Collection(VotesCollection).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "comms_id", Value: 1}, {Key: "entry_id", Value: 1}}, Options: options.Index().SetName("idx_comms_entry_votes_target")})
	return err
}
