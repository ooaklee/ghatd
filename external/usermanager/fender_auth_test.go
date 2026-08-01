package usermanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

type usermanagerAuthTestValidator struct{}

func (usermanagerAuthTestValidator) Validate(interface{}) error { return nil }

func TestMapRequestToCreateCommsRequestUsesOnlyAuthenticatedUserID(t *testing.T) {
	tests := []struct {
		name          string
		authenticated bool
		wantUserID    string
	}{
		{name: "anonymous placeholder omitted", authenticated: false, wantUserID: ""},
		{name: "authenticated user retained", authenticated: true, wantUserID: "user-123"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := accessmanagerhelpers.TransitWith(context.Background(), "user-123")
			ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, test.authenticated)
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/ums/comms",
				strings.NewReader(`{"full_name":"Example User","email":"user@example.com","type":"feedback","message":"Helpful feedback"}`),
			).WithContext(ctx)

			parsed, err := MapRequestToCreateCommsRequest(request, usermanagerAuthTestValidator{})
			if err != nil {
				t.Fatalf("MapRequestToCreateCommsRequest() error = %v", err)
			}
			if parsed.UserId != test.wantUserID {
				t.Fatalf("UserId = %q, want %q", parsed.UserId, test.wantUserID)
			}
		})
	}
}
