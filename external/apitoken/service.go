package apitoken

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// ApitokenRespository expected methods of a valid apitoken repository
type ApitokenRespository interface {
	GetAPITokens(ctx context.Context, req *GetAPITokensRequest) ([]UserAPIToken, error)
	GetAPITokenByID(ctx context.Context, apiTokenID string) (*UserAPIToken, error)
	DeleteAPITokenFor(ctx context.Context, userID string, apiTokenID string) error
	UpdateAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error)
	CreateUserAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error)
	DeleteResourcesByOwnerId(ctx context.Context, resourceType interface{}, ownerId string) error
	GetTotalApiTokens(ctx context.Context, userId, userNanoId, descriptionFilter, statusFilter, to, from string, onlyEphemeral bool, onlyPermanent bool) (int64, error)
}

// Service holds and manages apitoken business logic
type Service struct {
	ApitokenRespository ApitokenRespository
}

// NewService created apitoken service
func NewService(ApitokenRespository ApitokenRespository) *Service {
	return &Service{
		ApitokenRespository: ApitokenRespository,
	}
}

// GetTotalApiTokens gets the total on api tokens based on passed values
func (s *Service) GetTotalApiTokens(ctx context.Context, r *GetTotalApiTokensRequest) (int64, error) {

	return s.ApitokenRespository.GetTotalApiTokens(ctx, r.UserId, "", r.Description, r.Status, r.To, r.From, r.OnlyEphemeral, r.OnlyPermanent)
}

// DeleteApiTokensByOwnerId deletes the histories that belong to matching user id
func (s *Service) DeleteApiTokensByOwnerId(ctx context.Context, ownerId string) error {

	err := s.ApitokenRespository.DeleteResourcesByOwnerId(ctx, &UserAPIToken{}, ownerId)
	if err != nil {
		return err
	}

	return nil
}

// CreateAPIToken creates an API token adding,  any passed additional information
// TODO: Create tests
func (s *Service) CreateAPIToken(ctx context.Context, r *CreateAPITokenRequest) (*CreateAPITokenResponse, error) {
	log := logger.AcquireFrom(ctx)

	if r.UserID == "" {
		return nil, errors.New(ErrKeyRequiredUserIDMissing)
	}

	// Prep apiToken
	apiToken := UserAPIToken{
		CreatedByID:     r.UserID,
		CreatedByNanoId: r.UserNanoId,
	}

	apiToken.SetStatus(UserTokenStatusKeyActive).SetCreatedAtTimeToNow()

	// see if token short lived
	if r.TokenTtl != 0 {
		// Add duration
		createdAt, err := time.Parse("2006-01-02T15:04:05.999999999", apiToken.CreatedAt)
		if err != nil {
			log.Error("unable-to-set-ttl-for-short-lived-user-api-token", zap.String("token-created-at", apiToken.CreatedAt), zap.String("user-id", r.UserID), zap.Error(err))

			// TODO: create proper error map entry
			return nil, errors.New("ErrKeyErrorCreatingShortLivedAccessToken")
		}

		if err == nil {
			expiryDate := createdAt.Add(time.Duration(r.TokenTtl) * time.Second)

			apiToken.TtlExpiresAt = expiryDate.Format("2006-01-02T15:04:05.999999999")
		}
	}

	persistentApiToken, err := s.ApitokenRespository.CreateUserAPIToken(ctx, &apiToken)
	if err != nil {
		return nil, err
	}

	if r.UserNanoId != "" {
		// The API token should be a fusion of the
		// user nanoId and the actual generated token
		persistentApiToken.Value = r.UserNanoId + "." + persistentApiToken.Value
	}

	if r.UserNanoId == "" {
		log.Warn("api-token-created-with-no-nano-id-ref", zap.String("user-id", r.UserID), zap.String("token-id", persistentApiToken.ID))
	}

	return &CreateAPITokenResponse{
		APIToken: *persistentApiToken,
	}, nil
}

// ExtractValidateUserAPITokenMetadata retrieves data from passed user api token
// TODO: Create tests
func (s *Service) ExtractValidateUserAPITokenMetadata(ctx context.Context, r *http.Request) (*APITokenRequester, error) {
	log := logger.AcquireFrom(ctx)
	var requester APITokenRequester

	// secret identifers from headers
	userFullToken := r.Header.Get(common.SystemWideXApiToken)

	splittedToken := strings.Split(userFullToken, ".")

	if len(splittedToken) != 2 {
		log.Error("user-api-token-passed-does-not-contain-expected-two-segments", zap.Int("number-of-segments", len(splittedToken)))
		return nil, errors.New(ErrKeyInvalidAPIFormatDetected)
	}

	if splittedToken[0] == "" || splittedToken[1] == "" {
		var nonEmptySegment string

		if splittedToken[0] == "" {
			nonEmptySegment = splittedToken[1]
		}

		if splittedToken[1] == "" {
			nonEmptySegment = splittedToken[0]
		}

		log.Error("user-api-token-passed-does-not-contain-two-non-empty-segments", zap.String("non-empty-segments", nonEmptySegment))
		return nil, errors.New(ErrKeyInvalidAPIFormatDetected)
	}

	requester.UserAPIToken = splittedToken[1]
	requester.NanoId = splittedToken[0]

	// Prep passed token for verification
	k := sha256.New()
	_, _ = k.Write([]byte(requester.UserAPIToken))
	requester.UserAPITokenEncoded = k.Sum(nil)
	requester.UserAPIToken = ""

	// Look up tokens for user
	tokensResponse, err := s.GetAPITokensFor(ctx, &GetAPITokensForRequest{
		NanoId: requester.NanoId})
	if err != nil {
		return nil, err
	}

	tokens := tokensResponse.APITokens

	for _, token := range tokens {
		res := bytes.Compare(token.ValueSHA, requester.UserAPITokenEncoded)
		if res == 0 {
			if token.Status == UserTokenStatusKeyActive {
				requester.IsValid = true
				return &requester, nil
			}
		}
	}

	return nil, errors.New(ErrKeyUnableToValidateUserAPIToken)
}

// UpdateAPITokenLastUsedAt updates the API Token's last used at time to now if the token matches the ID passed
// TODO: Create tests
func (s *Service) UpdateAPITokenLastUsedAt(ctx context.Context, r *UpdateAPITokenLastUsedAtRequest) error {

	var targetTokenID string

	tokens, err := s.ApitokenRespository.GetAPITokens(ctx, &GetAPITokensRequest{
		CreatedByID: r.ClientID,
	})
	if err != nil {
		return err
	}

	for _, token := range tokens {
		res := bytes.Compare(token.ValueSHA, r.APITokenEncoded)
		if res == 0 {
			targetTokenID = token.ID
			break
		}
	}

	if targetTokenID == "" {
		return errors.New(ErrKeyNoMatchingUserAPITokenFound)
	}

	token, err := s.ApitokenRespository.GetAPITokenByID(ctx, targetTokenID)
	if err != nil {
		return err
	}

	token.SetLastUsedAtTimeToNow()

	_, err = s.ApitokenRespository.UpdateAPIToken(ctx, token)

	return err
}

// ActivateAPIToken updates the API Token's Status to `ACTIVE` if the token matches the ID passed
// TODO: Create tests
func (s *Service) ActivateAPIToken(ctx context.Context, r *ActivateAPITokenRequest) error {

	_, err := s.updateAPIToken(ctx, &updateAPITokenRequest{
		APITokenID: r.ID,
		Status:     UserTokenStatusKeyActive,
	})

	return err
}

// RevokeAPIToken updates the API Token's Status to `REVOKE` if the token matches the ID passed
// TODO: Create tests
func (s *Service) RevokeAPIToken(ctx context.Context, r *RevokeAPITokenRequest) error {

	_, err := s.updateAPIToken(ctx, &updateAPITokenRequest{
		APITokenID: r.ID,
		Status:     UserTokenStatusKeyRevoked,
	})

	return err
}

// DeleteAPIToken removes the api token matching the ID stored in repository
// as well as purging corresponding user embedded collection returning an error
// if any failures occur.
func (s *Service) DeleteAPIToken(ctx context.Context, r *DeleteAPITokenRequest) error {

	return s.ApitokenRespository.DeleteAPITokenFor(ctx, r.UserID, r.APITokenID)
}

// GetAPITokensFor returns all api tokens stored in repository for user ID
// TODO: Create tests
func (s *Service) GetAPITokensFor(ctx context.Context, r *GetAPITokensForRequest) (*GetAPITokensForResponse, error) {

	var err error

	log := logger.AcquireFrom(ctx)

	// get count of all of the user's api tokens
	totalApiTokens, err := s.ApitokenRespository.GetTotalApiTokens(ctx, r.ID, r.NanoId, r.Description, r.Status, "", "", r.OnlyEphemeral, r.OnlyPermanent)
	if err != nil {
		return &GetAPITokensForResponse{}, err
	}

	r.TotalCount = int(totalApiTokens)

	log.Info("total-api-tokens-for-user-found", zap.Int64("total", totalApiTokens))

	apitokens, err := s.ApitokenRespository.GetAPITokens(ctx, &GetAPITokensRequest{
		Order:           r.Order,
		PerPage:         r.PerPage,
		Page:            r.Page,
		Description:     r.Description,
		Status:          toolbox.StringStandardisedToUpper(r.Status),
		CreatedByID:     r.ID,
		CreatedByNanoId: r.NanoId,
		OnlyEphemeral:   r.OnlyEphemeral,
		OnlyPermanent:   r.OnlyPermanent,
	})
	if err != nil {
		return &GetAPITokensForResponse{
			APITokens: []UserAPIToken{},
		}, err
	}

	// Analyse token ttl information
	analysedApitokens := s.analyseTokenTTLData(ctx, &AnalyseTokenTTLDataRequest{
		ApiTokens: apitokens,
	})

	// if the anaylsed tokens differs from the original fetched amount, remove difference from total
	if len(analysedApitokens) < len(apitokens) {
		r.TotalCount = (r.TotalCount - (len(apitokens) - len(analysedApitokens)))
	}

	return s.generateGetAPITokensForResponse(ctx, r, analysedApitokens)
}

// GetAPIToken returns the API Token matching the ID stored in repository
// TODO: Create tests
func (s *Service) GetAPIToken(ctx context.Context, r *GetAPITokenRequest) (*GetAPITokenResponse, error) {

	apitoken, err := s.ApitokenRespository.GetAPITokenByID(ctx, r.ID)
	if err != nil {
		return nil, err
	}

	// Analyse token ttl information
	apitokens := s.analyseTokenTTLData(ctx, &AnalyseTokenTTLDataRequest{
		ApiTokens: []UserAPIToken{*apitoken},
	})

	if len(apitokens) < 1 {
		return nil, errors.New(ErrKeyNoMatchingUserAPITokenFound)
	}

	return &GetAPITokenResponse{
		APIToken: apitokens[0],
	}, nil
}

// GetAPITokens returns all api tokens stored in repository
// TODO: Create tests
func (s *Service) GetAPITokens(ctx context.Context, r *GetAPITokensRequest) (*GetAPITokensResponse, error) {

	log := logger.AcquireFrom(ctx)

	// get count of all of the user's api tokens
	totalApiTokens, err := s.ApitokenRespository.GetTotalApiTokens(ctx, r.CreatedByID, r.CreatedByNanoId, r.Description, r.Status, "", "", false, false)
	if err != nil {
		return &GetAPITokensResponse{}, err
	}

	r.TotalCount = int(totalApiTokens)

	log.Info("total-api-tokens-found", zap.Int64("total", totalApiTokens))

	apitokens, err := s.ApitokenRespository.GetAPITokens(ctx, r)
	if err != nil {
		return &GetAPITokensResponse{}, err
	}

	// Analyse token ttl information
	analysedApitokens := s.analyseTokenTTLData(ctx, &AnalyseTokenTTLDataRequest{
		ApiTokens: apitokens,
	})

	// if the anaylsed tokens differs from the original fetched amount, remove difference from total
	if len(analysedApitokens) < len(apitokens) {
		r.TotalCount = (r.TotalCount - (len(apitokens) - len(analysedApitokens)))
	}

	return s.generateGetAPITokensResponse(ctx, r, analysedApitokens)
}

// analyseTokenTTLData is checking the passed tokens to ensure they haven't
// expired, if short lived
func (s *Service) analyseTokenTTLData(ctx context.Context, r *AnalyseTokenTTLDataRequest) []UserAPIToken {

	var userApiTokensToRemove []string
	var userId string
	var validUserApiTokens []UserAPIToken
	log := logger.AcquireFrom(ctx)

	if r == nil {
		return []UserAPIToken{}
	}

	for _, apiToken := range r.ApiTokens {

		// check if token has ttl set
		if !apiToken.IsShortLivedToken() {
			validUserApiTokens = append(validUserApiTokens, apiToken)
			continue
		}

		if userId == "" {
			userId = apiToken.CreatedByID
		}

		expiration, err := time.Parse("2006-01-02T15:04:05.999999999", apiToken.TtlExpiresAt)
		if err != nil {
			log.Warn("unable-to-parse-user-api-token-expiry-date", zap.String("token-id", apiToken.ID), zap.String("expiry-date", apiToken.TtlExpiresAt), zap.String("user-id", apiToken.CreatedByID))
			continue
		}

		timeNow := time.Now()

		if timeNow.After(expiration) || timeNow.Equal(expiration) {
			userApiTokensToRemove = append(userApiTokensToRemove, apiToken.ID)
		}

		// if not expired as yet add to valid api tokens
		validUserApiTokens = append(validUserApiTokens, apiToken)
	}

	// make sure there are roles to remove
	if len(userApiTokensToRemove) < 1 {
		return r.ApiTokens
	}

	// remove expired tokens for user
	for _, userTokenId := range userApiTokensToRemove {

		err := s.DeleteAPIToken(ctx, &DeleteAPITokenRequest{
			APITokenID: userTokenId,
			UserID:     userId,
		})

		//  if it fails just log a message and carry on by passing newly structured user object
		if err != nil {
			log.Error("failed-to-remove-expired-user-api-token", zap.String("token-id", userTokenId), zap.String("user-id", userId))
		}
	}

	return validUserApiTokens
}

// updateAPIToken ammends the APIToken matching the ID stored in repository this method
// will be used by RevokeAPIToken & ActivateAPIToken.
//
// TODO: Consider whether user's should be able to update their description
func (s *Service) updateAPIToken(ctx context.Context, r *updateAPITokenRequest) (*updateAPITokenResponse, error) {

	apiToken, err := s.ApitokenRespository.GetAPITokenByID(ctx, r.APITokenID)
	if err != nil {
		return nil, err
	}

	if apiToken.Status == r.Status {
		return &updateAPITokenResponse{
			APIToken: *apiToken,
		}, nil
	}

	if r.Status != "" {
		if toolbox.StringInSlice(r.Status, validTokenStatuses) {
			apiToken.Status = r.Status
		} else {
			return nil, errors.New(ErrKeyTokenStatusInvalid)
		}
	}

	apiToken.SetUpdatedAtTimeToNow()

	_, err = s.ApitokenRespository.UpdateAPIToken(ctx, apiToken)
	if err != nil {
		return nil, err
	}

	return &updateAPITokenResponse{
		APIToken: *apiToken,
	}, nil
}

// generateGetAPITokensResponse returns appropiate response based on client request & apitokens pulled
// from repository
func (s *Service) generateGetAPITokensResponse(ctx context.Context, r *GetAPITokensRequest, apitokens []UserAPIToken) (*GetAPITokensResponse, error) {

	paginatedApiTokens, err := s.GetAPITokensPagination(ctx, apitokens, r.PerPage, r.Page, r.TotalCount)
	if err != nil {
		return &GetAPITokensResponse{}, err
	}

	return &GetAPITokensResponse{
		Total:            paginatedApiTokens.Total,
		TotalPages:       paginatedApiTokens.TotalPages,
		APITokens:        paginatedApiTokens.Resources,
		Page:             paginatedApiTokens.Page,
		APITokensPerPage: paginatedApiTokens.ResourcePerPage,
	}, nil
}

// generateGetAPITokensForResponse returns appropiate response based on client request & users pulled
// from repository
func (s *Service) generateGetAPITokensForResponse(ctx context.Context, r *GetAPITokensForRequest, apitokens []UserAPIToken) (*GetAPITokensForResponse, error) {

	paginatedApiTokens, err := s.GetAPITokensPagination(ctx, apitokens, r.PerPage, r.Page, r.TotalCount)
	if err != nil {
		return &GetAPITokensForResponse{}, err
	}

	return &GetAPITokensForResponse{
		Total:            paginatedApiTokens.Total,
		TotalPages:       paginatedApiTokens.TotalPages,
		APITokens:        paginatedApiTokens.Resources,
		Page:             paginatedApiTokens.Page,
		APITokensPerPage: paginatedApiTokens.ResourcePerPage,
	}, nil

}

// GetAPITokensPagination is handling making the call to centralised pagination
// logic to paginate on passed API Tokens resources
func (s *Service) GetAPITokensPagination(ctx context.Context, resource []UserAPIToken, perPage, page, totalApiTokens int) (*GetAPITokensPaginationResponse, error) {

	var resourceToInterfaceSlice []interface{}
	castedResources := []UserAPIToken{}
	log := logger.AcquireFrom(ctx)

	// convert resource slice to interface clice
	for _, element := range resource {
		resourceToInterfaceSlice = append(resourceToInterfaceSlice, element)
	}

	// Call pagination logic
	paginatedResource, err := toolbox.GetResourcePagination(ctx, &toolbox.GetResourcePaginationRequest{
		PerPage: perPage,
		Page:    page,
	}, resourceToInterfaceSlice, totalApiTokens)

	if err != nil {
		return nil, err
	}

	// convert paginated resource slice to correct type
	for _, resource := range paginatedResource.Resources {
		castedResource, ok := resource.(UserAPIToken)
		if !ok {
			log.Error("error-unable-to-cast-paginated-user-api-token-resource")
			continue
		}
		castedResources = append(castedResources, castedResource)
	}

	return &GetAPITokensPaginationResponse{
		Resources:       castedResources,
		Total:           paginatedResource.Total,
		TotalPages:      paginatedResource.TotalPages,
		ResourcePerPage: paginatedResource.ResourcePerPage,
		Page:            paginatedResource.Page,
	}, nil

}
