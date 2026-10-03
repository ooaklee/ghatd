package contacter

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

// CommsEntryKind distinguishes private collaboration from recorded correspondence.
// A reply is a record, not a command to send mail or a delivery receipt.
type CommsEntryKind string

const (
	// CommsEntryInternalNote is visible only to the administrative conversation API.
	CommsEntryInternalNote CommsEntryKind = "internal_note"
	// CommsEntryReply records a reply reported by an administrator.
	CommsEntryReply CommsEntryKind = "reply"
	// CommsEntryEmailInbound records an email imported by a trusted adapter.
	CommsEntryEmailInbound CommsEntryKind = "email_inbound"
	// CommsEntryEmailOutbound records an outbound email, without asserting delivery.
	CommsEntryEmailOutbound CommsEntryKind = "email_outbound"
	// MaxCommsEntryBodyBytes bounds each plain-text message, not the whole thread.
	MaxCommsEntryBodyBytes = 64 * 1024
)

var (
	// ErrCommsConversationUnavailable indicates missing wiring or invalid receipts.
	ErrCommsConversationUnavailable = errors.New("contacter/conversation-unavailable")
	// ErrCommsEntryInvalid rejects malformed entry, email or cursor input.
	ErrCommsEntryInvalid = errors.New("contacter/invalid-conversation-entry")
	// ErrCommsEntryConflict requires reconciliation of an existing immutable entry.
	ErrCommsEntryConflict = errors.New("contacter/conversation-entry-conflict")
	// ErrCommsEntryNotFound means the selected entry is absent from this thread.
	ErrCommsEntryNotFound = errors.New("contacter/conversation-entry-not-found")
	// ErrCommsActorRequired rejects missing or contradictory trusted attribution.
	ErrCommsActorRequired = errors.New("contacter/conversation-actor-required")
)

// CommsEntryEmailMetadata describes an imported email. Only the first three
// fields establish import identity, and must come from a verified adapter.
// RFC headers are untrusted context: never authentication or automatic routing.
type CommsEntryEmailMetadata struct {
	// Provider identifies the trusted provider integration, not a sender header.
	Provider string `json:"provider" bson:"provider"`
	// Mailbox identifies the configured receiving/sending mailbox in that provider.
	Mailbox string `json:"mailbox" bson:"mailbox"`
	// ProviderMessageID is stable within the provider/mailbox across redeliveries.
	ProviderMessageID string `json:"provider_message_id" bson:"provider_message_id"`
	// ThreadID is the provider's optional grouping hint, not an access boundary.
	ThreadID string `json:"thread_id,omitempty" bson:"thread_id,omitempty"`
	// MessageID preserves the RFC Message-ID header without trusting its uniqueness.
	MessageID string `json:"message_id,omitempty" bson:"message_id,omitempty"`
	// InReplyTo preserves the external parent header, not a trusted local parent.
	InReplyTo string `json:"in_reply_to,omitempty" bson:"in_reply_to,omitempty"`
	// References preserves the external header chain as untrusted metadata.
	References []string `json:"references,omitempty" bson:"references,omitempty"`
	// From is a bounded display address; it does not verify a user's identity.
	From string `json:"from" bson:"from"`
	// To preserves up to 50 recipient display addresses, without ownership proof.
	To []string `json:"to" bson:"to"`
	// Subject is plain text; adapters must not use it to merge conversations.
	Subject string `json:"subject,omitempty" bson:"subject,omitempty"`
	// OccurredAt is the adapter-reported email time, separate from ingestion time.
	OccurredAt time.Time `json:"occurred_at" bson:"occurred_at"`
}

// CommsEntry is immutable history stored separately from the legacy contact.
// Clients must render Body as text. Internal notes must never be mailed or
// included in customer-facing projections merely because they share a thread.
type CommsEntry struct {
	// ID is a stable, namespaced digest used for atomic deduplication and paging.
	ID string `json:"id" bson:"_id"`
	// CommsID identifies the original contact; linking never grants access.
	CommsID string `json:"comms_id" bson:"comms_id"`
	// ActorID attributes the administrator or trusted ingestion actor, not From.
	ActorID string `json:"actor_id" bson:"actor_id"`
	// Kind selects private note, recorded reply or imported email semantics.
	Kind CommsEntryKind `json:"kind" bson:"kind"`
	// Body is bounded UTF-8 plain text, stored verbatim after validation.
	Body string `json:"body" bson:"body"`
	// ParentEntryID optionally references an entry in this same conversation.
	ParentEntryID string `json:"parent_entry_id,omitempty" bson:"parent_entry_id,omitempty"`
	// Email is present only on imported email entries.
	Email *CommsEntryEmailMetadata `json:"email,omitempty" bson:"email,omitempty"`
	// RecordedAt is server ingestion time at MongoDB millisecond precision.
	RecordedAt time.Time `json:"recorded_at" bson:"recorded_at"`
}

// AppendCommsEntryRequest is the administrative, non-delivery append command.
// Retrying the same RequestID and content returns the original entry. Changing
// content under the same key conflicts; use a new key for a new reply/correction.
type AppendCommsEntryRequest struct {
	// ActorID is supplied by trusted management, never decoded from the transport.
	ActorID string `json:"-" query:"-" form:"-"`
	// CommsID is route-owned and independent of the caller's identity.
	CommsID string `json:"-"`
	// RequestID is a canonical UUIDv4 stable across retries of this command.
	RequestID string `json:"request_id"`
	// Kind must be internal_note or reply; imported emails use a separate port.
	Kind CommsEntryKind `json:"kind"`
	// Body is the plain-text note or reply, not HTML or a mail-send instruction.
	Body string `json:"body"`
	// ParentEntryID links a response to an existing entry in the same thread.
	ParentEntryID string `json:"parent_entry_id,omitempty"`
}

// ImportCommsEmailRequest is for a trusted provider adapter, not JSON decoding.
// The caller must verify webhook/mailbox authenticity and choose the contact;
// supplying sender headers or an ActorID alone does not authorize ingestion.
type ImportCommsEmailRequest struct {
	// Entry carries the trusted actor, destination, direction and email metadata.
	// ID and RecordedAt are ignored and regenerated by the service.
	Entry CommsEntry
}

// AppendCommsEntryResponse reports the persisted immutable entry and replay state.
type AppendCommsEntryResponse struct {
	// Entry retains original attribution and time on exact retries.
	Entry *CommsEntry `json:"entry"`
	// Replayed is true when this call found an identical existing entry.
	Replayed bool `json:"replayed"`
}

// ListCommsConversationRequest selects an admin-only, newest-first page.
type ListCommsConversationRequest struct {
	// ActorID must be verified/authorized by the integrating manager.
	ActorID string `json:"-" query:"-" form:"-"`
	// CommsID selects the original contact; a deleted root returns not found.
	CommsID string
	// Limit defaults to 25 and may not exceed 100.
	Limit int
	// Cursor is the opaque next_cursor returned for this same contact.
	Cursor string
}

// CommsConversationPage separates legacy snapshots from attributed history.
type CommsConversationPage struct {
	// Legacy includes the original message and old single-value admin fields.
	// These are not invented as historical entries with guessed authors/times.
	Legacy *Comms `json:"legacy"`
	// Entries are immutable and ordered by recorded_at then id, descending.
	Entries []CommsEntry `json:"entries"`
	// NextCursor is empty when no further page was observed; this is not a snapshot.
	NextCursor string `json:"next_cursor,omitempty"`
}

// ConversationRepository is an optional narrow capability on contacter stores.
// Insert must atomically deduplicate identity and compare immutable content on
// replay. Query returns at most limit+1 entries in descending (time, ID) order.
type ConversationRepository interface {
	InsertCommsEntry(context.Context, *CommsEntry) (*CommsEntry, bool, error)
	FindCommsEntry(context.Context, string, string) (*CommsEntry, error)
	QueryCommsEntries(context.Context, string, *CommsEntry, int) ([]CommsEntry, error)
}

// nilConversationPort detects typed-nil configuration without calling adapters.
func nilConversationPort(v any) bool {
	if v == nil {
		return true
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return x.IsNil()
	}
	return false
}

// conversationActor requires exact identity when context publishes credentials.
// Bare-context callers are trusted integrations; IDs themselves are not authority.
func conversationActor(ctx context.Context, actor string) error {
	if ctx == nil || !boundedIdentifier(actor, 128) {
		return ErrCommsActorRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Value(helpers.RequestorKey) != nil || ctx.Value(helpers.RequestorAuthenticatedKey) != nil || ctx.Value(helpers.RequestorUserKey) != nil {
		if helpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
			return ErrCommsActorRequired
		}
		if ctx.Value(helpers.RequestorUserKey) != nil {
			if u := helpers.AcquireUserFrom(ctx); u == nil || u.ID != actor {
				return ErrCommsActorRequired
			}
		}
	}
	return nil
}

// boundedIdentifier rejects padded, empty, oversized or header-control values.
func boundedIdentifier(v string, size int) bool {
	return v != "" && len(v) <= size && utf8.ValidString(v) && strings.TrimSpace(v) == v && !strings.ContainsAny(v, "\r\n\x00")
}

// entryID hashes an unambiguous tuple; namespaces separate client and mail keys.
func entryID(parts ...string) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// validEntryID accepts only canonical persisted digest identifiers.
func validEntryID(v string) bool {
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == sha256.Size && strings.ToLower(v) == v
}

// copyCommsEntry detaches all mutable email slices before invoking an adapter.
func copyCommsEntry(v *CommsEntry) *CommsEntry {
	x := *v
	if v.Email != nil {
		e := *v.Email
		e.To = append([]string(nil), e.To...)
		e.References = append([]string(nil), e.References...)
		x.Email = &e
	}
	return &x
}

// sameCommsEntryContent defines replay equality independently of receipt time.
// Import retries preserve the first ingestion actor even across adapter workers.
func sameCommsEntryContent(a, b *CommsEntry) bool {
	if a == nil || b == nil {
		return false
	}
	x, y := *copyCommsEntry(a), *copyCommsEntry(b)
	x.RecordedAt, y.RecordedAt = time.Time{}, time.Time{}
	if x.Email != nil && y.Email != nil {
		x.ActorID, y.ActorID = "", ""
	}
	return reflect.DeepEqual(x, y)
}

// validateCommsEntry validates owned fields without treating metadata as proof.
func validateCommsEntry(v *CommsEntry) error {
	if v == nil || !validEntryID(v.ID) || !boundedIdentifier(v.CommsID, 128) || !boundedIdentifier(v.ActorID, 128) || strings.TrimSpace(v.Body) == "" || !utf8.ValidString(v.Body) || len(v.Body) > MaxCommsEntryBodyBytes || v.RecordedAt.IsZero() {
		return ErrCommsEntryInvalid
	}
	if v.ParentEntryID != "" && (!validEntryID(v.ParentEntryID) || v.ParentEntryID == v.ID) {
		return ErrCommsEntryInvalid
	}
	switch v.Kind {
	case CommsEntryInternalNote, CommsEntryReply:
		if v.Email != nil {
			return ErrCommsEntryInvalid
		}
	case CommsEntryEmailInbound, CommsEntryEmailOutbound:
		e := v.Email
		if e == nil || !boundedIdentifier(e.Provider, 64) || !boundedIdentifier(e.Mailbox, 254) || !boundedIdentifier(e.ProviderMessageID, 998) || !boundedIdentifier(e.From, 998) || len(e.To) == 0 || len(e.To) > 50 || len(e.References) > 64 || e.OccurredAt.IsZero() {
			return ErrCommsEntryInvalid
		}
		if v.ID != entryID("email", e.Provider, e.Mailbox, e.ProviderMessageID) {
			return ErrCommsEntryInvalid
		}
		for _, s := range append(append([]string{e.Subject, e.ThreadID, e.MessageID, e.InReplyTo}, e.To...), e.References...) {
			if s != "" && !boundedIdentifier(s, 998) {
				return ErrCommsEntryInvalid
			}
		}
		for _, s := range e.To {
			if s == "" {
				return ErrCommsEntryInvalid
			}
		}
	default:
		return ErrCommsEntryInvalid
	}
	return nil
}

// conversationCursor is scoped to one contact and uses MongoDB time precision.
type conversationCursor struct {
	CommsID string `json:"c"`
	ID      string `json:"i"`
	Millis  int64  `json:"t"`
}

// encodeConversationCursor preserves the complete immutable ordering tuple.
func encodeConversationCursor(v CommsEntry) string {
	b, _ := json.Marshal(conversationCursor{v.CommsID, v.ID, v.RecordedAt.UnixMilli()})
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeConversationCursor treats malformed/cross-contact cursors as bad input.
func decodeConversationCursor(v, commsID string) (*CommsEntry, error) {
	if v == "" {
		return nil, nil
	}
	if len(v) > 1024 {
		return nil, ErrCommsEntryInvalid
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(v)
	if err != nil {
		return nil, ErrCommsEntryInvalid
	}
	var c conversationCursor
	if json.Unmarshal(b, &c) != nil || c.CommsID != commsID || !validEntryID(c.ID) || c.Millis <= 0 || c.Millis > 253402300799999 {
		return nil, ErrCommsEntryInvalid
	}
	return &CommsEntry{ID: c.ID, CommsID: c.CommsID, RecordedAt: time.UnixMilli(c.Millis).UTC()}, nil
}

// AppendCommsEntry records a private note or reported reply without sending mail.
func (s *Service) AppendCommsEntry(ctx context.Context, req *AppendCommsEntryRequest) (*AppendCommsEntryResponse, error) {
	if req == nil {
		return nil, ErrCommsEntryInvalid
	}
	r := *req
	u, err := uuid.Parse(r.RequestID)
	if err != nil || u.Version() != 4 || u.Variant() != uuid.RFC4122 || u.String() != r.RequestID || (r.Kind != CommsEntryInternalNote && r.Kind != CommsEntryReply) {
		return nil, ErrCommsEntryInvalid
	}
	v := &CommsEntry{ID: entryID("admin", r.CommsID, r.ActorID, r.RequestID), CommsID: r.CommsID, ActorID: r.ActorID, Kind: r.Kind, Body: r.Body, ParentEntryID: r.ParentEntryID, RecordedAt: time.Now().UTC().Truncate(time.Millisecond)}
	return s.appendConversation(ctx, v)
}

// ImportCommsEmail records a verified adapter's selected mail event. RFC header
// IDs never form its deduplication key. No signature or mailbox authentication
// is performed here; integrations must authorize before calling this lower port.
func (s *Service) ImportCommsEmail(ctx context.Context, req *ImportCommsEmailRequest) (*AppendCommsEntryResponse, error) {
	if req == nil || req.Entry.Email == nil || (req.Entry.Kind != CommsEntryEmailInbound && req.Entry.Kind != CommsEntryEmailOutbound) {
		return nil, ErrCommsEntryInvalid
	}
	v := copyCommsEntry(&req.Entry)
	e := v.Email
	v.ID = entryID("email", e.Provider, e.Mailbox, e.ProviderMessageID)
	v.RecordedAt = time.Now().UTC().Truncate(time.Millisecond)
	e.OccurredAt = e.OccurredAt.UTC().Truncate(time.Millisecond)
	return s.appendConversation(ctx, v)
}

// conversationRoot verifies capability, actor and exact selected-root receipts.
func (s *Service) conversationRoot(ctx context.Context, actor, commsID string) (*Comms, ConversationRepository, error) {
	if err := conversationActor(ctx, actor); err != nil {
		return nil, nil, err
	}
	if !boundedIdentifier(commsID, 128) {
		return nil, nil, ErrCommsIdRequired
	}
	if s == nil || nilConversationPort(s.contacterRepository) {
		return nil, nil, ErrCommsConversationUnavailable
	}
	p, ok := s.contacterRepository.(ConversationRepository)
	if !ok || nilConversationPort(p) {
		return nil, nil, ErrCommsConversationUnavailable
	}
	rows, err := s.contacterRepository.GetCommsByIds(ctx, []string{commsID})
	if err != nil {
		return nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, ErrCommsNotFound
	}
	if len(rows) != 1 || rows[0].Id != commsID {
		return nil, nil, ErrCommsConversationUnavailable
	}
	root := rows[0]
	return &root, p, nil
}

// appendConversation checks relationships before persistence. Root deletion and
// insertion are not a transaction; entries survive as inaccessible retained
// history if a privileged legacy workflow concurrently deletes the contact.
func (s *Service) appendConversation(ctx context.Context, v *CommsEntry) (*AppendCommsEntryResponse, error) {
	if err := validateCommsEntry(v); err != nil {
		return nil, err
	}
	_, p, err := s.conversationRoot(ctx, v.ActorID, v.CommsID)
	if err != nil {
		return nil, err
	}
	if v.ParentEntryID != "" {
		parent, err := p.FindCommsEntry(ctx, v.CommsID, v.ParentEntryID)
		if err != nil {
			return nil, err
		}
		if parent == nil || parent.CommsID != v.CommsID || parent.ID != v.ParentEntryID {
			return nil, ErrCommsConversationUnavailable
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stored, replayed, err := p.InsertCommsEntry(ctx, copyCommsEntry(v))
	if err != nil {
		return nil, err
	}
	if validateCommsEntry(stored) != nil || !sameCommsEntryContent(stored, v) {
		return nil, ErrCommsConversationUnavailable
	}
	return &AppendCommsEntryResponse{Entry: copyCommsEntry(stored), Replayed: replayed}, nil
}

// ListCommsConversation returns bounded admin-only history plus legacy snapshots.
// Paging is a deterministic keyset walk, not a consistent snapshot during writes.
func (s *Service) ListCommsConversation(ctx context.Context, req *ListCommsConversationRequest) (*CommsConversationPage, error) {
	if req == nil {
		return nil, ErrCommsEntryInvalid
	}
	r := *req
	if r.Limit == 0 {
		r.Limit = 25
	}
	if r.Limit < 1 || r.Limit > 100 {
		return nil, ErrCommsEntryInvalid
	}
	after, err := decodeConversationCursor(r.Cursor, r.CommsID)
	if err != nil {
		return nil, err
	}
	root, p, err := s.conversationRoot(ctx, r.ActorID, r.CommsID)
	if err != nil {
		return nil, err
	}
	rows, err := p.QueryCommsEntries(ctx, r.CommsID, after, r.Limit)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) > r.Limit+1 {
		return nil, ErrCommsConversationUnavailable
	}
	previous := after
	for i := range rows {
		v := &rows[i]
		if validateCommsEntry(v) != nil || v.CommsID != r.CommsID || previous != nil && !entryBefore(v, previous) {
			return nil, ErrCommsConversationUnavailable
		}
		previous = v
	}
	page := &CommsConversationPage{Legacy: root, Entries: []CommsEntry{}}
	if len(rows) > r.Limit {
		page.NextCursor = encodeConversationCursor(rows[r.Limit-1])
		rows = rows[:r.Limit]
	}
	for i := range rows {
		page.Entries = append(page.Entries, *copyCommsEntry(&rows[i]))
	}
	return page, nil
}

// entryBefore applies the exact descending keyset order, including time ties.
func entryBefore(a, b *CommsEntry) bool {
	return a.RecordedAt.Before(b.RecordedAt) || a.RecordedAt.Equal(b.RecordedAt) && a.ID < b.ID
}
