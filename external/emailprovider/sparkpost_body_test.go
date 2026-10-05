package emailprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSparkPostBodyWirePreservation(t *testing.T) {
	for _, tc := range []struct{ name, html, text string }{{"text-only", "", "Plain message"}, {"HTML-only", "<p>HTML message</p>", ""}, {"multipart", "<p>HTML message</p>", "Plain message"}} {
		t.Run(tc.name, func(t *testing.T) {
			payloads := make(chan map[string]any, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					http.Error(w, "invalid JSON", 400)
					return
				}
				payloads <- payload
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"results":{"id":"test-receipt"}}`))
			}))
			t.Cleanup(server.Close)
			client, _ := newTestSparkPostClient(t, server)
			email := validTestEmail()
			email.HTMLBody = tc.html
			email.TextBody = tc.text
			receipt, err := NewSparkPostEmailProvider(client).Send(context.Background(), email)
			require.NoError(t, err)
			require.Equal(t, "test-receipt", receipt.MessageID)
			content := (<-payloads)["content"].(map[string]any)
			for _, field := range []struct{ key, want string }{{"html", tc.html}, {"text", tc.text}, {"reply_to", email.ReplyTo}, {"subject", email.Subject}} {
				if field.want != "" {
					require.Equal(t, field.want, content[field.key], field.key)
				} else {
					require.True(t, content[field.key] == nil || content[field.key] == "", field.key)
				}
			}
		})
	}
}
