package policy

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// PolicyType represents the different policies supported
//
// Add to the list as use case grows
type PolicyType string

const (
	// TermsOfServicePolicy represents a policy that hold terms policy
	TermsOfServicePolicy PolicyType = "TERMS_OF_SERVICE"
	// TermsOfServicePolicy represents policy that hold the privacy policy
	PrivacyPolicy PolicyType = "PRIVACY"
	// TermsOfServicePolicy represents policy that hold the cookie policy
	CookiesPolicy PolicyType = "COOKIES"
	// TermsOfServicePolicy represents policy that hold the refund policy
	RefundPolicy PolicyType = "REFUND"
)

// PolicySection respresents the components needed to create
// a section in a policy
type PolicySection struct {
	Header          string
	HeaderId        string
	HeaderWithIndex bool
	Paragraphs      []template.HTML
}

// WebAppPolicy represents the elements needed to construct a
// valid policy on the web app
type WebAppPolicy struct {
	Name                 string
	Type                 PolicyType
	LastUpdated          string
	Sections             []PolicySection
	TableOfContentsItems []TableOfContentsItem
}

// GetPolicyType returns the value give  type set for

// TableOfContentsItem is the header and its corresponding href
// reference (internal link)
type TableOfContentsItem struct {
	HeaderHref  string
	HeaderTitle string
}

// GetTableOfContentsItems handles looping over the sections provided
// and pulling out the headers to create the respective splice of headers
// and corresponding href references
func (w *WebAppPolicy) GetTableOfContentsItems() []TableOfContentsItem {
	tableItems := []TableOfContentsItem{}

	for _, section := range w.Sections {
		tableItems = append(tableItems, TableOfContentsItem{
			HeaderHref:  w.generateHeaderHref(section.Header),
			HeaderTitle: section.Header,
		})
	}
	return tableItems
}

// generateHeaderHref handles converting standard string into a string that can be used for
// href attribute
func (w *WebAppPolicy) generateHeaderHref(sectionName string) string {

	return fmt.Sprintf("#%s",
		toolbox.StripNonAlphanumericCharactersRegex(
			[]byte(strings.ReplaceAll(
				toolbox.StringStandardisedToLower(sectionName),
				" ",
				"-")), []byte("-")))
}