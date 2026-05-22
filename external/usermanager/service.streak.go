package usermanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/streaker"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// RecordStreak records a streak for the currently authenticated user.
func (s *Service) RecordStreak(ctx context.Context, r *RecordStreakRequest) (*RecordStreakResponse, error) {
	if err := s.ensureStreakService(); err != nil {
		return nil, err
	}
	if r == nil || r.RecordStreakRequest == nil {
		return nil, ErrRequestFailedValidation
	}
	if _, _, err := s.validateStreakRequester(ctx, r.UserID); err != nil {
		return nil, err
	}

	baseRequest := *r.RecordStreakRequest
	baseRequest.OwnerId = strings.TrimSpace(r.UserID)
	baseRequest.CreatedByUserId = strings.TrimSpace(r.UserID)

	response, err := s.StreakService.RecordStreak(ctx, &baseRequest)
	if err != nil {
		return nil, err
	}

	return &RecordStreakResponse{RecordStreakResponse: response}, nil
}

// ListStreaks returns streak entries for the authenticated user.
func (s *Service) ListStreaks(ctx context.Context, r *ListStreaksRequest) (*ListStreaksResponse, error) {
	if err := s.ensureStreakService(); err != nil {
		return nil, err
	}
	if r == nil || r.ListStreaksRequest == nil {
		return nil, ErrRequestFailedValidation
	}

	requestingUserID := strings.TrimSpace(r.UserID)
	_, isAdmin, err := s.validateStreakRequester(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}

	baseRequest := *r.ListStreaksRequest
	baseRequest.OwnerId = streakOwnerScopeForRequester(requestingUserID, isAdmin, r.FilterUserID)
	if baseRequest.PeriodType == "" {
		baseRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	response, err := s.StreakService.ListStreaks(ctx, &baseRequest)
	if err != nil {
		return nil, err
	}

	return &ListStreaksResponse{ListStreaksResponse: response}, nil
}

// GetCurrentStreak returns the current streak count for the authenticated user.
func (s *Service) GetCurrentStreak(ctx context.Context, r *GetCurrentStreakRequest) (*GetCurrentStreakResponse, error) {
	if err := s.ensureStreakService(); err != nil {
		return nil, err
	}
	if r == nil || r.GetCurrentCountRequest == nil {
		return nil, ErrRequestFailedValidation
	}

	requestingUserID := strings.TrimSpace(r.UserID)
	_, isAdmin, err := s.validateStreakRequester(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}

	baseRequest := *r.GetCurrentCountRequest
	baseRequest.OwnerId = streakOwnerScopeForRequester(requestingUserID, isAdmin, r.FilterUserID)
	if baseRequest.PeriodType == "" {
		baseRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	response, err := s.StreakService.GetCurrentCount(ctx, &baseRequest)
	if err != nil {
		return nil, err
	}

	return &GetCurrentStreakResponse{GetCurrentCountResponse: response}, nil
}

// GetLongestStreak returns the personal best streak for the authenticated user.
func (s *Service) GetLongestStreak(ctx context.Context, r *GetLongestStreakRequest) (*GetLongestStreakResponse, error) {
	if err := s.ensureStreakService(); err != nil {
		return nil, err
	}
	if r == nil || r.GetLongestStreakRequest == nil {
		return nil, ErrRequestFailedValidation
	}

	requestingUserID := strings.TrimSpace(r.UserID)
	_, isAdmin, err := s.validateStreakRequester(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}

	baseRequest := *r.GetLongestStreakRequest
	baseRequest.OwnerId = streakOwnerScopeForRequester(requestingUserID, isAdmin, r.FilterUserID)
	if baseRequest.PeriodType == "" {
		baseRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	response, err := s.StreakService.GetLongestStreak(ctx, &baseRequest)
	if err != nil {
		return nil, err
	}

	return &GetLongestStreakResponse{GetLongestStreakResponse: response}, nil
}

// GetNumberOfStreaks returns the number of streak entries matching the filters.
func (s *Service) GetNumberOfStreaks(ctx context.Context, r *GetNumberOfStreaksRequest) (*GetNumberOfStreaksResponse, error) {
	if err := s.ensureStreakService(); err != nil {
		return nil, err
	}
	if r == nil || r.GetNumberOfStreaksRequest == nil {
		return nil, ErrRequestFailedValidation
	}

	requestingUserID := strings.TrimSpace(r.UserID)
	_, isAdmin, err := s.validateStreakRequester(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}

	baseRequest := *r.GetNumberOfStreaksRequest
	baseRequest.OwnerId = streakOwnerScopeForRequester(requestingUserID, isAdmin, r.FilterUserID)
	if baseRequest.PeriodType == "" {
		baseRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	response, err := s.StreakService.GetNumberOfStreaks(ctx, &baseRequest)
	if err != nil {
		return nil, err
	}

	return &GetNumberOfStreaksResponse{GetNumberOfStreaksResponse: response}, nil
}

func (s *Service) ensureStreakService() error {
	if s.StreakService == nil {
		return ErrStreakServiceNotEnabled
	}
	return nil
}

func (s *Service) validateStreakRequester(ctx context.Context, userID string) (*userv2.UniversalUser, bool, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	requestingUserID := strings.TrimSpace(userID)
	if requestingUserID == "" {
		log.Warn("streak-request-with-empty-requesting-user-id")
		return nil, false, ErrUnableToIdentifyUser
	}
	if s.UserService == nil {
		log.Warn("streak-request-without-user-service", zap.String("user-id", requestingUserID))
		return nil, false, ErrUserManagerError
	}

	requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: requestingUserID})
	if err != nil {
		log.Warn("unable-to-resolve-streak-requester", zap.String("user-id", requestingUserID), zap.Error(err))
		return nil, false, err
	}
	if requestingUser == nil || requestingUser.User == nil {
		log.Warn("streak-requester-not-found", zap.String("user-id", requestingUserID))
		return nil, false, ErrUserNotFound
	}

	return requestingUser.User, requestingUser.User.IsAdmin(), nil
}

func streakOwnerScopeForRequester(requestingUserID string, isAdmin bool, filterUserID string) string {
	if isAdmin && strings.TrimSpace(filterUserID) != "" {
		return strings.TrimSpace(filterUserID)
	}
	return strings.TrimSpace(requestingUserID)
}
