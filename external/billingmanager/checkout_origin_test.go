package billingmanager

import "testing"

func TestCheckoutOriginsNormalizeNumericDefaultPorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		returnURL string
		origins   []string
	}{
		{
			name:      "HTTPS leading-zero default port",
			returnURL: "https://APP.EXAMPLE.TEST:0443/app/plan?session_id={CHECKOUT_SESSION_ID}",
			origins: []string{
				"https://app.example.test",
				"https://APP.EXAMPLE.TEST:443",
				"https://app.example.test:0443",
			},
		},
		{
			name:      "HTTP leading-zero default port",
			returnURL: "http://LOCALHOST:080/app/plan",
			origins:   []string{"http://localhost", "http://localhost:80", "http://localhost:080"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			allowedOrigin, err := checkoutReturnURLOrigin(test.returnURL)
			if err != nil {
				t.Fatalf("checkoutReturnURLOrigin() error = %v", err)
			}
			for _, origin := range test.origins {
				if !checkoutRequestOriginAllowed(origin, "same-origin", allowedOrigin) {
					t.Errorf("checkoutRequestOriginAllowed(%q, allowed %q) = false", origin, allowedOrigin)
				}
			}
		})
	}
}

func TestCheckoutOriginsRejectInvalidTCPPorts(t *testing.T) {
	t.Parallel()

	for _, returnURL := range []string{
		"https://app.example.test:0/app/plan",
		"https://app.example.test:70000/app/plan",
	} {
		returnURL := returnURL
		t.Run(returnURL, func(t *testing.T) {
			t.Parallel()
			if _, err := checkoutReturnURLOrigin(returnURL); err == nil {
				t.Fatalf("checkoutReturnURLOrigin(%q) error = nil", returnURL)
			}
		})
	}

	for _, origin := range []string{
		"https://app.example.test:0",
		"https://app.example.test:70000",
	} {
		origin := origin
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			if checkoutRequestOriginAllowed(origin, "same-origin", "https://app.example.test") {
				t.Fatalf("checkoutRequestOriginAllowed(%q) = true", origin)
			}
		})
	}
}
