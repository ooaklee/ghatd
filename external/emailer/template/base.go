package template

import "fmt"

type NewBaseHtmlEmailTemplateRequest struct {
	// EmailPreview is the text that will be shown in the email preview
	EmailPreview string

	// EmailSubject is the subject that will be sent with the email
	EmailSubject string

	// EmailBodyMainContent is the main content that will be sent in the email
	// this should be html wrapped in <td> tags
	//
	// example:
	//
	// <td style="font-family: sans-serif; font-size: 14px; vertical-align: top;">
	// 	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
	// 		<br> A request has been made to log in to your account
	// 	</p>
	// 	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
	// 		Press the button below to verify the request and log in. The link will expire in <i><u>10 minutes</u></i>. Kindly refrain from sharing this email with anyone, even those who claim to be part of the team, as it grants access to your account.
	// 		<br><br>
	// 		Once you've logged in, feel free to delete this email.</>
	// 		<table border="0" cellpadding="0" cellspacing="0" class="btn btn-primary" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: 100%; box-sizing: border-box;">
	// 			<tbody>
	// 				<tr>
	// 					<td align="left" style="font-family: sans-serif; font-size: 14px; vertical-align: top; padding-bottom: 15px;">
	// 						<table border="0" cellpadding="0" cellspacing="0" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: auto;">
	// 							<tbody>
	// 								<tr>
	// 									<td style="font-family: sans-serif; font-size: 14px; vertical-align: top; background-color: #000000; border-radius: 5px; text-align: center;">
	// 										<a href="https://awesome-service.com/some/path/for/magic/login" title="Log in to Account" target="_blank" style="display: inline-block; color: #ffffff; background-color: #000000; border: solid 1px #000000; border-radius: 5px; box-sizing: border-box; cursor: pointer; text-decoration: none; font-size: 14px; font-weight: bold; margin: 0; padding: 12px 25px; text-transform: capitalize; border-color: #000000;">Log in to Account</a>
	// 									</td>
	// 								</tr>
	// 							</tbody>
	// 						</table>
	// 					</td>
	// 				</tr>
	// 			</tbody>
	// 		</table>
	// 	</p>
	// </td>
	EmailBodyMainContent string

	// FooterEnabled is whether or not to include the footer
	FooterEnabled bool

	// FooterYear is the year that will be shown in the footer
	FooterYear int

	// FooterEntityName is the name of the entity that will be shown in the footer
	FooterEntityName string

	// FooterEntityUrl is the url that receipt will be sent to when the user clicks on the
	// entity's name in the footer
	FooterEntityUrl string
}

func NewBaseHtmlEmailTemplate(emailPreview, emailSubject, emailMainContent string, footerEnabled bool, footerYear int, footerEntityName, footerEntityUrl string) string {
	var baseEmailTemplate string

	baseEmailTemplate += "<!doctype html><html>"

	baseEmailTemplate += `<head>
<meta name="viewport" content="width=device-width">
<meta http-equiv="Content-Type" content="text/html; charset=UTF-8">
<title>` +
		emailSubject +
		`</title>
<style>
@media only screen and (max-width: 620px) {
  table[class=body] h1 {
    font-size: 28px !important;
    margin-bottom: 10px !important;
  }

  table[class=body] p,
table[class=body] ul,
table[class=body] ol,
table[class=body] td,
table[class=body] span,
table[class=body] a {
    font-size: 16px !important;
  }

  table[class=body] .wrapper,
table[class=body] .article {
    padding: 10px !important;
  }

  table[class=body] .content {
    padding: 0 !important;
  }

  table[class=body] .container {
    padding: 0 !important;
    width: 100% !important;
  }

  table[class=body] .main {
    border-left-width: 0 !important;
    border-radius: 0 !important;
    border-right-width: 0 !important;
  }

  table[class=body] .btn table {
    width: 100% !important;
  }

  table[class=body] .btn a {
    width: 100% !important;
  }

  table[class=body] .img-responsive {
    height: auto !important;
    max-width: 100% !important;
    width: auto !important;
  }
}
@media all {
  .ExternalClass {
    width: 100%;
  }

  .ExternalClass,
.ExternalClass p,
.ExternalClass span,
.ExternalClass font,
.ExternalClass td,
.ExternalClass div {
    line-height: 100%;
  }

  .apple-link a {
    color: inherit !important;
    font-family: inherit !important;
    font-size: inherit !important;
    font-weight: inherit !important;
    line-height: inherit !important;
    text-decoration: none !important;
  }

  #MessageViewBody a {
    color: inherit;
    text-decoration: none;
    font-size: inherit;
    font-family: inherit;
    font-weight: inherit;
    line-height: inherit;
  }

  .btn-primary table td:hover {
    background-color: #000000 !important;
  }

  .btn-primary a:hover {
    background-color: #323232 !important;
    border-color: #323232 !important;

  }

  #linkToReachOut {
    color: #000000;
    text-decoration: none;
  }


  #linkToReachOut:hover {
    text-decoration: underline;
  }
}
</style></head>`

	baseEmailTemplate += `<body class="" style="background-color: #f6f6f6; font-family: sans-serif; -webkit-font-smoothing: antialiased; font-size: 14px; line-height: 1.4; margin: 0; padding: 0; -ms-text-size-adjust: 100%; -webkit-text-size-adjust: 100%;">
	<span class="preheader" style="color: transparent; display: none; height: 0; max-height: 0; max-width: 0; opacity: 0; overflow: hidden; mso-hide: all; visibility: hidden; width: 0;">` +
		emailPreview +
		`</span>
	<table border="0" cellpadding="0" cellspacing="0" class="body" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: 100%; background-color: #f6f6f6;">
		<tr>
			<td style="font-family: sans-serif; font-size: 14px; vertical-align: top;">&nbsp;</td>
			<td class="container" style="font-family: sans-serif; font-size: 14px; vertical-align: top; display: block; Margin: 0 auto; max-width: 580px; padding: 10px; width: 580px;">
				<div class="content" style="box-sizing: border-box; display: block; Margin: 0 auto; max-width: 580px; padding: 10px;">
					<table class="main" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: 100%; background: #ffffff; border-radius: 3px;">
						<tr>
							<td class="wrapper" style="font-family: sans-serif; font-size: 14px; vertical-align: top; box-sizing: border-box; padding: 20px;">
								<table border="0" cellpadding="0" cellspacing="0" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: 100%;">
									<tr>
										` + emailMainContent + `
									</tr>
								</table>
							</td>
						</tr>
					</table>
					` + handleFooter(footerEnabled, footerYear, footerEntityName, footerEntityUrl) + `
				</div>
			</td>
			<td style="font-family: sans-serif; font-size: 14px; vertical-align: top;">&nbsp;</td>
		</tr>			
	</table>
</body>`

	baseEmailTemplate += "</html>"

	return baseEmailTemplate
}

// handleFooter generates the HTML for the email footer based on the provided parameters.
// If footerEnabled is false, an empty string is returned.
// Otherwise, the function returns an HTML block containing the footer information,
// including the provided year and a link to the Entity that's applied on the provided Entity's Name .
func handleFooter(footerEnabled bool, footerYear int, footerEntityName, footerEntityUrl string) string {
	if !footerEnabled {
		return ""
	}

	return `<div class="footer" style="clear: both; Margin-top: 10px; text-align: center; width: 100%;">
<table border="0" cellpadding="0" cellspacing="0" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: 100%;">
	<tr>
		<td class="content-block powered-by" style="font-family: sans-serif; vertical-align: top; padding-bottom: 10px; padding-top: 10px; font-size: 12px; color: #999999; text-align: center;">
			` + fmt.Sprintf("%d", footerYear) + ` © <a href="` + footerEntityUrl + `" style="color: #999999; font-size: 12px; text-align: center; text-decoration: none;"> ` + footerEntityName + `</a>
		</td>
	</tr>
</table>
</div>`
}
