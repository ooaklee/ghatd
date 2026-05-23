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
	ListStreaks(ctx context.Context, req *ListStreaksRequest) ([]*Streak, error)
}

// Service represents the streaker service.
type Service struct {
	StreakRepository StreakRepository
}

type resolvedRecordStreakRequest struct {
	scope           StreakScope
	createdByUserId string
	occurredAt      time.Time
	periodType      StreakPeriodType
	periodKey       string
	periodTimezone  string
	statsReq        StreakStatsRequest
}

// NewService returns a new instance of the streaker service.
func NewService(streakRepository StreakRepository) *Service {
	return &Service{
		StreakRepository: streakRepository,
	}
}

// GetStreakByScopeAndPeriodOrCreate returns the streak entry for the resolved
// scope and period, creating it when it does not already exist.
func (s *Service) GetStreakByScopeAndPeriodOrCreate(ctx context.Context, req *RecordStreakRequest) (*RecordStreakResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("initiating-get-streak-by-scope-and-period-or-create-request", zap.Any("request", req))

	resolvedReq, existingRes, err := s.getStreakByScopeAndPeriodFromRecordRequest(ctx, req, log)
	if err != nil || existingRes != nil {
		return existingRes, err
	}

	createdRes, err := s.createResolvedStreak(ctx, req, resolvedReq, log)
	if err != nil {
		return nil, err
	}

	log.Debug("get-streak-by-scope-and-period-or-create-request-successful", zap.Any("request", req), zap.Any("created-streak", createdRes.Streak))
	return createdRes, nil
}

// RecordStreak records a completion and computes the streak count for its scope.
func (s *Service) RecordStreak(ctx context.Context, req *RecordStreakRequest) (*RecordStreakResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("initiating-record-streak-request", zap.Any("request", req))

	resolvedReq, existingRes, err := s.getStreakByScopeAndPeriodFromRecordRequest(ctx, req, log)
	if err != nil || existingRes != nil {
		return existingRes, err
	}

	createdRes, err := s.createResolvedStreak(ctx, req, resolvedReq, log)
	if err != nil {
		return nil, err
	}

	log.Debug("record-streak-request-successful", zap.Any("request", req), zap.Any("created-streak", createdRes.Streak))
	return createdRes, nil
}

// getStreakByScopeAndPeriodFromRecordRequest validates and resolves a record
// request, returning an existing streak response when the period was already
// recorded.
func (s *Service) getStreakByScopeAndPeriodFromRecordRequest(ctx context.Context, req *RecordStreakRequest, log *zap.Logger) (*resolvedRecordStreakRequest, *RecordStreakResponse, error) {
	if req == nil {
		return nil, nil, ErrStreakTypeIsRequired
	}

	scope := NormaliseScope(StreakScope{
		StreakType: normaliseStreakType(req.StreakType),
		OwnerId:    req.OwnerId,
		TargetType: normaliseStreakType(req.TargetType),
		TargetId:   req.TargetId,
	})

	if err := validateRequiredScope(scope); err != nil {
		return nil, nil, err
	}

	createdByUserId := strings.TrimSpace(req.CreatedByUserId)
	if createdByUserId == "" {
		return nil, nil, ErrCreatedByUserIdIsRequired
	}

	periodType, err := NormalisePeriodType(req.PeriodType)
	if err != nil {
		return nil, nil, err
	}

	occurredAt := time.Now().UTC()
	if strings.TrimSpace(req.OccurredAt) != "" {
		occurredAt, err = ParseStreakTime(req.OccurredAt)
		if err != nil {
			return nil, nil, err
		}
	}

	periodTimezone, _, err := NormalisePeriodTimezone(req.PeriodTimezone)
	if err != nil {
		return nil, nil, err
	}

	periodKey, err := BuildPeriodKeyForTimezone(occurredAt, periodType, req.PeriodKey, periodTimezone)
	if err != nil {
		return nil, nil, err
	}

	statsReq := StreakStatsRequest{
		StreakType: scope.StreakType,
		OwnerId:    scope.OwnerId,
		TargetType: scope.TargetType,
		TargetId:   scope.TargetId,
		PeriodType: periodType,
	}

	resolvedReq := &resolvedRecordStreakRequest{
		scope:           scope,
		createdByUserId: createdByUserId,
		occurredAt:      occurredAt,
		periodType:      periodType,
		periodKey:       periodKey,
		periodTimezone:  periodTimezone,
		statsReq:        statsReq,
	}

	existing, err := s.StreakRepository.GetStreakByScopeAndPeriod(ctx, &GetLatestStreakRequest{
		StreakStatsRequest: statsReq,
		PeriodKey:          periodKey,
	})
	if err == nil && existing != nil {
		return resolvedReq, &RecordStreakResponse{Streak: existing}, nil
	}
	if err != nil && !errors.Is(err, ErrResourceNotFound) {
		log.Error("failed-to-resolve-streak-error-checking-period", zap.Any("request", req), zap.Error(err))
		return nil, nil, err
	}

	return resolvedReq, nil, nil
}

// createResolvedStreak creates a streak from a previously validated and
// resolved record request, carrying forward consecutive count and previous
// streak metadata when available.
func (s *Service) createResolvedStreak(ctx context.Context, req *RecordStreakRequest, resolvedReq *resolvedRecordStreakRequest, log *zap.Logger) (*RecordStreakResponse, error) {
	var previous *Streak
	previous, err := s.StreakRepository.GetLatestStreak(ctx, &GetLatestStreakRequest{
		StreakStatsRequest: resolvedReq.statsReq,
	})
	if err != nil && !errors.Is(err, ErrResourceNotFound) {
		log.Error("failed-to-create-streak-error-getting-latest-streak", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	currentCount := 1
	if previous != nil && IsConsecutivePeriod(previous.PeriodKey, resolvedReq.periodKey, resolvedReq.periodType) {
		currentCount = previous.CurrentCount + 1
	}

	newStreak := &Streak{
		StreakName:      strings.TrimSpace(req.StreakName),
		StreakType:      resolvedReq.scope.StreakType,
		OwnerId:         resolvedReq.scope.OwnerId,
		TargetType:      resolvedReq.scope.TargetType,
		TargetId:        resolvedReq.scope.TargetId,
		PeriodType:      resolvedReq.periodType,
		PeriodKey:       resolvedReq.periodKey,
		PeriodTimezone:  resolvedReq.periodTimezone,
		OccurredAt:      FormatStreakTime(resolvedReq.occurredAt),
		CurrentCount:    currentCount,
		CreatedByUserId: resolvedReq.createdByUserId,
		Metadata:        req.Metadata,
	}

	if previous != nil {
		newStreak.Previous = previous.ToPreviousEntry()
	}

	createdStreak, err := s.StreakRepository.CreateStreak(ctx, newStreak)
	if err != nil {
		log.Error("failed-to-create-streak-error-creating-streak", zap.Any("request", req), zap.Any("new-streak", newStreak), zap.Error(err))
		return nil, err
	}

	return &RecordStreakResponse{Streak: createdStreak}, nil
}

// CreateRawStreak creates a precomputed streak entry.
func (s *Service) CreateRawStreak(ctx context.Context, req *CreateRawStreakRequest) (*RecordStreakResponse, error) {
	if req == nil || req.Streak == nil {
		return nil, ErrStreakTypeIsRequired
	}

	streak := req.Streak
	if streak.Id == "" {
		return nil, ErrIdIsRequired
	}
	if streak.NanoId == "" {
		return nil, ErrNanoIdIsRequired
	}
	if streak.CurrentCount <= 0 {
		return nil, ErrCurrentCountCannotBeZero
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
	if err != nil && errors.Is(err, ErrResourceNotFound) {
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
	if err != nil && errors.Is(err, ErrResourceNotFound) {
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

// ListStreaks lists streak entries matching the provided filters.
func (s *Service) ListStreaks(ctx context.Context, req *ListStreaksRequest) (*ListStreaksResponse, error) {
	listReq, err := normaliseListStreaksRequest(req)
	if err != nil {
		return nil, err
	}

	streaks, err := s.StreakRepository.ListStreaks(ctx, listReq)
	if err != nil && errors.Is(err, ErrResourceNotFound) {
		return &ListStreaksResponse{Streaks: []*Streak{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if streaks == nil {
		streaks = []*Streak{}
	}

	return &ListStreaksResponse{Streaks: streaks}, nil
}

// normaliseStatsRequest validates and standardises a stats request before it is
// passed to the repository layer.
func normaliseStatsRequest(req *StreakStatsRequest) (*StreakStatsRequest, error) {
	if req == nil {
		return nil, ErrStreakTypeIsRequired
	}

	scope := NormaliseScope(StreakScope{
		StreakType: normaliseStreakType(req.StreakType),
		OwnerId:    req.OwnerId,
		TargetType: normaliseStreakType(req.TargetType),
		TargetId:   req.TargetId,
	})

	if scope.StreakType == "" {
		return nil, ErrStreakTypeIsRequired
	}

	if scope.OwnerId == "" {
		return nil, ErrOwnerIdIsRequired
	}

	if req.PeriodType == "" {
		return nil, ErrPeriodTypeIsRequired
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

func normaliseListStreaksRequest(req *ListStreaksRequest) (*ListStreaksRequest, error) {
	if req == nil {
		return nil, ErrOwnerIdIsRequired
	}

	scope := NormaliseScope(StreakScope{
		StreakType: normaliseStreakType(req.StreakType),
		OwnerId:    req.OwnerId,
		TargetType: normaliseStreakType(req.TargetType),
		TargetId:   req.TargetId,
	})
	if scope.OwnerId == "" {
		return nil, ErrOwnerIdIsRequired
	}
	if req.PeriodType == "" {
		return nil, ErrPeriodTypeIsRequired
	}

	periodType, err := NormalisePeriodType(req.PeriodType)
	if err != nil {
		return nil, err
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	perPage := req.PerPage
	if perPage <= 0 {
		perPage = 100
	}
	if perPage > 200 {
		perPage = 200
	}

	return &ListStreaksRequest{
		StreakStatsRequest: StreakStatsRequest{
			StreakType: scope.StreakType,
			OwnerId:    scope.OwnerId,
			TargetType: scope.TargetType,
			TargetId:   scope.TargetId,
			PeriodType: periodType,
		},
		PeriodKey:      strings.TrimSpace(req.PeriodKey),
		PeriodKeyFrom:  strings.TrimSpace(req.PeriodKeyFrom),
		PeriodKeyTo:    strings.TrimSpace(req.PeriodKeyTo),
		OccurredAtFrom: strings.TrimSpace(req.OccurredAtFrom),
		OccurredAtTo:   strings.TrimSpace(req.OccurredAtTo),
		Page:           page,
		PerPage:        perPage,
		Sort:           strings.TrimSpace(req.Sort),
	}, nil
}

// validateRequiredScope returns the first missing required field for a fully
// qualified streak scope.
func validateRequiredScope(scope StreakScope) error {
	if scope.StreakType == "" {
		return ErrStreakTypeIsRequired
	}
	if scope.OwnerId == "" {
		return ErrOwnerIdIsRequired
	}
	if scope.TargetType == "" {
		return ErrTargetTypeIsRequired
	}
	if scope.TargetId == "" {
		return ErrTargetIdIsRequired
	}

	return nil
}

// normaliseStreakType trims and converts a streak type value into the canonical
// kebab-case representation.
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
