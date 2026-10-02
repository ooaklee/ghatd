// Package encryption provides authenticated payload encryption. Key storage,
// rotation, payload schemas and identity construction belong to the host.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

var (
	// ErrInvalidKey rejects keys other than an operator-supplied 32-byte AES key.
	ErrInvalidKey = errors.New("encryption/invalid-256-bit-key")
	// ErrInvalidIdentity requires explicit additional authenticated data (AAD).
	ErrInvalidIdentity = errors.New("encryption/identity-required")
	// ErrInvalidPayload means truncation, tampering, wrong identity or wrong key.
	// It intentionally reveals no plaintext, ciphertext or decryption diagnostics.
	ErrInvalidPayload = errors.New("encryption/invalid-payload")
)

// PayloadCipher seals bytes using AES-256-GCM and a fresh random 96-bit nonce.
// It is immutable and safe for concurrent use. The zero value fails closed.
// The durable format is nonce || ciphertext || tag, with no implicit version or
// key ID. Hosts must persist a stable key and plan rotation explicitly.
type PayloadCipher struct {
	// aead holds expanded key material, never an alias to the caller's key slice.
	aead cipher.AEAD
}

// NewPayloadCipher constructs a cipher from exactly 32 bytes. It does not generate,
// persist or log keys; changing the key makes previous ciphertext unreadable.
func NewPayloadCipher(key []byte) (*PayloadCipher, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &PayloadCipher{aead: aead}, nil
}

// Seal returns detached nonce-prefixed ciphertext. Identity must be nonempty,
// unambiguous, domain-separated AAD; use the exact same bytes to Open. Plaintext
// may be empty. Neither input slice is modified or retained. Do not encrypt more
// than 2^32 messages with one key because random nonce collisions become unsafe.
func (c *PayloadCipher) Seal(plain, identity []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrInvalidKey
	}
	if len(identity) == 0 {
		return nil, ErrInvalidIdentity
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, identity), nil
}

// Open authenticates the entire payload and expected identity before returning
// detached plaintext. On failure no partial plaintext is returned. Identity and
// key rotation decisions are not inferred from untrusted ciphertext.
func (c *PayloadCipher) Open(sealed, identity []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrInvalidKey
	}
	if len(identity) == 0 {
		return nil, ErrInvalidIdentity
	}
	if len(sealed) < c.aead.NonceSize()+c.aead.Overhead() {
		return nil, ErrInvalidPayload
	}
	plain, err := c.aead.Open(nil, sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():], identity)
	if err != nil {
		return nil, ErrInvalidPayload
	}
	return plain, nil
}
