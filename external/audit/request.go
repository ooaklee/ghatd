package audit

// LogAuditEventRequest holds attributes needed to create
// a log audit request
type LogAuditEventRequest struct {

	// ActorId is the id of the entity carrying out the action
	ActorId string

	// Action is the type of action
	Action AuditAction

	// TargetId is the id of the resource being targetted
	TargetId string

	// TargetType is the resource type of the target
	TargetType TargetType

	// Domain is the name of the domain the event is sourced from
	Domain string

	// Details is the additional context around the entry
	Details interface{}
}

// GetTotalAuditLogEventsRequest is holding attributes need to get audit log stats
type GetTotalAuditLogEventsRequest struct {
	// UserId the user's UUID
	UserId string

	To string

	From string

	Domains     string
	Actions     []AuditAction
	TargetId    string
	TargetTypes []TargetType
}
