package waitlist

import (
	"errors"
	"regexp"
)

var configIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// StoreConfig configures new enrollments. Existing records are never reconsented
// or backfilled. SequenceEnrollment requires transaction-capable Mongo storage.
type StoreConfig struct {
	// ConsentVersion labels the displayed promise; empty selects the neutral default.
	ConsentVersion string
	// SequenceEnrollment assigns positive immutable ordinals during transactional joins.
	SequenceEnrollment bool
}

// CommsConfig records the host's signup promise on newly created contacts only.
// Use the same consent version in StoreConfig; neither setting changes identity.
type CommsConfig struct {
	// ConsentVersion records the displayed promise on new contacts; empty selects
	// the neutral default and must match the audience's StoreConfig value.
	ConsentVersion string
}

func consentVersion(value string) (string, error) {
	if value == "" {
		return ConsentVersion, nil
	}
	if !configIdentifier.MatchString(value) {
		return "", errors.New("invalid waitlist consent version")
	}
	return value, nil
}
