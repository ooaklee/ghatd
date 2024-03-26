package audit

import (
	"context"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// AuditRespository expected methods of a valid audit repository
type AuditRespository interface {
	CreateAuditLogEvent(ctx context.Context, event *AuditLogEntry) error
	GetTotalAuditLogEvents(ctx context.Context, userId string, to string, from string, domains string, actions []AuditAction, targetId string, targetTypes []TargetType) (int64, error)
}

// Service holds and manages audit business logic
type Service struct {
	AuditRespository AuditRespository
}

// NewService created audit service
func NewService(AuditRespository AuditRespository) *Service {
	return &Service{
		AuditRespository: AuditRespository,
	}
}

// LogAuditEvent handles creating an log entry event into audit repository
// TODO: Create tests
func (s *Service) LogAuditEvent(ctx context.Context, r *LogAuditEventRequest) error {
	log := logger.AcquireFrom(ctx)

	entry := AuditLogEntry{
		ActorId:    r.ActorId,
		Action:     r.Action,
		TargetId:   r.TargetId,
		TargetType: r.TargetType,
		Domain: func(domainName string) string {
			finalDomainName, err := toolbox.StringConvertToKebabCase(domainName)
			if err != nil {
				finalDomainName := toolbox.StringConvertToSnakeCase(toolbox.StringStandardisedToLower(domainName))
				log.Warn("unable-to-normalise-domain-name-with-tag-rules", zap.String("original-name", domainName), zap.String("final-name", finalDomainName))
				return finalDomainName
			}

			return finalDomainName
		}(r.Domain),
		Details: r.Details,
	}

	err := s.AuditRespository.CreateAuditLogEvent(ctx, &entry)
	if err != nil {
		return err
	}

	return nil
}

// GetTotalAuditLogEvents gets the total on audit-based on passed values
func (s *Service) GetTotalAuditLogEvents(ctx context.Context, r *GetTotalAuditLogEventsRequest) (int64, error) {
	return s.AuditRespository.GetTotalAuditLogEvents(ctx, r.UserId, r.To, r.From, r.Domains, r.Actions, r.TargetId, r.TargetTypes)
}
