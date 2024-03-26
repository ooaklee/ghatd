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
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// ApitokenRespository expected methods of a valid apitoken repository
type ApitokenRespository interface {
	GetAPITokens(ctx context.Context, queryFilter bson.D, requestFilter *bson.D) ([]UserAPIToken, error)
	GetAPITokenByID(ctx context.Context, apiTokenID string) (*UserAPIToken, error)
	// DeleteAPITokenFor(ctx context.Context, userID string, apiTokenID string) error
	UpdateAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error)
	CreateUserAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error)
	GetAPITokensFor(ctx context.Context, userID string, requestFilter *bson.D) ([]UserAPIToken, error)
	GetAPITokensForNanoId(ctx context.Context, userNanoId string, requestFilter *bson.D) ([]UserAPIToken, error)
	DeleteResourcesByOwnerId(ctx context.Context, resourceType interface{}, ownerId string) error
	GetTotalApiTokens(ctx context.Context, userId string, to string, from string, onlyEphemeral bool, onlyPermanent bool) (int64, error)
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

	return s.ApitokenRespository.GetTotalApiTokens(ctx, r.UserId, r.To, r.From, r.OnlyEphemeral, r.OnlyPermanent)
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

	tokens, err := s.ApitokenRespository.GetAPITokensFor(ctx, r.ClientID, &bson.D{})
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

	// TODO: Implement logic
	// return s.ApitokenRespository.DeleteAPITokenFor(ctx, r.UserID, r.APITokenID)
	return nil
}

// GetAPITokensFor returns all api tokens stored in repository for user ID
// TODO: Create tests
func (s *Service) GetAPITokensFor(ctx context.Context, r *GetAPITokensForRequest) (*GetAPITokensForResponse, error) {

	var apitokens []UserAPIToken
	var err error

	sortFilter := s.generateGetAPITokensOrderSortFilter(r.Order)

	// If a full User Id is passed, use that to do check
	if r.ID != "" {
		apitokens, err = s.ApitokenRespository.GetAPITokensFor(ctx, r.ID, sortFilter)
		if err != nil {
			return &GetAPITokensForResponse{
				APITokens: []UserAPIToken{},
			}, err
		}
	}

	// If a user's Id is passed, use that to do check
	if r.NanoId != "" {
		apitokens, err = s.ApitokenRespository.GetAPITokensForNanoId(ctx, r.NanoId, sortFilter)
		if err != nil {
			return &GetAPITokensForResponse{
				APITokens: []UserAPIToken{},
			}, err
		}
	}

	// Analyse token ttl information
	apitokens = s.analyseTokenTTLData(ctx, &AnalyseTokenTTLDataRequest{
		ApiTokens: apitokens,
	})

	return &GetAPITokensForResponse{
		APITokens: apitokens,
	}, nil
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

	sortFilter := s.generateGetAPITokensOrderSortFilter(r.Order)

	findQuery := s.generateGetAPITokensOrderQueryFilter(r)

	apitokens, err := s.ApitokenRespository.GetAPITokens(ctx, findQuery, sortFilter)
	if err != nil {
		return &GetAPITokensResponse{}, err
	}

	// Analyse token ttl information
	apitokens = s.analyseTokenTTLData(ctx, &AnalyseTokenTTLDataRequest{
		ApiTokens: apitokens,
	})

	return s.generateGetAPITokensResponse(ctx, r, apitokens)
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

// GetAPITokensPagination is handling making the call to centralised pagination
// logic to paginate on passed API Tokens resources
func (s *Service) GetAPITokensPagination(ctx context.Context, resource []UserAPIToken, perPage, page int) (*GetAPITokensPaginationResponse, error) {

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
	}, resourceToInterfaceSlice)

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

// generateGetAPITokensResponse returns appropiate response based on client request & apitokens pulled
// from repository
func (s *Service) generateGetAPITokensResponse(ctx context.Context, r *GetAPITokensRequest, apitokens []UserAPIToken) (*GetAPITokensResponse, error) {

	paginatedAPITokens, err := s.GetAPITokensPagination(ctx, apitokens, r.PerPage, r.Page)
	if err != nil {
		return &GetAPITokensResponse{}, err
	}

	return &GetAPITokensResponse{
		Total:            paginatedAPITokens.Total,
		TotalPages:       paginatedAPITokens.TotalPages,
		APITokens:        paginatedAPITokens.Resources,
		Page:             paginatedAPITokens.Page,
		APITokensPerPage: paginatedAPITokens.ResourcePerPage,
	}, nil
}

// generateGetAPITokensOrderSortFilter returns filter that describes how apitokens should be sorted
// when returned from repository
func (s *Service) generateGetAPITokensOrderSortFilter(orderBy string) *bson.D {

	sortFilter := bson.D{}

	switch orderBy {
	case GetAPITokenOrderCreatedAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: APITokenRespositoryFieldPathCreatedAt, Value: 1})
	case GetAPITokenOrderCreatedAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: APITokenRespositoryFieldPathCreatedAt, Value: -1})

	case GetAPITokenOrderLastUsedAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: APITokenRespositoryFieldPathLastUsedAt, Value: 1})
	case GetAPITokenOrderLastUsedAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: APITokenRespositoryFieldPathLastUsedAt, Value: -1})

	case GetAPITokenOrderUpdatedAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: APITokenRespositoryFieldPathUpdatedAt, Value: 1})
	case GetAPITokenOrderUpdatedAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: APITokenRespositoryFieldPathUpdatedAt, Value: -1})

	default:
		sortFilter = append(sortFilter, bson.E{Key: APITokenRespositoryFieldPathCreatedAt, Value: -1})
	}

	return &sortFilter
}

// generateGetAPITokensOrderQueryFilter returns filter that describes how apitokens should be
// filtered
func (s *Service) generateGetAPITokensOrderQueryFilter(r *GetAPITokensRequest) bson.D {

	findQuery := bson.D{}

	if r.Description != "" {
		findQuery = append(findQuery, bson.E{Key: APITokenRespositoryFieldPathDescription, Value: bson.M{"$in": []string{r.Description}}})
	}

	if r.Status != "" {
		findQuery = append(findQuery, bson.E{Key: APITokenRespositoryFieldPathStatus, Value: bson.M{"$in": []string{toolbox.StringStandardisedToUpper(r.Status)}}})
	}

	if r.CreatedByID != "" {
		findQuery = append(findQuery, bson.E{Key: APITokenRespositoryFieldPathCreatedByID, Value: r.CreatedByID})
	}

	return findQuery
}
