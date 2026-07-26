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

	///////////////////////////////////////////////////////////////

	securityAndCompliancePolicy := WebAppPolicy{
		Name:        "Security and Compliance",
		Type:        SecurityAndCompliancePolicy,
		LastUpdated: "31 March, 2026",
		Sections: []PolicySection{
			{
				Paragraphs: []template.HTML{
					template.HTML(`Effective 31 March, 2026`),
					template.HTML(`This document summarises our approach to protecting the security, privacy, integrity, and availability of the data you entrust to us.`),
					template.HTML(`This policy is intended as a starting point for services built with GHATD. Each service should update it to reflect its actual deployment model, infrastructure providers, data retention practices, and operational controls.`),
				},
			},
			{
				Header:          `Deployment and Infrastructure`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Deployment and Infrastructure"), " ", "-"),
				Paragraphs: []template.HTML{
					template.HTML(`GHATD is designed around a one-binary, deploy-anywhere model. A service built with GHATD may run on dedicated servers, virtual machines, containers, serverless platforms, managed cloud services, or a combination of these depending on the needs of the operator.`),
					template.HTML(`Where third-party infrastructure providers are used, they may support areas such as DNS, DDoS protection, hosting, databases, object storage, email delivery, session management, caching, queues, monitoring, and backups.`),
				},
			},
			{
				Header:          `Authentication`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Authentication"), " ", "-"),
				Paragraphs: []template.HTML{
					template.HTML(`The Service may support authentication methods such as email magic links, OAuth providers, password-less login, or other mechanisms configured by the operator. Authentication options should be updated in this policy to match the live Service.`),
				},
			},
			{
				Header:          `Session Management`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Session Management"), " ", "-"),
				Paragraphs: []template.HTML{
					template.HTML(`Session state is managed using the Service's configured authentication and storage systems. Depending on the deployment, this may include secure cookies, signed tokens, a database, an in-memory store, Upstash, Redis-compatible services, or another session backend.`),
				},
			},
			{
				Header:          `Data Protection`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Data Protection"), " ", "-"),
				Paragraphs: []template.HTML{
					template.HTML(`We use reasonable technical and organisational measures to protect personal information and service data. These measures may include encryption in transit, access controls, audit logging, backup procedures, and provider-level security controls where managed infrastructure is used.`),
					template.HTML(`Data deletion, retention, and backup schedules depend on the Service's configuration and should be documented according to the systems actually in use.`),
				},
			},
			{
				Header:          `Security Practices`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Security Practices"), " ", "-"),
				Paragraphs: []template.HTML{
					template.HTML(`<ul>
						<li>&ensp;<span>&#8226;</span> <b>Operational Access</b>: Access to production systems should be limited to authorised personnel who need it to operate, maintain, or secure the Service.</li>
						<li>&ensp;<span>&#8226;</span> <b>Testing</b>: Changes should be tested before release where practical, especially for authentication, billing, data access, and other critical workflows.</li>
						<li>&ensp;<span>&#8226;</span> <b>Provider Review</b>: When managed services are used, operators should review their security, privacy, and compliance documentation.</li>
					</ul>`),
				},
			},
			{
				Header:          `Third-Party Providers`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Third-Party Providers"), " ", "-"),
				Paragraphs: []template.HTML{
					template.HTML(`A GHATD service may use third-party providers where appropriate. Example providers and trust resources include:<br><br><ul>
						<li>&ensp;<span>&#8226;</span> <b>Cloudflare</b>: <a href="https://www.cloudflare.com/trust-hub/compliance-resources/" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">Compliance Resources</a></li>
						<li>&ensp;<span>&#8226;</span> <b>Hetzner</b>: <a href="https://www.hetzner.com/unternehmen/zertifizierung/" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">Certification Information</a></li>
						<li>&ensp;<span>&#8226;</span> <b>Amazon Web Services (AWS)</b>: <a href="https://aws.amazon.com/compliance/programs/" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">Compliance Programs</a></li>
						<li>&ensp;<span>&#8226;</span> <b>MongoDB Atlas</b>: <a href="https://www.mongodb.com/cloud/trust/compliance" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">Compliance Resources</a></li>
						<li>&ensp;<span>&#8226;</span> <b>Upstash</b>: <a href="https://upstash.com/docs/common/help/compliance" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">Compliance Information</a></li>
					</ul>`),
					template.HTML(`For details on third-party services that may receive personal information, please refer to our <a href="/policy/privacy" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">Privacy Policy</a>.`),
				},
			},
			{
				Header:          `Contact Us`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Contact Us"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{
					template.HTML(fmt.Sprintf(`If you have any questions about this Security and Compliance document, please contact us:<br><br><ul>
						<li>&ensp;<span>&#8226;</span> Via email: <a href="mailto:%s?subject=About%%20your%%20security%%20and%%20compliance%%20policy"  class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">%s</a></li>
						<li>&ensp;<span>&#8226;</span> Via our <a href="/contact" class="text-primary opacity-80 font-bold hover:cursor-pointer hover:opacity-100">contact page</a></li>
					</ul>`, s.BusinessEntityEmail, s.BusinessEntityEmail)),
				},
			},
		},
	}

	// generate the table of contents based on the
	// sections passed in above
	securityAndCompliancePolicy.TableOfContentsItems = securityAndCompliancePolicy.GetTableOfContentsItems()

	s.Policies = append(s.Policies, securityAndCompliancePolicy)

}

// AddPolicy adds a new policy to the list of policies
func (s *Store) AddPolicy(policy WebAppPolicy) {
	s.Policies = append(s.Policies, policy)
}

// RemovePolicyByType removes the first policy with the provided type.
// It reports whether a matching policy was found.
func (s *Store) RemovePolicyByType(policyType PolicyType) bool {
	for i, policy := range s.Policies {
		if policy.Type == policyType {
			s.removePolicyAt(i)
			return true
		}
	}

	return false
}

// RemovePolicyByName removes the first policy with the provided name.
// Names are normalised using the same rules as Service.GetPolicyByName.
// It reports whether a matching policy was found.
func (s *Store) RemovePolicyByName(policyName string) bool {
	standardisedName := standardisePolicyName(policyName)

	for i, policy := range s.Policies {
		if standardisePolicyName(policy.Name) == standardisedName {
			s.removePolicyAt(i)
			return true
		}
	}

	return false
}

// removePolicyAt removes the policy at index while preserving the order of the remaining policies.
func (s *Store) removePolicyAt(index int) {
	copy(s.Policies[index:], s.Policies[index+1:])
	s.Policies[len(s.Policies)-1] = WebAppPolicy{}
	s.Policies = s.Policies[:len(s.Policies)-1]
}
