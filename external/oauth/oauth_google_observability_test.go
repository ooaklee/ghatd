package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"
)

type observedGoogleRequest struct {
	path          string
	rawQuery      string
	authorization string
	traceparent   string
}

// TestGoogleProviderUsesContextClientAndBearerHeader verifies context propagation and bearer-token transport.
func TestGoogleProviderUsesContextClientAndBearerHeader(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	observedRequests := make(chan observedGoogleRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedRequests <- observedGoogleRequest{
			path:          request.URL.Path,
			rawQuery:      request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"),
			traceparent:   request.Header.Get("traceparent"),
		}

		switch request.URL.Path {
		case "/token":
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"access_token": "sensitive-access-token",
				"token_type":   "Bearer",
			})
		case "/userinfo":
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(GoogleProviderOauthUserInfo{
				OauthProviderUserId: "provider-user",
				Email:               "person@example.test",
				VerifiedEmail:       true,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	client := &http.Client{
		Transport: observability.NewRoundTripper(server.Client().Transport),
		Timeout:   2 * time.Second,
	}
	googleProvider := NewGoogleProvider(&NewGoogleProviderRequest{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://app.example.test/callback",
		HTTPClient:   client,
	})
	googleProvider.config.Endpoint = oauth2.Endpoint{
		AuthURL:  server.URL + "/authorize",
		TokenURL: server.URL + "/token",
	}
	googleProvider.providerUserInfoEndpoint = server.URL + "/userinfo"

	ctx, rootSpan := provider.Tracer("oauth-test").Start(context.Background(), "callback")
	defer rootSpan.End()
	userInfo, err := googleProvider.ProviderGetUserData(
		ctx,
		url.Values{"code": []string{"authorization-code"}},
	)
	require.NoError(t, err)
	require.Equal(t, "person@example.test", userInfo.GetUserEmail())

	tokenRequest := <-observedRequests
	userInfoRequest := <-observedRequests
	require.Equal(t, "/token", tokenRequest.path)
	require.Equal(t, "/userinfo", userInfoRequest.path)
	assert.Empty(t, userInfoRequest.rawQuery, "access tokens must not be placed in the URL")
	assert.Equal(t, "Bearer sensitive-access-token", userInfoRequest.authorization)

	tokenSpanContext := remoteSpanContext(t, tokenRequest.traceparent)
	userInfoSpanContext := remoteSpanContext(t, userInfoRequest.traceparent)
	assert.Equal(t, tokenSpanContext.TraceID(), userInfoSpanContext.TraceID())
	assert.NotEqual(t, tokenSpanContext.SpanID(), userInfoSpanContext.SpanID())
}

// remoteSpanContext extracts and validates a remote span context from a traceparent value.
func remoteSpanContext(t *testing.T, traceparent string) oteltrace.SpanContext {
	t.Helper()
	require.NotEmpty(t, traceparent)

	ctx := propagation.TraceContext{}.Extract(
		context.Background(),
		propagation.MapCarrier{"traceparent": traceparent},
	)
	spanContext := oteltrace.SpanContextFromContext(ctx)
	require.True(t, spanContext.IsValid())
	require.True(t, spanContext.IsRemote())
	return spanContext
}

// TestGoogleProviderRejectsFailedUserInfoStatus verifies unsuccessful user-info responses return a provider error.
func TestGoogleProviderRejectsFailedUserInfoStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"access_token":"token","token_type":"Bearer"}`))
			return
		}
		http.Error(writer, "not authorised", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	provider := NewGoogleProvider(&NewGoogleProviderRequest{HTTPClient: server.Client()})
	provider.config.Endpoint = oauth2.Endpoint{TokenURL: server.URL + "/token"}
	provider.providerUserInfoEndpoint = server.URL + "/userinfo"

	_, err := provider.ProviderGetUserData(
		context.Background(),
		url.Values{"code": []string{"authorization-code"}},
	)
	require.ErrorIs(t, err, ErrProviderFailedGettingUserInfo)
}
