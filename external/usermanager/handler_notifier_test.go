package usermanager_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/reply/v2"
)

// mockUmsService is a function-pointer mock of [usermanager.UsermanagerService].
//
// Each public method delegates to a *Func field when set; otherwise it
// returns a safe zero value.  This keeps test setup focused on the exact
// methods under test while letting the compiler verify the interface.
type mockUmsService struct {
	getNotifierConfigFunc             func(ctx context.Context, r *usermanager.GetNotifierConfigRequest) (*usermanager.GetNotifierConfigResponse, error)
	registerNotificationAddressFunc   func(ctx context.Context, r *usermanager.RegisterNotificationAddressRequest) (*usermanager.RegisterNotificationAddressResponse, error)
	listNotificationAddressesFunc     func(ctx context.Context, r *usermanager.ListNotificationAddressesRequest) (*usermanager.ListNotificationAddressesResponse, error)
	deleteNotificationAddressFunc     func(ctx context.Context, r *usermanager.DeleteNotificationAddressRequest) error
	getNotificationPreferencesFunc    func(ctx context.Context, r *usermanager.GetNotificationPreferencesRequest) (*usermanager.GetNotificationPreferencesResponse, error)
	updateNotificationPreferencesFunc func(ctx context.Context, r *usermanager.UpdateNotificationPreferencesRequest) (*usermanager.UpdateNotificationPreferencesResponse, error)
	notifyUserFunc                    func(ctx context.Context, r *usermanager.NotifyUserRequest) (*usermanager.NotifyUserResponse, error)
}

// stubErr is returned when a mockUmsService method is called without a matching *Func field.
var stubErr = errors.New("mock not implemented")

func (m *mockUmsService) GetNotifierConfig(ctx context.Context, r *usermanager.GetNotifierConfigRequest) (*usermanager.GetNotifierConfigResponse, error) {
	if m.getNotifierConfigFunc != nil {
		return m.getNotifierConfigFunc(ctx, r)
	}
	return nil, stubErr
}

func (m *mockUmsService) RegisterNotificationAddress(ctx context.Context, r *usermanager.RegisterNotificationAddressRequest) (*usermanager.RegisterNotificationAddressResponse, error) {
	if m.registerNotificationAddressFunc != nil {
		return m.registerNotificationAddressFunc(ctx, r)
	}
	return nil, stubErr
}

func (m *mockUmsService) ListNotificationAddresses(ctx context.Context, r *usermanager.ListNotificationAddressesRequest) (*usermanager.ListNotificationAddressesResponse, error) {
	if m.listNotificationAddressesFunc != nil {
		return m.listNotificationAddressesFunc(ctx, r)
	}
	return nil, stubErr
}

func (m *mockUmsService) DeleteNotificationAddress(ctx context.Context, r *usermanager.DeleteNotificationAddressRequest) error {
	if m.deleteNotificationAddressFunc != nil {
		return m.deleteNotificationAddressFunc(ctx, r)
	}
	return stubErr
}

func (m *mockUmsService) GetNotificationPreferences(ctx context.Context, r *usermanager.GetNotificationPreferencesRequest) (*usermanager.GetNotificationPreferencesResponse, error) {
	if m.getNotificationPreferencesFunc != nil {
		return m.getNotificationPreferencesFunc(ctx, r)
	}
	return nil, stubErr
}

func (m *mockUmsService) UpdateNotificationPreferences(ctx context.Context, r *usermanager.UpdateNotificationPreferencesRequest) (*usermanager.UpdateNotificationPreferencesResponse, error) {
	if m.updateNotificationPreferencesFunc != nil {
		return m.updateNotificationPreferencesFunc(ctx, r)
	}
	return nil, stubErr
}

func (m *mockUmsService) NotifyUser(ctx context.Context, r *usermanager.NotifyUserRequest) (*usermanager.NotifyUserResponse, error) {
	if m.notifyUserFunc != nil {
		return m.notifyUserFunc(ctx, r)
	}
	return nil, stubErr
}

// remaining UsermanagerService methods — not used by notifier tests
func (m *mockUmsService) GetUserMicroProfile(ctx context.Context, r *usermanager.GetUserMicroProfileRequest) (*usermanager.GetUserMicroProfileResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetUserProfile(ctx context.Context, r *usermanager.GetUserProfileRequest) (*usermanager.GetUserProfileResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetUserByID(ctx context.Context, r *usermanager.GetUserByIDRequest) (*usermanager.GetUserByIDResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetUsers(ctx context.Context, r *usermanager.GetUsersRequest) (*usermanager.GetUsersResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) UpdateUserProfile(ctx context.Context, r *usermanager.UpdateUserProfileRequest) (*usermanager.UpdateUserProfileResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) DeleteUserPermanently(ctx context.Context, r *usermanager.DeleteUserPermanentlyRequest) error {
	return stubErr
}
func (m *mockUmsService) CreateComms(ctx context.Context, req *usermanager.CreateCommsRequest) (*usermanager.CreateCommsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetComms(ctx context.Context, req *usermanager.GetCommsRequest) (*usermanager.GetCommsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) UpdateComms(ctx context.Context, req *usermanager.UpdateCommsRequest) (*usermanager.UpdateCommsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetCommsStats(ctx context.Context, req *usermanager.GetCommsStatsRequest) (*usermanager.GetCommsStatsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetEnrichedUserProfile(ctx context.Context, r *usermanager.GetEnrichedUserProfileRequest) (*usermanager.GetEnrichedUserProfileResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetUserGroupMemberships(ctx context.Context, r *usermanager.GetUserGroupMembershipsRequest) (*usermanager.GetUserGroupMembershipsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetUserGroups(ctx context.Context, r *usermanager.GetUserGroupsRequest) (*usermanager.GetUserGroupsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetLatestNotificationOverviews(ctx context.Context, r *usermanager.GetLatestNotificationOverviewsRequest) (*usermanager.GetLatestNotificationOverviewsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetMyGroupInvitations(ctx context.Context, r *usermanager.GetMyGroupInvitationsRequest) (*usermanager.GetMyGroupInvitationsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) AcceptMyGroupInvitation(ctx context.Context, r *usermanager.AcceptMyGroupInvitationRequest) (*usermanager.AcceptMyGroupInvitationResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) RejectMyGroupInvitation(ctx context.Context, r *usermanager.RejectMyGroupInvitationRequest) (*usermanager.RejectMyGroupInvitationResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetGroupDetail(ctx context.Context, r *usermanager.GetGroupDetailRequest) (*usermanager.GetGroupDetailResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetGroupStats(ctx context.Context, r *usermanager.GetGroupStatsRequest) (*usermanager.GetGroupStatsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) CreateGroup(ctx context.Context, r *usermanager.CreateGroupRequest) (*usermanager.CreateGroupResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) UpdateGroup(ctx context.Context, r *usermanager.UpdateGroupRequest) (*usermanager.UpdateGroupResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) DeleteGroup(ctx context.Context, r *usermanager.DeleteGroupRequest) (*usermanager.DeleteGroupResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetGroupsByUserID(ctx context.Context, r *usermanager.GetGroupsByUserIDRequest) (*usermanager.GetGroupsByUserIDResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetGroupsConfig(ctx context.Context, r *usermanager.GetGroupsConfigRequest) (*usermanager.GetGroupsConfigResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetGroupLineage(ctx context.Context, r *usermanager.GetGroupLineageRequest) (*usermanager.GetGroupLineageResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetGroupDescendants(ctx context.Context, r *usermanager.GetGroupDescendantsRequest) (*usermanager.GetGroupDescendantsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) ValidateGroupName(ctx context.Context, r *usermanager.ValidateGroupNameRequest) (*usermanager.ValidateGroupNameResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) AddGroupMember(ctx context.Context, r *usermanager.AddGroupMemberRequest) (*usermanager.AddGroupMemberResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) RemoveGroupMember(ctx context.Context, r *usermanager.RemoveGroupMemberRequest) (*usermanager.RemoveGroupMemberResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) UpdateGroupMember(ctx context.Context, r *usermanager.UpdateGroupMemberRequest) (*usermanager.UpdateGroupMemberResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) UpdateGroupOwner(ctx context.Context, r *usermanager.UpdateGroupOwnerRequest) (*usermanager.UpdateGroupOwnerResponse, error) {
	return nil, stubErr
}

// mockValidator wraps [validator.Validate] so tests can force validation failures.
type mockValidator struct {
	validateFunc func(s interface{}) error
}

func (m *mockValidator) Validate(s interface{}) error {
	if m.validateFunc != nil {
		return m.validateFunc(s)
	}
	return validator.New().Struct(s)
}

// newTestHandler builds a [usermanager.Handler] wired with the given mock
// service, a real-validating mock validator, and the notifier error manifest
// so that notifier sentinel errors are mapped to the correct HTTP codes.
func newTestHandler(svc usermanager.UsermanagerService) *usermanager.Handler {
	return usermanager.NewHandler(&usermanager.NewHandlerRequest{
		Service:   svc,
		Validator: &mockValidator{},
		ErrorMaps: []reply.ErrorManifest{notifier.NotifierErrorMap},
	})
}

// authenticatedRequest returns an *http.Request whose context carries the
// given user ID so fender mappers treat the caller as signed-in.
func authenticatedRequest(method, target string, body []byte, userID string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
	return req
}

// responseData unmarshals the JSON "data" envelope from an httptest recorder.
func responseData(t *testing.T, rec *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.NoError(t, json.Unmarshal(envelope.Data, target))
}

// responseBlank asserts the delete-success response body uses the same
// local reply/v2 data envelope observed in production: {"data":"{}"}.
func responseBlank(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var envelope struct {
		Data string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	assert.Equal(t, "{}", envelope.Data)
}

// responseErrorCode reads the first error "code" from the reply/v2
// errors array envelope: {"errors":[{"code":"..."}]}.
func responseErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Errors)
	return envelope.Errors[0].Code
}

// ---------------------------------------------------------------------------
// GetNotifierConfig
// ---------------------------------------------------------------------------

func TestHandler_GetNotifierConfig_Success(t *testing.T) {
	t.Parallel()

	expectedConfig := &notifier.NotifierConfig{
		SupportedChannels: []notifier.NotificationChannel{notifier.NotificationChannelWebPush},
		WebPush:           notifier.WebPushClientConfig{Enabled: true, VAPIDPublicKey: "vapid-pub"},
		FCM:               notifier.FCMClientConfig{Enabled: false},
	}

	svc := &mockUmsService{
		getNotifierConfigFunc: func(ctx context.Context, r *usermanager.GetNotifierConfigRequest) (*usermanager.GetNotifierConfigResponse, error) {
			require.Equal(t, "user-1", r.UserId)
			return &usermanager.GetNotifierConfigResponse{
				GetNotifierConfigResponse: &notifier.GetNotifierConfigResponse{Config: expectedConfig},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/config", nil, "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetNotifierConfig(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var config notifier.NotifierConfig
	responseData(t, rec, &config)
	assert.Equal(t, expectedConfig.WebPush.VAPIDPublicKey, config.WebPush.VAPIDPublicKey)
}

func TestHandler_GetNotifierConfig_Unauthenticated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetNotifierConfig(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "USM00-002", responseErrorCode(t, rec))
}

func TestHandler_GetNotifierConfig_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		getNotifierConfigFunc: func(ctx context.Context, r *usermanager.GetNotifierConfigRequest) (*usermanager.GetNotifierConfigResponse, error) {
			return nil, usermanager.ErrNotifierServiceNotEnabled
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/config", nil, "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetNotifierConfig(rec, req) })
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "USM00-018", responseErrorCode(t, rec))
}

// ---------------------------------------------------------------------------
// RegisterNotificationAddress
// ---------------------------------------------------------------------------

func TestHandler_RegisterNotificationAddress_WebPush(t *testing.T) {
	t.Parallel()

	expectedSummary := notifier.NotificationAddressSummary{
		ID: "addr-webpush-1", Channel: notifier.NotificationChannelWebPush, Status: "ACTIVE",
		DeviceName: "Chrome on Test", Platform: "WEB",
	}

	svc := &mockUmsService{
		registerNotificationAddressFunc: func(ctx context.Context, r *usermanager.RegisterNotificationAddressRequest) (*usermanager.RegisterNotificationAddressResponse, error) {
			require.Equal(t, "user-1", r.UserId)
			require.NotNil(t, r.RegisterAddressRequest)
			require.Equal(t, notifier.NotificationChannelWebPush, r.RegisterAddressRequest.Channel)
			return &usermanager.RegisterNotificationAddressResponse{
				RegisterAddressResponse: &notifier.RegisterAddressResponse{Address: expectedSummary},
			}, nil
		},
	}

	body := `{"channel":"WEBPUSH","device_name":"Chrome on Test","platform":"WEB","webpush":{"endpoint":"https://example","keys":{"p256dh":"k","auth":"a"}}}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/addresses", []byte(body), "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.RegisterNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusCreated, rec.Code)

	var summary notifier.NotificationAddressSummary
	responseData(t, rec, &summary)
	assert.Equal(t, "addr-webpush-1", summary.ID)
	assert.Equal(t, notifier.NotificationChannelWebPush, summary.Channel)
}

func TestHandler_RegisterNotificationAddress_FCM(t *testing.T) {
	t.Parallel()

	expectedSummary := notifier.NotificationAddressSummary{
		ID: "addr-fcm-1", Channel: notifier.NotificationChannelFCM, Status: "ACTIVE",
		DeviceName: "Pixel 7", Platform: "ANDROID",
	}

	svc := &mockUmsService{
		registerNotificationAddressFunc: func(ctx context.Context, r *usermanager.RegisterNotificationAddressRequest) (*usermanager.RegisterNotificationAddressResponse, error) {
			require.NotNil(t, r.RegisterAddressRequest)
			require.NotNil(t, r.RegisterAddressRequest.FCM)
			require.Equal(t, "fcm-token-abc", r.RegisterAddressRequest.FCM.Token)
			return &usermanager.RegisterNotificationAddressResponse{
				RegisterAddressResponse: &notifier.RegisterAddressResponse{Address: expectedSummary},
			}, nil
		},
	}

	body := `{"channel":"FCM","device_name":"Pixel 7","platform":"ANDROID","fcm":{"token":"fcm-token-abc"}}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/addresses", []byte(body), "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.RegisterNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusCreated, rec.Code)

	var summary notifier.NotificationAddressSummary
	responseData(t, rec, &summary)
	assert.Equal(t, "addr-fcm-1", summary.ID)
}

func TestHandler_RegisterNotificationAddress_InvalidBody(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := authenticatedRequest(http.MethodPost, "/addresses", []byte(`{invalid}`), "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.RegisterNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "NTF00-002", responseErrorCode(t, rec))
}

func TestHandler_RegisterNotificationAddress_Unauthenticated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodPost, "/addresses", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.RegisterNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_RegisterNotificationAddress_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		registerNotificationAddressFunc: func(ctx context.Context, r *usermanager.RegisterNotificationAddressRequest) (*usermanager.RegisterNotificationAddressResponse, error) {
			return nil, notifier.ErrNotificationUserIDRequired
		},
	}

	body := `{"channel":"WEBPUSH","webpush":{"endpoint":"https://example","keys":{"p256dh":"k","auth":"a"}}}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/addresses", []byte(body), "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.RegisterNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "NTF00-008", responseErrorCode(t, rec))
}

// ---------------------------------------------------------------------------
// ListNotificationAddresses
// ---------------------------------------------------------------------------

func TestHandler_ListNotificationAddresses_Success(t *testing.T) {
	t.Parallel()

	summaries := []notifier.NotificationAddressSummary{
		{ID: "addr-1", Channel: notifier.NotificationChannelWebPush, Status: "ACTIVE", DeviceName: "Chrome", Platform: "WEB"},
		{ID: "addr-2", Channel: notifier.NotificationChannelFCM, Status: "ACTIVE", DeviceName: "Pixel 7", Platform: "ANDROID"},
	}

	svc := &mockUmsService{
		listNotificationAddressesFunc: func(ctx context.Context, r *usermanager.ListNotificationAddressesRequest) (*usermanager.ListNotificationAddressesResponse, error) {
			require.Equal(t, "user-1", r.UserId)
			return &usermanager.ListNotificationAddressesResponse{
				ListNotificationAddressesResponse: &notifier.ListNotificationAddressesResponse{Addresses: summaries},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/addresses", nil, "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.ListNotificationAddresses(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var addresses []notifier.NotificationAddressSummary
	responseData(t, rec, &addresses)
	assert.Len(t, addresses, 2)

	// secrets must not leak — verify raw JSON contains no endpoint, keys, or token
	body := rec.Body.String()
	assert.NotContains(t, body, "endpoint")
	assert.NotContains(t, body, "p256dh")
	assert.NotContains(t, body, "auth")
	assert.NotContains(t, body, "token")
}

func TestHandler_ListNotificationAddresses_Empty(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		listNotificationAddressesFunc: func(ctx context.Context, r *usermanager.ListNotificationAddressesRequest) (*usermanager.ListNotificationAddressesResponse, error) {
			return &usermanager.ListNotificationAddressesResponse{
				ListNotificationAddressesResponse: &notifier.ListNotificationAddressesResponse{Addresses: []notifier.NotificationAddressSummary{}},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/addresses", nil, "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.ListNotificationAddresses(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var addresses []notifier.NotificationAddressSummary
	responseData(t, rec, &addresses)
	assert.Len(t, addresses, 0)
}

func TestHandler_ListNotificationAddresses_Unauthenticated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodGet, "/addresses", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.ListNotificationAddresses(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---------------------------------------------------------------------------
// DeleteNotificationAddress
// ---------------------------------------------------------------------------

func TestHandler_DeleteNotificationAddress_Success(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		deleteNotificationAddressFunc: func(ctx context.Context, r *usermanager.DeleteNotificationAddressRequest) error {
			require.Equal(t, "user-1", r.UserId)
			require.Equal(t, "addr-to-delete", r.DeleteNotificationAddressRequest.AddressID)
			return nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodDelete, "/addresses/addr-to-delete", nil, "user-1")
	req = mux.SetURLVars(req, map[string]string{usermanager.UserManagerURIVariableAddressID: "addr-to-delete"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.DeleteNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)
	responseBlank(t, rec)
}

func TestHandler_DeleteNotificationAddress_NotFound(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		deleteNotificationAddressFunc: func(ctx context.Context, r *usermanager.DeleteNotificationAddressRequest) error {
			return notifier.ErrNotificationAddressNotFound
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodDelete, "/addresses/nonexistent", nil, "user-1")
	req = mux.SetURLVars(req, map[string]string{usermanager.UserManagerURIVariableAddressID: "nonexistent"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.DeleteNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "NTF00-004", responseErrorCode(t, rec))
}

func TestHandler_DeleteNotificationAddress_Unauthenticated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodDelete, "/addresses/addr-1", nil)
	req = mux.SetURLVars(req, map[string]string{usermanager.UserManagerURIVariableAddressID: "addr-1"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.DeleteNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_DeleteNotificationAddress_MissingAddressID(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := authenticatedRequest(http.MethodDelete, "/addresses/", nil, "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.DeleteNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "USM00-004", responseErrorCode(t, rec))
}

// ---------------------------------------------------------------------------
// GetNotificationPreferences
// ---------------------------------------------------------------------------

func TestHandler_GetNotificationPreferences_Success(t *testing.T) {
	t.Parallel()

	prefs := &notifier.NotificationPreferences{
		UserID: "user-1", Enabled: true,
		Channels: map[string]bool{string(notifier.NotificationChannelWebPush): true, string(notifier.NotificationChannelFCM): false},
	}

	svc := &mockUmsService{
		getNotificationPreferencesFunc: func(ctx context.Context, r *usermanager.GetNotificationPreferencesRequest) (*usermanager.GetNotificationPreferencesResponse, error) {
			require.Equal(t, "user-1", r.UserId)
			return &usermanager.GetNotificationPreferencesResponse{
				GetNotificationPreferencesResponse: &notifier.GetNotificationPreferencesResponse{Preferences: prefs},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/preferences", nil, "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var response notifier.NotificationPreferences
	responseData(t, rec, &response)
	assert.True(t, response.Enabled)
	assert.False(t, response.Channels[string(notifier.NotificationChannelFCM)])
}

func TestHandler_GetNotificationPreferences_DefaultsReturned(t *testing.T) {
	t.Parallel()

	defaultPrefs := notifier.DefaultNotificationPreferences("user-1")

	svc := &mockUmsService{
		getNotificationPreferencesFunc: func(ctx context.Context, r *usermanager.GetNotificationPreferencesRequest) (*usermanager.GetNotificationPreferencesResponse, error) {
			return &usermanager.GetNotificationPreferencesResponse{
				GetNotificationPreferencesResponse: &notifier.GetNotificationPreferencesResponse{Preferences: defaultPrefs},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/preferences", nil, "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var response notifier.NotificationPreferences
	responseData(t, rec, &response)
	assert.True(t, response.Enabled)
	assert.True(t, response.Channels[string(notifier.NotificationChannelWebPush)])
	assert.True(t, response.Channels[string(notifier.NotificationChannelFCM)])
}

func TestHandler_GetNotificationPreferences_Unauthenticated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodGet, "/preferences", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---------------------------------------------------------------------------
// UpdateNotificationPreferences
// ---------------------------------------------------------------------------

func TestHandler_UpdateNotificationPreferences_Success(t *testing.T) {
	t.Parallel()

	updated := &notifier.NotificationPreferences{
		UserID: "user-1", Enabled: false,
		Channels: map[string]bool{string(notifier.NotificationChannelWebPush): false, string(notifier.NotificationChannelFCM): true},
	}

	svc := &mockUmsService{
		updateNotificationPreferencesFunc: func(ctx context.Context, r *usermanager.UpdateNotificationPreferencesRequest) (*usermanager.UpdateNotificationPreferencesResponse, error) {
			require.Equal(t, "user-1", r.UserId)
			require.NotNil(t, r.UpdateNotificationPreferencesRequest)
			return &usermanager.UpdateNotificationPreferencesResponse{
				UpdateNotificationPreferencesResponse: &notifier.UpdateNotificationPreferencesResponse{Preferences: updated},
			}, nil
		},
	}

	body := `{"enabled":false,"channels":{"WEBPUSH":false,"FCM":true}}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPatch, "/preferences", []byte(body), "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.UpdateNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var response notifier.NotificationPreferences
	responseData(t, rec, &response)
	assert.False(t, response.Enabled)
	assert.False(t, response.Channels[string(notifier.NotificationChannelWebPush)])
}

func TestHandler_UpdateNotificationPreferences_InvalidBody(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := authenticatedRequest(http.MethodPatch, "/preferences", []byte(`{bad}`), "user-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.UpdateNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "NTF00-009", responseErrorCode(t, rec))
}

func TestHandler_UpdateNotificationPreferences_Unauthenticated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodPatch, "/preferences", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.UpdateNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---------------------------------------------------------------------------
// NotifyUser (admin/service send)
// ---------------------------------------------------------------------------

func TestHandler_NotifyUser_Success(t *testing.T) {
	t.Parallel()

	results := []notifier.NotificationSendResult{
		{Channel: notifier.NotificationChannelWebPush, Attempted: 1, Sent: true},
	}

	svc := &mockUmsService{
		notifyUserFunc: func(ctx context.Context, r *usermanager.NotifyUserRequest) (*usermanager.NotifyUserResponse, error) {
			require.Equal(t, "admin-id", r.UserId)
			require.NotNil(t, r.NotifyUserRequest)
			require.Equal(t, "target-user-789", r.NotifyUserRequest.UserID)
			require.Equal(t, "Hello", r.NotifyUserRequest.Title)
			return &usermanager.NotifyUserResponse{
				NotifyUserResponse: &notifier.NotifyUserResponse{Results: results},
			}, nil
		},
	}

	body := `{"title":"Hello","message":"World"}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/users/target-user-789/notifications", []byte(body), "admin-id")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-789"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUser(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var sendResults []notifier.NotificationSendResult
	responseData(t, rec, &sendResults)
	require.Len(t, sendResults, 1)
	assert.True(t, sendResults[0].Sent)
}

func TestHandler_NotifyUser_InvalidBody(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := authenticatedRequest(http.MethodPost, "/users/target-user-789/notifications", []byte(`{bad}`), "admin-id")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-789"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUser(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "NTF00-002", responseErrorCode(t, rec))
}

func TestHandler_NotifyUser_UnauthenticatedCaller(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodPost, "/users/target-user-789/notifications", bytes.NewBufferString(`{"title":"Hi","message":"there"}`))
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-789"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUser(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_NotifyUser_MissingUserIDPathVar(t *testing.T) {
	t.Parallel()

	body := `{"title":"Hi","message":"there"}`

	h := newTestHandler(&mockUmsService{})
	req := authenticatedRequest(http.MethodPost, "/users//notifications", []byte(body), "admin-id")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUser(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "USM00-004", responseErrorCode(t, rec))
}

func TestHandler_NotifyUser_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		notifyUserFunc: func(ctx context.Context, r *usermanager.NotifyUserRequest) (*usermanager.NotifyUserResponse, error) {
			return nil, notifier.ErrNotificationNoActiveAddresses
		},
	}

	body := `{"title":"Hi","message":"there"}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/users/target-user-789/notifications", []byte(body), "admin-id")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-789"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUser(rec, req) })
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "NTF00-006", responseErrorCode(t, rec))
}

func TestHandler_NotifyUser_PartialSuccess_ErrorResponse(t *testing.T) {
	// When the service returns partial results AND an error, the handler
	// writes an HTTP error response with the mapped error code.  The
	// per-channel results are not included in the error response body.
	t.Parallel()

	results := []notifier.NotificationSendResult{
		{Channel: notifier.NotificationChannelWebPush, Attempted: 1, Sent: true},
		{Channel: notifier.NotificationChannelFCM, Attempted: 1, Sent: false, Skipped: true, Error: "sender not enabled"},
	}

	svc := &mockUmsService{
		notifyUserFunc: func(ctx context.Context, r *usermanager.NotifyUserRequest) (*usermanager.NotifyUserResponse, error) {
			return &usermanager.NotifyUserResponse{
				NotifyUserResponse: &notifier.NotifyUserResponse{Results: results},
			}, notifier.ErrNotificationSendFailed
		},
	}

	body := `{"title":"Hi","message":"there"}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/users/target-user-789/notifications", []byte(body), "admin-id")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-789"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUser(rec, req) })
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "NTF00-007", responseErrorCode(t, rec))
}
