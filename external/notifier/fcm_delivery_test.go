package notifier

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFCMDeliveryLifecycle(t *testing.T) {
	cases := []struct {
		name                       string
		count                      int
		duplicate                  bool
		errorCode                  string
		cleanupFails               bool
		wantDelivered, wantCleaned int
		wantError                  bool
	}{
		{name: "data and duplicate token", count: 1, duplicate: true, wantDelivered: 1},
		{name: "multicast batch boundary", count: 501, wantDelivered: 501},
		{name: "unregistered token", count: 1, errorCode: "UNREGISTERED", wantCleaned: 1},
		{name: "partial delivery with expired token", count: 2, errorCode: "UNREGISTERED", wantDelivered: 1, wantCleaned: 1},
		{name: "invalid payload must not disable token", count: 1, errorCode: "INVALID_ARGUMENT", wantError: true},
		{name: "sender mismatch must not disable token", count: 1, errorCode: "SENDER_ID_MISMATCH", wantError: true},
		{name: "cleanup failure is observable", count: 1, errorCode: "UNREGISTERED", cleanupFails: true, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			credentials, _ := json.Marshal(map[string]string{"type": "service_account", "project_id": "test-project", "private_key_id": "test-key", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "client_email": "test@test-project.iam.gserviceaccount.com", "token_uri": "https://oauth2.googleapis.com/token"})
			path := filepath.Join(t.TempDir(), "credentials.json")
			if err := os.WriteFile(path, credentials, 0600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			calls := make(map[string]int)
			transport := notifierRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				status := 200
				body := `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`
				if req.URL.Host == "fcm.googleapis.com" {
					var payload struct {
						Message struct {
							Token        string                       `json:"token"`
							Data         map[string]string            `json:"data"`
							Notification struct{ Title, Body string } `json:"notification"`
						} `json:"message"`
					}
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						return nil, err
					}
					if payload.Message.Data["targetPath"] != "/settings#notifications" || payload.Message.Data["count"] != "2" || payload.Message.Notification.Title != "Hello" {
						t.Errorf("notification data or title lost")
					}
					mu.Lock()
					calls[payload.Message.Token]++
					mu.Unlock()
					body = `{"name":"projects/test-project/messages/test-message"}`
					if tc.errorCode != "" && payload.Message.Token == "private-token-0" {
						status = 400
						if tc.errorCode == "UNREGISTERED" {
							status = 404
						}
						if tc.errorCode == "SENDER_ID_MISMATCH" {
							status = 403
						}
						body = fmt.Sprintf(`{"error":{"code":%d,"message":"private-token-0 must not leak","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":%q}]}}`, status, tc.errorCode)
					}
				} else if req.URL.Host != "oauth2.googleapis.com" {
					return nil, errors.New("unexpected destination")
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}, Request: req}, nil
			})
			sender := NewFCMSender(FCMSenderConfig{Enabled: true, CredentialsFile: path, ProjectID: "test-project"})
			sender.httpClient = &http.Client{Transport: transport}
			var cleaned []string
			sender.SetInvalidAddressHandler(func(_ context.Context, hash string) error {
				if tc.cleanupFails {
					return errors.New("database unavailable")
				}
				cleaned = append(cleaned, hash)
				return nil
			})
			addresses := make([]NotificationAddress, tc.count)
			for i := range addresses {
				addresses[i] = NotificationAddress{Channel: NotificationChannelFCM, AddressHash: fmt.Sprintf("hash-%d", i), FCM: &FCMAddress{Token: fmt.Sprintf("private-token-%d", i)}}
			}
			if tc.duplicate {
				addresses = append(addresses, addresses[0])
			}
			report, err := sender.SendWithReport(context.Background(), "Hello", "Body", addresses, map[string]interface{}{"targetPath": "/settings#notifications", "count": 2})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %v", err, tc.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "private-token") {
				t.Fatal("provider error leaked token")
			}
			if report.Delivered != tc.wantDelivered || report.Cleaned != tc.wantCleaned || len(cleaned) != tc.wantCleaned {
				t.Fatalf("report=%+v, cleaned=%v", report, cleaned)
			}
			if len(calls) != tc.count {
				t.Fatalf("sent %d unique tokens, want %d", len(calls), tc.count)
			}
			for _, count := range calls {
				if count != 1 {
					t.Fatal("duplicate delivery")
				}
			}
		})
	}
}

func TestFCMMessageData(t *testing.T) {
	cases := []struct {
		name      string
		data      map[string]interface{}
		want      map[string]string
		wantError bool
	}{
		{name: "empty"},
		{name: "string and structured data", data: map[string]interface{}{"path": "/settings", "extra": map[string]interface{}{"id": 2}, "flag": true}, want: map[string]string{"path": "/settings", "extra": `{"id":2}`, "flag": "true"}},
		{name: "unsupported value", data: map[string]interface{}{"value": make(chan int)}, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fcmMessageData(tc.data)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got=%v", got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("key=%s got=%s want=%s", k, got[k], v)
				}
			}
		})
	}
}
