package oauthhelper

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAppleSigningKeyPrecedenceAndFallback(t *testing.T) {
	type keyCase struct {
		name, encoded, fileContents string
		missing, noPath             bool
		want, wantError             string
	}
	const key = "synthetic key bytes; the provider validates PEM separately"
	encoded := base64.StdEncoding.EncodeToString([]byte(key))
	for _, test := range []keyCase{
		{name: "encoded without file", encoded: encoded, noPath: true, want: key},
		{name: "encoded with missing file", encoded: encoded, missing: true, want: key},
		{name: "encoded overrides obsolete file", encoded: encoded, fileContents: "obsolete file contents", want: key},
		{name: "file fallback", fileContents: key, want: key},
		{name: "empty file is left for provider validation"},
		{name: "missing file", missing: true, wantError: "apple OAuth signing key cannot be read"},
		{name: "missing path", noPath: true, wantError: "apple OAuth signing key cannot be read"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fallback.p8")
			if test.noPath {
				path = ""
			} else if !test.missing {
				require.NoError(t, os.WriteFile(path, []byte(test.fileContents), 0600))
			}
			actual, err := LoadAppleSigningKey(test.encoded, path)
			if test.wantError != "" {
				require.Nil(t, actual)
				require.EqualError(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []byte(test.want), actual)
		})
	}
}

func TestLoadAppleSigningKeyRejectsInvalidEncodingWithoutFallback(t *testing.T) {
	type encodingCase struct{ name, encoded string }
	for _, test := range []encodingCase{
		{"illegal characters", "secret-invalid!"}, {"space", " "}, {"newline decodes empty", "\n"},
		{"wrong padding", "c2VjcmV0="}, {"illegal suffix", "c2VjcmV0!"}, {"nonzero trailing bits", "Zh=="},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "readable.p8")
			require.NoError(t, os.WriteFile(path, []byte("fallback must not mask an invalid secret"), 0600))
			key, err := LoadAppleSigningKey(test.encoded, path)
			require.Nil(t, key)
			require.EqualError(t, err, "apple OAuth signing key base64 is invalid")
			require.NotContains(t, err.Error(), path)
		})
	}
}
