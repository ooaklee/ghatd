package browsersecurity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// IdentityConfig comes only from trusted host session resolution, never from
// request JSON, headers or unverified claims. Guard admission is not authority;
// the owning manager must recheck live session and exact action permissions.
type IdentityConfig struct {
	// BindingParts identify the host's current session/principal and purpose.
	// Order matters. They are hashed immediately; raw credentials are not retained
	// or exposed in the CSRF cookie. Use at most 16 parts of at most 8 KiB each,
	// with at most 64 KiB total. Public identities omit these parts.
	BindingParts []string
	// Public chooses anonymous nonce binding and immediate-peer rate identity.
	Public bool
	// ActorID optionally chooses a verified member's stable rate identity. It
	// never supplies an actor to a domain command or selects a target account.
	ActorID string
	// NativeClientID and NativeAuthenticated are a host-proven session and token
	// audience, checked against Config.TrustedNativeClientIDs by the guard.
	NativeClientID      string
	NativeAuthenticated bool
}

// Identity is an opaque transport binding, not a credential or domain principal.
// Its zero value fails closed. NewIdentity does not authenticate its inputs.
type Identity struct {
	binding             string
	public              bool
	actorID             string
	nativeClientID      string
	nativeAuthenticated bool
}

// NewIdentity hashes the trusted session inputs without retaining their slices.
// Hosts must preserve their binding order if retaining already issued cookies.
func NewIdentity(config IdentityConfig) (Identity, error) {
	if len(config.ActorID) > 256 || len(config.NativeClientID) > 128 ||
		(config.NativeAuthenticated && config.NativeClientID == "") {
		return Identity{}, ErrInvalidRequest
	}
	identity := Identity{public: config.Public, actorID: config.ActorID,
		nativeClientID: config.NativeClientID, nativeAuthenticated: config.NativeAuthenticated}
	if config.Public {
		if len(config.BindingParts) != 0 || config.ActorID != "" || config.NativeClientID != "" || config.NativeAuthenticated {
			return Identity{}, ErrInvalidRequest
		}
		identity.binding = "public"
		return identity, nil
	}
	if len(config.BindingParts) == 0 || len(config.BindingParts) > 16 {
		return Identity{}, ErrInvalidRequest
	}
	total := 0
	for _, part := range config.BindingParts {
		if len(part) > 8192 {
			return Identity{}, ErrInvalidRequest
		}
		total += len(part)
	}
	if total > 65536 {
		return Identity{}, ErrInvalidRequest
	}
	body, err := json.Marshal(config.BindingParts)
	if err != nil {
		return Identity{}, ErrInvalidRequest
	}
	sum := sha256.Sum256(body)
	identity.binding = hex.EncodeToString(sum[:])
	return identity, nil
}
