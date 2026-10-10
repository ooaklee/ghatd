package apitoken

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// ApitokenRespository is the persistence port for credential lifecycle operations.
// Implementations preserve the caller's context and return detached snapshots.
// The historical spelling is retained for source compatibility.
type ApitokenRespository interface {
	// SetAPITokenStatusFor atomically changes status for the exact stored owner.
	SetAPITokenStatusFor(context.Context, string, string, string) error
	// GetAPITokenByDigest resolves a secret within one exact owner namespace,
	// independently of presentation pagination or token-list limits.
	GetAPITokenByDigest(context.Context, string, []byte) (*UserAPIToken, error)
	// TouchAPIToken updates only last_used_at for the exact active credential.
	// It must not read/replace the token or overwrite a concurrent revocation.
	TouchAPIToken(context.Context, string, string, []byte, time.Time) error
	// GetAPITokens returns the stored tokens matching the request's filters,
	// sorting and pagination as a detached snapshot.
	GetAPITokens(ctx context.Context, req *GetAPITokensRequest) ([]UserAPIToken, error)
	// GetAPITokenByID returns the stored token with the matching identifier.
	GetAPITokenByID(ctx context.Context, apiTokenID string) (*UserAPIToken, error)
	// DeleteAPITokenFor deletes only the exact owner/credential pair regardless of
	// expiry or status; an absent or differently owned record is indistinguishable.
	DeleteAPITokenFor(ctx context.Context, userID string, apiTokenID string) error
	// CreateUserAPIToken persists the supplied token resource, generating its
	// identifiers, and returns the stored record.
	CreateUserAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error)
	// DeleteResourcesByOwnerId deletes all token resources belonging to the
	// specified owner identifier.
	DeleteResourcesByOwnerId(ctx context.Context, ownerId string) error
	// GetTotalApiTokens returns the count of stored tokens matching the supplied
	// owner, text, status, time-range and permanence filters.
	GetTotalApiTokens(ctx context.Context, userId, userNanoId, descriptionFilter, statusFilter, to, from string, onlyEphemeral bool, onlyPermanent bool) (int64, error)
}

// Service validates credential lifecycle operations before delegating storage.
// Authentication of management callers and inventory policy belong to the manager.
type Service struct {
	// ApitokenRespository owns persistence; configure it before serving requests.
	ApitokenRespository ApitokenRespository
}

// NewService wires a persistence adapter; it does not establish caller authority.
func NewService(ApitokenRespository ApitokenRespository) *Service {
	return &Service{
		ApitokenRespository: ApitokenRespository,
	}
}

// GetTotalApiTokens counts a filtered inventory without pagination. An empty
// owner selects all records and is restricted to trusted administrative callers.
func (s *Service) GetTotalApiTokens(ctx context.Context, r *GetTotalApiTokensRequest) (int64, error) {
	if err := s.checkContext(ctx); err != nil {
		return 0, err
	}
	if r == nil || (r.OnlyEphemeral && r.OnlyPermanent) {
		return 0, ErrInvalidTokenQuery
	}
	logger := logger.AcquireOperationFrom(ctx, "external/apitoken", "get-total-api-tokens")
	logger.Debug("handling-get-total-api-tokens-request")

	count, err := s.ApitokenRespository.GetTotalApiTokens(ctx, r.UserId, "", r.Description, r.Status, r.To, r.From, r.OnlyEphemeral, r.OnlyPermanent)
	if err != nil {
		return 0, err
	}
	if err := validateTokenCount(ctx, count); err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteApiTokensByOwnerId removes every credential for a trusted owner ID.
// It is an administrative lifecycle operation, not caller authentication.
func (s *Service) DeleteApiTokensByOwnerId(ctx context.Context, ownerId string) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	if ownerId == "" {
		return ErrRequiredUserIDMissing
	}
	logger := logger.AcquireOperationFrom(ctx, "external/apitoken", "delete-api-tokens-by-owner-id")
	logger.Debug("handling-delete-api-tokens-by-owner-id-request")

	err := s.ApitokenRespository.DeleteResourcesByOwnerId(ctx, ownerId)
	if err != nil {
		return err
	}

	return nil
}

// CreateAPIToken issues a credential for an explicitly supplied owner namespace.
// It does not authorize the caller or enforce inventory policy; the manager must
// do both. Zero TTL is permanent. Negative or duration-overflowing TTLs fail
// before persistence. Never publish the returned secret before an enclosing
// transaction commits; a retry can generate a different aborted credential.
func (s *Service) CreateAPIToken(ctx context.Context, r *CreateAPITokenRequest) (*CreateAPITokenResponse, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.UserID == "" {
		return nil, ErrRequiredUserIDMissing
	}
	if !validCredentialPart(r.UserNanoId, 256) {
		return nil, ErrInvalidAPIFormatDetected
	}
	if r.TokenTtl < 0 || r.TokenTtl > int64((1<<63-1)/time.Second) {
		return nil, ErrInvalidTokenTTL
	}
	description := strings.TrimSpace(r.Description)
	now := time.Now().UTC()

	// Prep apiToken
	apiToken := UserAPIToken{
		CreatedByID:     r.UserID,
		CreatedByNanoId: r.UserNanoId,
		CreatedAt:       now.Format(time.RFC3339Nano),
	}

	apiToken.SetStatus(UserTokenStatusKeyActive)

	if description == "" {
		apiToken.GenerateNewCodename()
	} else {
		apiToken.Description = description
	}

	// see if token short lived
	if r.TokenTtl != 0 {
		apiToken.TtlExpiresAt = now.Add(time.Duration(r.TokenTtl) * time.Second).Format(time.RFC3339Nano)
	}

	proposed := apiToken
	persistentApiToken, err := s.ApitokenRespository.CreateUserAPIToken(ctx, &apiToken)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if persistentApiToken == nil || persistentApiToken.ID == "" || persistentApiToken.CreatedByID != r.UserID || persistentApiToken.CreatedByNanoId != r.UserNanoId || !validCredentialPart(persistentApiToken.Value, 512) {
		return nil, ErrServiceUnavailable
	}
	digest := sha256.Sum256([]byte(persistentApiToken.Value))
	if subtle.ConstantTimeCompare(digest[:], persistentApiToken.ValueSHA) != 1 || persistentApiToken.Status != proposed.Status || persistentApiToken.TtlExpiresAt != proposed.TtlExpiresAt {
		return nil, ErrServiceUnavailable
	}
	// Keep the repository's stored snapshot separate from the one-time wire secret.
	created := *persistentApiToken
	created.ValueSHA = append([]byte(nil), persistentApiToken.ValueSHA...)
	created.Value = r.UserNanoId + "." + created.Value

	return &CreateAPITokenResponse{
		APIToken: created,
	}, nil
}

// ExtractValidateUserAPITokenMetadata verifies a single API-token header against
// persisted identity, digest, status and expiry. Missing/corrupt records fail
// closed. Never log header fragments or infer validity from a paginated list.
func (s *Service) ExtractValidateUserAPITokenMetadata(ctx context.Context, r *http.Request) (*APITokenRequester, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || len(r.Header.Values(common.SystemWideXApiToken)) != 1 {
		return nil, ErrInvalidAPIFormatDetected
	}
	raw := r.Header.Get(common.SystemWideXApiToken)
	nanoID, secret, found := strings.Cut(raw, ".")
	if !found || !validCredentialPart(nanoID, 256) || !validCredentialPart(secret, 512) {
		return nil, ErrInvalidAPIFormatDetected
	}
	digest := sha256.Sum256([]byte(secret))
	token, err := s.ApitokenRespository.GetAPITokenByDigest(ctx, nanoID, digest[:])
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if token == nil || token.ID == "" || token.CreatedByID == "" || token.CreatedByNanoId != nanoID || token.Status != UserTokenStatusKeyActive || subtle.ConstantTimeCompare(token.ValueSHA, digest[:]) != 1 {
		return nil, ErrUnableToValidateUserAPIToken
	}
	if token.TtlExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339Nano, token.TtlExpiresAt)
		if err != nil || !time.Now().Before(expires) {
			return nil, ErrUnableToValidateUserAPIToken
		}
	}
	return &APITokenRequester{TokenID: token.ID, UserID: token.CreatedByID, NanoId: nanoID, UserAPITokenEncoded: digest[:], IsValid: true}, nil
}

// UpdateAPITokenLastUsedAt records a verified credential's use without reading
// or rewriting mutable authority fields. Missing identity fails closed; callers
// may treat a failed telemetry update as best effort after authentication.
func (s *Service) UpdateAPITokenLastUsedAt(ctx context.Context, r *UpdateAPITokenLastUsedAtRequest) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	if r == nil || r.TokenID == "" || r.ClientID == "" || len(r.APITokenEncoded) != sha256.Size {
		return ErrNoMatchingUserAPITokenFound
	}
	return s.ApitokenRespository.TouchAPIToken(ctx, r.TokenID, r.ClientID, append([]byte(nil), r.APITokenEncoded...), time.Now().UTC())
}

// ActivateAPIToken changes only status for an exact trusted owner/credential pair.
// Repeating activation succeeds while that pair exists; caller authority is external.
func (s *Service) ActivateAPIToken(ctx context.Context, r *ActivateAPITokenRequest) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	if r == nil || r.UserID == "" || r.ID == "" {
		return ErrResourceNotFound
	}
	return s.ApitokenRespository.SetAPITokenStatusFor(ctx, r.UserID, r.ID, UserTokenStatusKeyActive)
}

// RevokeAPIToken marks an exact owner's credential revoked without deleting its
// inventory slot. It does not change the secret, expiry or other credentials.
func (s *Service) RevokeAPIToken(ctx context.Context, r *RevokeAPITokenRequest) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	if r == nil || r.UserID == "" || r.ID == "" {
		return ErrResourceNotFound
	}
	return s.ApitokenRespository.SetAPITokenStatusFor(ctx, r.UserID, r.ID, UserTokenStatusKeyRevoked)
}

// DeleteAPIToken deletes the exact owner/credential pair and frees its inventory
// slot. Absence and a different owner are indistinguishable to the caller.
func (s *Service) DeleteAPIToken(ctx context.Context, r *DeleteAPITokenRequest) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	if r == nil || r.UserID == "" || r.APITokenID == "" {
		return ErrResourceNotFound
	}
	logger := logger.AcquireOperationFrom(ctx, "external/apitoken", "delete-api-token")
	logger.Debug("handling-delete-api-token-request")

	return s.ApitokenRespository.DeleteAPITokenFor(ctx, r.UserID, r.APITokenID)
}

// GetAPITokensFor returns one management page for the required owner identity.
// It retains expired/revoked tokens and never performs read-triggered deletion.
func (s *Service) GetAPITokensFor(ctx context.Context, r *GetAPITokensForRequest) (*GetAPITokensForResponse, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || (r.ID == "" && r.NanoId == "") || (r.OnlyEphemeral && r.OnlyPermanent) {
		return nil, ErrInvalidTokenQuery
	}
	request := *r
	r = &request

	var err error

	logger := logger.AcquirePackageFrom(ctx, "external/apitoken")

	// default
	if r.Order == "" {
		r.Order = "created_at_desc"
	}

	if r.PerPage == 0 {
		r.PerPage = 25
	}

	if r.Page == 0 {
		r.Page = 1
	}

	// get count of all of the user's api tokens
	totalApiTokens, err := s.ApitokenRespository.GetTotalApiTokens(ctx, r.ID, r.NanoId, r.Description, r.Status, "", "", r.OnlyEphemeral, r.OnlyPermanent)
	if err != nil {
		return nil, err
	}
	if err := validateTokenCount(ctx, totalApiTokens); err != nil {
		return nil, err
	}

	r.TotalCount = int(totalApiTokens)

	logger.Debug("total-api-tokens-for-user-found", zap.Int64("total", totalApiTokens))

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
		return nil, err
	}

	// Listing is non-mutating: retained expired/revoked records remain manageable.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	analysedApitokens := append([]UserAPIToken(nil), apitokens...)

	// generate human readable
	for i, token := range analysedApitokens {
		token.Value = ""
		token.ValueSHA = append([]byte(nil), token.ValueSHA...)
		analysedApitokens[i] = *token.GenerateHumanReadable()
	}

	paginatedResource, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: r.PerPage,
		Page:    r.Page,
	}, analysedApitokens, r.TotalCount)
	if err != nil {
		return nil, err
	}

	return &GetAPITokensForResponse{
		Total:            paginatedResource.Total,
		TotalPages:       paginatedResource.TotalPages,
		APITokens:        paginatedResource.Resources,
		Page:             paginatedResource.Page,
		APITokensPerPage: paginatedResource.ResourcePerPage,
	}, nil
}

// GetAPIToken reads a management snapshot without removing expired credentials.
// The manager must verify ownership; this trusted lookup is by credential ID.
func (s *Service) GetAPIToken(ctx context.Context, r *GetAPITokenRequest) (*GetAPITokenResponse, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.ID == "" {
		return nil, ErrInvalidTokenQuery
	}
	logger := logger.AcquireOperationFrom(ctx, "external/apitoken", "get-api-token")
	logger.Debug("handling-get-api-token-request")

	apitoken, err := s.ApitokenRespository.GetAPITokenByID(ctx, r.ID)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if apitoken == nil || apitoken.ID != r.ID {
		return nil, ErrServiceUnavailable
	}
	copy := *apitoken
	copy.Value = ""
	copy.ValueSHA = append([]byte(nil), apitoken.ValueSHA...)
	return &GetAPITokenResponse{APIToken: *copy.GenerateHumanReadable()}, nil
}

// GetAPITokens returns one filtered management page; an unscoped query is a
// trusted administrative operation, not an owner authorization mechanism.
func (s *Service) GetAPITokens(ctx context.Context, r *GetAPITokensRequest) (*GetAPITokensResponse, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || (r.OnlyEphemeral && r.OnlyPermanent) {
		return nil, ErrInvalidTokenQuery
	}
	request := *r
	r = &request

	logger := logger.AcquirePackageFrom(ctx, "external/apitoken")

	// default
	if r.Order == "" {
		r.Order = "created_at_desc"
	}

	if r.PerPage == 0 {
		r.PerPage = 25
	}

	if r.Page == 0 {
		r.Page = 1
	}

	// get count of all of the user's api tokens
	totalApiTokens, err := s.ApitokenRespository.GetTotalApiTokens(ctx, r.CreatedByID, r.CreatedByNanoId, r.Description, r.Status, "", "", r.OnlyEphemeral, r.OnlyPermanent)
	if err != nil {
		return nil, err
	}
	if err := validateTokenCount(ctx, totalApiTokens); err != nil {
		return nil, err
	}

	r.TotalCount = int(totalApiTokens)

	logger.Info("total-api-tokens-found", zap.Int64("total", totalApiTokens))

	apitokens, err := s.ApitokenRespository.GetAPITokens(ctx, r)
	if err != nil {
		return nil, err
	}

	// Listing is non-mutating: retained expired/revoked records remain manageable.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	analysedApitokens := append([]UserAPIToken(nil), apitokens...)

	// generate human readable
	for i, token := range analysedApitokens {
		token.Value = ""
		token.ValueSHA = append([]byte(nil), token.ValueSHA...)
		analysedApitokens[i] = *token.GenerateHumanReadable()
	}

	paginatedResource, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: r.PerPage,
		Page:    r.Page,
	}, analysedApitokens, r.TotalCount)
	if err != nil {
		return nil, err
	}

	return &GetAPITokensResponse{
		Total:            paginatedResource.Total,
		TotalPages:       paginatedResource.TotalPages,
		APITokens:        paginatedResource.Resources,
		Page:             paginatedResource.Page,
		APITokensPerPage: paginatedResource.ResourcePerPage,
	}, nil
}

// checkContext rejects unusable service wiring and canceled work before custom
// persistence dispatch. Stores still own in-flight cancellation and transactions;
// an error after a write is not proof that the write failed to commit.
func (s *Service) checkContext(ctx context.Context) error {
	if s == nil || s.ApitokenRespository == nil || ctx == nil {
		return ErrServiceUnavailable
	}
	return ctx.Err()
}

// validateTokenCount prevents invalid store results from entering pagination.
// The round trip detects counts that cannot be represented by its int fields.
func validateTokenCount(ctx context.Context, count int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if count < 0 || int64(int(count)) != count {
		return ErrServiceUnavailable
	}
	return nil
}
