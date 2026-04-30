package common

// NotificationSource represents the source of a notification
type NotificationSource string

const (
	// NotificationSourcePost indicates the notification is related to a post
	NotificationSourcePost NotificationSource = "post"

	// NotificationSourceGroup indicates the notification is related to a group
	NotificationSourceGroup NotificationSource = "group"
)

// NotificationKind represents the type of a notification
type NotificationKind string

const (
	NotificationKindPostArticle            NotificationKind = "post_article"
	NotificationKindPostChangelog          NotificationKind = "post_changelog"
	NotificationKindPostFaq                NotificationKind = "post_faq"
	NotificationKindPostGlossary           NotificationKind = "post_glossary"
	NotificationKindGroupInviteOutstanding NotificationKind = "group_invite_outstanding"
)

// NotificationOverview represents a summary of a notification for display
type NotificationOverview struct {

	// ID is the unique identifier for the notification
	ID string `json:"id"`

	// Source indicates the origin of the notification (e.g., post, group)
	Source NotificationSource `json:"source"`

	// Kind indicates the specific type of notification (e.g., post_article, group_invite_outstanding)
	Kind NotificationKind `json:"kind"`

	// Title is a brief title summarising the notification
	Title string `json:"title"`

	// NotificationTitle is a more detailed title for the notification, if available
	NotificationTitle string `json:"notification_title,omitempty"`

	// OccurredAt is the timestamp when the event that triggered the notification occurred
	OccurredAt string `json:"occurred_at,omitempty"`

	// UpdatedAt is the timestamp when the notification was last updated
	UpdatedAt string `json:"updated_at,omitempty"`

	// Href is the URL associated with the notification, if available
	Href string `json:"href,omitempty"`

	// RevisionHash is the hash of the revision associated with the notification, if available
	RevisionHash string `json:"revision_hash,omitempty"`

	// Metadata contains additional information about the notification
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GetLatestNotificationOverviewsRequest holds the request parameters for fetching the latest notification overviews
type GetLatestNotificationOverviewsRequest struct {

	// UserID is the ID of the user for whom to fetch notifications
	UserID string `json:"user_id,omitempty" query:"user_id"`

	// UserEmail is the email of the user for whom to fetch notifications
	UserEmail string `json:"user_email,omitempty" query:"user_email"`

	// Kinds is an optional filter to specify which kinds of notifications to fetch
	Kinds string `json:"kinds,omitempty" query:"kinds"`

	//
	Limit int `json:"limit,omitempty" query:"limit"`
}

// GetLatestNotificationOverviewsResponse holds the response for fetching the latest notification overviews
type GetLatestNotificationOverviewsResponse struct {

	// Overviews is a list of notification summaries for the user
	Overviews []NotificationOverview `json:"overviews"`
}
