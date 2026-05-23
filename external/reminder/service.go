package reminder

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// ReminderRepository describes the persistence operations needed by the reminder service.
type ReminderRepository interface {
	CreateReminder(ctx context.Context, reminder *Reminder) (*Reminder, error)
	GetReminderByID(ctx context.Context, id string) (*Reminder, error)
	ListReminders(ctx context.Context, userID string, status string, targetType string, targetId string, page, perPage int) ([]*Reminder, error)
	GetRemindersForTargetTypeByUserID(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*Reminder, error)
	GetActiveRemindersForTargetTypeByUserID(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*Reminder, error)
	UpdateReminderByID(ctx context.Context, reminder *Reminder) (*Reminder, error)
	PatchReminder(ctx context.Context, id string, update map[string]interface{}) error
	DeleteReminderByID(ctx context.Context, id string) error
	CountReminders(ctx context.Context, filter *ReminderFilter) (int64, error)
	GetDueReminders(ctx context.Context, filter *ReminderFilter, limit int64) ([]*Reminder, error)
	CreateReminderExecution(ctx context.Context, execution *ReminderExecution) (*ReminderExecution, error)
	ListReminderExecutions(ctx context.Context, filter *ReminderExecutionFilter, page, perPage int) ([]*ReminderExecution, error)
}

// Service coordinates reminder validation, ownership checks, and persistence.
type Service struct {
	ReminderRepository ReminderRepository
}

// NewService returns a reminder service backed by the provided repository.
func NewService(reminderRepository ReminderRepository) *Service {
	return &Service{
		ReminderRepository: reminderRepository,
	}
}

// CreateReminder validates and creates a reminder declaration for a user.
func (s *Service) CreateReminder(ctx context.Context, req *CreateReminderRequest) (*CreateReminderResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("initiating-create-reminder-request", zap.Any("request", req))

	if req == nil {
		return nil, ErrUserIDIsRequired
	}

	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return nil, ErrUserIDIsRequired
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, ErrTitleIsRequired
	}

	targetTime := strings.TrimSpace(req.TargetTime)
	if targetTime == "" {
		return nil, ErrTargetTimeIsRequired
	}

	timezone, _, err := NormaliseReminderTimezone(req.Timezone)
	if err != nil {
		return nil, err
	}

	nextDueAt, err := BuildNextDueAt(targetTime, timezone, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	status, err := ValidateAndNormaliseStatus(req.Status)
	if err != nil {
		return nil, err
	}

	now := toolbox.TimeNowUTC()

	reminder := &Reminder{
		UserID:          userID,
		TargetType:      strings.TrimSpace(req.TargetType),
		TargetId:        strings.TrimSpace(req.TargetId),
		Title:           title,
		Description:     strings.TrimSpace(req.Description),
		TargetTime:      targetTime,
		Timezone:        timezone,
		NextDueAt:       nextDueAt,
		Status:          status,
		TaskData:        req.TaskData,
		CreatedByUserID: userID,
		CreatedAt:       now,
	}

	createdReminder, err := s.ReminderRepository.CreateReminder(ctx, reminder)
	if err != nil {
		log.Error("failed-to-create-reminder-error", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	log.Debug("create-reminder-request-successful", zap.Any("reminder-id", createdReminder.Id))
	return &CreateReminderResponse{Reminder: createdReminder}, nil
}

// GetReminderByID retrieves one reminder declaration and enforces optional user ownership.
func (s *Service) GetReminderByID(ctx context.Context, req *GetReminderByIDRequest) (*GetReminderByIDResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil || strings.TrimSpace(req.Id) == "" {
		return nil, ErrIdIsRequired
	}

	reminder, err := s.ReminderRepository.GetReminderByID(ctx, strings.TrimSpace(req.Id))
	if err != nil {
		if errors.Is(err, ErrResourceNotFound) {
			return nil, ErrResourceNotFound
		}
		log.Error("failed-to-get-reminder-error", zap.String("reminder-id", req.Id), zap.Error(err))
		return nil, err
	}
	if strings.TrimSpace(req.UserID) != "" && reminder.UserID != strings.TrimSpace(req.UserID) {
		return nil, ErrNotAuthorized
	}

	return &GetReminderByIDResponse{Reminder: reminder}, nil
}

// ListReminders returns reminder declarations matching the provided filters.
func (s *Service) ListReminders(ctx context.Context, req *ListRemindersRequest) (*ListRemindersResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil {
		req = &ListRemindersRequest{}
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	perPage := req.PerPage
	if perPage <= 0 {
		perPage = 25
	}

	status := strings.TrimSpace(req.Status)
	if status != "" {
		_, err := ValidateAndNormaliseStatus(ReminderStatus(status))
		if err != nil {
			return nil, ErrInvalidStatus
		}
	}

	reminders, err := s.ReminderRepository.ListReminders(
		ctx,
		strings.TrimSpace(req.UserID),
		status,
		strings.TrimSpace(req.TargetType),
		strings.TrimSpace(req.TargetId),
		page,
		perPage,
	)
	if err != nil {
		log.Error("failed-to-list-reminders-error", zap.String("user-id", req.UserID), zap.Error(err))
		return nil, err
	}

	return &ListRemindersResponse{
		Reminders: reminders,
		Meta: map[string]interface{}{
			"page":     page,
			"per_page": perPage,
		},
	}, nil
}

// UpdateReminderByID applies user-owned changes to one reminder declaration.
func (s *Service) UpdateReminderByID(ctx context.Context, req *UpdateReminderByIDRequest) (*UpdateReminderByIDResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil || strings.TrimSpace(req.Id) == "" {
		return nil, ErrIdIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrUserIDIsRequired
	}

	existing, err := s.ReminderRepository.GetReminderByID(ctx, strings.TrimSpace(req.Id))
	if err != nil {
		if errors.Is(err, ErrResourceNotFound) {
			return nil, ErrResourceNotFound
		}
		log.Error("failed-to-update-reminder-error-fetching", zap.String("reminder-id", req.Id), zap.Error(err))
		return nil, err
	}

	if existing.UserID != req.UserID {
		return nil, ErrNotAuthorized
	}

	if existing.Status == ReminderStatusDeleted {
		return nil, ErrInvalidReminderStatus
	}

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return nil, ErrTitleIsRequired
		}
		existing.Title = title
	}
	if req.TargetType != nil {
		existing.TargetType = strings.TrimSpace(*req.TargetType)
	}
	if req.TargetId != nil {
		existing.TargetId = strings.TrimSpace(*req.TargetId)
	}
	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}
	scheduleChanged := false
	if req.TargetTime != nil {
		existing.TargetTime = strings.TrimSpace(*req.TargetTime)
		scheduleChanged = true
	}
	if req.Timezone != nil {
		existing.Timezone = strings.TrimSpace(*req.Timezone)
		scheduleChanged = true
	}
	if req.Status != nil {
		newStatus, err := ValidateAndNormaliseStatus(*req.Status)
		if err != nil {
			return nil, err
		}
		if existing.Status == ReminderStatusCompleted && newStatus != ReminderStatusActive {
			return nil, ErrInvalidReminderStatus
		}
		existing.Status = newStatus
	}
	if req.TaskData != nil {
		existing.TaskData = req.TaskData
	}
	if scheduleChanged || strings.TrimSpace(existing.NextDueAt) == "" {
		timezone, _, err := NormaliseReminderTimezone(existing.Timezone)
		if err != nil {
			return nil, err
		}
		nextDueAt, err := BuildNextDueAt(existing.TargetTime, timezone, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		existing.Timezone = timezone
		existing.NextDueAt = nextDueAt
	}

	existing.UpdatedByUserID = req.UserID

	updated, err := s.ReminderRepository.UpdateReminderByID(ctx, existing)
	if err != nil {
		log.Error("failed-to-update-reminder-error", zap.String("reminder-id", req.Id), zap.Error(err))
		return nil, err
	}

	return &UpdateReminderByIDResponse{Reminder: updated}, nil
}

// DeleteReminderByID removes one reminder declaration after verifying ownership.
func (s *Service) DeleteReminderByID(ctx context.Context, req *DeleteReminderByIDRequest) error {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil || strings.TrimSpace(req.Id) == "" {
		return ErrIdIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return ErrUserIDIsRequired
	}

	existing, err := s.ReminderRepository.GetReminderByID(ctx, strings.TrimSpace(req.Id))
	if err != nil {
		if errors.Is(err, ErrResourceNotFound) {
			return ErrResourceNotFound
		}
		log.Error("failed-to-delete-reminder-error-fetching", zap.String("reminder-id", req.Id), zap.Error(err))
		return err
	}

	if existing.UserID != req.UserID {
		return ErrNotAuthorized
	}

	err = s.ReminderRepository.DeleteReminderByID(ctx, req.Id)
	if err != nil {
		log.Error("failed-to-delete-reminder-error", zap.String("reminder-id", req.Id), zap.Error(err))
		return err
	}

	return nil
}

// DisableReminderByID marks a user-owned reminder as disabled.
func (s *Service) DisableReminderByID(ctx context.Context, req *DisableReminderByIDRequest) (*UpdateReminderByIDResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil || strings.TrimSpace(req.Id) == "" {
		return nil, ErrIdIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrUserIDIsRequired
	}

	existing, err := s.ReminderRepository.GetReminderByID(ctx, strings.TrimSpace(req.Id))
	if err != nil {
		if errors.Is(err, ErrResourceNotFound) {
			return nil, ErrResourceNotFound
		}
		log.Error("failed-to-disable-reminder-error-fetching", zap.String("reminder-id", req.Id), zap.Error(err))
		return nil, err
	}

	if existing.UserID != req.UserID {
		return nil, ErrNotAuthorized
	}

	if existing.Status == ReminderStatusDeleted {
		return nil, ErrInvalidReminderStatus
	}

	existing.Status = ReminderStatusDisabled
	existing.UpdatedByUserID = req.UserID

	updated, err := s.ReminderRepository.UpdateReminderByID(ctx, existing)
	if err != nil {
		log.Error("failed-to-disable-reminder-error", zap.String("reminder-id", req.Id), zap.Error(err))
		return nil, err
	}

	return &UpdateReminderByIDResponse{Reminder: updated}, nil
}

// GetReminderStats returns aggregate reminder counts for admin dashboards.
func (s *Service) GetReminderStats(ctx context.Context, req *GetReminderStatsRequest) (*GetReminderStatsResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil {
		req = &GetReminderStatsRequest{}
	}

	userIDs := normaliseReminderUserIDs(req.UserID, req.UserIDs)
	baseFilter := &ReminderFilter{UserIDs: userIDs}

	total, err := s.ReminderRepository.CountReminders(ctx, baseFilter)
	if err != nil {
		log.Error("failed-to-count-reminders-total", zap.Error(err))
		return nil, err
	}

	active, err := s.ReminderRepository.CountReminders(ctx, &ReminderFilter{UserIDs: userIDs, Status: string(ReminderStatusActive)})
	if err != nil {
		log.Error("failed-to-count-reminders-active", zap.Error(err))
		return nil, err
	}

	completed, err := s.ReminderRepository.CountReminders(ctx, &ReminderFilter{UserIDs: userIDs, Status: string(ReminderStatusCompleted)})
	if err != nil {
		log.Error("failed-to-count-reminders-completed", zap.Error(err))
		return nil, err
	}

	disabled, err := s.ReminderRepository.CountReminders(ctx, &ReminderFilter{UserIDs: userIDs, Status: string(ReminderStatusDisabled)})
	if err != nil {
		log.Error("failed-to-count-reminders-disabled", zap.Error(err))
		return nil, err
	}

	dueReminders, err := s.ReminderRepository.GetDueReminders(ctx, &ReminderFilter{
		UserIDs:   userIDs,
		Status:    string(ReminderStatusActive),
		DueBefore: toolbox.TimeNowUTC(),
	}, 0)
	if err != nil {
		log.Error("failed-to-get-due-reminders-count", zap.Error(err))
		return nil, err
	}

	return &GetReminderStatsResponse{
		Stats: &ReminderStats{
			TotalReminders:  total,
			ActiveReminders: active,
			DueReminders:    int64(len(dueReminders)),
			CompletedCount:  completed,
			DisabledCount:   disabled,
		},
	}, nil
}

// GetDueReminders returns active reminders ready for scheduler dispatch.
func (s *Service) GetDueReminders(ctx context.Context, req *GetDueRemindersRequest) (*GetDueRemindersResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil {
		req = &GetDueRemindersRequest{}
	}

	dueBefore := req.DueBefore
	if dueBefore == "" {
		dueBefore = toolbox.TimeNowUTC()
	}

	status := ReminderStatusActive
	if req.Status != "" {
		var err error
		status, err = ValidateAndNormaliseStatus(req.Status)
		if err != nil {
			return nil, err
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	userIDs := normaliseReminderUserIDs(req.UserID, req.UserIDs)

	reminders, err := s.ReminderRepository.GetDueReminders(ctx, &ReminderFilter{
		Status:    string(status),
		DueBefore: dueBefore,
		UserIDs:   userIDs,
	}, limit)
	if err != nil {
		log.Error("failed-to-get-due-reminders-error", zap.Error(err))
		return nil, err
	}

	return &GetDueRemindersResponse{Reminders: reminders}, nil
}

// GetRemindersForTargetTypeByUserID returns a user's reminders for a target type and optional target ID.
func (s *Service) GetRemindersForTargetTypeByUserID(ctx context.Context, req *GetRemindersForTargetTypeByUserIDRequest) (*ListRemindersResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrUserIDIsRequired
	}
	if strings.TrimSpace(req.TargetType) == "" {
		return nil, ErrTargetTypeIsRequired
	}

	page, perPage := normaliseReminderPagination(req.Page, req.PerPage)
	reminders, err := s.ReminderRepository.GetRemindersForTargetTypeByUserID(ctx, strings.TrimSpace(req.UserID), strings.TrimSpace(req.TargetType), strings.TrimSpace(req.TargetId), page, perPage)
	if err != nil {
		return nil, err
	}

	return &ListRemindersResponse{
		Reminders: reminders,
		Meta:      reminderPaginationMeta(page, perPage),
	}, nil
}

// GetActiveRemindersForTargetTypeByUserID returns active reminders for a user's target type.
func (s *Service) GetActiveRemindersForTargetTypeByUserID(ctx context.Context, req *GetActiveRemindersForTargetTypeByUserIDRequest) (*ListRemindersResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrUserIDIsRequired
	}
	if strings.TrimSpace(req.TargetType) == "" {
		return nil, ErrTargetTypeIsRequired
	}

	page, perPage := normaliseReminderPagination(req.Page, req.PerPage)
	reminders, err := s.ReminderRepository.GetActiveRemindersForTargetTypeByUserID(ctx, strings.TrimSpace(req.UserID), strings.TrimSpace(req.TargetType), strings.TrimSpace(req.TargetId), page, perPage)
	if err != nil {
		return nil, err
	}

	return &ListRemindersResponse{
		Reminders: reminders,
		Meta:      reminderPaginationMeta(page, perPage),
	}, nil
}

func normaliseReminderPagination(page, perPage int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 25
	}
	return page, perPage
}

func normaliseReminderUserIDs(userID string, userIDs []string) []string {
	seen := map[string]struct{}{}
	normalised := []string{}
	add := func(value string) {
		for _, item := range toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(value) {
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			normalised = append(normalised, item)
		}
	}

	add(userID)
	for _, id := range userIDs {
		add(id)
	}

	return normalised
}

func reminderPaginationMeta(page, perPage int) map[string]interface{} {
	return map[string]interface{}{
		"page":     page,
		"per_page": perPage,
	}
}

// RecordReminderExecution stores the result of one reminder scheduler or notification attempt.
func (s *Service) RecordReminderExecution(ctx context.Context, req *RecordReminderExecutionRequest) (*RecordReminderExecutionResponse, error) {
	if req == nil || strings.TrimSpace(req.ReminderId) == "" {
		return nil, ErrIdIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrUserIDIsRequired
	}

	status, err := ValidateAndNormaliseExecutionStatus(req.Status)
	if err != nil {
		return nil, err
	}

	scheduledFor := strings.TrimSpace(req.ScheduledFor)
	if scheduledFor == "" {
		return nil, ErrTargetTimeIsRequired
	}

	executedAt := strings.TrimSpace(req.ExecutedAt)
	if executedAt == "" && (status == ReminderExecutionStatusSent || status == ReminderExecutionStatusFailed || status == ReminderExecutionStatusSkipped) {
		executedAt = toolbox.TimeNowUTC()
	}

	attempt := req.Attempt
	if attempt <= 0 {
		attempt = 1
	}

	createdByUserID := strings.TrimSpace(req.CreatedByUserID)
	if createdByUserID == "" {
		createdByUserID = strings.TrimSpace(req.UserID)
	}

	execution := &ReminderExecution{
		ReminderId:      strings.TrimSpace(req.ReminderId),
		UserID:          strings.TrimSpace(req.UserID),
		TargetType:      strings.TrimSpace(req.TargetType),
		TargetId:        strings.TrimSpace(req.TargetId),
		ScheduledFor:    scheduledFor,
		ExecutedAt:      executedAt,
		Status:          status,
		Attempt:         attempt,
		NotificationRef: strings.TrimSpace(req.NotificationRef),
		Error:           strings.TrimSpace(req.Error),
		Metadata:        req.Metadata,
		CreatedByUserID: createdByUserID,
		CreatedAt:       toolbox.TimeNowUTC(),
	}

	created, err := s.ReminderRepository.CreateReminderExecution(ctx, execution)
	if err != nil {
		return nil, err
	}

	return &RecordReminderExecutionResponse{Execution: created}, nil
}

// ListReminderExecutions returns execution tracking records for operational review.
func (s *Service) ListReminderExecutions(ctx context.Context, req *ListReminderExecutionsRequest) (*ListReminderExecutionsResponse, error) {
	if req == nil {
		req = &ListReminderExecutionsRequest{}
	}

	status := req.Status
	if status != "" {
		var err error
		status, err = ValidateAndNormaliseExecutionStatus(status)
		if err != nil {
			return nil, err
		}
	}

	page, perPage := normaliseReminderPagination(req.Page, req.PerPage)
	executions, err := s.ReminderRepository.ListReminderExecutions(ctx, &ReminderExecutionFilter{
		ReminderId: strings.TrimSpace(req.ReminderId),
		UserID:     strings.TrimSpace(req.UserID),
		TargetType: strings.TrimSpace(req.TargetType),
		TargetId:   strings.TrimSpace(req.TargetId),
		Status:     status,
	}, page, perPage)
	if err != nil {
		return nil, err
	}

	return &ListReminderExecutionsResponse{
		Executions: executions,
		Meta:       reminderPaginationMeta(page, perPage),
	}, nil
}
