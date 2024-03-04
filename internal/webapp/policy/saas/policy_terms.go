package saas

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/toolbox"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/webapp/policy"
)

// NewGeneratedTermsPolicyRequest holds the items needed to generate a new
// terms and condition policy
type NewGeneratedTermsPolicyRequest struct {
	ServiceName       string
	ServiceWebsite    string
	ServiceEmail      string
	LegalBusinessName string
}

// NewGeneratedTermsPolicy creates a new web app policy for terms and conditions
func NewGeneratedTermsPolicy(r *NewGeneratedTermsPolicyRequest) *policy.WebAppPolicy {

	termsOfServicePolicy := policy.WebAppPolicy{
		Name:        "Terms and Conditions",
		LastUpdated: "03 January, 2024",
		Sections: []policy.PolicySection{
			{
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`These Terms and Conditions ("Terms", "Terms and Conditions") govern your relationship
				with <a href="%s">%s</a> website
				(the "Service") operated by %s ("us", "we", or "our").`, r.ServiceWebsite, r.ServiceWebsite, r.LegalBusinessName)),
					template.HTML(`Please read these Terms and Conditions carefully before using the Service.`),
					template.HTML(`Your access to and use of the Service is conditioned on your acceptance of and
				compliance with these Terms. These Terms apply to all visitors, users and others who
				access or use the Service.`),
					template.HTML(`By accessing or using the Service you agree to be bound by these Terms. If you
				disagree with any part of the terms then you may not access the Service.`),
				},
			},
			{
				Header:          "Subscriptions",
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Subscriptions"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`Some parts of the Service are billed on a subscription basis ("Subscription(s)"). You
				will be billed in advance on a recurring and periodic basis ("Billing Cycle").
				Billing cycles are set either on a monthly or annual basis, depending on the type of
				subscription plan you select when purchasing a Subscription.`),
					template.HTML(fmt.Sprintf(`At the end of each Billing Cycle, your Subscription will automatically renew under
				the exact same conditions unless you cancel it or %s cancels it. You may
				cancel your Subscription renewal either through your online account management page
				or by contacting %s customer support team.`, r.ServiceName, r.ServiceName)),
					template.HTML(fmt.Sprintf(`A valid payment method, including credit card, is required to process the payment for
				your Subscription. You shall provide %s with accurate and complete
				billing information including full name, address, state, zip code, telephone number,
				and a valid payment method information. By submitting such payment information, you
				automatically authorize %s to charge all Subscription fees incurred
				through your account to any such payment instruments.`, r.ServiceName, r.ServiceName)),
					template.HTML(fmt.Sprintf(`Should automatic billing fail to occur for any reason, %s will issue an
				electronic invoice indicating that you must proceed manually, within a certain
				deadline date, with the full payment corresponding to the billing period as
				indicated on the invoice.`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`It's strictly forbidden to create multiple paid accounts in order to circumvent
				limitations and quotas of paid subscriptions.`)),
				},
			},
			{
				Header:          `Free Trial`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Free Trial"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`%s may, at its sole discretion, offer a Subscription with a free trial for
					a limited period of time ("Free Trial").`, r.ServiceName)),
					template.HTML(`You may be required to enter your billing information in order to sign up for the
					Free Trial.`),
					template.HTML(fmt.Sprintf(`If you do enter your billing information when signing up for the Free Trial, you will
					not be charged by %s until the Free Trial has expired. On the last day of
					the Free Trial period, unless you cancelled your Subscription, you will be
					automatically charged the applicable Subscription fees for the type of Subscription
					you have selected.`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`At any time and without notice, %s reserves the right to (i) modify the
					terms and conditions of the Free Trial offer, or (ii) cancel such Free Trial offer.`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`It's strictly forbidden to create multiple accounts in order to circumvent
					limitations induced by the free trial.`)),
				},
			},
			{
				Header:          `Fee Changes`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Fee Changes"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`%s, in its sole discretion and at any time, may modify the Subscription
					fees for the Subscriptions. Any Subscription fee change will become effective at the
					end of the then-current Billing Cycle.`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`%s will provide you with a reasonable prior notice of any change in
					Subscription fees to give you an opportunity to terminate your Subscription before
					such change becomes effective.`, r.ServiceName)),
					template.HTML(`Your continued use of the Service after the Subscription fee change comes into effect
					constitutes your agreement to pay the modified Subscription fee amount.`),
				},
			},
			{
				Header:          `Refunds`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Refunds"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`Certain refund requests for Subscriptions may be considered by %s on a
					case-by-case basis and granted in sole discretion of %s.`, r.ServiceName, r.ServiceName)),
				},
			},
			{
				Header:          `Accounts`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Accounts"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`When you create an account with us, you must provide us information that is accurate,
					complete, and current at all times. Failure to do so constitutes a breach of the
					Terms, which may result in immediate termination of your account on our Service.`),
					template.HTML(`It is your responsibility to ensure the security of your account and any activities or actions that take place under your account, whether you access the Service through our platform or any third-party service. This includes accessing the Service via an API token or any other form of authentication.`),
					template.HTML(`You agree not share your API token(s) or account access with anyone. You must notify us
                    immediately upon becoming aware of any breach of security or unauthorized use of
                    your account.`),
				},
			},
			{
				Header:          `Links To Other Web Sites`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Links To Other Web Sites"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`Our Service may contain links to third-party web sites or services that are not owned
					or controlled by %s.`, r.ServiceName)),
					template.HTML(fmt.Sprintf(`%s has no control over, and assumes no responsibility for, the content,
					privacy policies, or practices of any third party web sites or services. You further
					acknowledge and agree that %s shall not be responsible or liable,
					directly or indirectly, for any damage or loss caused or alleged to be caused by or
					in connection with use of or reliance on any such content, goods or services
					available on or through any such web sites or services.`, r.ServiceName, r.ServiceName)),
					template.HTML(`We strongly advise you to read the terms and conditions and privacy policies of any
                                    third-party web sites or services that you visit.`),
				},
			},
			{
				Header:          `Termination`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Termination"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`We may terminate or suspend your account immediately, without prior notice or
					liability, for any reason whatsoever, including without limitation if you breach the
					Terms.`),
					template.HTML(`Upon termination, your right to use the Service will immediately cease. If you wish
					to terminate your account, you may simply discontinue using the Service.`),
				},
			},
			{
				Header:          `Limitation Of Liability`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Limitation Of Liability"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(fmt.Sprintf(`In no event shall %s, nor its directors, employees, partners, agents,
					suppliers, or affiliates, be liable for any indirect, incidental, special,
					consequential or punitive damages, including without limitation, loss of profits,
					data, use, goodwill, or other intangible losses, resulting from (i) your access to
					or use of or inability to access or use the Service; (ii) any conduct or content of
					any third party on the Service; (iii) any content obtained from the Service; and
					(iv) unauthorized access, use or alteration of your transmissions or content,
					whether based on warranty, contract, tort (including negligence) or any other legal
					theory, whether or not we have been informed of the possibility of such damage, and
					even if a remedy set forth herein is found to have failed of its essential purpose.`, r.ServiceName)),
					template.HTML(`We are not obliged to verify the manner in which you or other users use the Website,
					Platform, Configuration or Services and we shall not be liable for the manner of
					such usage. We assume that you use the Website Platform and Services legally and
					ethically and that you have obtained permission, if necessary, to use it on the
					targeted websites and/or other data sources.`),
					template.HTML(`We shall not be liable for the outcomes of activities for which you use our Website,
					Platform, Configuration or Services. Provided that a third-party service or product
					is established on the Platform or on any of its functionalities, we shall not be
					liable for such a service or product, their functioning or manner and consequences
					of their usage.`),
					template.HTML(`We shall not be liable for any of your unlawful actions in connection to the usage of
					the Website, Platform, Configuration or Services with respect to third parties (e.g.
					breach of intellectual property rights, rights to the name or company name, unfair
					competition, breach of terms of websites or applications and programs of third
					parties).`),
					template.HTML(`We shall not guarantee or be liable for the availability of the Website, Platform or
						Services (or products arising therefrom) or for their performance, reliability or
						responsiveness or any other performance or time parameters. We shall neither be
						liable for the functionality or availability of the services of other providers that
						we mediate to you solely. We shall neither be liable for your breach of service
						usage terms of such providers.`),
				},
			},
			{
				Header:          `Your Obligation to Indemnify`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Your Obligation to Indemnify"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`You agree to indemnify, defend and hold us, our agents, affiliates, subsidiaries,
					directors, officers, employees, and applicable third parties (e.g., all relevant
					partner(s), licensors, licensees, consultants and contractors) (“Indemnified
					Person(s)”) harmless from and against any third-party claim, liability, loss, and
					expense (including damage awards, settlement amounts, and reasonable legal fees),
					brought against any Indemnified Person(s), arising out of your use of the Website,
					Platform, Configurations or Services and/or your breach of any of these terms. You
					acknowledge and agree that each Indemnified Person has the right to assert and
					enforce its rights under this section directly on its own behalf as a third-party
					beneficiary.`),
				},
			},
			{
				Header:          `Disclaimer and Warning`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Disclaimer and Warning"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`Your use of the Service is at your sole risk. The Service is provided on an "AS IS"
					and "AS AVAILABLE" basis. The Service is provided without warranties of any kind,
					whether express or implied, including, but not limited to, implied warranties of
					merchantability, fitness for a particular purpose, non-infringement or course of
					performance.`),
					template.HTML(fmt.Sprintf(`%s its subsidiaries, affiliates, and its licensors do not warrant that a)
					the Service will function uninterrupted, secure or available at any particular time
					or location; b) any errors or defects will be corrected; c) the Service is free of
					viruses or other harmful components; or d) the results of using the Service will
					meet your requirements.`, r.ServiceName)),
					template.HTML(`We may immediately suspend your use of the Website, Platform, Configurations and/or
                    Services if we are contacted by a third-party or local authority due to your activity on our platform. If such an event occurs, we
                    will not disclose your information without a court order mandating us to do so
                    unless we in our best judgment determine that there would be an adverse consequence
                    if we do not. If, however, we receive a court order demanding the release of your
                    information to a third party, we will comply. If such action becomes necessary, you
                    agree to indemnify and hold us and (as applicable) our parent(s), subsidiaries,
                    affiliates, officers, directors, agents, contractors and employees, harmless from
                    any claim or demand, including reasonable attorneys' fees, made by any third party
                    arising from any complaint, suit, disagreement or other repercussions resulting from
                    your use of the Website, Platform, Configurations or Services.`),
					template.HTML(`Should any third party claim its rights against us in connection to your actions, we
					may immediately eliminate any contents gathered, saved or disseminated by you from
					servers used by us. In the event of a judicial dispute with a third party related to
					your actions, you are obliged to provide us with all necessary cooperation in order
					to resolve such a dispute successfully and you are also obliged to reimburse
					continuously any purposeful expenses arising to us due to such a dispute. With
					respect to this, should an obligation arise to reimburse any claim of a third party,
					you agree to pay us the full scope of the damages.`),
				},
			},
			{
				Header:          `Governing Law`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Governing Law"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`These Terms shall be governed and construed in accordance with the laws of the United Kingdom (UK),
					without regard to its conflict of law provisions.`),
					template.HTML(`Our failure to enforce any right or provision of these Terms will not be considered a
					waiver of those rights. If any provision of these Terms is held to be invalid or
					unenforceable by a court, the remaining provisions of these Terms will remain in
					effect. These Terms constitute the entire agreement between us regarding our
					Service, and supersede and replace any prior agreements we might have between us
					regarding the Service.`),
				},
			},
			{
				Header:          `Changes`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Changes"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`We reserve the right, at our sole discretion, to modify or replace these Terms at any
					time. If a revision is material we will try to provide at least 30 days notice prior
					to any new terms taking effect. What constitutes a material change will be
					determined at our sole discretion.`),
					template.HTML(`By continuing to access or use our Service after those revisions become effective,
					you agree to be bound by the revised terms. If you do not agree to the new terms,
					please stop using the Service.`),
				},
			},
			{
				Header:          `Contact Us`,
				HeaderId:        strings.ReplaceAll(toolbox.StringStandardisedToLower("Contact Us"), " ", "-"),
				HeaderWithIndex: true,
				Paragraphs: []template.HTML{

					template.HTML(`If you have any questions about these Terms, please contact us.`),
					template.HTML(fmt.Sprintf(`By email: <a href="mailto:%s?subject=About%%20your%%20terms">%s</a>`, r.ServiceEmail, r.ServiceEmail)),
				},
			},
		},
	}

	// generate the table of contents based on the
	// sections passed in above
	termsOfServicePolicy.TableOfContentsItems = termsOfServicePolicy.GetTableOfContentsItems()

	return &termsOfServicePolicy
}
