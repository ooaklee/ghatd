package teleprovider

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nyaruka/phonenumbers"
)

// e164 requires an explicit country calling code and canonical international digits.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// contactID accepts supported canonical contact identities without deriving a phone number.
var contactID = regexp.MustCompile(`^[0-9]{1,32}@(c\.us|s\.whatsapp\.net|lid)$`)

// groupID accepts the provider's canonical group identity formats.
var groupID = regexp.MustCompile(`^[0-9]+(?:-[0-9]+)?@g\.us$`)

// inviteCode bounds an invite token without accepting complete invite URLs.
var inviteCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// invalid constructs a safe classified error without provider diagnostics.
func invalid(code string) error { return &Error{Code: code} }

// nilLike recognises both nil interfaces and typed-nil adapter values.
func nilLike(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// ValidateNumber rejects local or noncanonical numbers; no country is inferred.
func ValidateNumber(number string) error {
	if !e164.MatchString(number) {
		return invalid(CodeInvalidNumber)
	}
	parsed, err := phonenumbers.Parse(number, "")
	if err != nil || !phonenumbers.IsValidNumber(parsed) || phonenumbers.Format(parsed, phonenumbers.E164) != number {
		return invalid(CodeInvalidNumber)
	}
	return nil
}

// Service owns validation and orchestration. No datastore, cache or HTTP driver
// is used here; OpenWA implements the provider boundary in openwa.go.
type Service struct {
	// provider owns provider-specific I/O behind the service's validation boundary.
	provider Provider
	// disabled prevents operations when the host has not selected an active provider.
	disabled bool
}

// NewTeleProvider constructs a disabled service, the configured OpenWA adapter or
// an injected provider. It validates configuration without making network calls.
func NewTeleProvider(config Config) (*Service, error) {
	if config.Adapter != nil {
		if nilLike(config.Adapter) || config.Provider == "" || config.Provider == "disabled" || config.OpenWA != (OpenWAConfig{}) || config.HTTPClient != nil {
			return nil, invalid(CodeInvalidRequest)
		}
		return &Service{provider: config.Adapter}, nil
	}
	if config.Provider == "" || config.Provider == "disabled" {
		return &Service{disabled: true}, nil
	}
	if config.Provider != "openwa" {
		return nil, invalid(CodeUnsupported)
	}
	p, err := newOpenWA(config)
	if err != nil {
		return nil, err
	}
	return &Service{provider: p}, nil
}

// guard rejects an absent/cancelled context or unavailable service before provider I/O.
func (s *Service) guard(ctx context.Context) error {
	if ctx == nil {
		return invalid(CodeInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.disabled || nilLike(s.provider) {
		return invalid(CodeUnavailable)
	}
	return nil
}

// Capabilities returns configured provider support, or no capabilities for a disabled service.
// It does not establish session readiness or permission to contact a destination.
func (s *Service) Capabilities() Capabilities {
	if s == nil || s.disabled || nilLike(s.provider) {
		return Capabilities{}
	}
	return s.provider.Capabilities()
}

// CheckNumber validates canonical E.164 before consulting the provider and verifies
// that returned evidence matches the input. Failures remain unknown; an explicit
// negative is a successful lookup, rather than proof about number ownership.
func (s *Service) CheckNumber(ctx context.Context, number string) (Registration, error) {
	result := Registration{Status: "unknown", Number: number, CheckedAt: time.Now().UTC()}
	if err := ValidateNumber(number); err != nil {
		result.Code = CodeInvalidNumber
		return result, err
	}
	if err := s.guard(ctx); err != nil {
		result.Code = CodeUnavailable
		return result, err
	}
	got, err := s.provider.CheckNumber(ctx, number)
	if err != nil {
		result.Code = CodeUnavailable
		var e *Error
		if errors.As(err, &e) {
			result.Code = e.Code
		}
		return result, err
	}
	if got.Number != number || got.CheckedAt.IsZero() || (got.Status != "registered" && got.Status != "not_registered") || (got.Status == "registered" && !contactID.MatchString(got.CanonicalID)) || (got.Status == "not_registered" && got.CanonicalID != "") {
		result.Code = CodeInvalidResponse
		return result, invalid(CodeInvalidResponse)
	}
	if got.Status == "not_registered" {
		got.Code = CodeNotRegistered
	} else {
		got.Code = ""
	}
	return got, nil
}

// validText bounds a non-blank UTF-8 message to 4,096 code points and rejects NUL.
func validText(s string) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= 4096 && !strings.ContainsRune(s, 0)
}

// opaque bounds an opaque provider identity without interpreting or normalising it.
func opaque(s string) bool {
	return s != "" && len(s) <= 256 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00")
}

// SendDirect accepts either E.164 or a canonical ID established by the caller.
// It resolves numbers before one send attempt; canonical IDs rely on caller-established authority.
// Checking registration does not establish current destination authority or prove delivery.
func (s *Service) SendDirect(ctx context.Context, destination, text string) (MessageReceipt, error) {
	if err := s.guard(ctx); err != nil {
		return MessageReceipt{}, err
	}
	if !validText(text) {
		return MessageReceipt{}, invalid(CodeInvalidRequest)
	}
	id := destination
	if !contactID.MatchString(id) {
		checked, err := s.CheckNumber(ctx, destination)
		if err != nil {
			return MessageReceipt{}, err
		}
		if checked.Status != "registered" {
			return MessageReceipt{}, invalid(CodeNotRegistered)
		}
		id = checked.CanonicalID
	}
	return s.send(ctx, MessageRequest{ChatID: id, Text: text})
}

// SendGroup sends validated text once to a canonical group identity supplied by
// the caller. A successful receipt confirms acceptance, rather than delivery.
func (s *Service) SendGroup(ctx context.Context, id, text string) (MessageReceipt, error) {
	if err := s.guard(ctx); err != nil {
		return MessageReceipt{}, err
	}
	if !groupID.MatchString(id) || !validText(text) {
		return MessageReceipt{}, invalid(CodeInvalidRequest)
	}
	return s.send(ctx, MessageRequest{ChatID: id, Text: text})
}

// ReplyToMessage sends text once while quoting an engine-specific message ID in
// a canonical contact or group chat. The caller owns quoted-ID provenance.
func (s *Service) ReplyToMessage(ctx context.Context, id, quoted, text string) (MessageReceipt, error) {
	if err := s.guard(ctx); err != nil {
		return MessageReceipt{}, err
	}
	if (!contactID.MatchString(id) && !groupID.MatchString(id)) || !opaque(quoted) || !validText(text) {
		return MessageReceipt{}, invalid(CodeInvalidRequest)
	}
	return s.send(ctx, MessageRequest{ChatID: id, Text: text, QuotedMessageID: quoted})
}

// send delegates one text operation and requires a complete acceptance receipt.
// An invalid mutation receipt remains uncertain and must not be blindly replayed.
func (s *Service) send(ctx context.Context, req MessageRequest) (MessageReceipt, error) {
	result, err := s.provider.SendText(ctx, req)
	if err != nil {
		return MessageReceipt{}, err
	}
	if result.State != "accepted" || !opaque(result.MessageID) || result.AcceptedAt.IsZero() {
		return MessageReceipt{}, &Error{Code: CodeUncertain, Uncertain: true}
	}
	return result, nil
}

// CreateGroup validates all participants before any lookup, resolves E.164 numbers
// to canonical IDs and deduplicates them before one creation attempt. Known
// unsupported engines fail before I/O; malformed success receipts remain uncertain.
func (s *Service) CreateGroup(ctx context.Context, name string, participants []string) (GroupReceipt, error) {
	if err := s.guard(ctx); err != nil {
		return GroupReceipt{}, err
	}
	if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 || len(participants) == 0 || len(participants) > 256 {
		return GroupReceipt{}, invalid(CodeInvalidRequest)
	}
	caps := s.Capabilities()
	if caps.GroupCreationKnown && !caps.CreateGroup {
		return GroupReceipt{}, invalid(CodeUnsupported)
	}
	ids := make([]string, 0, len(participants))
	seen := map[string]bool{}
	// Validate the entire input before performing any registration queries.
	for _, p := range participants {
		if !contactID.MatchString(p) {
			if err := ValidateNumber(p); err != nil {
				return GroupReceipt{}, err
			}
		}
	}
	for _, p := range participants {
		if !contactID.MatchString(p) {
			r, err := s.CheckNumber(ctx, p)
			if err != nil {
				return GroupReceipt{}, err
			}
			if r.Status != "registered" {
				return GroupReceipt{}, invalid(CodeNotRegistered)
			}
			p = r.CanonicalID
		}
		if !seen[p] {
			ids = append(ids, p)
			seen[p] = true
		}
	}
	result, err := s.provider.CreateGroup(ctx, GroupRequest{Name: name, Participants: ids})
	if err != nil {
		return GroupReceipt{}, err
	}
	if !groupID.MatchString(result.GroupID) || result.Name == "" {
		return GroupReceipt{}, &Error{Code: CodeUncertain, Uncertain: true}
	}
	return result, nil
}

// JoinGroup accepts an invite code (rather than a URL) once and validates the
// returned group ID. An ambiguous outcome requires caller reconciliation.
func (s *Service) JoinGroup(ctx context.Context, code string) (GroupReceipt, error) {
	if err := s.guard(ctx); err != nil {
		return GroupReceipt{}, err
	}
	if !inviteCode.MatchString(code) {
		return GroupReceipt{}, invalid(CodeInvalidRequest)
	}
	r, err := s.provider.JoinGroup(ctx, code)
	if err != nil {
		return GroupReceipt{}, err
	}
	if !groupID.MatchString(r.GroupID) {
		return GroupReceipt{}, &Error{Code: CodeUncertain, Uncertain: true}
	}
	return r, nil
}

// ListReplies reads a bounded newest-first raw page and returns only incoming rows.
// Its cursor advances from the last raw row, including outgoing-only pages, towards
// older history. Callers own deduplication, durable acceptance and checkpoints.
func (s *Service) ListReplies(ctx context.Context, query MessageQuery) (MessagePage, error) {
	if err := s.guard(ctx); err != nil {
		return MessagePage{}, err
	}
	if (!contactID.MatchString(query.ChatID) && !groupID.MatchString(query.ChatID)) || query.Limit < 0 || query.Limit > 100 || (query.After != "" && !opaque(query.After)) {
		return MessagePage{}, invalid(CodeInvalidRequest)
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	page, err := s.provider.ListMessages(ctx, query)
	if err != nil {
		return MessagePage{}, err
	}
	if len(page.Messages) > query.Limit {
		return MessagePage{}, invalid(CodeInvalidResponse)
	}
	result := MessagePage{Messages: []Message{}, HasMore: len(page.Messages) == query.Limit}
	seen := map[string]bool{}
	for _, m := range page.Messages {
		if !opaque(m.RowID) || m.ChatID != query.ChatID || m.SessionID == "" || (m.Direction != "incoming" && m.Direction != "outgoing") || seen[m.RowID] {
			return MessagePage{}, invalid(CodeInvalidResponse)
		}
		seen[m.RowID] = true
		if m.Direction == "incoming" {
			result.Messages = append(result.Messages, m)
		}
	}
	if result.HasMore {
		result.NextCursor = page.Messages[len(page.Messages)-1].RowID
	}
	return result, nil
}
