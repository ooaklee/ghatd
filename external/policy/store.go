package policy

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// Store holds policy data
type Store struct {
	Policies []WebAppPolicy

	// BusinessEntityName is the name of the business entity
	BusinessEntityName string

	// BusinessEntityEmail is the email of the business entity
	BusinessEntityEmail string

	// BusinessEntityWebsite is the website of the business entity
	BusinessEntityWebsite string

	// LegalBusinessEntityName is the legal name of the business entity
	LegalBusinessEntityName string
}

// NewStore creates a new policy store
func NewStore(
	businessEntityName string,
	businessEntityEmail string,
	businessEntityWebsite string,
	legalBusinessEntityName string,
) *Store {
	return &Store{
		BusinessEntityName:      businessEntityName,
		BusinessEntityEmail:     businessEntityEmail,
		BusinessEntityWebsite:   businessEntityWebsite,
		LegalBusinessEntityName: legalBusinessEntityName,
	}
}

// GetPolicies returns the policies stored in the store
func (s *Store) GetPolicies() []WebAppPolicy {
	return s.Policies
}

// GenerateStaticPolicies generates the static policies
func (s *Store) GenerateStaticPolicies() {

	termsOfServicePolicy := WebAppPolicy{
		Name:        "Terms and Conditions",
		Type:        TermsOfServicePolicy,
		LastUpdated: "02 January, 2025",
		Sections: []PolicySection{
			{
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`These Terms and Conditions ("Terms") govern your relationship with <a href="%s" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">%s</a> website (the "Service").`, s.BusinessEntityWebsite, strings.Split(s.BusinessEntityWebsite, "//")[1])),
					template.HTML(`Please read these Terms carefully before using the Service. Your access to and use of the Service is conditioned on your acceptance of and compliance with these Terms. These Terms apply to all visitors, users, and others who access or use the Service.`),
					template.HTML(`By accessing or using the Service, you agree to be bound by these Terms. If you disagree with any part of the terms, then you may not access the Service.`),
				},
			},
			{
				Header:          `Contact Us`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Contact Us"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`If you have any questions about these Terms, please contact us:<br><br><ul>
						<li>&ensp;<span>&#8226;</span> Via email: <a href="mailto:%s?subject=About%%20your%%20terms"  class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">%s</a></li>
						<li>&ensp;<span>&#8226;</span> Via our <a href="/contact" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">contact page</a></li>
					</ul>`, s.BusinessEntityEmail, s.BusinessEntityEmail)),
				},
			},
		},
	}

	// generate the table of contents based on the
	// sections passed in above
	termsOfServicePolicy.TableOfContentsItems = termsOfServicePolicy.GetTableOfContentsItems()

	// add the terms of service policy to the policies slice
	s.Policies = append(s.Policies, termsOfServicePolicy)

	///////////////////////////////////////////////////////////////

	privacyPolicy := WebAppPolicy{
		Name:        "Privacy Policy",
		Type:        PrivacyPolicy,
		LastUpdated: "02 January, 2025",
		Sections: []PolicySection{
			{
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`<b>%s</b> ("us", "we", or "our") operates the <a href="%s" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">%s</a> website (the "Service").`, s.LegalBusinessEntityName, s.BusinessEntityWebsite, strings.Split(s.BusinessEntityWebsite, "//")[1])),
					template.HTML(`This Privacy Policy explains how we collect, use, and share your information when you use our Service. We are committed to protecting your privacy.`),
					template.HTML(`We use your data to provide and improve the Service. By using the Service, you agree to the collection and use of information in accordance with this policy. Unless otherwise defined in this Privacy Policy, terms used in this Privacy Policy have the same meanings as in our <a href="/policy/terms" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">Terms and Conditions</a>.`),
				},
			},
			{
				Header:          `Contact Us`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Contact Us"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`If you have any questions about this Privacy Policy, please contact us:<br><br><ul>
						<li>&ensp;<span>&#8226;</span> Via email: <a href="mailto:%s?subject=About%%20your%%20privacy%%20policy"  class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">%s</a></li>
						<li>&ensp;<span>&#8226;</span> Via our <a href="/contact" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">contact page</a></li>
					</ul>`, s.BusinessEntityEmail, s.BusinessEntityEmail)),
				},
			},
			{
				Header:          `PLACEHOLDER`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("PLACEHOLDER"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(``),
					template.HTML(fmt.Sprintf(`1. %s`, s.BusinessEntityName)),
				},
			},
		},
	}

	// generate the table of contents based on the
	// sections passed in above
	privacyPolicy.TableOfContentsItems = privacyPolicy.GetTableOfContentsItems()

	// add the privacy policy to the policies slice
	s.Policies = append(s.Policies, privacyPolicy)

	///////////////////////////////////////////////////////////////

	cookiePolicy := WebAppPolicy{
		Name:        "Cookie Policy",
		Type:        CookiesPolicy,
		LastUpdated: "02 January, 2025",
		Sections: []PolicySection{
			{
				Paragraphs: []template.HTML{
					template.HTML(fmt.Sprintf(`<b>%s</b> ("us", "we", or "our"), operator of <a href="%s" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">%s</a> (the "Service"), uses cookies and other similar technologies to provide, protect, and improve our Service.`, s.LegalBusinessEntityName, s.BusinessEntityWebsite, strings.Split(s.BusinessEntityWebsite, "//")[1])),
					template.HTML(`This Cookie Policy supplements our <a href="/policy/privacy" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100"> Privacy Policy</a> and <a href="/policy/terms" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100"> Terms and Conditions</a>, it explains how and why these technologies are used, as well as the choices available to you.`),
					template.HTML(`Note that you can change your preferences with the <b>Cookie Preferences</b> menu, located at the bottom of most pages.`),
				},
			},
			{
				Header:          `Contact Us`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Contact Us"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`If you have any questions about this Cookie Policy, please contact us:<br><br><ul>
						<li>&ensp;<span>&#8226;</span> Via email: <a href="mailto:%s?subject=About%%20your%%20cookie%%20policy"  class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">%s</a></li>
						<li>&ensp;<span>&#8226;</span> Via our <a href="/contact" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">contact page</a></li>
					</ul>`, s.BusinessEntityEmail, s.BusinessEntityEmail)),
				},
			},
			// {
			// 	Header:          `PLACEHOLDER`,
			// 	HeaderWithIndex: true,
			// 	HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("PLACEHOLDER"), " ", "-"),
			// 	Paragraphs: []template.HTML{

			// 		template.HTML(``),
			// 		template.HTML(fmt.Sprintf(`1. %s`, businessEntityName)),
			// 	},
			// },

		},
	}

	// generate the table of contents based on the
	// sections passed in above
	cookiePolicy.TableOfContentsItems = cookiePolicy.GetTableOfContentsItems()

	s.Policies = append(s.Policies, cookiePolicy)

}

// AddPolicy adds a new policy to the list of policies
func (s *Store) AddPolicy(policy WebAppPolicy) {
	s.Policies = append(s.Policies, policy)
}
