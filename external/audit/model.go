package audit

import (
	"github.com/ooaklee/ghatd/external/toolbox"
)

// AuditLogEntry holds the shape that represents a typical
// audit log entry
type AuditLogEntry struct {
	Id         string      `json:"id" bson:"_id"`
	ActorId    string      `json:"actor_id" bson:"actor_id,omitempty"`
	Action     AuditAction `json:"action" bson:"action"`
	TargetId   string      `json:"target_id" bson:"target_id,omitempty"`
	TargetType TargetType  `json:"target_type" bson:"target_type"`
	Domain     string      `json:"domain" bson:"domain"`
	ActionAt   string      `json:"action_at" bson:"action_at"`
	Details    interface{} `json:"details" bson:"details,omitempty"`
}

// SetActionAtTimeToNow sets the createdAt time to now (UTC)
func (a *AuditLogEntry) SetActionAtTimeToNow() *AuditLogEntry {
	a.ActionAt = toolbox.TimeNowUTC()
	return a
}

// GenerateNewUuid creates a new Uuid for Log Entry
func (a *AuditLogEntry) GenerateNewUuid() *AuditLogEntry {
	a.Id = toolbox.GenerateUuidV4()
	return a
}

// UserEmailOutboundEventDetails holds the extra details
// we care about when logging User Email Outbound Events
type UserEmailOutboundEventDetails struct {
	To            string `json:"to" bson:"to,omitempty"`
	From          string `json:"from" bson:"from,omitempty"`
	Subject       string `json:"subject" bson:"subject,omitempty"`
	SentAt        string `json:"sent_at" bson:"sent_at,omitempty"`
	EmailProvider string `json:"email_provider" bson:"email_provider,omitempty"`

	// EmailType (Other or Security) - Other promotional, newletter from platform etc.
	// Security - Log in requests, Change email, Delete account etc.
	EmailType EmailType `json:"-" bson:"email_type,omitempty"`
}

// UserSsoEventDetails holds the extra details
// we care about when using sso
type UserSsoEventDetails struct {
	SsoProvider string `json:"sso_provider" bson:"sso_provider,omitempty"`
}

// UserAccountDeleteEventDetails holds the extra details
// we care about when deleting a user account
type UserAccountDeleteEventDetails struct {
	UserEmail     string `json:"email" bson:"email,omitempty"`
	UserFirstName string `json:"first_name" bson:"first_name,omitempty"`
}
