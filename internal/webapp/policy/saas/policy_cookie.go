package saas

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/toolbox"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/webapp/policy"
)

// NewGeneratedCookiePolicyRequest holds the items needed to generate a new
// cookie policy
type NewGeneratedCookiePolicyRequest struct {
	ServiceName       string
	ServiceWebsite    string
	ServiceEmail      string
	LegalBusinessName string
}

// NewGeneratedCookiePolicy creates a new web app policy for the cookies
func NewGeneratedCookiePolicy(r *NewGeneratedCookiePolicyRequest) *policy.WebAppPolicy {

	cookiePolicy := policy.WebAppPolicy{
		Name:        "Cookie Policy",
		LastUpdated: "03 January, 2024",
		Sections: []policy.PolicySection{
			{
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`<b>%s</b>, the operator of the website <a href="%s">%s</a> and <b>%s</b> platform, uses various technologies such as cookies, mobile identifiers, tracking URLs, log data, and other similar tools to help provide, protect, and enhance the %s Platform. `, r.LegalBusinessName, r.ServiceWebsite, r.ServiceWebsite, r.ServiceName, r.ServiceName)),
					template.HTML(fmt.Sprintf(`This Cookie Policy supplements <a href="/privacy-policy">the %s Privacy Policy</a> and explains how and why these technologies are used, as well as the choices available to you.`, r.ServiceName)),
				},
			},
			{

				Header:          `Purpose of Using These Technologies`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Purpose of Using These Technologies"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`The purpose of using these technologies includes enabling, facilitating, and streamlining the functioning of the %s Platform and its complementing services. They are also used to monitor and analyze the performance, operation, and effectiveness of the %s Platform, enforce legal agreements that govern the use of the platform, detect and prevent fraud, ensure trust and safety, and conduct investigations. Moreover, they are used for purposes of customer support, analytics, research, product development, regulatory compliance, serving tailored advertising, and showing you content (e.g., advertisements) that is more relevant to you.
					`, r.ServiceName, r.ServiceName)),
				},
			},
			{

				Header:          `Cookies`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Cookies"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`When you visit the %s Platform, cookies may be placed on your device. Cookies are small text files that websites send to your computer or other Internet-connected device to identify your browser uniquely or to store information or settings in your browser. Cookies allow us to recognize you when you return and provide you with a customized experience. In many cases, the information collected using cookies and similar tools is only used in a non-identifiable manner without reference to personal information. However, in some cases, the information we collect using cookies and other technology is associated with your personal information.`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`There are three types of cookies used on the %s Platform: <b><code>Session Cookies</code></b>, <b><code>Persistent Cookies</code></b> and <b><code>Preference Cookies</code></b>. Session cookies expire when you close your browser, while persistent cookies remain on your device after you close your browser and can be used again the next time you access the %s Platform. Preference cookies allow us to remember your preferences and various settings`, r.ServiceName, r.ServiceName)),
				},
			},
			{

				Header:          `Managing Your Cookie Preferences`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Managing Your Cookie Preferences"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`You can manage your cookie preferences and opt-out of having cookies and other data collection technologies used by adjusting the settings on your browser. However, please note that if you choose to remove or reject cookies or clear local storage, this could affect the features, availability, and functionality of the %s Platform.`, r.ServiceName)),
				},
			},
			{

				Header:          `Pixel Tags, Web Beacons, and Tracking URLs`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Pixel Tags- Web Beacons- and Tracking URLs"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`We may also use other technologies such as pixel tags, web beacons, and tracking URLs to determine whether you performed a specific action. These tools help us measure response to our communications and improve our web pages and your user experience.`),
				},
			},
			{

				Header:          `Server Logs and Other Technologies`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Server Logs and Other Technologies"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`Additionally, we collect information from server logs and other technologies such as the device you use to access the %s Platform, your operating system type, browser type, domain, and other system settings, and the country and time zone where your device is located. Our server logs also record the IP address of the device you use to connect to the Internet. We may also collect information about the website you were visiting before you came to the %s Platform and the website you visit after you leave the %s Platform.`, r.ServiceName, r.ServiceName, r.ServiceName)),
				},
			},
			{

				Header:          `Device Information`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Device Information"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`We may use device-related information to authenticate users, associate you with different devices that you may use to access our content, including for fraud-protection purposes.`),
				},
			},
			{

				Header:          `Third Parties`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Third Parties"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`%s permits third parties to collect the information described above through our Service and discloses such information to third parties for business purposes as described in this Privacy Policy, including but not limited to providing advertising on our Service and elsewhere based on users’ online activities over time and across different sites, services, devices.`, r.ServiceName)),
				},
			},
			{
				Header:          `Contact Us`,
				HeaderWithIndex: true,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Contact Us"), " ", "-"),
				Paragraphs: []template.HTML{

					template.HTML(`If you have any questions about this Cookie Policy, please contact us:`),
					template.HTML(fmt.Sprintf(`By email:  <a href="mailto:%s?subject=About%%20your%%20cookie%%20policy">%s</a> `, r.ServiceEmail, r.ServiceEmail)),
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
	cookiePolicy.TableOfContentsItems = cookiePolicy.GetTableOfContentsItems()

	return &cookiePolicy
}
