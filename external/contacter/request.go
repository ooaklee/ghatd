package contacter

// GetTotalCommsRequest holds everything needed to make
// the request to get the total count of comms from repository
type GetTotalCommsRequest struct {

	// FullName is the full name to filter by
	FullName string

	// Emails is the list of emails to filter by
	Emails []string

	// CommsTypes is the list of comms types to filter by
	CommsTypes []CommsType

	// MessageContains is the message/ subtext to filter by
	MessageContains string

	// DisplayedAs is the list of displayed as subjects to filter by
	DisplayedAs []string

	// CustomSubjectContains is the text to filter the custom subject by
	CustomSubjectContains string

	// CreatedAtFrom is to filter by the date to which the comms was created from
	CreatedAtFrom string

	// CreatedAtTo is to filter by the date to which the comms was created at up to
	CreatedAtTo string

	// UserLoggedIn is in to filter by whether the user making the comms was logged
	UserLoggedIn bool

	// UserNotLoggedIn is in to filter by whether the user making the comms was not logged
	UserNotLoggedIn bool
}

// GetCommsRequest holds everything needed to make
// the request to get comms
type GetCommsRequest struct {

	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc,
	Order string `query:"order"`

	// Total number of comms to return per page, if available. Default 25.
	// Accepts anything between 1 and 100
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// TotalCount specifies the total count of all comms
	TotalCount int

	// TotalPages specifies the total pages of results
	TotalPages int

	// Meta whether response should contain meta information
	Meta bool `query:"meta"`

	// FullName filters for comms with the provided full name
	FullName string `query:"full_name"`

	// UserLoggedIn filters for comms made by logged in users
	UserLoggedIn bool `query:"user_logged_in"`

	// UserNotLoggedIn filters for comms made by not logged in users
	UserNotLoggedIn bool `query:"user_not_logged_in"`

	// FromEmails filters for comms from the provided emails
	// comma-separated list of emails
	FromEmails string `query:"from_emails"`

	// WithTypes filters for comms with the provided types
	// comma-separated list of types
	WithTypes string `query:"with_types"`

	// MessageContains filters for comms with the provided message
	MessageContains string `query:"message_contains"`

	// DisplayedAs filters for comms with the provided displayed as subjects
	// comma-separated list of diplay subjects
	DisplayedAs string `query:"displayed_as"`

	// CustomSubjectContains filters for comms with the provided custom subject
	CustomSubjectContains string `query:"custom_subject_contains"`

	// CreatedAtFrom filters for comms created at from the provided date
	CreatedAtFrom string `query:"created_at_from"`

	// CreatedAtTo filters for comms created at up to the provided date
	CreatedAtTo string `query:"created_at_to"`
}

// GetMetaData returns a map of metadata about the GetCommsRequest, including the
// number of resources per page, the total number of resources, the total
// number of pages, and the current page.
func (g *GetCommsRequest) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap["resources_per_page"] = g.PerPage
	responseMap["total_resources"] = g.TotalCount
	responseMap["total_pages"] = g.TotalPages
	responseMap["page"] = g.Page

	return responseMap
}

// CreateCommsRequest holds everything needed to make
// the request to create a comms
//
//	{
//		"full_name": "John Doe",
//		"email": "johndoe@email.com",
//		"type": "feedback",
//		"message": "I love what you've done with this",
//		"meta": {
//		  "displayed_as": "Feedback",
//		}
//	  }
type CreateCommsRequest struct {

	// UserId is the ID of the user making requests to
	// get the comms for the platform
	UserId string

	// FullName is the full name of the person who made the comms
	FullName string `json:"full_name" validate:"required"`

	// Email is the email of the person who made the comms
	Email string `json:"email" validate:"required,email"`

	// Type is the type of the comms
	Type CommsType `json:"type" validate:"required"`

	// Message is the body of the comms
	Message string `json:"message"`

	// Meta is the meta data for the comms
	Meta map[string]interface{} `json:"meta,omitempty"`
}
