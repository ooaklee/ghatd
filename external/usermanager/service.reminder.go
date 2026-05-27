package usermanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/reminder"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// CreateReminder creates a reminder for the currently authenticated user.
func (s *Service) CreateReminder(ctx context.Context, r *CreateReminderRequest) (*CreateReminderResponse, error) {
	if err := s.ensureReminderService(); err != nil {
		return nil, err
	}

	response, err := s.ReminderService.CreateReminder(ctx, r.CreateReminderRequest)
	if err != nil {
		return nil, err
	}

	return &CreateReminderResponse{CreateReminderResponse: response}, nil
}

// GetReminderByID returns one reminder owned by the currently authenticated user.
func (s *Service) GetReminderByID(ctx context.Context, r *GetReminderByIDRequest) (*GetReminderByIDResponse, error) {
	if err := s.ensureReminderService(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRequestFailedValidation
	}

	_, isAdmin, err := s.validateReminderRequester(ctx, r.UserID)
	if err != nil {
		return nil, err
	}

	effectiveUserID := strings.TrimSpace(r.UserID)
	if isAdmin {
		effectiveUserID = ""
	}

	response, err := s.ReminderService.GetReminderByID(ctx, &reminder.GetReminderByIDRequest{
		UserID: effectiveUserID,
		Id:     r.Id,
	})
	if err != nil {
		return nil, err
	}

	return &GetReminderByIDResponse{GetReminderByIDResponse: response}, nil
}

// ListReminders returns reminders for the authenticated user, or across users when the requester is an admin.
func (s *Service) ListReminders(ctx context.Context, r *ListRemindersRequest) (*ListRemindersResponse, error) {
	if err := s.ensureReminderService(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRequestFailedValidation
	}

	requestingUserID := strings.TrimSpace(r.UserID)
	_, isAdmin, err := s.validateReminderRequester(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}
	effectiveUserID, _ := reminderUserScopeForRequester(requestingUserID, isAdmin, r.FilterUserID, nil)

	response, err := s.ReminderService.ListReminders(ctx, &reminder.ListRemindersRequest{
		UserID:     effectiveUserID,
		Status:     r.Status,
		TargetType: r.TargetType,
		TargetId:   r.TargetId,
		Page:       r.Page,
		PerPage:    r.PerPage,
	})
	if err != nil {
		return nil, err
	}

	return &ListRemindersResponse{ListRemindersResponse: response}, nil
}

// UpdateReminderByID updates one reminder owned by the currently authenticated user.
func (s *Service) UpdateReminderByID(ctx context.Context, r *UpdateReminderByIDRequest) (*UpdateReminderByIDResponse, error) {
	if err := s.ensureReminderService(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRequestFailedValidation
	}
	if _, _, err := s.validateReminderRequester(ctx, r.UserID); err != nil {
		return nil, err
	}

	response, err := s.ReminderService.UpdateReminderByID(ctx, r.UpdateReminderByIDRequest)
	if err != nil {
		return nil, err
	}

	return &UpdateReminderByIDResponse{UpdateReminderByIDResponse: response}, nil
}

// DeleteReminderByID deletes one reminder owned by the currently authenticated user.
func (s *Service) DeleteReminderByID(ctx context.Context, r *DeleteReminderByIDRequest) error {
	if err := s.ensureReminderService(); err != nil {
		return err
	}
	if r == nil {
		return ErrRequestFailedValidation
	}
	if _, _, err := s.validateReminderRequester(ctx, r.UserID); err != nil {
		return err
	}

	return s.ReminderService.DeleteReminderByID(ctx, &reminder.DeleteReminderByIDRequest{
		UserID: r.UserID,
		Id:     r.Id,
	})
}

// DisableReminderByID disables one reminder owned by the currently authenticated user.
func (s *Service) DisableReminderByID(ctx context.Context, r *DisableReminderByIDRequest) (*UpdateReminderByIDResponse, error) {
	if err := s.ensureReminderService(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRequestFailedValidation
	}
	if _, _, err := s.validateReminderRequester(ctx, r.UserID); err != nil {
		return nil, err
	}

	response, err := s.ReminderService.DisableReminderByID(ctx, &reminder.DisableReminderByIDRequest{
		UserID: r.UserID,
		Id:     r.Id,
	})
	if err != nil {
		return nil, err
	}

	return &UpdateReminderByIDResponse{UpdateReminderByIDResponse: response}, nil
}

// GetReminderStats returns aggregate reminder statistics for admin/service views.
func (s *Service) GetReminderStats(ctx context.Context, r *GetReminderStatsRequest) (*GetReminderStatsResponse, error) {
	if err := s.ensureReminderService(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRequestFailedValidation
	}
	requestingUserID := strings.TrimSpace(r.UserID)
	_, isAdmin, err := s.validateReminderRequester(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}
	filterUserID, filterUserIDs := reminderUserScopeForRequester(requestingUserID, isAdmin, r.FilterUserID, r.FilterUserIDs)

	response, err := s.ReminderService.GetReminderStats(ctx, &reminder.GetReminderStatsRequest{
		UserID:  filterUserID,
		UserIDs: filterUserIDs,
	})
	if err != nil {
		return nil, err
	}

	return &GetReminderStatsResponse{GetReminderStatsResponse: response}, nil
}

// GetDueReminders returns reminders ready for scheduler dispatch.
func (s *Service) GetDueReminders(ctx context.Context, r *GetDueRemindersRequest) (*GetDueRemindersResponse, error) {
	if err := s.ensureReminderService(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRequestFailedValidation
	}
	requestingUserID := strings.TrimSpace(r.UserID)
	_, isAdmin, err := s.validateReminderRequester(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}
	filterUserID, filterUserIDs := reminderUserScopeForRequester(requestingUserID, isAdmin, r.FilterUserID, r.FilterUserIDs)

	response, err := s.ReminderService.GetDueReminders(ctx, &reminder.GetDueRemindersRequest{
		DueBefore: r.DueBefore,
		UserID:    filterUserID,
		UserIDs:   filterUserIDs,
		Limit:     int64(r.Limit),
	})
	if err != nil {
		return nil, err
	}

	return &GetDueRemindersResponse{GetDueRemindersResponse: response}, nil
}

// ensureReminderService verifies that reminder operations are enabled for the service.
func (s *Service) ensureReminderService() error {
	if s.ReminderService == nil {
		return ErrReminderServiceNotEnabled
	}
	return nil
}

// validateReminderRequester resolves the requesting user and reports whether the user is an admin.
func (s *Service) validateReminderRequester(ctx context.Context, userID string) (*userv2.UniversalUser, bool, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")
	requestingUserID := strings.TrimSpace(userID)
	if requestingUserID == "" {
		logger.Warn("reminder-request-with-empty-requesting-user-id")
		return nil, false, ErrUnableToIdentifyUser
	}
	if s.UserService == nil {
		logger.Warn("reminder-request-without-user-service", zap.String("user-id", requestingUserID))
		return nil, false, ErrUserManagerError
	}

	requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: requestingUserID})
	if err != nil {
		logger.Warn("unable-to-resolve-reminder-requester", zap.String("user-id", requestingUserID), zap.Error(err))
		return nil, false, err
	}
	if requestingUser == nil || requestingUser.User == nil {
		logger.Warn("reminder-requester-not-found", zap.String("user-id", requestingUserID))
		return nil, false, ErrUserNotFound
	}

	return requestingUser.User, requestingUser.User.IsAdmin(), nil
}

// normaliseUMSReminderUserIDs trims, splits, deduplicates, and preserves the order of reminder user filters.
func normaliseUMSReminderUserIDs(userID string, userIDs []string) []string {
	seen := map[string]struct{}{}
	normalised := []string{}
	add := func(value string) {
		for _, item := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			normalised = append(normalised, trimmed)
		}
	}

	add(userID)
	for _, id := range userIDs {
		add(id)
	}

	return normalised
}

// reminderUserScopeForRequester returns the reminder user filter allowed for the requester.
func reminderUserScopeForRequester(requestingUserID string, isAdmin bool, filterUserID string, filterUserIDs []string) (string, []string) {
	if !isAdmin {
		return requestingUserID, nil
	}

	userIDs := normaliseUMSReminderUserIDs(filterUserID, filterUserIDs)
	if len(userIDs) == 1 {
		return userIDs[0], nil
	}
	if len(userIDs) > 1 {
		return "", userIDs
	}
	return "", nil
}
