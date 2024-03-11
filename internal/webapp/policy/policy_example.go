package policy

// NewGeneratedExamplePolicyRequest holds the items needed to generate a new policy
type NewGeneratedExamplePolicyRequest struct {
	ServiceName       string
	ServiceWebsite    string
	ServiceEmail      string
	LegalBusinessName string
}

// NewGeneratedExamplePolicy creates a new example web app policy
func NewGeneratedExamplePolicy(r *NewGeneratedExamplePolicyRequest) *WebAppPolicy {

	examplePolicy := WebAppPolicy{
		Name:        "Example",
		LastUpdated: "01 January, 1970",
		Sections:    []PolicySection{
			// {
			// 	Header:          `PLACEHOLDER`,
			// 	HeaderWithIndex: true,
			// 	HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("PLACEHOLDER"), " ", "-"),
			// 	Paragraphs: []template.HTML{

			// 		template.HTML(``),
			// 		template.HTML(fmt.Sprintf(`1. %s`, r.ServiceName)),
			// 	},
			// },
		},
	}

	// generate the table of contents based on the
	// sections passed in above
	examplePolicy.TableOfContentsItems = examplePolicy.GetTableOfContentsItems()

	return &examplePolicy
}
