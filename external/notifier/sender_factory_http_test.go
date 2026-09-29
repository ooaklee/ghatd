package notifier

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type notifierRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f notifierRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type notifierRoundTripper struct{ fn notifierRoundTripperFunc }

func (r *notifierRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.fn(req)
}

func TestStandardSendersRequestWithHTTPClient(t *testing.T) {
	client := &http.Client{Timeout: 12 * time.Second}
	original := &StandardSendersRequest{FCM: &FCMSenderConfig{Enabled: true}}
	updated := original.WithHTTPClient(client)
	if original.HTTPClient != nil || updated == original || updated.HTTPClient != client || updated.FCM != original.FCM {
		t.Fatal("WithHTTPClient should copy the request and preserve its other fields")
	}
	var nilRequest *StandardSendersRequest
	if nilRequest.WithHTTPClient(client) != nil {
		t.Fatal("nil receiver should remain nil")
	}
}

func TestNewStandardSendersCopiesHTTPClientPolicy(t *testing.T) {
	transport := &notifierRoundTripper{fn: func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}}
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second}
	result, err := NewStandardSenders((&StandardSendersRequest{
		FCM: &FCMSenderConfig{Enabled: true, ProjectID: "test-project"},
	}).WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Cleanup()
	webPush := findSender[*WebPushSender](result.Senders)
	fcm := findSender[*FCMSender](result.Senders)
	if webPush == nil || fcm == nil || webPush.httpClient == nil || fcm.httpClient == nil {
		t.Fatal("expected both senders to receive an HTTP client")
	}
	if webPush.httpClient == client || fcm.httpClient == client || webPush.httpClient == fcm.httpClient {
		t.Fatal("senders should have private HTTP client values")
	}
	if webPush.httpClient.Transport != transport || fcm.httpClient.Transport != transport || webPush.httpClient.Timeout != client.Timeout || fcm.httpClient.Timeout != client.Timeout {
		t.Fatal("sender clients should preserve the supplied transport and timeout")
	}
	webPush.httpClient.Timeout = time.Second
	if client.Timeout != 12*time.Second || fcm.httpClient.Timeout != 12*time.Second {
		t.Fatal("changing one sender client should not affect the caller or other sender")
	}

	defaultResult, err := NewStandardSenders(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer defaultResult.Cleanup()
	if findSender[*WebPushSender](defaultResult.Senders).httpClient != nil {
		t.Fatal("nil client should retain the existing default behavior")
	}
}

func TestWebPushSenderUsesInjectedHTTPClient(t *testing.T) {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	userKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	transport := notifierRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.URL.Host != "push.example" || req.Method != http.MethodPost {
			t.Fatalf("unexpected Web Push request: %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
	})
	result, err := NewStandardSenders((&StandardSendersRequest{
		WebPush: &WebPushSenderConfig{VAPIDPublicKey: publicKey, VAPIDPrivateKey: privateKey},
	}).WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Cleanup()
	address := NotificationAddress{
		Channel: NotificationChannelWebPush,
		WebPush: &WebPushAddress{
			Endpoint: "https://push.example/subscription",
			Keys: WebPushKeys{
				Auth:   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
				P256DH: base64.RawURLEncoding.EncodeToString(userKey.PublicKey().Bytes()),
			},
		},
	}
	if err := findSender[*WebPushSender](result.Senders).Send(context.Background(), "subject", "message", []NotificationAddress{address}, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("Web Push request did not use the injected client")
	}
}

func TestFCMSenderUsesInjectedHTTPClientForAuthAndDelivery(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	credentials, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "test-project",
		"private_key_id": "test-key", "private_key": privateKey,
		"client_email": "test@test-project.iam.gserviceaccount.com",
		"token_uri":    "https://oauth2.googleapis.com/token",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, credentials, 0600); err != nil {
		t.Fatal(err)
	}
	var authCalls, deliveryCalls int
	transport := notifierRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Host {
		case "oauth2.googleapis.com":
			authCalls++
			body = `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`
		case "fcm.googleapis.com":
			deliveryCalls++
			if req.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatalf("FCM request missing token: %q", req.Header.Get("Authorization"))
			}
			body = `{"name":"projects/test-project/messages/test-message"}`
		default:
			t.Fatalf("unexpected HTTP destination: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}, Request: req}, nil
	})
	result, err := NewStandardSenders((&StandardSendersRequest{
		FCM: &FCMSenderConfig{Enabled: true, CredentialsFile: path, ProjectID: "test-project"},
	}).WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Cleanup()
	address := NotificationAddress{Channel: NotificationChannelFCM, FCM: &FCMAddress{Token: "registration-token"}}
	if err := findSender[*FCMSender](result.Senders).Send(context.Background(), "subject", "message", []NotificationAddress{address}, nil); err != nil {
		t.Fatal(err)
	}
	if authCalls != 1 || deliveryCalls != 1 {
		t.Fatalf("expected token and FCM calls through injected client; got auth=%d delivery=%d", authCalls, deliveryCalls)
	}
}
