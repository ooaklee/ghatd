package contacter

import (
	"encoding/json"
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

	// CommsTypeFeedbackCompanion represents a feedback comms specifically about/from the companion app
	CommsTypeFeedbackCompanion CommsType = "feedback-companion"

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

// CommsTypeMap defines the communication types accepted by a contacter
// service. The map value is a client-facing label so host applications can
// keep their contact taxonomy and presentation copy together.
//
// Maps passed to NewService are normalised and copied before use. This lets a
// host select a subset of the defaults or add application-specific types
// without changing the contacter package.
type CommsTypeMap map[CommsType]string

// DefaultCommsTypeMap returns a fresh copy of the communication types that
// contacter historically accepted.
func DefaultCommsTypeMap() CommsTypeMap {
	return CommsTypeMap{
		CommsTypeGeneralInquiry:                "General Inquiry",
		CommsTypeCustomerSupport:               "Customer Support",
		CommsTypeTechnicalSupport:              "Technical Support",
		CommsTypeFeatureRequest:                "Feature Request",
		CommsTypeFeedback:                      "Feedback",
		CommsTypeFeedbackCompanion:             "Companion Feedback",
		CommsTypeProductInformation:            "Product Information",
		CommsTypePressInquiry:                  "Press Inquiry",
		CommsTypePartnershipOpportunities:      "Partnership Opportunities",
		CommsTypeComplaints:                    "Complaints",
		CommsTypeWebsiteIssues:                 "Website Issues",
		CommsTypeDonatingSupportingUsQuestions: "Donating/Supporting Us Questions",
		CommsTypeOther:                         "Other",
	}
}

// Has reports whether the map contains the provided communication type.
func (m CommsTypeMap) Has(commsType CommsType) bool {
	_, ok := m[normaliseCommsType(string(commsType))]
	return ok
}

// Clone returns a normalised defensive copy. Other is always present because
// it remains contacter's lossless fallback for an unrecognised provided type.
func (m CommsTypeMap) Clone() CommsTypeMap {
	if m == nil {
		m = DefaultCommsTypeMap()
	}

	cloned := make(CommsTypeMap, len(m)+1)
	for commsType, displayName := range m {
		normalised := normaliseCommsType(string(commsType))
		if normalised == "" {
			continue
		}
		cloned[normalised] = strings.TrimSpace(displayName)
	}
	if _, ok := cloned[CommsTypeOther]; !ok {
		cloned[CommsTypeOther] = "Other"
	}

	return cloned
}

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

	// AdminNotes are notes added by admins regarding this comms
	AdminNotes string `json:"admin_notes,omitempty" bson:"admin_notes"`

	// ReachedOutAt is the timestamp when an admin reached out to the user
	ReachedOutAt string `json:"reached_out_at,omitempty" bson:"reached_out_at,omitempty"`

	// AdminReply is the reply message from the admin to the user
	AdminReply string `json:"admin_reply,omitempty" bson:"admin_reply"`

	// LinkedCommsIds are the IDs of other comms linked to this one
	LinkedCommsIds []string `json:"linked_comms_ids,omitempty" bson:"linked_comms_ids"`
}

// take string, sanitize, and set correct comms type
func (c *Comms) SetCommsType(providedType string, configuredTypes ...CommsTypeMap) *Comms {
	types := DefaultCommsTypeMap()
	if len(configuredTypes) > 0 {
		types = configuredTypes[0].Clone()
	}

	commType := normaliseCommsType(providedType)
	c.ProvidedType = ""
	if !types.Has(commType) {
		c.ProvidedType = string(commType)
		commType = CommsTypeOther
	}

	c.Type = commType

	return c
}

func normaliseCommsType(providedType string) CommsType {
	normalised, err := toolbox.StringConvertToKebabCase(providedType)
	if err != nil {
		normalised = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(providedType)), " ", "-")
	}
	return CommsType(normalised)
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

// CommsStats holds aggregated platform comms statistics.
// Designed to grow as new stat dimensions are added.
type CommsStats struct {
	// Total is the total number of comms
	Total int64 `json:"total"`

	// RepliedTo is the number of comms with admin replies
	RepliedTo int64 `json:"replied_to"`

	// ReachedOut is the number of comms where admin has reached out
	ReachedOut int64 `json:"reached_out"`

	// WithAdminNotes is the number of comms with admin notes
	WithAdminNotes int64 `json:"with_admin_notes"`

	// WithLinkedComms is the number of comms with linked comms
	WithLinkedComms int64 `json:"with_linked_comms"`

	// FromLoggedInUsers is the number of comms from logged-in users
	FromLoggedInUsers int64 `json:"from_logged_in_users"`

	// FromGuests is the number of comms from guest users
	FromGuests int64 `json:"from_guests"`

	// MostRecentCommsAt is the timestamp of the most recent comms
	MostRecentCommsAt string `json:"most_recent_comms_at,omitempty"`

	// AverageReplyTimeMinutes is the average time in minutes it takes to reply to comms
	// (calculated from CreatedAt to ReachedOutAt for comms that have been reached out)
	AverageReplyTimeMinutes float64 `json:"average_reply_time_minutes"`

	// ByType holds counts of comms by type
	ByType CommsTypeStats `json:"by_type"`

	// ByStatus holds status information
	ByStatus CommsStatusStats `json:"by_status"`
}

// CommsTypeStats holds counts of comms by type. The named fields preserve the
// existing Go API, while Additional allows configured custom types to appear
// in the same flat JSON object.
type CommsTypeStats struct {
	GeneralInquiry                int64            `json:"general_inquiry"`
	CustomerSupport               int64            `json:"customer_support"`
	TechnicalSupport              int64            `json:"technical_support"`
	FeatureRequest                int64            `json:"feature_request"`
	Feedback                      int64            `json:"feedback"`
	FeedbackCompanion             int64            `json:"feedback_companion"`
	ProductInformation            int64            `json:"product_information"`
	PressInquiry                  int64            `json:"press_inquiry"`
	PartnershipOpportunities      int64            `json:"partnership_opportunities"`
	Complaints                    int64            `json:"complaints"`
	WebsiteIssues                 int64            `json:"website_issues"`
	DonatingSupportingUsQuestions int64            `json:"donating_supporting_us_questions"`
	Other                         int64            `json:"other"`
	Additional                    map[string]int64 `json:"-"`
}

// Count returns the count for a communication type.
func (s CommsTypeStats) Count(commsType CommsType) int64 {
	switch normaliseCommsType(string(commsType)) {
	case CommsTypeGeneralInquiry:
		return s.GeneralInquiry
	case CommsTypeCustomerSupport:
		return s.CustomerSupport
	case CommsTypeTechnicalSupport:
		return s.TechnicalSupport
	case CommsTypeFeatureRequest:
		return s.FeatureRequest
	case CommsTypeFeedback:
		return s.Feedback
	case CommsTypeFeedbackCompanion:
		return s.FeedbackCompanion
	case CommsTypeProductInformation:
		return s.ProductInformation
	case CommsTypePressInquiry:
		return s.PressInquiry
	case CommsTypePartnershipOpportunities:
		return s.PartnershipOpportunities
	case CommsTypeComplaints:
		return s.Complaints
	case CommsTypeWebsiteIssues:
		return s.WebsiteIssues
	case CommsTypeDonatingSupportingUsQuestions:
		return s.DonatingSupportingUsQuestions
	case CommsTypeOther:
		return s.Other
	default:
		return s.Additional[commsTypeStatsKey(commsType)]
	}
}

// Set records a count for a default or application-specific communication
// type.
func (s *CommsTypeStats) Set(commsType CommsType, count int64) {
	switch normaliseCommsType(string(commsType)) {
	case CommsTypeGeneralInquiry:
		s.GeneralInquiry = count
	case CommsTypeCustomerSupport:
		s.CustomerSupport = count
	case CommsTypeTechnicalSupport:
		s.TechnicalSupport = count
	case CommsTypeFeatureRequest:
		s.FeatureRequest = count
	case CommsTypeFeedback:
		s.Feedback = count
	case CommsTypeFeedbackCompanion:
		s.FeedbackCompanion = count
	case CommsTypeProductInformation:
		s.ProductInformation = count
	case CommsTypePressInquiry:
		s.PressInquiry = count
	case CommsTypePartnershipOpportunities:
		s.PartnershipOpportunities = count
	case CommsTypeComplaints:
		s.Complaints = count
	case CommsTypeWebsiteIssues:
		s.WebsiteIssues = count
	case CommsTypeDonatingSupportingUsQuestions:
		s.DonatingSupportingUsQuestions = count
	case CommsTypeOther:
		s.Other = count
	default:
		if s.Additional == nil {
			s.Additional = make(map[string]int64)
		}
		s.Additional[commsTypeStatsKey(commsType)] = count
	}
}

// AsMap returns every default and application-specific count using the
// established snake_case response keys.
func (s CommsTypeStats) AsMap() map[string]int64 {
	counts := map[string]int64{
		"general_inquiry":                  s.GeneralInquiry,
		"customer_support":                 s.CustomerSupport,
		"technical_support":                s.TechnicalSupport,
		"feature_request":                  s.FeatureRequest,
		"feedback":                         s.Feedback,
		"feedback_companion":               s.FeedbackCompanion,
		"product_information":              s.ProductInformation,
		"press_inquiry":                    s.PressInquiry,
		"partnership_opportunities":        s.PartnershipOpportunities,
		"complaints":                       s.Complaints,
		"website_issues":                   s.WebsiteIssues,
		"donating_supporting_us_questions": s.DonatingSupportingUsQuestions,
		"other":                            s.Other,
	}
	for key, count := range s.Additional {
		counts[key] = count
	}
	return counts
}

// MarshalJSON keeps custom type counts in the same flat by_type object as the
// default counts.
func (s CommsTypeStats) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.AsMap())
}

// UnmarshalJSON restores both default and custom type counts.
func (s *CommsTypeStats) UnmarshalJSON(data []byte) error {
	counts := make(map[string]int64)
	if err := json.Unmarshal(data, &counts); err != nil {
		return err
	}
	*s = CommsTypeStats{}
	for key, count := range counts {
		s.Set(CommsType(strings.ReplaceAll(key, "_", "-")), count)
	}
	return nil
}

func commsTypeStatsKey(commsType CommsType) string {
	return strings.ReplaceAll(string(normaliseCommsType(string(commsType))), "-", "_")
}

// CommsStatusStats holds status information for comms
type CommsStatusStats struct {
	ReachedOut    int64 `json:"reached_out"`
	NotReachedOut int64 `json:"not_reached_out"`
}
