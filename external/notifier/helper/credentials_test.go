package notifierhelper

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateVAPIDKeyPair(t *testing.T) {
	type keyCase struct {
		name, change string
		wantError    bool
	}
	for _, test := range []keyCase{
		{"matching unpadded", "", false}, {"matching padded", "padding", false},
		{"mismatched pair", "mismatch", true}, {"invalid public encoding", "public encoding", true},
		{"invalid private encoding", "private encoding", true}, {"missing public", "empty public", true},
		{"missing private", "empty private", true}, {"wrong private length", "length", true},
		{"zero private scalar", "zero", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			key, err := ecdh.P256().GenerateKey(rand.Reader)
			require.NoError(t, err)
			public, private := base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(key.Bytes())
			switch test.change {
			case "padding":
				public, private = base64.URLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.URLEncoding.EncodeToString(key.Bytes())
			case "mismatch":
				other, err := ecdh.P256().GenerateKey(rand.Reader)
				require.NoError(t, err)
				public = base64.RawURLEncoding.EncodeToString(other.PublicKey().Bytes())
			case "public encoding":
				public = "private-secret-marker!"
			case "private encoding":
				private = "private-secret-marker!"
			case "empty public":
				public = ""
			case "empty private":
				private = ""
			case "length":
				private = base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3})
			case "zero":
				private = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
			}
			err = ValidateVAPIDKeyPair(public, private)
			if test.wantError {
				require.ErrorIs(t, err, ErrVAPIDKeyPairInvalid)
				require.NotContains(t, err.Error(), "private-secret-marker")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func serviceAccountFixture(t *testing.T) (map[string]string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return map[string]string{"type": "service_account", "project_id": "fixture-project", "client_email": "sender@fixture-project.iam.gserviceaccount.com", "token_uri": "https://oauth2.googleapis.com/token", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}, key
}

func TestValidateFCMServiceAccountJSON(t *testing.T) {
	type credentialCase struct {
		name, field, value, special string
		wantError                   error
	}
	for _, test := range []credentialCase{
		{name: "matching strict profile"},
		{name: "wrong expected project", special: "wrong expected", wantError: ErrFCMServiceAccountInvalid},
		{name: "empty expected project", special: "empty expected", wantError: ErrFCMServiceAccountInvalid},
		{name: "blank expected project", special: "blank expected", wantError: ErrFCMServiceAccountInvalid},
		{name: "wrong project", field: "project_id", value: "other", wantError: ErrFCMServiceAccountInvalid},
		{name: "project is not trimmed", field: "project_id", value: " fixture-project ", wantError: ErrFCMServiceAccountInvalid},
		{name: "wrong type", field: "type", value: "authorized_user", wantError: ErrFCMServiceAccountInvalid},
		{name: "wrong email suffix", field: "client_email", value: "private-secret-marker@example.test", wantError: ErrFCMServiceAccountInvalid},
		{name: "wrong token URI", field: "token_uri", value: "https://example.test/token", wantError: ErrFCMServiceAccountInvalid},
		{name: "empty JSON", special: "empty", wantError: ErrFCMServiceAccountInvalid},
		{name: "malformed JSON", special: "malformed", wantError: ErrFCMServiceAccountInvalid},
		{name: "wrong JSON field type", special: "wrong JSON type", wantError: ErrFCMServiceAccountInvalid},
		{name: "missing signing key", field: "private_key", value: "", wantError: ErrFCMSigningKeyInvalid},
		{name: "not PEM", field: "private_key", value: "private-secret-marker", wantError: ErrFCMSigningKeyInvalid},
		{name: "malformed PKCS8", special: "bad DER", wantError: ErrFCMSigningKeyInvalid},
		{name: "PKCS1 is outside profile", special: "PKCS1", wantError: ErrFCMSigningKeyInvalid},
		{name: "EC is outside profile", special: "EC", wantError: ErrFCMRSASigningKeyRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			credential, key := serviceAccountFixture(t)
			expected := "fixture-project"
			if test.field != "" {
				credential[test.field] = test.value
			}
			switch test.special {
			case "wrong expected":
				expected = "other"
			case "empty expected":
				expected = ""
			case "blank expected":
				expected = " "
			case "bad DER":
				credential["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("private-secret-marker")}))
			case "PKCS1":
				credential["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
			case "EC":
				ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)
				der, err := x509.MarshalPKCS8PrivateKey(ec)
				require.NoError(t, err)
				credential["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
			}
			raw, err := json.Marshal(credential)
			require.NoError(t, err)
			switch test.special {
			case "empty":
				raw = nil
			case "malformed":
				raw = []byte("private-secret-marker {")
			case "wrong JSON type":
				raw = []byte(`{"project_id":123}`)
			}
			err = ValidateFCMServiceAccountJSON(raw, expected)
			if test.wantError != nil {
				require.ErrorIs(t, err, test.wantError)
				require.NotContains(t, err.Error(), "private-secret-marker")
				require.NotContains(t, err.Error(), credential["project_id"])
			} else {
				require.NoError(t, err)
			}
		})
	}
}
