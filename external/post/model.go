package post

import (
	"encoding/xml"
	"strings"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// PostType represents the type of post
type PostType string

const (

	// PostTypeChangelog represents a changelog item
	PostTypeChangelog PostType = "changelog"

	// PostTypeArticle represents an article that'll often be used
	// to represent a blog post
	PostTypeArticle PostType = "article"

	// PostTypeFaq represents an faq entry
	PostTypeFaq PostType = "faq"

	// PostTypeGlossary represents a glossary item
	PostTypeGlossary PostType = "glossary"

	// PostTypeOther represents a other type of post
	PostTypeOther PostType = "other"
)

// TextFormat represents the format of the post
type TextFormat string

const (

	// TextFormatMarkdown represents the markdown text format
	TextFormatMarkdown TextFormat = "markdown"

	// TextFormatHtml represents the html text format
	TextFormatHtml TextFormat = "html"
)

// HeaderImageType represents the type of the header image
type HeaderImageType string

const (

	// HeaderImageTypeSvg represents the SVG header image
	HeaderImageTypeSvg HeaderImageType = "svg"

	// HeaderImageTypeUrl represents the header image that procided
	// in the form of a URL
	HeaderImageTypeUrl HeaderImageType = "url"
)

// SVG represents the structure of an inline SVG element with accessibility attributes.
// It is used to parse and validate SVG content for post header images, ensuring proper
// accessibility markup is present (role="img", title element, etc.).
//
// Expected format:
//
//	<svg role="img">
//	    <title>Descriptive Title Here</title>
//	    <use xlink:href="#some-icon"></use>
//	</svg>
type SVG struct {
	// XMLName is the XML element name, expected to be "svg"
	XMLName xml.Name `xml:"svg"`

	// Text contains any character data within the SVG element
	Text string `xml:",chardata"`

	// Role is the ARIA role attribute, should be "img" for accessibility
	Role string `xml:"role,attr"`

	// Title is the descriptive title element used as alt text alternative for screen readers
	Title string `xml:"title"`

	// Use represents the <use> element that references an icon or graphic
	Use struct {
		// Text contains any character data within the use element
		Text string `xml:",chardata"`

		// Href is the xlink:href attribute pointing to the referenced graphic
		Href string `xml:"href,attr"`
	} `xml:"use"`
}

// Post represents a post item from a user
//
//			{
//			    "id": "[SOME_UUID]",
//			    "nano_id": "[SOME_NANO_ID]",
//			    "url_friendly_id: "wrapping-up-q2-2025",
//			    "type": "article",
//		        "title": "Wrapping up: Q2 2025",
//			    "header_image: "<svg> || https://"
//	            "header_image_type": "url",
//			    "text_format": "markdown",
//			    "text": "We do our best to continuously\n\n# Conversion tracking\nShort links are great..",
//			    "visibility": [], # If empty, the will be visible to all
//			    "tags":   ["news", "overview"],
//			    "publish_date": "", # If blank, then will be 'draft'
//			    "publish_as": "", # if blank, pull information from created_by_id
//			    "created_by_id": "[SOME_UUID]",
//			    "created_at": "YYYY-MM-DDThh:mm:ss",
//			    "updated_by_id": "",
//			    "updated_at": "YYYY-MM-DDThh:mm:ss",
//			    "deleted_by_id": "",
//			    "deleted_at": "", # if date provided soft delete
//			}
type Post struct {

	// Id is the unique identifier for the post item
	Id string `json:"id" bson:"_id"`

	// NanoId is the nano ID for the post item
	NanoId string `json:"nano_id" bson:"_nano_id"`

	// UrlFriendlyId is a unique URL friendly id that is used to
	// identify a given post, it should have a its given type as
	// a prefix
	UrlFriendlyId string `json:"url_friendly_id" bson:"_url_friendly_id"`

	// Title is the title to describe the post item
	Title string `json:"title" bson:"title"`

	// HeaderImage is the source of the header image for the given post
	// not all post types will support header images
	HeaderImage string `json:"header_image,omitempty" bson:"header_image,omitempty"`

	// HeaderImageType is the type/format of the header image
	HeaderImageType HeaderImageType `json:"header_image_type,omitempty" bson:"header_image_type,omitempty"`

	// HeaderImageAltText is the alt text for the header image
	HeaderImageAltText string `json:"header_image_alt_text,omitempty" bson:"header_image_alt_text,omitempty"`

	// ProvidedType is the type of the post item provided by the user
	// if the type is not valid, it will be set to other and this field
	// will be set to the provided type
	ProvidedType string `json:"provided_type,omitempty" bson:"provided_type,omitempty"`

	// Type is the type of the post item
	Type PostType `json:"type" bson:"type"`

	// ProvidedTextFormat is the text format of the post item provided by the user
	// if the type is not valid, it will be set to markdown and this field
	// will be set to the provided text format
	ProvidedTextFormat string `json:"provided_text_format,omitempty" bson:"provided_text_format,omitempty"`

	// TextFormat is the format the post text will be written in, i.e. markdown
	TextFormat TextFormat `json:"text_format" bson:"text_format"`

	// Text is the body of text for the post
	Text string `json:"text" bson:"text"`

	// Tags are the categorisations that can be used to group the post further, i.e
	// announcements, bug-fix, product, exciting-news
	Tags []string `json:"tags,omitempty" bson:"tags,omitempty"`

	// Visibility outlines the persons who can see this post item, if blank, everyone will be
	// able to access the post item.
	// TODO: implement later
	Visibility []string `json:"visibility,omitempty" bson:"visibility,omitempty"`

	// PublishedAs is the override that should be used when specifying who published
	// the post item. If blank should use the CreatedByUserId
	PublishedAs string `json:"published_as,omitempty" bson:"published_as"`

	// PublishedAt is the date and time the post item was published, if
	// blank, then the post should be treated as a 'darft'
	PublishedAt string `json:"published_at,omitempty" bson:"published_at"`

	// PublishedByUserId is the ID of the user most recently published the post item
	PublishedByUserId string `json:"published_by_user_id,omitempty" bson:"published_by_user_id"`

	// CreatedAt is the date and time the post item was created
	CreatedAt string `json:"created_at" bson:"created_at"`

	// CreatedByUserId is the ID of the user who created the post item
	CreatedByUserId string `json:"created_by_user_id,omitempty" bson:"created_by_user_id"`

	// UpdatedAt is the date and time the post item was updated
	UpdatedAt string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`

	// UpdatedByUserId is the ID of the user who most recently updated the post item
	UpdatedByUserId string `json:"updated_by_user_id,omitempty" bson:"updated_by_user_id,omitempty"`

	// DeletedAt is the date and time the post item was deleted
	DeletedAt string `json:"deleted_at,omitempty" bson:"deleted_at"`

	// DeletedByUserId is the ID of the user who soft deleted the post item
	DeletedByUserId string `json:"deleted_by_user_id,omitempty" bson:"deleted_by_user_id"`
}

// PostOverview represents a lightweight overview of a post item
type PostOverview struct {

	// Id is the unique identifier for the post
	Id string `json:"id"`

	// Title is the title of the post
	Title string `json:"title"`

	// PublishedAt is the date and time the post was published
	PublishedAt string `json:"published_at"`

	// UpdatedAt is the date and time the post was last updated
	UpdatedAt string `json:"updated_at,omitempty"`

	// NotificationTitle is the title of the notification for the post
	NotificationTitle string `json:"notification_title,omitempty"`

	// Type is the type of the post item
	Type PostType `json:"type" bson:"type"`

	// UrlFriendlyId is the URL-friendly version of the post title
	UrlFriendlyId string `json:"url_friendly_id"`

	// Href is the generated relative URL to access the post
	Href string `json:"href"`

	// RevisionHash is the hash representing the revision of the post
	RevisionHash string `json:"revision_hash,omitempty"`
}

// ToOverview converts a Post to its Overview representation
func (p *Post) ToOverview() *PostOverview {
	overview := &PostOverview{
		Id:            p.Id,
		Title:         p.Title,
		PublishedAt:   p.PublishedAt,
		UpdatedAt:     p.UpdatedAt,
		Type:          p.Type,
		UrlFriendlyId: p.UrlFriendlyId,
	}

	var href string
	var notificationTitle string
	switch p.Type {
	case PostTypeChangelog:
		href = "/whats-new"
		notificationTitle = "New: Platform Updates"
	case PostTypeFaq:
		notificationTitle = "New: FAQ"
		href = "/faq#" + p.UrlFriendlyId
	case PostTypeGlossary:
		notificationTitle = "New: Glossary Terms"
		href = "/glossary#" + p.UrlFriendlyId
	case PostTypeArticle:
		notificationTitle = "New: Blog Posts"
		href = "/blog" + "/" + strings.Replace(p.UrlFriendlyId, string(PostTypeArticle)+"-", "", 1)
	}

	overview.Href = href
	overview.NotificationTitle = notificationTitle

	// Generate revision hash
	revisionHashSource := strings.Join([]string{
		overview.Id,
		string(overview.Type),
		overview.Title,
		overview.PublishedAt,
		overview.UpdatedAt,
	}, "|")

	overview.RevisionHash = GenerateSha256Hash(revisionHashSource)

	return overview
}

// ToNotificationOverview converts a Post to a NotificationOverview representation
func (p *Post) ToNotificationOverview() *common.NotificationOverview {
	overview := p.ToOverview()
	if overview == nil {
		return nil
	}

	return &common.NotificationOverview{
		ID:                overview.Id,
		Source:            common.NotificationSourcePost,
		Kind:              notificationKindForPostType(overview.Type),
		Title:             overview.Title,
		NotificationTitle: overview.NotificationTitle,
		OccurredAt:        overview.PublishedAt,
		UpdatedAt:         overview.UpdatedAt,
		Href:              overview.Href,
		RevisionHash:      overview.RevisionHash,
		Metadata: map[string]interface{}{
			"type":            overview.Type,
			"url_friendly_id": overview.UrlFriendlyId,
		},
	}
}

// notificationKindForPostType maps a PostType to a common.NotificationKind for use in notifications related to posts
func notificationKindForPostType(postType PostType) common.NotificationKind {
	switch postType {
	case PostTypeArticle:
		return common.NotificationKindPostArticle
	case PostTypeChangelog:
		return common.NotificationKindPostChangelog
	case PostTypeFaq:
		return common.NotificationKindPostFaq
	case PostTypeGlossary:
		return common.NotificationKindPostGlossary
	default:
		return common.NotificationKind("post_" + strings.ToLower(string(postType)))
	}
}

// SetPostTextFormat takes string,sanitise, and set correct post item text format
func (c *Post) SetPostTextFormat(providedTextFmt string) *Post {

	var err error

	// make provided type kebab case
	providedTextFmt, err = toolbox.StringConvertToKebabCase(providedTextFmt)
	if err != nil {
		providedTextFmt = strings.ReplaceAll(
			strings.ToLower(providedTextFmt),
			" ",
			"-",
		)
	}

	// convert to post item type
	textFormat := TextFormat(providedTextFmt)

	// make sure the post item type is valid
	if textFormat != TextFormatMarkdown &&
		textFormat != TextFormatHtml {
		c.ProvidedType = providedTextFmt
		textFormat = TextFormatMarkdown
	}

	c.TextFormat = textFormat

	return c
}

// SetPostType take string, sanitise, and set correct post item type
func (c *Post) SetPostType(providedType string) *Post {

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

	// convert to post item type
	postType := PostType(providedType)

	// make sure the post item type is valid
	if postType != PostTypeChangelog &&
		postType != PostTypeFaq &&
		postType != PostTypeArticle &&
		postType != PostTypeOther &&
		postType != PostTypeGlossary {
		postType = PostTypeOther
		c.ProvidedType = providedType
	}

	c.Type = postType

	return c
}

// SetHeaderImageType checks the provided header image and
// set correct header image type
func (c *Post) SetHeaderImageType() (*Post, error) {

	if c.Type != PostTypeArticle {
		c.HeaderImageType = ""

		return c, nil
	}

	standardisedHeaderImage := strings.TrimSpace(
		toolbox.StringStandardisedToLower(c.HeaderImage),
	)

	if strings.HasPrefix(standardisedHeaderImage, "http") || strings.HasPrefix(standardisedHeaderImage, "/") {
		c.HeaderImageType = HeaderImageTypeUrl
		return c, nil
	}

	if strings.HasPrefix(standardisedHeaderImage, "<svg") &&
		strings.HasSuffix(standardisedHeaderImage, "</svg>") {
		c.HeaderImageType = HeaderImageTypeSvg
		return c, nil
	}

	return c, ErrPostInvalidHeaderImageFailedTypeAssigment
}

// ValidateHeaderImageHasRequiredAltTextAlternativeElementsForInlineSvg validates that the header image (if SVG) has required
// alt text alternative elements for accessibility
// more info https://stackoverflow.com/q/4697100
// expected format
//
// <svg role="img">
//
//	<title>Descriptive Title Here</title>
//	<use xlink:href="#some-icon"></use>
//
// </svg>
func (c *Post) ValidateHeaderImageHasRequiredAltTextAlternativeElementsForInlineSvg() error {

	if c.HeaderImageType != HeaderImageTypeSvg {
		return nil
	}

	var svgContent SVG
	err := xml.Unmarshal([]byte(c.HeaderImage), &svgContent)
	if err != nil {
		return ErrPostInvalidHeaderImageFailedSvgUnmarshal
	}

	if svgContent.Role != "img" {
		return ErrPostInvalidHeaderImageMissingRoleImgAttribute
	}

	if strings.TrimSpace(svgContent.Title) == "" {
		return ErrPostInvalidHeaderImageMissingTitleElement
	}

	return nil
}

// GenerateNanoId generates a new Nano Id for the post item
func (c *Post) GenerateNanoId() *Post {

	c.NanoId = toolbox.GenerateNanoId()

	return c
}

// GenerateId generates a new Id for the post item
func (c *Post) GenerateId() *Post {

	c.Id = toolbox.GenerateUuidV4()

	return c
}

// GenerateUrlFriendlyId generate a new URL friendly id for the post item, we
// should use the post type as a prefix
func (c *Post) GenerateUrlFriendlyId() *Post {

	var (
		urlFriendlyIdComponents []string
		rawTitle                string
	)

	urlFriendlyIdComponents = append(
		urlFriendlyIdComponents,
		string(c.Type),
	)

	// get post title, strip all special character, make lowercase,
	// swap space for hyphen
	rawTitle = c.Title
	rawTitle = strings.TrimSpace(rawTitle)
	rawTitle = toolbox.NonAlphanumericRegexEnglishAlphabetStringsRegex.ReplaceAllString(rawTitle, "")
	rawTitle = toolbox.StringStandardisedToLower(rawTitle)
	standardisedRawTitleComponents := strings.Split(rawTitle, " ")

	urlFriendlyIdComponents = append(
		urlFriendlyIdComponents,
		standardisedRawTitleComponents...,
	)

	c.UrlFriendlyId = strings.Join(
		urlFriendlyIdComponents,
		"-",
	)

	return c
}

// SetCreatedAtTimeToNow sets the created at date and time for the post item to now
func (c *Post) SetCreatedAtTimeToNow() *Post {

	c.CreatedAt = toolbox.TimeNowUTC()

	return c
}

// SetUpdatedAtTimeToNow sets the updated at date and time for the post item to now
func (c *Post) SetUpdatedAtTimeToNow() *Post {

	c.UpdatedAt = toolbox.TimeNowUTC()

	return c
}

// SetDeletedAtTimeToNow sets the deleted at date and time for the post item to now
// if set, the the post is soft deleted
func (c *Post) SetDeletedAtTimeToNow() *Post {

	c.DeletedAt = toolbox.TimeNowUTC()

	return c
}
