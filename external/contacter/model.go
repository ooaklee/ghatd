package contacter

import (
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// CommsType represents the type of comms
type CommsType string

const (

	// CommsTypeGeneralInquiry represents a general inquiry
	CommsTypeGeneralInquiry CommsType = "general-inquiry"

	// CommsTypeCustomerSupport represents a customer support comms
	CommsTypeCustomerSupport CommsType = "customer-support"

	// CommsTypeTechnicalSupport represents a technical support comms
	CommsTypeTechnicalSupport CommsType = "technical-support"

	// CommsTypeFeatureRequest represents a feature request comms
	CommsTypeFeatureRequest CommsType = "feature-request"

	// CommsTypeFeedback represents a feedback comms
	CommsTypeFeedback CommsType = "feedback"

	// CommsTypeProductInformation represents a product information comms
	CommsTypeProductInformation CommsType = "product-information"

	// CommsTypePressInquiry represents a press inquiry comms
	CommsTypePressInquiry CommsType = "press-inquiry"

	// CommsTypePartnershipOpportunities represents a partnership opportunities comms
	CommsTypePartnershipOpportunities CommsType = "partnership-opportunities"

	// CommsTypeComplaints represents a complaints comms
	CommsTypeComplaints CommsType = "complaints"

	// CommsTypeWebsiteIssues represents a website issues comms
	CommsTypeWebsiteIssues CommsType = "website-issues"

	// CommsTypeDonatingSupportingUsQuestions represents a donating/supporting us questions comms
	CommsTypeDonatingSupportingUsQuestions CommsType = "donating-supporting-us-questions"

	// CommsTypeOther represents a other comms
	CommsTypeOther CommsType = "other"
)

// Comms represents a comms from a user
//
//	{
//		"id": "asdcv4-f6783-098uh-09is",
//		"nano_id": "987tfghjk98",
//		"full_name": "John Doe",
//		"email": "johndoe@email.com",
//		"type": "other",
//		"message": "I love cats",
//		"meta": {
//		  "displayed_as": "Other",
//		  "subject": "Just wanted to share!",
//		},
//		"user_id": "98uh789-1209u-09uh-098ygfc" # Only added if the user was logged in (or could be found in the system),
//		"user_logged_in": true, # If this was false and the above was filed would indicate the user_id was found by matching email on system
//		"created_at": "2025-03-31T23:04:40+XXX"
//	}
type Comms struct {

	// Id is the unique identifier for the comms
	Id string `json:"id" bson:"_id"`

	// NanoId is the nano ID for the comms
	NanoId string `json:"nano_id" bson:"_nano_id"`

	// FullName is the full name of the person who made the comms
	FullName string `json:"full_name" bson:"full_name"`

	// Email is the email of the person who made the comms
	Email string `json:"email" bson:"email"`

	// Type is the type of the comms
	Type CommsType `json:"type" bson:"type"`

	// ProvidedType is the type of the comms provided by the user
	// if the type is not valid, it will be set to other and this field
	// will be set to the provided type
	ProvidedType string `json:"provided_type,omitempty" bson:"provided_type,omitempty"`

	// Message is the body of the comms
	Message string `json:"message" bson:"message"`

	// Meta is the meta data for the comms
	Meta map[string]interface{} `json:"meta,omitempty" bson:"meta,omitempty"`

	// UserId is the ID of the user who made the comms
	UserId string `json:"user_id,omitempty" bson:"user_id,omitempty"`

	// UserLoggedIn is true if the user was logged in when the comms was made
	UserLoggedIn bool `json:"user_logged_in" bson:"user_logged_in"`

	// CreatedAt is the date and time the comms was created
	CreatedAt string `json:"created_at" bson:"created_at"`

	// UpdatedAt is the date and time the comms was updated
	UpdatedAt string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
}

// take string, sanitize, and set correct comms type
func (c *Comms) SetCommsType(providedType string) *Comms {

	var err error

	// make provided type kebab case
	providedType, err = toolbox.StringConvertToKebabCase(providedType)
	if err != nil {
		providedType = strings.ReplaceAll(
			strings.ToLower(providedType),
			" ",
			"-",
		)
	}

	// convert to comms type
	commType := CommsType(providedType)

	// make sure the comms type is valid
	if commType != CommsTypeGeneralInquiry &&
		commType != CommsTypeCustomerSupport &&
		commType != CommsTypeTechnicalSupport &&
		commType != CommsTypeFeatureRequest &&
		commType != CommsTypeFeedback &&
		commType != CommsTypeProductInformation &&
		commType != CommsTypePressInquiry &&
		commType != CommsTypePartnershipOpportunities &&
		commType != CommsTypeComplaints &&
		commType != CommsTypeWebsiteIssues &&
		commType != CommsTypeDonatingSupportingUsQuestions &&
		commType != CommsTypeOther {
		commType = CommsTypeOther
		c.ProvidedType = providedType
	}

	c.Type = commType

	return c
}

// GenerateNanoId generates a new Nano Id for the comms
func (c *Comms) GenerateNanoId() *Comms {

	c.NanoId = toolbox.GenerateNanoId()

	return c
}

// GenerateId generates a new Id for the comms
func (c *Comms) GenerateId() *Comms {

	c.Id = toolbox.GenerateUuidV4()

	return c
}

// SetCreatedAtTimeToNow sets the created at date and time for the comms to now
func (c *Comms) SetCreatedAtTimeToNow() *Comms {

	c.CreatedAt = toolbox.TimeNowUTC()

	return c
}

// SetUpdatedAtTimeToNow sets the updated at date and time for the comms to now
func (c *Comms) SetUpdatedAtTimeToNow() *Comms {

	c.UpdatedAt = toolbox.TimeNowUTC()

	return c
}

// SetStandardisedEmail sets the email of the person who made the comms
func (c *Comms) SetStandardisedEmail(email string) *Comms {

	c.Email = toolbox.StringStandardisedToLower(email)

	return c
}

// SetStandardisedFullName sets the full name of the person who made the comms
func (c *Comms) SetStandardisedFullName(fullName string) *Comms {

	// Make full name title case and remove excess spaces
	c.FullName = toolbox.StringConvertToTitleCase(fullName)

	return c
}
