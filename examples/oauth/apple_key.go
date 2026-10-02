package oauthsetup

import (
	"encoding/base64"
	"fmt"
	"os"
)

// LoadAppleSigningKey demonstrates a host-owned adapter for encoded or file keys.
// A non-empty standard-base64 value takes precedence and is decoded in memory.
// Invalid encoding fails without falling back or exposing the key or file path.
// Pass the returned PEM bytes to NewAppleProvider, which validates the P-256 key.
// This example does not read environment variables or write temporary files.
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
