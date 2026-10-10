package notifierhelper

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
)

var (
	// ErrVAPIDKeyPairInvalid refuses malformed or mismatched P-256 keys.
	ErrVAPIDKeyPairInvalid = errors.New("notifierhelper: invalid VAPID key pair")
	// ErrFCMServiceAccountInvalid refuses incomplete/mismatched credential identity.
	ErrFCMServiceAccountInvalid = errors.New("notifierhelper: invalid FCM service account")
	// ErrFCMSigningKeyInvalid refuses a missing or unparsable PKCS8 key.
	ErrFCMSigningKeyInvalid = errors.New("notifierhelper: invalid FCM signing key")
	// ErrFCMRSASigningKeyRequired refuses non-RSA PKCS8 signing keys.
	ErrFCMRSASigningKeyRequired = errors.New("notifierhelper: FCM RSA signing key required")
)

// ValidateVAPIDKeyPair checks URL-base64 public/private keys for one matching
// P-256 pair, accepting padded and unpadded strings. It performs no I/O or
// enablement inference. Failures return only ErrVAPIDKeyPairInvalid.
func ValidateVAPIDKeyPair(public, private string) error {
	publicBytes, publicErr := base64.RawURLEncoding.DecodeString(strings.TrimRight(public, "="))
	privateBytes, privateErr := base64.RawURLEncoding.DecodeString(strings.TrimRight(private, "="))
	key, keyErr := ecdh.P256().NewPrivateKey(privateBytes)
	if publicErr != nil || privateErr != nil || keyErr != nil || !bytes.Equal(key.PublicKey().Bytes(), publicBytes) {
		return ErrVAPIDKeyPairInvalid
	}
	return nil
}

// ValidateFCMServiceAccountJSON applies an optional strict startup profile to
// resolved JSON: exact nonblank project identity, service_account type, Google
// token URI, service-account email suffix and a PKCS8 RSA key. It does not
// authenticate the account/key with Google. No file/env/provider I/O occurs;
// existing sender construction and SDK credential acceptance are unchanged.
// Identity/JSON, PKCS8 parsing and key-type failures have distinct fixed sentinels.
func ValidateFCMServiceAccountJSON(raw []byte, expectedProjectID string) error {
	var credential struct {
		Type        string `json:"type"`
		ProjectID   string `json:"project_id"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if strings.TrimSpace(expectedProjectID) == "" || json.Unmarshal(raw, &credential) != nil || credential.Type != "service_account" || credential.ProjectID != expectedProjectID || !strings.HasSuffix(credential.ClientEmail, ".iam.gserviceaccount.com") || credential.TokenURI != "https://oauth2.googleapis.com/token" {
		return ErrFCMServiceAccountInvalid
	}
	block, _ := pem.Decode([]byte(credential.PrivateKey))
	if block == nil {
		return ErrFCMSigningKeyInvalid
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return ErrFCMSigningKeyInvalid
	}
	if _, ok := key.(*rsa.PrivateKey); !ok {
		return ErrFCMRSASigningKeyRequired
	}
	return nil
}
