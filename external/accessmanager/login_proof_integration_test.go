package accessmanager_test

import (
	"sync"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// Each case owns its database and namespaced Redis runtime. The simultaneous
// attempts exercise the actual signer, repository and atomic Redis deletion.
func TestLoginProofConcurrentConsumptionIntegration(t *testing.T) {
	for _, flow := range []string{"login", "email-verification", "verification-via-login"} {
		t.Run(flow, func(t *testing.T) {
			f := newConnectionFixture(t)
			var proof *auth.TokenDetails
			var err error
			if flow == "login" {
				proof, err = f.auth.CreateInitalToken(f.ctx, f.account)
			} else {
				f.account.Status = user.AccountStatusKeyProvisioned
				f.account.Verification.EmailVerified = false
				_, err = f.users.UpdateUser(f.ctx, &user.UpdateUserRequest{User: f.account})
				require.NoError(t, err)
				proof, err = f.auth.CreateEmailVerificationToken(f.ctx, f.account)
			}
			require.NoError(t, err)
			if flow != "login" {
				// Normalize only this test's transport fields; the signer uses
				// distinct login and email-verification token slots.
				proof.EphemeralUUID, proof.EphemeralToken, proof.EtTTL = proof.EmailVerificationUUID, proof.EmailVerificationToken, proof.EvTTL
			}
			require.NoError(t, f.ephemeral.StoreToken(f.ctx, proof.EphemeralUUID, f.account.ID, proof.EtTTL))
			type attempt struct {
				token string
				err   error
			}
			results := make(chan attempt, 12)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for range 12 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					var result attempt
					if flow == "email-verification" {
						response, err := f.service.ValidateEmailVerificationCode(f.ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: proof.EphemeralToken})
						result.err = err
						if response != nil {
							result.token = response.AccessToken
						}
					} else {
						response, err := f.service.LoginUser(f.ctx, &accessmanager.LoginUserRequest{Token: proof.EphemeralToken})
						result.err = err
						if response != nil {
							result.token = response.AccessToken
						}
					}
					results <- result
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			winners := 0
			var failures []error
			for result := range results {
				if result.err != nil {
					failures = append(failures, result.err)
					require.Empty(t, result.token)
					continue
				}
				winners++
				require.NotEmpty(t, result.token)
				_, err := f.service.AuthenticateSession(f.ctx, result.token)
				require.NoError(t, err)
			}
			require.Equal(t, 1, winners, failures)
			details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, proof.EphemeralToken)
			require.NoError(t, err)
			_, err = f.ephemeral.FetchAuth(f.ctx, details)
			require.True(t, ephemeral.IsAuthNotFound(err))
		})
	}
}

func TestEmailVerificationRejectsLoginAndSessionIntegration(t *testing.T) {
	for _, purpose := range []string{"login", "access"} {
		t.Run(purpose, func(t *testing.T) {
			f := newConnectionFixture(t)
			token := f.tokens.AccessToken
			if purpose == "login" {
				proof, err := f.auth.CreateInitalToken(f.ctx, f.account)
				require.NoError(t, err)
				require.NoError(t, f.ephemeral.StoreToken(f.ctx, proof.EphemeralUUID, f.account.ID, proof.EtTTL))
				token = proof.EphemeralToken
			}
			response, err := f.service.ValidateEmailVerificationCode(f.ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: token})
			require.Nil(t, response)
			require.ErrorIs(t, err, auth.ErrUnauthorized)
			// Rejected purposes are not consumed by this endpoint.
			details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, token)
			require.NoError(t, err)
			owner, err := f.ephemeral.FetchAuth(f.ctx, details)
			require.NoError(t, err)
			require.Equal(t, f.account.ID, owner)
		})
	}
}
