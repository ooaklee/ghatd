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
	"github.com/ooaklee/ghatd/external/common"
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
	getNotifierConfigFunc              func(ctx context.Context, r *usermanager.GetNotifierConfigRequest) (*usermanager.GetNotifierConfigResponse, error)
	registerNotificationAddressFunc    func(ctx context.Context, r *usermanager.RegisterNotificationAddressRequest) (*usermanager.RegisterNotificationAddressResponse, error)
	listNotificationAddressesFunc      func(ctx context.Context, r *usermanager.ListNotificationAddressesRequest) (*usermanager.ListNotificationAddressesResponse, error)
	deleteNotificationAddressFunc      func(ctx context.Context, r *usermanager.DeleteNotificationAddressRequest) error
	getNotificationPreferencesFunc     func(ctx context.Context, r *usermanager.GetNotificationPreferencesRequest) (*usermanager.GetNotificationPreferencesResponse, error)
	updateNotificationPreferencesFunc  func(ctx context.Context, r *usermanager.UpdateNotificationPreferencesRequest) (*usermanager.UpdateNotificationPreferencesResponse, error)
	getLatestNotificationOverviewsFunc func(ctx context.Context, r *usermanager.GetLatestNotificationOverviewsRequest) (*usermanager.GetLatestNotificationOverviewsResponse, error)
	notifyUserFunc                     func(ctx context.Context, r *usermanager.NotifyUserRequest) (*usermanager.NotifyUserResponse, error)
	notifyUsersFunc                    func(ctx context.Context, r *usermanager.NotifyUsersRequest) (*usermanager.NotifyUsersResponse, error)
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

func (m *mockUmsService) NotifyUsers(ctx context.Context, r *usermanager.NotifyUsersRequest) (*usermanager.NotifyUsersResponse, error) {
	if m.notifyUsersFunc != nil {
		return m.notifyUsersFunc(ctx, r)
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
	if m.getLatestNotificationOverviewsFunc != nil {
		return m.getLatestNotificationOverviewsFunc(ctx, r)
	}
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
func (m *mockUmsService) CreateReminder(ctx context.Context, r *usermanager.CreateReminderRequest) (*usermanager.CreateReminderResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetReminderByID(ctx context.Context, r *usermanager.GetReminderByIDRequest) (*usermanager.GetReminderByIDResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) ListReminders(ctx context.Context, r *usermanager.ListRemindersRequest) (*usermanager.ListRemindersResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) UpdateReminderByID(ctx context.Context, r *usermanager.UpdateReminderByIDRequest) (*usermanager.UpdateReminderByIDResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) DeleteReminderByID(ctx context.Context, r *usermanager.DeleteReminderByIDRequest) error {
	return stubErr
}
func (m *mockUmsService) DisableReminderByID(ctx context.Context, r *usermanager.DisableReminderByIDRequest) (*usermanager.UpdateReminderByIDResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetReminderStats(ctx context.Context, r *usermanager.GetReminderStatsRequest) (*usermanager.GetReminderStatsResponse, error) {
	return nil, stubErr
}
func (m *mockUmsService) GetDueReminders(ctx context.Context, r *usermanager.GetDueRemindersRequest) (*usermanager.GetDueRemindersResponse, error) {
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
// GetLatestNotificationOverviews
// ---------------------------------------------------------------------------

func TestHandler_GetLatestNotificationOverviews_AdminRouteTargetsPathUser(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		getLatestNotificationOverviewsFunc: func(ctx context.Context, r *usermanager.GetLatestNotificationOverviewsRequest) (*usermanager.GetLatestNotificationOverviewsResponse, error) {
			require.Equal(t, "admin-1", r.UserId)
			require.NotNil(t, r.GetLatestNotificationOverviewsRequest)
			require.Equal(t, "target-user-1", r.GetLatestNotificationOverviewsRequest.UserID)
			require.Equal(t, "group_invite_outstanding", r.Kinds)
			require.Equal(t, 5, r.Limit)
			return &usermanager.GetLatestNotificationOverviewsResponse{
				GetLatestNotificationOverviewsResponse: &common.GetLatestNotificationOverviewsResponse{
					Overviews: []common.NotificationOverview{
						{ID: "overview-1", Source: common.NotificationSourceGroup, Kind: common.NotificationKindGroupInviteOutstanding, Title: "Invitation"},
					},
				},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/api/v1/ums/notifications/target-user-1/latest?kinds=group_invite_outstanding&limit=5", nil, "admin-1")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-1"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetLatestNotificationOverviews(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var overviews []common.NotificationOverview
	responseData(t, rec, &overviews)
	require.Len(t, overviews, 1)
	assert.Equal(t, "overview-1", overviews[0].ID)
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

func TestHandler_RegisterNotificationAddress_AdminRouteCanRegisterForTargetUser(t *testing.T) {
	t.Parallel()

	expectedSummary := notifier.NotificationAddressSummary{
		ID: "addr-admin-target", UserID: "target-user-1", Channel: notifier.NotificationChannelWebPush, Status: "ACTIVE",
		DeviceName: "Target Browser", Platform: "WEB",
	}

	svc := &mockUmsService{
		registerNotificationAddressFunc: func(ctx context.Context, r *usermanager.RegisterNotificationAddressRequest) (*usermanager.RegisterNotificationAddressResponse, error) {
			require.Equal(t, "admin-1", r.UserId)
			require.NotNil(t, r.RegisterAddressRequest)
			require.Equal(t, "target-user-1", r.RegisterAddressRequest.UserID)
			require.Equal(t, notifier.NotificationChannelWebPush, r.RegisterAddressRequest.Channel)
			return &usermanager.RegisterNotificationAddressResponse{
				RegisterAddressResponse: &notifier.RegisterAddressResponse{Address: expectedSummary},
			}, nil
		},
	}

	body := `{"channel":"WEBPUSH","device_name":"Target Browser","platform":"WEB","webpush":{"endpoint":"https://example","keys":{"p256dh":"k","auth":"a"}}}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/api/v1/ums/notifications/addresses?user_id=target-user-1", []byte(body), "admin-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.RegisterNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusCreated, rec.Code)

	var summary notifier.NotificationAddressSummary
	responseData(t, rec, &summary)
	assert.Equal(t, "target-user-1", summary.UserID)
	assert.Equal(t, "addr-admin-target", summary.ID)
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

func TestHandler_ListNotificationAddresses_AdminRouteCanListPlatformDevices(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		listNotificationAddressesFunc: func(ctx context.Context, r *usermanager.ListNotificationAddressesRequest) (*usermanager.ListNotificationAddressesResponse, error) {
			require.Equal(t, "admin-1", r.UserId)
			require.True(t, r.AdminView)
			require.True(t, r.IncludeUsers)
			require.NotNil(t, r.ListNotificationAddressesRequest)
			require.Empty(t, r.ListNotificationAddressesRequest.UserID)
			require.Equal(t, notifier.NotificationChannelWebPush, r.ListNotificationAddressesRequest.Channel)
			require.Equal(t, notifier.NotificationAddressStatusActive, r.ListNotificationAddressesRequest.Status)
			require.Equal(t, 2, r.ListNotificationAddressesRequest.Page)
			require.Equal(t, 25, r.ListNotificationAddressesRequest.PerPage)
			require.True(t, r.ListNotificationAddressesRequest.Meta)
			return &usermanager.ListNotificationAddressesResponse{
				Addresses: []usermanager.NotificationAddressWithUser{
					{
						NotificationAddressSummary: notifier.NotificationAddressSummary{
							ID: "addr-1", UserID: "user-1", Channel: notifier.NotificationChannelWebPush, Status: notifier.NotificationAddressStatusActive,
						},
						User: &usermanager.EnrichedUserProfile{ID: "user-1", Email: "user@example.com", FullName: "User One"},
					},
				},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/api/v1/ums/notifications/addresses?channel=webpush&status=active&page=2&per_page=25&meta=true", nil, "admin-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.ListNotificationAddresses(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var addresses []usermanager.NotificationAddressWithUser
	responseData(t, rec, &addresses)
	require.Len(t, addresses, 1)
	assert.Equal(t, "user-1", addresses[0].UserID)
	require.NotNil(t, addresses[0].User)
	assert.Equal(t, "user@example.com", addresses[0].User.Email)
}

func TestHandler_ListNotificationAddresses_AdminRouteCanFilterByUser(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		listNotificationAddressesFunc: func(ctx context.Context, r *usermanager.ListNotificationAddressesRequest) (*usermanager.ListNotificationAddressesResponse, error) {
			require.True(t, r.AdminView)
			require.Equal(t, "user-2", r.ListNotificationAddressesRequest.UserID)
			require.False(t, r.IncludeUsers)
			return &usermanager.ListNotificationAddressesResponse{Addresses: []usermanager.NotificationAddressWithUser{}}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/api/v1/ums/notifications/addresses?user_id=user-2&include_users=false", nil, "admin-1")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.ListNotificationAddresses(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)
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

func TestHandler_DeleteNotificationAddress_AdminRouteTargetsPathUser(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		deleteNotificationAddressFunc: func(ctx context.Context, r *usermanager.DeleteNotificationAddressRequest) error {
			require.Equal(t, "admin-id", r.UserId)
			require.Equal(t, "target-user-1", r.DeleteNotificationAddressRequest.UserID)
			require.Equal(t, "addr-to-delete", r.DeleteNotificationAddressRequest.AddressID)
			return nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodDelete, "/api/v1/ums/notifications/target-user-1/addresses/addr-to-delete", nil, "admin-id")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-1", usermanager.UserManagerURIVariableAddressID: "addr-to-delete"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.DeleteNotificationAddress(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)
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

func TestHandler_GetNotificationPreferences_AdminRouteReturnsUserDetails(t *testing.T) {
	t.Parallel()

	prefs := &notifier.NotificationPreferences{
		UserID: "target-user-1", Enabled: true,
		Channels: map[string]bool{string(notifier.NotificationChannelWebPush): true},
	}

	svc := &mockUmsService{
		getNotificationPreferencesFunc: func(ctx context.Context, r *usermanager.GetNotificationPreferencesRequest) (*usermanager.GetNotificationPreferencesResponse, error) {
			require.Equal(t, "admin-id", r.UserId)
			require.Equal(t, "target-user-1", r.GetNotificationPreferencesRequest.UserID)
			require.True(t, r.IncludeUser)
			return &usermanager.GetNotificationPreferencesResponse{
				Preferences: &usermanager.NotificationPreferencesWithUser{
					NotificationPreferences: prefs,
					User:                    &usermanager.EnrichedUserProfile{ID: "target-user-1", Email: "target@example.com"},
				},
			}, nil
		},
	}

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodGet, "/api/v1/ums/notifications/target-user-1/preferences", nil, "admin-id")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-1"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.GetNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var response usermanager.NotificationPreferencesWithUser
	responseData(t, rec, &response)
	assert.Equal(t, "target-user-1", response.UserID)
	require.NotNil(t, response.User)
	assert.Equal(t, "target@example.com", response.User.Email)
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

func TestHandler_UpdateNotificationPreferences_AdminRouteTargetsPathUser(t *testing.T) {
	t.Parallel()

	updated := &notifier.NotificationPreferences{
		UserID: "target-user-1", Enabled: false,
		Channels: map[string]bool{string(notifier.NotificationChannelWebPush): false},
	}

	svc := &mockUmsService{
		updateNotificationPreferencesFunc: func(ctx context.Context, r *usermanager.UpdateNotificationPreferencesRequest) (*usermanager.UpdateNotificationPreferencesResponse, error) {
			require.Equal(t, "admin-1", r.UserId)
			require.NotNil(t, r.UpdateNotificationPreferencesRequest)
			require.Equal(t, "target-user-1", r.UpdateNotificationPreferencesRequest.UserID)
			require.True(t, r.IncludeUser)
			require.False(t, r.UpdateNotificationPreferencesRequest.Channels[string(notifier.NotificationChannelWebPush)])
			return &usermanager.UpdateNotificationPreferencesResponse{
				Preferences: &usermanager.NotificationPreferencesWithUser{
					NotificationPreferences: updated,
					User:                    &usermanager.EnrichedUserProfile{ID: "target-user-1", Email: "target@example.com"},
				},
			}, nil
		},
	}

	body := `{"channels":{"WEBPUSH":false}}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPatch, "/api/v1/ums/notifications/target-user-1/preferences", []byte(body), "admin-1")
	req = mux.SetURLVars(req, map[string]string{"userId": "target-user-1"})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.UpdateNotificationPreferences(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var response usermanager.NotificationPreferencesWithUser
	responseData(t, rec, &response)
	assert.Equal(t, "target-user-1", response.UserID)
	require.NotNil(t, response.User)
	assert.Equal(t, "target@example.com", response.User.Email)
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

// ---------------------------------------------------------------------------
// NotifyUsers (admin broadcast)
// ---------------------------------------------------------------------------

func TestHandler_NotifyUsers_GOOD_TargetedMultiUser(t *testing.T) {
	t.Parallel()

	results := []notifier.NotifyUsersResult{
		{
			UserID: "user-1",
			Results: []notifier.NotificationSendResult{
				{Channel: notifier.NotificationChannelWebPush, Attempted: 1, Sent: true},
			},
		},
		{
			UserID: "user-2",
			Results: []notifier.NotificationSendResult{
				{Channel: notifier.NotificationChannelWebPush, Attempted: 2, Sent: true},
				{Channel: notifier.NotificationChannelFCM, Attempted: 1, Sent: true},
			},
		},
	}

	svc := &mockUmsService{
		notifyUsersFunc: func(ctx context.Context, r *usermanager.NotifyUsersRequest) (*usermanager.NotifyUsersResponse, error) {
			require.Equal(t, "admin-id", r.UserId)
			require.NotNil(t, r.NotifyUsersRequest)
			require.Equal(t, []string{"user-1", "user-2"}, r.NotifyUsersRequest.UserIDs)
			require.Equal(t, "Broadcast Title", r.NotifyUsersRequest.Title)
			require.Equal(t, "Broadcast Message", r.NotifyUsersRequest.Message)
			return &usermanager.NotifyUsersResponse{
				NotifyUsersResponse: &notifier.NotifyUsersResponse{Results: results},
			}, nil
		},
	}

	body := `{"user_ids":["user-1","user-2"],"title":"Broadcast Title","message":"Broadcast Message"}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/api/v1/ums/notifications", []byte(body), "admin-id")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUsers(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var notifyResults []notifier.NotifyUsersResult
	responseData(t, rec, &notifyResults)
	require.Len(t, notifyResults, 2)
	assert.Equal(t, "user-1", notifyResults[0].UserID)
	assert.True(t, notifyResults[0].Results[0].Sent)
	assert.Equal(t, "user-2", notifyResults[1].UserID)
	require.Len(t, notifyResults[1].Results, 2)
}

func TestHandler_NotifyUsers_GOOD_BroadcastAll(t *testing.T) {
	t.Parallel()

	results := []notifier.NotifyUsersResult{
		{
			UserID: "user-a",
			Results: []notifier.NotificationSendResult{
				{Channel: notifier.NotificationChannelWebPush, Attempted: 1, Sent: true},
			},
		},
		{
			UserID: "user-b",
			Results: []notifier.NotificationSendResult{
				{Channel: notifier.NotificationChannelFCM, Attempted: 1, Sent: true},
			},
		},
		{
			UserID: "user-c",
			Results: []notifier.NotificationSendResult{
				{Channel: notifier.NotificationChannelWebPush, Attempted: 1, Sent: true},
			},
		},
	}

	svc := &mockUmsService{
		notifyUsersFunc: func(ctx context.Context, r *usermanager.NotifyUsersRequest) (*usermanager.NotifyUsersResponse, error) {
			require.Empty(t, r.NotifyUsersRequest.UserIDs)
			require.Empty(t, r.NotifyUsersRequest.Channels)
			return &usermanager.NotifyUsersResponse{
				NotifyUsersResponse: &notifier.NotifyUsersResponse{Results: results},
			}, nil
		},
	}

	body := `{"title":"All Hands","message":"Meeting at 3pm"}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/api/v1/ums/notifications", []byte(body), "admin-id")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUsers(rec, req) })
	assert.Equal(t, http.StatusOK, rec.Code)

	var notifyResults []notifier.NotifyUsersResult
	responseData(t, rec, &notifyResults)
	require.Len(t, notifyResults, 3)
	assert.Equal(t, "user-a", notifyResults[0].UserID)
	assert.Equal(t, "user-b", notifyResults[1].UserID)
	assert.Equal(t, "user-c", notifyResults[2].UserID)
}

func TestHandler_NotifyUsers_BAD_MissingSubjectOrMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{"missing title", `{"user_ids":["u1"],"message":"body"}`},
		{"missing message", `{"user_ids":["u1"],"title":"title"}`},
		{"missing both", `{"user_ids":["u1"]}`},
		{"empty title", `{"user_ids":["u1"],"title":"","message":"body"}`},
		{"empty message", `{"user_ids":["u1"],"title":"title","message":""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(&mockUmsService{})
			req := authenticatedRequest(http.MethodPost, "/api/v1/ums/notifications", []byte(tt.body), "admin-id")
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() { h.NotifyUsers(rec, req) })
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "USM00-004", responseErrorCode(t, rec))
		})
	}
}

func TestHandler_NotifyUsers_BAD_InvalidChannel(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		notifyUsersFunc: func(ctx context.Context, r *usermanager.NotifyUsersRequest) (*usermanager.NotifyUsersResponse, error) {
			return nil, notifier.ErrInvalidNotificationChannel
		},
	}

	body := `{"user_ids":["u1"],"title":"Hello","message":"World","channels":["SMS","WEBPUSH"]}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/api/v1/ums/notifications", []byte(body), "admin-id")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUsers(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "NTF00-003", responseErrorCode(t, rec))
}

func TestHandler_NotifyUsers_Unauthenticated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ums/notifications", bytes.NewBufferString(`{"title":"Hi","message":"there"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUsers(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_NotifyUsers_InvalidBody(t *testing.T) {
	t.Parallel()

	h := newTestHandler(&mockUmsService{})
	req := authenticatedRequest(http.MethodPost, "/api/v1/ums/notifications", []byte(`{bad}`), "admin-id")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUsers(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "NTF00-002", responseErrorCode(t, rec))
}

func TestHandler_NotifyUsers_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &mockUmsService{
		notifyUsersFunc: func(ctx context.Context, r *usermanager.NotifyUsersRequest) (*usermanager.NotifyUsersResponse, error) {
			return nil, notifier.ErrInvalidNotificationAddressBody
		},
	}

	body := `{"title":"Hi","message":"there"}`

	h := newTestHandler(svc)
	req := authenticatedRequest(http.MethodPost, "/api/v1/ums/notifications", []byte(body), "admin-id")
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { h.NotifyUsers(rec, req) })
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "NTF00-002", responseErrorCode(t, rec))
}
