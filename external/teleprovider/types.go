package teleprovider

import (
	"context"
	"net/http"
	"time"
)

// Stable error codes let callers choose recovery without exposing provider diagnostics.
const (
	// CodeInvalidNumber rejects a number without canonical E.164 and a valid country code.
	CodeInvalidNumber = "TELE_INVALID_NUMBER"
	// CodeInvalidRequest rejects invalid operation arguments or provider configuration.
	CodeInvalidRequest = "TELE_INVALID_REQUEST"
	// CodeNotRegistered records an authoritative negative registration result.
	CodeNotRegistered = "TELE_NUMBER_NOT_REGISTERED"
	// CodeUnavailable covers a disabled provider or an unavailable read operation.
	CodeUnavailable = "TELE_PROVIDER_UNAVAILABLE"
	// CodeSessionNotReady means the selected provider session cannot serve the operation.
	CodeSessionNotReady = "TELE_SESSION_NOT_READY"
	// CodeRateLimited requires callers to honour any accompanying retry delay.
	CodeRateLimited = "TELE_RATE_LIMITED"
	// CodeUnsupported means the provider or its configured engine cannot perform the operation.
	CodeUnsupported = "TELE_UNSUPPORTED_OPERATION"
	// CodeRejected records a provider's explicit rejection of a mutation.
	CodeRejected = "TELE_OPERATION_REJECTED"
	// CodeUncertain means a mutation's outcome cannot be confirmed and must not be blindly replayed.
	CodeUncertain = "TELE_OUTCOME_UNCERTAIN"
	// CodeInvalidResponse rejects incomplete, malformed or inconsistent read evidence.
	CodeInvalidResponse = "TELE_INVALID_RESPONSE"
	// CodeInvalidInvite records the provider's rejection of an invalid or unavailable invite.
	CodeInvalidInvite = "TELE_INVALID_INVITE"
	// CodeAuthFailed refers to provider credentials, rather than application sign-in.
	CodeAuthFailed = "TELE_PROVIDER_AUTH_FAILED"
	// CodePermissionDenied means the provider key lacks access to the requested operation.
	CodePermissionDenied = "TELE_PROVIDER_ACCESS_DENIED"
	// CodeCursorUnavailable reports a rejected stored-row cursor as a visible history gap.
	CodeCursorUnavailable = "TELE_CURSOR_UNAVAILABLE"
)

// Error exposes safe classification without disclosing provider diagnostics.
// An uncertain mutation needs caller reconciliation before any retry.
type Error struct {
	// Code is the stable application error classification returned by Error.
	Code string
	// RetryAfter is the provider's minimum suggested delay; zero supplies no guidance.
	RetryAfter time.Duration
	// Uncertain indicates that a mutation may already have taken effect.
	Uncertain bool
	// cause preserves cancellation or transport errors for server-side inspection only.
	cause error
	// retryable permits bounded adapter read retries, never mutation retries.
	retryable bool
	// status retains the upstream HTTP status for operation-specific error translation.
	status int
}

// Error returns only the safe code, excluding credentials and provider diagnostics.
func (e *Error) Error() string { return e.Code }

// Unwrap exposes the underlying transport or context error for errors.Is/As.
// Callers must not serialise its potentially sensitive diagnostics to clients.
func (e *Error) Unwrap() error { return e.cause }

// Config is resolved by the host. Adapter is an extension seam for an additional
// provider; provider-specific credentials stay out of service operations.
type Config struct {
	// Provider selects openwa, disabled or an injected adapter's non-empty provider name.
	// An empty value also disables the service.
	Provider string
	// OpenWA supplies the built-in adapter's connection settings; leave empty with Adapter.
	OpenWA OpenWAConfig
	// Adapter injects an owning provider implementation and excludes OpenWA/HTTPClient.
	Adapter Provider
	// HTTPClient supplies the OpenWA transport; the adapter copies it and refuses redirects.
	HTTPClient *http.Client
	// Timeout bounds an entire OpenWA operation, including read retries. Zero uses 15 seconds;
	// explicit values must be between one millisecond and one minute.
	Timeout time.Duration
	// ReadRetryLimit is zero (no retry) through two; mutations always make one attempt.
	ReadRetryLimit int
}

// OpenWAConfig binds requests to an explicit endpoint, credential and session.
// These values remain server-side and are supplied through the host environment.
type OpenWAConfig struct {
	// Endpoint is the API base URL, including /api; plain HTTP is limited to loopback.
	Endpoint string
	// APIKey authenticates provider requests and must never enter logs or client responses.
	APIKey string
	// SessionID is the canonical non-zero UUID used by every operation; no session is inferred.
	SessionID string
	// Engine is baileys, whatsapp-web.js or empty; it determines known group-creation support.
	Engine string
}

// Registration is registration evidence, never proof of ownership or delivery.
type Registration struct {
	// Status is registered, not_registered or unknown; failures must not imply absence.
	Status string `json:"status"`
	// Code supplies an optional safe explanation for a negative or unknown result.
	Code string `json:"code,omitempty"`
	// Number is the checked canonical E.164 input, including its country calling code.
	Number string `json:"normalized_number"`
	// CheckedAt records the check time; unknown results record an attempt, not ownership verification.
	CheckedAt time.Time `json:"checked_at"`
	// CanonicalID is an opaque provider contact identity for server-side composition only.
	CanonicalID string `json:"-"`
}

// MessageRequest is an adapter-ready text operation with caller-established destination authority.
type MessageRequest struct {
	// ChatID is a canonical contact or group ID, rather than an unverified local phone number.
	ChatID string
	// Text is non-blank UTF-8 text limited by the service to 4,096 code points, without NUL.
	Text string
	// QuotedMessageID optionally identifies a provider message to quote, using its engine's format.
	QuotedMessageID string
}

// MessageReceipt confirms provider acceptance; it is not delivery or recipient acknowledgement.
type MessageReceipt struct {
	// State must be accepted for a successful service result.
	State string
	// MessageID is the opaque identity returned by the provider for the accepted message.
	MessageID string
	// AcceptedAt is the provider's acceptance timestamp in UTC.
	AcceptedAt time.Time
}

// GroupRequest is the adapter-ready group definition after participant validation and deduplication.
type GroupRequest struct {
	// Name is a non-blank UTF-8 group name limited by the service to 100 code points.
	Name string
	// Participants contains canonical contact IDs already resolved by the service.
	Participants []string
}

// GroupReceipt identifies a provider group after a create or join operation.
type GroupReceipt struct {
	// GroupID is a canonical @g.us identity; it must not be interpreted as a phone number.
	GroupID string
	// Name is returned for creation; the join endpoint may leave it empty.
	Name string
}

// MessageQuery scopes a bounded read of stored provider messages to one chat.
type MessageQuery struct {
	// ChatID is the canonical contact or group identity whose stored rows are requested.
	ChatID string
	// Limit bounds raw rows before outgoing messages are filtered; zero defaults to 50,
	// and explicit service values must be between one and 100.
	Limit int
	// After is an opaque stored-row ID that pages towards older rows, not a new-message watermark.
	After string
}

// Message preserves provider identity without interpreting opaque IDs as phones.
type Message struct {
	// RowID is the provider's storage identity used for pagination and scoped deduplication.
	RowID string
	// MessageID is the engine's WhatsApp message identity, distinct from RowID.
	MessageID string
	// SessionID identifies the configured provider session that owns the row.
	SessionID string
	// ChatID identifies the contact or group conversation containing the row.
	ChatID string
	// Sender preserves the provider's from identity, which may be a group ID.
	Sender string
	// Author optionally identifies the person who wrote a group message.
	Author string
	// Direction is incoming or outgoing; ListReplies retains only incoming rows.
	Direction string
	// Text contains the stored body without media expansion or business interpretation.
	Text string
	// Type preserves the provider's message-kind label.
	Type string
	// Status preserves provider status without asserting delivery or acknowledgement.
	Status string
	// QuotedMessageID is the quoted engine message ID when supplied by provider metadata.
	QuotedMessageID string
	// Timestamp is the provider's message time; equal times do not define pagination order.
	Timestamp time.Time
}

// MessagePage is newest-first. A new poll starts with an empty cursor; repeated
// pages may contain already consumed IDs. Callers own acceptance and checkpoints.
type MessagePage struct {
	// Messages contains validated rows; ListReplies filters out outgoing echoes.
	Messages []Message
	// NextCursor advances towards older raw rows, including on outgoing-only pages.
	NextCursor string
	// HasMore indicates a full raw page may have older rows, not that another row is guaranteed.
	HasMore bool
}

// Capabilities describes configured provider support without probing session readiness.
type Capabilities struct {
	// CheckNumber supports registration lookup without establishing number ownership.
	CheckNumber bool
	// DirectMessages supports text operations addressed to a canonical contact.
	DirectMessages bool
	// GroupMessages supports text operations addressed to a canonical group.
	GroupMessages bool
	// JoinGroup supports accepting a provider group invite code.
	JoinGroup bool
	// Replies supports reading stored messages for incoming-reply processing.
	Replies bool
	// CreateGroup advertises creation support for the configured provider.
	CreateGroup bool
	// GroupCreationKnown distinguishes an unsupported engine from an unspecified one.
	GroupCreationKnown bool
}

// RegistrationProvider is the narrow registration port for a provider adapter.
type RegistrationProvider interface {
	// CheckNumber returns matching positive or explicit negative evidence for canonical E.164.
	CheckNumber(context.Context, string) (Registration, error)
}

// MessagingProvider is the adapter port for sending validated text requests once.
type MessagingProvider interface {
	// SendText returns acceptance evidence or an error requiring caller-led recovery.
	SendText(context.Context, MessageRequest) (MessageReceipt, error)
}

// GroupProvider is the adapter port for validated group mutations without automatic replay.
type GroupProvider interface {
	// CreateGroup submits a validated name and canonical participant IDs to the provider.
	CreateGroup(context.Context, GroupRequest) (GroupReceipt, error)
	// JoinGroup accepts a validated invite code and returns the joined group identity.
	JoinGroup(context.Context, string) (GroupReceipt, error)
}

// ReplyProvider is the adapter port for raw stored-message pages, including outgoing rows.
type ReplyProvider interface {
	// ListMessages reads a scoped newest-first page; the service validates and filters its rows.
	ListMessages(context.Context, MessageQuery) (MessagePage, error)
}

// Provider composes the owning adapter ports consumed by Service.
// Adapters must honour context cancellation and preserve the requested session/chat scope.
// Host authorisation, consent, durable processing and checkpointing remain caller responsibilities.
type Provider interface {
	RegistrationProvider
	MessagingProvider
	GroupProvider
	ReplyProvider
	// Capabilities reports configured support, including whether group creation is known.
	Capabilities() Capabilities
}
