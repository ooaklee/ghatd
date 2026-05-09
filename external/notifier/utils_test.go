package notifier

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestResolveCredentialsFile_EmptyBase64_ReturnsFile(t *testing.T) {
	path, err := ResolveCredentialsFile("", "/etc/fcm.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/etc/fcm.json" {
		t.Errorf("expected /etc/fcm.json, got %q", path)
	}
}

func TestResolveCredentialsFile_EmptyBase64_EmptyFile(t *testing.T) {
	path, err := ResolveCredentialsFile("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestResolveCredentialsFile_ValidBase64_WritesTempFile(t *testing.T) {
	original := `{"project_id":"test-123"}`
	b64 := base64.StdEncoding.EncodeToString([]byte(original))

	path, err := ResolveCredentialsFile(b64, "/etc/fcm.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	if path == "/etc/fcm.json" {
		t.Errorf("expected a temp file path, got the fallback path %q", path)
	}
	if !strings.HasPrefix(path, os.TempDir()) {
		t.Errorf("expected path under %s, got %q", os.TempDir(), path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}
	if string(data) != original {
		t.Errorf("expected %q in temp file, got %q", original, string(data))
	}
}

func TestResolveCredentialsFile_InvalidBase64_Error(t *testing.T) {
	_, err := ResolveCredentialsFile("!!!not-valid-base64!!!", "/etc/fcm.json")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "decode base64 credentials") {
		t.Errorf("expected decode error, got %v", err)
	}
}
