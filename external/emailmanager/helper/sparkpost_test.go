package emailmanagerhelper

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/stretchr/testify/require"
)

func TestExplicitSparkPostCompositionAndLocalCapture(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    Config
		wantError bool
		capture   bool
	}{
		{name: "registry account", config: Config{Providers: []ProviderConfig{{ID: "mail", Vendor: "sparkpost", Token: "test-token", SparkPostBaseURL: "https://api.eu.sparkpost.com"}}}},
		{name: "local named account", config: Config{CaptureLocally: true, Providers: []ProviderConfig{{ID: "mail", Vendor: "sparkpost", Token: "unused-sensitive-token"}}}, capture: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cfg := tc.config
			cfg.HTTPClient = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("external request forbidden")
			})}
			services, err := BuildServices(cfg)
			if tc.wantError {
				require.ErrorIs(t, err, ErrConfiguration)
				require.Equal(t, Services{}, services)
			} else {
				require.NoError(t, err)
				if tc.capture {
					manager, err := testManager(t, services)
					require.NoError(t, err)
					receipt, err := manager.SendEmailWithResult(context.Background(), &emailmanager.SendEmailRequest{
						MailType: emailprovider.Transactional, To: "to@example.test", From: "from@example.test", Subject: "Local test", TextBody: "Captured locally",
					})
					require.NoError(t, err)
					require.Equal(t, emailprovider.Captured, receipt.State)
					require.Len(t, services.Local.Inbox().List(), 1)
				} else {
					require.Equal(t, "SPARKPOST", services.Default.Name())
				}
			}
			require.Zero(t, calls)
		})
	}
}
