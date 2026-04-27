package usermanager

import (
	"time"

	"github.com/ooaklee/ghatd/external/group"
)

// UsageInfo represents usage statistics
type UsageInfo struct {
	Allowance      string `json:"allowance"`
	Used           string `json:"used"`
	UsedPercentage string `json:"used_percentage"`
}

// GroupMemberInfo represents minimal information about a group member
// Used for displaying membership details without full user/group data
type GroupMemberInfo struct {
	// ID is the unique identifier of the member (user or group)
	ID string `json:"id"`

	// FullName is the complete name of the member
	FullName string `json:"full_name"`

	// Type indicates the member type (USER or GROUP)
	Type string `json:"type"`

	// Email is the email address (for users)
	Email string `json:"email,omitempty"`

	// Role is the member's role within the group
	Role string `json:"role,omitempty"`

	// JoinedAt is when the member joined the group
	JoinedAt string `json:"joined_at,omitempty"`
}

// GroupSummary represents summary information about a group
type GroupSummary struct {
	// ID is the group's unique identifier
	ID string `json:"id"`

	// NanoID is the group's alternative identifier
	NanoID string `json:"nano_id,omitempty"`

	// Name is the group's name
	Name string `json:"name"`

	// Type is the group type (TEAM, DEPARTMENT, etc.)
	Type string `json:"type"`

	// Description is a brief description of the group
	Description string `json:"description,omitempty"`

	// MemberCount is the total number of members
	MemberCount int `json:"member_count"`

	// Status is the group's current status
	Status string `json:"status"`

	// CreatedAt is when the group was created
	CreatedAt string `json:"created_at,omitempty"`
}

// GroupSeatInfo describes current usage vs capacity for a group
type GroupSeatInfo struct {
	Used     int `json:"used"`
	Capacity int `json:"capacity"`
}

// GroupStatsMeta holds auxiliary stats for a group
type GroupStatsMeta struct {
	MemberCount   int                  `json:"member_count"`
	RoleBreakdown map[string]int       `json:"role_breakdown,omitempty"`
	Metadata      *group.GroupMetadata `json:"metadata,omitempty"`
}

// GroupStats represents user-focused stats for a group
type GroupStats struct {
	ID         string         `json:"id"`
	NanoID     string         `json:"nano_id,omitempty"`
	TotalUsers int            `json:"total_users"`
	Seats      GroupSeatInfo  `json:"seats"`
	Visibility string         `json:"visibility,omitempty"`
	Email      string         `json:"email,omitempty"`
	Meta       GroupStatsMeta `json:"meta"`
}

// UserGroupMembership represents a user's membership in a specific group
type UserGroupMembership struct {
	// Group contains summary information about the group
	Group GroupSummary `json:"group"`

	// IsMember indicates if the user is a member
	IsMember bool `json:"is_member"`

	// Role is the user's role in the group
	Role string `json:"role,omitempty"`

	// IsOwner indicates if the user owns the group
	IsOwner bool `json:"is_owner"`

	// JoinedAt is when the user joined the group
	JoinedAt string `json:"joined_at,omitempty"`
}

// EnrichedUserProfile represents a user profile enriched with group membership data
type EnrichedUserProfile struct {
	// ID is the user's unique identifier
	ID string `json:"id"`

	// Email is the user's email address
	Email string `json:"email"`

	// FullName is the user's full name
	FullName string `json:"full_name"`

	// FirstName is the user's first name
	FirstName string `json:"first_name,omitempty"`

	// LastName is the user's last name
	LastName string `json:"last_name,omitempty"`

	// Status is the user's account status
	Status string `json:"status"`

	// Roles are the user's platform roles
	Roles []string `json:"roles,omitempty"`

	// Teams are the teams the user belongs to
	Teams []UserGroupMembership `json:"teams,omitempty"`

	// Departments are the departments the user belongs to
	Departments []UserGroupMembership `json:"departments,omitempty"`

	// Groups are all groups the user belongs to
	Groups []UserGroupMembership `json:"groups,omitempty"`

	// CreatedAt is when the user account was created
	CreatedAt string `json:"created_at,omitempty"`

	// LastLoginAt is when the user last logged in
	LastLoginAt string `json:"last_login_at,omitempty"`
}

// GroupMembershipAction represents an action to be performed on group membership
type GroupMembershipAction struct {
	// Action is the type of action (ADD, REMOVE, UPDATE_ROLE)
	Action string `json:"action"`

	// GroupID is the target group identifier
	GroupID string `json:"group_id"`

	// Role is the role to assign (for ADD/UPDATE_ROLE actions)
	Role string `json:"role,omitempty"`

	// RemoveFromOtherGroups indicates whether to remove user from other groups of the same type
	RemoveFromOtherGroups bool `json:"remove_from_other_groups,omitempty"`
}

// BulkGroupMembershipUpdate represents a batch update of group memberships
type BulkGroupMembershipUpdate struct {
	// UserID is the user whose memberships are being updated
	UserID string `json:"user_id"`

	// Actions are the membership actions to perform
	Actions []GroupMembershipAction `json:"actions"`
}

// GroupMembershipUpdateResult represents the result of a membership update
type GroupMembershipUpdateResult struct {
	// Success indicates if the operation succeeded
	Success bool `json:"success"`

	// GroupID is the affected group
	GroupID string `json:"group_id"`

	// Action is the action that was performed
	Action string `json:"action"`

	// Error is any error message (if Success is false)
	Error string `json:"error,omitempty"`

	// UpdatedAt is when the update occurred
	UpdatedAt time.Time `json:"updated_at"`
}
