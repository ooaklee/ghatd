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

func TestWebConnectionReturnURI(t *testing.T) {
	for _, raw := range []string{"https://app.example/settings", "http://localhost:5173/settings", "http://127.0.0.1/settings", "http://[::1]:5173/settings"} {
		if !validWebConnectionReturnURI(raw) {
			t.Errorf("valid Settings address rejected: %s", raw)
		}
		if ValidDisconnectRedirectURI(raw) {
			t.Errorf("web address accepted as native: %s", raw)
		}
	}
	for _, raw := range []string{"", "http://app.example/settings", "https://user@app.example/settings", "https://app.example/settings/", "https://app.example/%73ettings", "https://app.example/settings?", "https://app.example/settings?token=secret", "https://app.example/settings#account", "https://app.example/auth/login", "com.example.settings:/oauth/disconnect"} {
		if validWebConnectionReturnURI(raw) {
			t.Errorf("unsafe Settings address accepted: %s", raw)
		}
	}
}
