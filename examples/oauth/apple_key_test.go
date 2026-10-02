package oauthsetup

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAppleSigningKeyPrecedenceAndFallback(t *testing.T) {
	key := []byte("synthetic key bytes; the provider validates PEM separately")
	encoded := base64.StdEncoding.EncodeToString(key)
	file := filepath.Join(t.TempDir(), "fallback.p8")
	require.NoError(t, os.WriteFile(file, []byte("obsolete file contents"), 0600))
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing.p8"), file} {
		actual, err := LoadAppleSigningKey(encoded, path)
		require.NoError(t, err)
		require.Equal(t, key, actual)
	}
	require.NoError(t, os.WriteFile(file, key, 0600))
	actual, err := LoadAppleSigningKey("", file)
	require.NoError(t, err)
	require.Equal(t, key, actual)
	_, err = LoadAppleSigningKey("", filepath.Join(t.TempDir(), "missing.p8"))
	require.EqualError(t, err, "apple OAuth signing key cannot be read")
}

func TestLoadAppleSigningKeyRejectsInvalidEncodingWithoutFallback(t *testing.T) {
	file := filepath.Join(t.TempDir(), "readable.p8")
	require.NoError(t, os.WriteFile(file, []byte("fallback must not mask an invalid secret"), 0600))
	for _, encoded := range []string{"secret-invalid!", " ", "\n", "c2VjcmV0=", "c2VjcmV0!", "Zh=="} {
		key, err := LoadAppleSigningKey(encoded, file)
		require.Nil(t, key)
		require.EqualError(t, err, "apple OAuth signing key base64 is invalid")
	}
}
