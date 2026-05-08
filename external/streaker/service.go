package streaker

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// StreakRepository is the expected repository surface for streaker.
type StreakRepository interface {
	CreateStreak(ctx context.Context, streak *Streak) (*Streak, error)
	CreateRawStreak(ctx context.Context, streak *Streak) (*Streak, error)
	GetStreakByScopeAndPeriod(ctx context.Context, req *GetLatestStreakRequest) (*Streak, error)
	GetLatestStreak(ctx context.Context, req *GetLatestStreakRequest) (*Streak, error)
	GetLongestStreak(ctx context.Context, req *GetLongestStreakRequest) (*Streak, error)
	GetTotalStreaks(ctx context.Context, req *GetNumberOfStreaksRequest) (int64, error)
}

// Service represents the streaker service.
type Service struct {
	StreakRepository StreakRepository
}

// NewService returns a new instance of the streaker service.
func NewService(streakRepository StreakRepository) *Service {
	return &Service{
		StreakRepository: streakRepository,
	}
}

// RecordStreak records a completion and computes the streak count for its scope.
func (s *Service) RecordStreak(ctx context.Context, req *RecordStreakRequest) (*RecordStreakResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("initiating-record-streak-request", zap.Any("request", req))

	if req == nil {
		return nil, errors.New(ErrKeyStreakTypeIsRequired)
	}

	scope := NormaliseScope(StreakScope{
		StreakType: normaliseStreakType(req.StreakType),
		OwnerId:    req.OwnerId,
		TargetType: normaliseStreakType(req.TargetType),
		TargetId:   req.TargetId,
	})

	if err := validateRequiredScope(scope); err != nil {
		return nil, err
	}

	createdByUserId := strings.TrimSpace(req.CreatedByUserId)
	if createdByUserId == "" {
		return nil, errors.New(ErrKeyCreatedByUserIdIsRequired)
	}

	periodType, err := NormalisePeriodType(req.PeriodType)
	if err != nil {
		return nil, err
	}

	occurredAt := time.Now().UTC()
	if strings.TrimSpace(req.OccurredAt) != "" {
		occurredAt, err = ParseStreakTime(req.OccurredAt)
		if err != nil {
			return nil, err
		}
	}

	periodKey, err := BuildPeriodKey(occurredAt, periodType, req.PeriodKey)
	if err != nil {
		return nil, err
	}

	statsReq := StreakStatsRequest{
		StreakType: scope.StreakType,
		OwnerId:    scope.OwnerId,
		TargetType: scope.TargetType,
		TargetId:   scope.TargetId,
		PeriodType: periodType,
	}

	existing, err := s.StreakRepository.GetStreakByScopeAndPeriod(ctx, &GetLatestStreakRequest{
		StreakStatsRequest: statsReq,
		PeriodKey:          periodKey,
	})
	if err == nil && existing != nil {
		return &RecordStreakResponse{Streak: existing}, nil
	}
	if err != nil && err.Error() != ErrKeyResourceNotFound {
		log.Error("failed-to-record-streak-error-checking-period", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	var previous *Streak
	previous, err = s.StreakRepository.GetLatestStreak(ctx, &GetLatestStreakRequest{
		StreakStatsRequest: statsReq,
	})
	if err != nil && err.Error() != ErrKeyResourceNotFound {
		log.Error("failed-to-record-streak-error-getting-latest-streak", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	currentCount := 1
	if previous != nil && IsConsecutivePeriod(previous.PeriodKey, periodKey, periodType) {
		currentCount = previous.CurrentCount + 1
	}

	newStreak := &Streak{
		StreakName:      strings.TrimSpace(req.StreakName),
		StreakType:      scope.StreakType,
		OwnerId:         scope.OwnerId,
		TargetType:      scope.TargetType,
		TargetId:        scope.TargetId,
		PeriodType:      periodType,
		PeriodKey:       periodKey,
		OccurredAt:      FormatStreakTime(occurredAt),
		CurrentCount:    currentCount,
		CreatedByUserId: createdByUserId,
		Metadata:        req.Metadata,
	}

	if previous != nil {
		newStreak.Previous = previous.ToPreviousEntry()
	}

	createdStreak, err := s.StreakRepository.CreateStreak(ctx, newStreak)
	if err != nil {
		log.Error("failed-to-record-streak-error-creating-streak", zap.Any("request", req), zap.Any("new-streak", newStreak), zap.Error(err))
		return nil, err
	}

	log.Debug("record-streak-request-successful", zap.Any("request", req), zap.Any("created-streak", createdStreak))
	return &RecordStreakResponse{Streak: createdStreak}, nil
}

// CreateRawStreak creates a precomputed streak entry.
func (s *Service) CreateRawStreak(ctx context.Context, req *CreateRawStreakRequest) (*RecordStreakResponse, error) {
	if req == nil || req.Streak == nil {
		return nil, errors.New(ErrKeyStreakTypeIsRequired)
	}

	streak := req.Streak
	if streak.Id == "" {
		return nil, errors.New(ErrKeyIdIsRequired)
	}
	if streak.NanoId == "" {
		return nil, errors.New(ErrKeyNanoIdIsRequired)
	}
	if streak.CurrentCount <= 0 {
		return nil, errors.New(ErrKeyCurrentCountCannotBeZero)
	}
	if err := validateRequiredScope(NormaliseScope(streak.ToScope())); err != nil {
		return nil, err
	}

	createdStreak, err := s.StreakRepository.CreateRawStreak(ctx, streak)
	if err != nil {
		return nil, err
	}

	return &RecordStreakResponse{Streak: createdStreak}, nil
}

// GetLongestStreak gets the longest streak matching the request filters.
func (s *Service) GetLongestStreak(ctx context.Context, req *GetLongestStreakRequest) (*GetLongestStreakResponse, error) {
	var sourceReq *StreakStatsRequest
	if req != nil {
		sourceReq = &req.StreakStatsRequest
	}

	statsReq, err := normaliseStatsRequest(sourceReq)
	if err != nil {
		return nil, err
	}

	streak, err := s.StreakRepository.GetLongestStreak(ctx, &GetLongestStreakRequest{StreakStatsRequest: *statsReq})
	if err != nil && err.Error() == ErrKeyResourceNotFound {
		return &GetLongestStreakResponse{LongestCount: 0}, nil
	}
	if err != nil {
		return nil, err
	}

	return &GetLongestStreakResponse{
		Streak:       streak,
		LongestCount: streak.CurrentCount,
	}, nil
}

// GetLongestStreakByStreakTypeAndUserID gets the longest streak for a user and type.
func (s *Service) GetLongestStreakByStreakTypeAndUserID(ctx context.Context, req *GetLongestStreakRequest) (*GetLongestStreakResponse, error) {
	return s.GetLongestStreak(ctx, req)
}

// GetCurrentCount gets the latest current count matching the request filters.
func (s *Service) GetCurrentCount(ctx context.Context, req *GetCurrentCountRequest) (*GetCurrentCountResponse, error) {
	var sourceReq *StreakStatsRequest
	if req != nil {
		sourceReq = &req.StreakStatsRequest
	}

	statsReq, err := normaliseStatsRequest(sourceReq)
	if err != nil {
		return nil, err
	}

	streak, err := s.StreakRepository.GetLatestStreak(ctx, &GetLatestStreakRequest{StreakStatsRequest: *statsReq})
	if err != nil && err.Error() == ErrKeyResourceNotFound {
		return &GetCurrentCountResponse{CurrentCount: 0}, nil
	}
	if err != nil {
		return nil, err
	}

	return &GetCurrentCountResponse{
		Streak:       streak,
		CurrentCount: streak.CurrentCount,
	}, nil
}

// GetCurrentCountByStreakTypeAndUserID gets the current count for a user and type.
func (s *Service) GetCurrentCountByStreakTypeAndUserID(ctx context.Context, req *GetCurrentCountRequest) (*GetCurrentCountResponse, error) {
	return s.GetCurrentCount(ctx, req)
}

// GetNumberOfStreaks gets the number of streak entries matching the request filters.
func (s *Service) GetNumberOfStreaks(ctx context.Context, req *GetNumberOfStreaksRequest) (*GetNumberOfStreaksResponse, error) {
	var sourceReq *StreakStatsRequest
	if req != nil {
		sourceReq = &req.StreakStatsRequest
	}

	statsReq, err := normaliseStatsRequest(sourceReq)
	if err != nil {
		return nil, err
	}

	total, err := s.StreakRepository.GetTotalStreaks(ctx, &GetNumberOfStreaksRequest{StreakStatsRequest: *statsReq})
	if err != nil {
		return nil, err
	}

	return &GetNumberOfStreaksResponse{Total: total}, nil
}

// GetNumberOfStreaksByStreakTypeAndUserID gets the number of streak entries for a user and type.
func (s *Service) GetNumberOfStreaksByStreakTypeAndUserID(ctx context.Context, req *GetNumberOfStreaksRequest) (*GetNumberOfStreaksResponse, error) {
	return s.GetNumberOfStreaks(ctx, req)
}

func normaliseStatsRequest(req *StreakStatsRequest) (*StreakStatsRequest, error) {
	if req == nil {
		return nil, errors.New(ErrKeyStreakTypeIsRequired)
	}

	scope := NormaliseScope(StreakScope{
		StreakType: normaliseStreakType(req.StreakType),
		OwnerId:    req.OwnerId,
		TargetType: normaliseStreakType(req.TargetType),
		TargetId:   req.TargetId,
	})

	if scope.StreakType == "" {
		return nil, errors.New(ErrKeyStreakTypeIsRequired)
	}

	if scope.OwnerId == "" {
		return nil, errors.New(ErrKeyOwnerIdIsRequired)
	}

	if req.PeriodType == "" {
		return nil, errors.New(ErrKeyPeriodTypeIsRequired)
	}

	periodType := req.PeriodType
	if periodType != "" {
		var err error
		periodType, err = NormalisePeriodType(periodType)
		if err != nil {
			return nil, err
		}
	}

	return &StreakStatsRequest{
		StreakType: scope.StreakType,
		OwnerId:    scope.OwnerId,
		TargetType: scope.TargetType,
		TargetId:   scope.TargetId,
		PeriodType: periodType,
	}, nil
}

func validateRequiredScope(scope StreakScope) error {
	if scope.StreakType == "" {
		return errors.New(ErrKeyStreakTypeIsRequired)
	}
	if scope.OwnerId == "" {
		return errors.New(ErrKeyOwnerIdIsRequired)
	}
	if scope.TargetType == "" {
		return errors.New(ErrKeyTargetTypeIsRequired)
	}
	if scope.TargetId == "" {
		return errors.New(ErrKeyTargetIdIsRequired)
	}

	return nil
}

func normaliseStreakType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	normalised, err := toolbox.StringConvertToKebabCase(value)
	if err != nil {
		return toolbox.StringStandardisedToLower(value)
	}

	return normalised
}
