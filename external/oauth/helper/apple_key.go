package oauthhelper

import (
	"encoding/base64"
	"fmt"
	"os"
)

// LoadAppleSigningKey resolves explicit encoded or file-backed signing-key bytes.
// A non-empty standard-base64 value takes precedence and is decoded in memory.
// Invalid encoding fails without falling back or exposing the key or file path.
// Pass the returned PEM bytes to NewAppleProvider, which validates the P-256 key.
// It reads only the supplied file when encoded is empty, never environment
// variables or temporary files. Inputs must come from trusted host configuration.
func LoadAppleSigningKey(encoded, path string) ([]byte, error) {
	if encoded != "" {
		key, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(key) == 0 {
			return nil, fmt.Errorf("apple OAuth signing key base64 is invalid")
		}
		return key, nil
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("apple OAuth signing key cannot be read")
	}
	return key, nil
}
