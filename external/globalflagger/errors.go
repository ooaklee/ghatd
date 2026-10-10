package globalflagger

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ooaklee/ghatd/external/catalogue"
)

// Package-specific validation errors. Storage outcomes reuse the shared
// catalogue sentinels (ErrNotFound, ErrStaleWrite, ErrAlreadyExists,
// ErrUnavailable, ErrInvalidPayload).
var (
	// ErrCodeRequired reports a missing or blank flag code.
	ErrCodeRequired = fmt.Errorf("%w: flag code is required", catalogue.ErrInvalidPayload)
	// ErrCodeInvalid reports a malformed code. Codes are stable UPPERCASE
	// identifiers of ASCII letters, digits and single interior hyphens
	// (GB, EU, GB-SCT), at most 16 characters.
	ErrCodeInvalid = fmt.Errorf("%w: flag code must be uppercase letters, digits and interior hyphens (max 16 characters)", catalogue.ErrInvalidPayload)
	// ErrNameRequired reports a missing or blank display name.
	ErrNameRequired = fmt.Errorf("%w: flag name is required", catalogue.ErrInvalidPayload)
	// ErrNameTooLong reports a display name longer than 128 characters.
	ErrNameTooLong = fmt.Errorf("%w: flag name exceeds 128 characters", catalogue.ErrInvalidPayload)
	// ErrActorRequired reports a missing actor identifier on a command.
	// Actor identity is trusted command context, never public input.
	ErrActorRequired = fmt.Errorf("%w: actor id is required", catalogue.ErrInvalidPayload)
	// ErrActorInvalid reports an actor identifier that is not trustworthy:
	// longer than 256 characters or containing CR/LF control characters
	// that could forge audit log lines.
	ErrActorInvalid = fmt.Errorf("%w: actor id must be non-blank, at most 256 characters and free of control characters", catalogue.ErrInvalidPayload)
	// ErrExpectedRevision reports a non-positive expected revision on an
	// admin mutation.
	ErrExpectedRevision = fmt.Errorf("%w: expected revision must be positive", catalogue.ErrInvalidPayload)
	// ErrCatalogueFull reports the bounded catalogue size ceiling reached.
	// Flags are reference data, not user content.
	ErrCatalogueFull = fmt.Errorf("%w: flag catalogue is full", catalogue.ErrInvalidPayload)
	// ErrAlreadyDeleted reports a soft-delete of an already-deleted record.
	ErrAlreadyDeleted = errors.New("globalflagger/already-deleted")
	// ErrNotDeleted reports a restore of a record that is not deleted.
	ErrNotDeleted = errors.New("globalflagger/not-deleted")
)

// codePattern matches stable UPPERCASE identifiers: letter/digit start and
// end, single interior hyphens only.
var codePattern = regexp.MustCompile(`^[A-Z0-9]([A-Z0-9-]{0,14}[A-Z0-9])?$`)

// MaxSVGBytes is the ceiling on accepted (and stored) sanitised SVG size.
const MaxSVGBytes = 256 << 10 // 256 KiB

// validateCode checks a flag code and returns its normalised form.
func validateCode(code string) (string, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "", ErrCodeRequired
	}
	if len(trimmed) > 16 || !codePattern.MatchString(trimmed) {
		return "", ErrCodeInvalid
	}
	return trimmed, nil
}

// validateName checks a display name and returns its normalised form.
func validateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrNameRequired
	}
	if len(trimmed) > 128 {
		return "", ErrNameTooLong
	}
	return trimmed, nil
}

// validateActor checks that the trusted command-context actor identifier
// is trustworthy for audit attribution: trimmed non-blank, at most 256
// characters, and free of CR/LF (and other line-breaking control
// characters) that could forge audit records downstream.
func validateActor(actor string) error {
	trimmed := strings.TrimSpace(actor)
	if trimmed == "" {
		return ErrActorRequired
	}
	if len(trimmed) > 256 || strings.ContainsAny(trimmed, "\r\n\v\f") || strings.ContainsRune(trimmed, 0x2028) || strings.ContainsRune(trimmed, 0x2029) {
		return ErrActorInvalid
	}
	return nil
}
