package oauth

import "testing"

func TestDisconnectRedirectURI(t *testing.T) {
	for _, raw := range []string{"com.example.settings:/oauth/disconnect", "boasi.io.bedrock.settings:/oauth/disconnect"} {
		if !ValidDisconnectRedirectURI(raw) {
			t.Errorf("valid URI rejected: %s", raw)
		}
	}
	for _, raw := range []string{"", "http://localhost/oauth/disconnect", "https://app.example/oauth/disconnect", "com.example.settings://oauth/disconnect", "com.example.settings:/oauth/callback", "com.example.settings:/oauth/disconnect?", "com.example.settings:/oauth/disconnect?x=1", "com.example.settings:/oauth/disconnect#proof", "com.example.settings:/oauth/%64isconnect", "com.example.settings:/oauth/disconnect/", "COM.example.settings:/oauth/disconnect", "javascript:/oauth/disconnect"} {
		if ValidDisconnectRedirectURI(raw) {
			t.Errorf("unsafe URI accepted: %s", raw)
		}
	}
}
