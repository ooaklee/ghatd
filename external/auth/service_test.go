package auth_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/auth"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

func TestServiceExtractTokenMetadataHandlesRequestWithoutURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := auth.NewService(&auth.NewServiceRequest{
		AccessTokenSecret:  "access-secret",
		RefreshTokenSecret: "refresh-secret",
	})
	tokenDetails, err := service.CreateToken(ctx, &userv2.UniversalUser{
		ID:     "user-1",
		Status: userv2.AccountStatusKeyActive,
	})
	require.NoError(t, err)

	req := &http.Request{Header: http.Header{}}
	req.Header.Set("Authorization", "Bearer "+tokenDetails.AccessToken)

	var details *auth.TokenAccessDetails
	require.NotPanics(t, func() {
		details, err = service.ExtractTokenMetadata(ctx, req)
	})
	require.NoError(t, err)
	require.Equal(t, "user-1", details.UserID)
}
