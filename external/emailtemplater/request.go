package emailtemplater

import "errors"

// GenerateFromBaseTemplateRequest holds information for generating a custom email from base template
type GenerateFromBaseTemplateRequest struct {
	// EmailSubject the email subject
	EmailSubject string

	// EmailPreview the email preview
	EmailPreview string

	// EmailBody the email body which should be HTML wrapped in <td> tags
	//
	// Example:
	//
	// <td style="font-family: sans-serif; font-size: 14px; vertical-align: top;">
	// 	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
	// 		<br> A request has been made to log in to your account
	// 	</p>
	// 	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
	// 		Press the button below to verify the request and log in.
	// 		<table border="0" cellpadding="0" cellspacing="0" class="btn btn-primary" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: 100%; box-sizing: border-box;">
	// 			<tbody>
	// 				<tr>
	// 					<td align="left" style="font-family: sans-serif; font-size: 14px; vertical-align: top; padding-bottom: 15px;">
	// 						<table border="0" cellpadding="0" cellspacing="0" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: auto;">
	// 							<tbody>
	// 								<tr>
	// 									<td style="font-family: sans-serif; font-size: 14px; vertical-align: top; background-color: #000000; border-radius: 5px; text-align: center;">
	// 										<a href="https://awesome-service.com/some/path" title="Log in to Account" target="_blank" style="display: inline-block; color: #ffffff; background-color: #000000; border: solid 1px #000000; border-radius: 5px; box-sizing: border-box; cursor: pointer; text-decoration: none; font-size: 14px; font-weight: bold; margin: 0; padding: 12px 25px; text-transform: capitalize; border-color: #000000;">Log in to Account</a>
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
	EmailBody string

	// RawEmailBody is the raw email body before any processing
	//
	// Example:
	//
	// A request has been made to log in to your account
	//
	// Click the button below to verify the request and log in.
	// [Log in to Account](https://awesome-service.com/some/path)
	//
	// Once you've logged in, feel free to delete this email.
	//
	// If you did not make this request, please ignore this email.
	//
	// Thanks,
	// The Awesome Service Team
	//
	// Note: This field is optional
	RawEmailBody string

	// EmailTo the email address that should be in "To" field
	EmailTo string

	// OverrideEmailFrom the email address that should override the default
	// value in "From" field (optional)
	OverrideEmailFrom string

	// OverrideEmailReplyTo the email address that should override the default
	// value in reply to field (optional)
	OverrideEmailReplyTo string

	// WithFooter specifies whether the email should have a footer
	WithFooter bool
}

// GetEmailTo implements TemplateRequest
func (r *GenerateFromBaseTemplateRequest) GetEmailTo() string {
	return r.EmailTo
}

// Validate implements TemplateRequest
func (r *GenerateFromBaseTemplateRequest) Validate() error {
	if r.EmailTo == "" {
		return errors.New(ErrKeyEmailTemplaterMissingRecipient)
	}
	if r.EmailSubject == "" {
		return errors.New(ErrKeyEmailTemplaterMissingSubject)
	}
	if r.EmailBody == "" {
		return errors.New(ErrKeyEmailTemplaterMissingBody)
	}
	return nil
}

// GenerateVerificationEmailRequest holds information needed to generate a verification email
type GenerateVerificationEmailRequest struct {
	// FirstName the recipient's first name
	FirstName string

	// LastName the recipient's last name
	LastName string

	// Email the recipient's email
	Email string

	// Token the token that will be used to authorise email verification when clicked
	Token string

	// IsDashboardRequest whether the request originates from the dashboard portal
	IsDashboardRequest bool

	// RequestUrl where the user should be redirected to once signed in
	RequestUrl string
}

// GetEmailTo implements TemplateRequest
func (r *GenerateVerificationEmailRequest) GetEmailTo() string {
	return r.Email
}

// Validate implements TemplateRequest
func (r *GenerateVerificationEmailRequest) Validate() error {
	if r.Email == "" {
		return errors.New(ErrKeyEmailTemplaterMissingRecipient)
	}
	if r.Token == "" {
		return errors.New(ErrKeyEmailTemplaterMissingToken)
	}
	if r.FirstName == "" || r.LastName == "" {
		return errors.New(ErrKeyEmailTemplaterMissingPersonalInfo)
	}
	return nil
}

// GenerateLoginEmailRequest holds information needed to generate a login email
type GenerateLoginEmailRequest struct {
	// Email the recipient's email
	Email string

	// Token the token that will be used to confirm user is authorised when clicked
	Token string

	// IsDashboardRequest whether the request originates from the dashboard portal
	IsDashboardRequest bool

	// RequestUrl where the user should be redirected to once signed in
	RequestUrl string
}

// GetEmailTo implements TemplateRequest
func (r *GenerateLoginEmailRequest) GetEmailTo() string {
	return r.Email
}

// Validate implements TemplateRequest
func (r *GenerateLoginEmailRequest) Validate() error {
	if r.Email == "" {
		return errors.New(ErrKeyEmailTemplaterMissingRecipient)
	}
	if r.Token == "" {
		return errors.New(ErrKeyEmailTemplaterMissingToken)
	}
	return nil
}
