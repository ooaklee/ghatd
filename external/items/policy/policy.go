package policy

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"

	"github.com/PaesslerAG/jsonpath"
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
	Header          string          `json:"header"`
	HeaderId        string          `json:"header_id"`
	HeaderWithIndex bool            `json:"header_with_index"`
	Paragraphs      []template.HTML `json:"paragraphs"`
}

// WebAppPolicy represents the elements needed to construct a
// valid policy on the web app
type WebAppPolicy struct {
	Name                 string                `json:"name"`
	Type                 PolicyType            `json:"type"`
	LastUpdated          string                `json:"last_updated"`
	Sections             []PolicySection       `json:"sections"`
	TableOfContentsItems []TableOfContentsItem `json:"table_of_contents_items"`
}

// GetPolicyType returns the value give  type set for

// TableOfContentsItem is the header and its corresponding href
// reference (internal link)
type TableOfContentsItem struct {
	HeaderHref  string `json:"header_href"`
	HeaderTitle string `json:"header_title"`
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

// GetAttributeByJsonPath returns the value of the attribute at the given JSON path
// It marshals the User struct to JSON, then uses the jsonpath package to extract the value at the given path.
// If there is an error during the marshaling or jsonpath extraction, it returns the error.
func (w *WebAppPolicy) GetAttributeByJsonPath(jsonPath string) (any, error) {
	jsonDataByteAsMap := make(map[string]interface{})

	jsonDataByte, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(jsonDataByte, &jsonDataByteAsMap)
	if err != nil {
		return nil, err
	}

	result, err := jsonpath.Get(jsonPath, jsonDataByteAsMap)
	if err != nil {
		return nil, err
	}

	return result, nil
}
