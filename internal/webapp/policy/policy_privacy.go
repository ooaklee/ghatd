package policy

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/toolbox"
)

// NewGeneratedPrivacyPolicyRequest holds the items needed to generate a new
// privacy policy
type NewGeneratedPrivacyPolicyRequest struct {
	ServiceName       string
	ServiceWebsite    string
	ServiceEmail      string
	LegalBusinessName string
}

// NewGeneratedPrivacyPolicy creates a new web app policy for privacy
func NewGeneratedPrivacyPolicy(r *NewGeneratedPrivacyPolicyRequest) *WebAppPolicy {

	termsOfServicePolicy := WebAppPolicy{
		Name:        "Privacy Policy",
		LastUpdated: "03 January, 2024",
		Sections: []PolicySection{
			{
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`<b>%s</b> ("us", "we", or "our") operates the <a href="%s">%s</a> website (the "Service").`, r.LegalBusinessName, r.ServiceWebsite, r.ServiceWebsite)),
					template.HTML(fmt.Sprintf(`This page informs you of our policies regarding the collection, use, and disclosure of personal data when you use our Service and the choices you have associated with that data. Our Privacy Policy for %s is created with the help of the Free Privacy Policy Generator.`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`We use your data to provide and improve the Service. By using the Service, you agree to the collection and use of information in accordance with this policy. Unless otherwise defined in this Privacy Policy, terms used in this Privacy Policy have the same meanings as in our Terms and Conditions, accessible from <a href="/terms">%s/terms</a>.`, r.ServiceWebsite)),
				},
			},
			{
				Header:          `Information Collection And Use`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Information Collection And Use"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`We collect several different types of information for various purposes to provide and improve our Service to you.`),
				},
			},
			{
				Header:          `Types of Data Collected`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Types of Data Collected"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`Personal Data While using our Service, we may ask you to provide us with certain personally identifiable information that can be used to contact or identify you ("Personal Data"). Personally identifiable information may include, but is not limited to:`),
					template.HTML(`<b><code>Email address</code></b>, <b><code>First name</code></b> and <b><code>last name</code></b>, <b><code>Phone number</code></b>, <b><code>Address</code></b>, <b><code>State</code></b>, <b><code>Province</code></b>, <b><code>ZIP/Postal code</code></b>, <b><code>City Cookies</code></b> and <b><code>Usage Data</code></b>. We may also collect information how the Service is accessed and used ("Usage Data"). This Usage Data may include information such as your computer's Internet Protocol address (e.g. IP address), browser type, browser version, the pages of our Service that you visit, the time and date of your visit, the time spent on those pages, unique device identifiers and other diagnostic data.`),
				},
			},
			{
				Header:          `Tracking And Cookies Data`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Tracking And Cookies Data"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`We use cookies and similar tracking technologies to track the activity on our Service and hold certain information.`),
					template.HTML(`Cookies are files with small amount of data which may include an anonymous unique identifier. Cookies are sent to your browser from a website and stored on your device. Tracking technologies also used are beacons, tags, and scripts to collect and track information and to improve and analyze our Service.`),
					template.HTML(`You can instruct your browser to refuse all cookies or to indicate when a cookie is being sent. However, if you do not accept cookies, you will not be able to use large portions of our Service.`),
					template.HTML(`Examples of Cookies we use:`),
					template.HTML(`<b><code>Session Cookies</code></b>. We use Session Cookies to operate our Service. <b><code>Preference Cookies</code></b>. We use Preference Cookies to remember your preferences and various settings. <b><code>Security Cookies</code></b>. We use Security Cookies for security purposes.`),
				},
			},
			{
				Header:          `Use of Data`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Use of Data"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`%s uses the collected data for various purposes:`, r.ServiceName)),
					template.HTML(`<ol>
					<li class="font-medium">• To provide and maintain the Service </li>
					<li class="font-medium">• To notify you about changes to our Service </li>
					<li class="font-medium">• To allow you to participate in interactive features of our Service when you choose to do so </li>
					<li class="font-medium">• To provide customer care and support</li>
					<li class="font-medium">• To provide analysis or valuable information so that we can improve the Service</li>
					<li class="font-medium">• To monitor the usage of the Service</li>
					<li class="font-medium">• To detect, prevent and address technical issues</li>
					</ol>`),
				},
			},
			{
				Header:          `Transfer Of Data`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Transfer Of Data"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`Your information, including Personal Data, may be transferred to — and maintained on — computers located outside of your state, province, country or other governmental jurisdiction where the data protection laws may differ than those from your jurisdiction.`),
					template.HTML(`If you are located outside United Kingdom (UK) and choose to provide information to us, please note that we transfer the data, including Personal Data, to United Kingdom (UK) and process it there.`),
					template.HTML(`Your consent to this Privacy Policy followed by your submission of such information represents your agreement to that transfer.`),
					template.HTML(fmt.Sprintf(`%s will take all steps reasonably necessary to ensure that your data is treated securely and in accordance with this Privacy Policy and no transfer of your Personal Data will take place to an organisation or a country unless there are adequate controls in place including the security of your data and other personal information.`, r.ServiceName)),
				},
			},
			{
				Header:          `Disclosure Of Data`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Disclosure Of Data"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`Legal Requirements %s may disclose your Personal Data in the good faith belief that such action is necessary to:`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`<li class="font-medium">To comply with a legal obligation</li> 
					<li class="font-medium">To protect and defend the rights or property of %s</li>
					<li class="font-medium">To prevent or investigate possible wrongdoing in connection with the Service</li>
					<li class="font-medium">To protect the personal safety of users of the Service or the public</li>
					<li class="font-medium">To protect against legal liability</li>`, r.ServiceName)),
				},
			},
			{
				Header:          `Security Of Data`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Security Of Data"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`The security of your data is important to us, but remember that no method of transmission over the Internet, or method of electronic storage is 100% secure. While we strive to use commercially acceptable means to protect your Personal Data, we cannot guarantee its absolute security.`),
				},
			},
			{
				Header:          `Service Providers`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Service Providers"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`We may employ third-party companies and individuals to facilitate our Service ("Service Providers"), provide the Service on our behalf, perform Service-related services, or assist us in analyzing how our Service is used.`),
					template.HTML(`These third parties have access to your Personal Data only to perform these tasks on our behalf and are obligated not to disclose or use it for any other purpose.`),
				},
			},
			{
				Header:          `Analytics`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Analytics"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`We may use third-party Service Providers to monitor and analyse the use of our Service. These service providers include:`),
					template.HTML(`<h3 id="new-relic" class="text-2xl font-semibold">New Relic</h3>
					<p class="font-medium">As of the writing of this policy, New Relic does not offer a solution for you to opt-out.</p><br>
					<p class="font-medium">For more information on their policy, visit their policy page: <a class="text-primary font-bold" href="https://docs.newrelic.com/docs/security/security-privacy/data-privacy/data-privacy-new-relic/">https://docs.newrelic.com/docs/security/security-privacy/data-privacy/data-privacy-new-relic/</a></p>`),
					template.HTML(`<h3 id="google-analytics-by-google" class="text-2xl font-semibold">Google Analytics By Google</h3>
					<p class="font-medium">You can prevent Google from using your information for analytics purposes by opting-out. To opt-out of Google Analytics service, please visit this page: <a class="text-primary font-bold" href="https://tools.google.com/dlpage/gaoptout">https://tools.google.com/dlpage/gaoptout</a></p><br>
					<p class="font-medium">For more information on what type of information Google Analytics collects, please visit their Terms page: <a class="text-primary font-bold" href="https://marketingplatform.google.com/about/analytics/terms/us/">https://marketingplatform.google.com/about/analytics/terms/us/</a></p>`),
				},
			},
			{
				Header:          `Links To Other Sites`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Links To Other Sites"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`Our Service may contain links to other sites that are not operated by us. If you click on a third party link, you will be directed to that third party's site. We strongly advise you to review the Privacy Policy of every site you visit.`),
					template.HTML(`We have no control over and assume no responsibility for the content, privacy policies or practices of any third party sites or services.`),
				},
			},
			{
				Header:          `Children's Privacy`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Children-s Privacy"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`Our Service does not address anyone under the age of 18 ("Children").
					We do not knowingly collect personally identifiable information from anyone under the age of 18. If you are a parent or guardian and you are aware that your Children has provided us with Personal Data, please contact us. If we become aware that we have collected Personal Data from children without verification of parental consent, we take steps to remove that information from our servers.
					`),
				},
			},
			{
				Header:          `Changes To This Privacy Policy`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Changes To This Privacy Policy"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`We may update our Privacy Policy from time to time. We will notify you of any changes by posting the new Privacy Policy on this page.`),
					template.HTML(`We will let you know via email and/or a prominent notice on our Service, prior to the change becoming effective and update the "effective date" at the top of this Privacy Policy.`),
					template.HTML(`You are advised to review this Privacy Policy periodically for any changes. Changes to this Privacy Policy are effective when they are posted on this page.`),
				},
			},
			{
				Header:          `Contact Us`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Contact Us"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`If you have any questions about this Privacy Policy, please contact us:`),
					template.HTML(fmt.Sprintf(`By email:  <a href="mailto:%s?subject=About%%20your%%20privacy%%20policy">%s</a> `, r.ServiceEmail, r.ServiceEmail)),
				},
			},
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
	termsOfServicePolicy.TableOfContentsItems = termsOfServicePolicy.GetTableOfContentsItems()

	return &termsOfServicePolicy
}
