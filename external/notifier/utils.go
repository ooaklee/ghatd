package notifier

import (
	"encoding/base64"
	"fmt"
	"os"
)

// ResolveCredentialsFile returns the file path to use for FCM credentials.
//
// If base64Credentials is non-empty, it is decoded and written to a temp
// file whose path is returned. The caller is responsible for removing the
// temp file when it is no longer needed. New callers should prefer
// ResolveCredentialsFileWithCleanup so cleanup is harder to forget.
//
// If base64Credentials is empty, credentialsFile is returned as-is.
func ResolveCredentialsFile(base64Credentials, credentialsFile string) (string, error) {
	path, _, err := ResolveCredentialsFileWithCleanup(base64Credentials, credentialsFile)
	return path, err
}

// ResolveCredentialsFileWithCleanup returns the file path to use for FCM
// credentials and a cleanup function for any temporary file it creates.
//
// If base64Credentials is empty, credentialsFile is returned as-is and the
// cleanup function is a no-op.
func ResolveCredentialsFileWithCleanup(base64Credentials, credentialsFile string) (string, func(), error) {
	if base64Credentials == "" {
		return credentialsFile, func() {}, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(base64Credentials)
	if err != nil {
		return "", func() {}, fmt.Errorf("notifier: decode base64 credentials: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "fcm-credentials-*.json")
	if err != nil {
		return "", func() {}, fmt.Errorf("notifier: create temp credentials file: %w", err)
	}
	if err := os.Chmod(tmpFile.Name(), 0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", func() {}, fmt.Errorf("notifier: secure temp credentials file: %w", err)
	}

	if _, err := tmpFile.Write(decoded); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", func() {}, fmt.Errorf("notifier: write temp credentials file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", func() {}, fmt.Errorf("notifier: close temp credentials file: %w", err)
	}

	return tmpFile.Name(), func() { _ = os.Remove(tmpFile.Name()) }, nil
}
