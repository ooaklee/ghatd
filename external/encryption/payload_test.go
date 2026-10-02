package encryption

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayloadCipherCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name     string
		plain    []byte
		identity string
	}{
		{"empty plaintext", nil, "record:v1:1"},
		{"exact bytes", []byte(" \n{\"amount\":12,\"name\":\"é\"}\t"), "record:v1:2"},
		{"binary", []byte{0, 255, 128, 1}, "receipt:fingerprint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := bytes.Repeat([]byte{7}, 32)
			c, err := NewPayloadCipher(key)
			require.NoError(t, err)
			// This is the pre-extraction format, deliberately using the standard
			// library rather than the new encoder to prove stored-byte compatibility.
			block, err := aes.NewCipher(key)
			require.NoError(t, err)
			legacy, err := cipher.NewGCM(block)
			require.NoError(t, err)
			nonce := bytes.Repeat([]byte{3}, legacy.NonceSize())
			stored := legacy.Seal(append([]byte(nil), nonce...), nonce, tc.plain, []byte(tc.identity))
			plain, err := c.Open(stored, []byte(tc.identity))
			require.NoError(t, err)
			require.Equal(t, tc.plain, plain)
			for i := range key {
				key[i] = 0
			} // cipher owns expanded key state
			sealed, err := c.Seal(tc.plain, []byte(tc.identity))
			require.NoError(t, err)
			plain, err = legacy.Open(nil, sealed[:12], sealed[12:], []byte(tc.identity))
			require.NoError(t, err)
			require.Equal(t, tc.plain, plain)
			second, err := c.Seal(tc.plain, []byte(tc.identity))
			require.NoError(t, err)
			require.NotEqual(t, sealed[:12], second[:12], "fresh nonce")
		})
	}
}

func TestPayloadCipherRejectsInvalidInputs(t *testing.T) {
	for _, size := range []int{0, 16, 24, 31, 33} {
		t.Run(strconv.Itoa(size)+" bytes", func(t *testing.T) {
			_, err := NewPayloadCipher(make([]byte, size))
			require.ErrorIs(t, err, ErrInvalidKey)
		})
	}
	for _, tc := range []struct {
		name string
		mode string
		want error
	}{
		{"nil cipher", "nil", ErrInvalidKey}, {"zero cipher", "zero", ErrInvalidKey},
		{"empty AAD", "aad", ErrInvalidIdentity}, {"truncated nonce", "nonce", ErrInvalidPayload},
		{"truncated tag", "tag", ErrInvalidPayload}, {"tampered", "tamper", ErrInvalidPayload},
		{"wrong identity", "identity", ErrInvalidPayload}, {"wrong key", "key", ErrInvalidPayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewPayloadCipher(bytes.Repeat([]byte{7}, 32))
			require.NoError(t, err)
			identity := []byte("record:v1")
			sealed, err := c.Seal([]byte("private"), identity)
			require.NoError(t, err)
			switch tc.mode {
			case "nil":
				c = nil
			case "zero":
				c = &PayloadCipher{}
			case "aad":
				identity = nil
			case "nonce":
				sealed = sealed[:2]
			case "tag":
				sealed = sealed[:15]
			case "tamper":
				sealed[len(sealed)-1] ^= 1
			case "identity":
				identity = []byte("other")
			case "key":
				c, err = NewPayloadCipher(make([]byte, 32))
				require.NoError(t, err)
			}
			plain, err := c.Open(sealed, identity)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, plain)
			if tc.mode == "nil" || tc.mode == "zero" || tc.mode == "aad" {
				_, err = c.Seal(nil, identity)
				require.ErrorIs(t, err, tc.want)
			}
		})
	}
}
