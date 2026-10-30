package contentmanager

import "github.com/ooaklee/reply/v2"

// ContentManagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var ContentManagerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrUnauthorisedCMUser:               {Title: "Forbidden", Detail: "User is not authorised to carry out this action", StatusCode: 403, Code: "CM00-01"},
	ErrContentManagerUnavailable:        {Title: "Service Unavailable", Detail: "Content service is temporarily unavailable", StatusCode: 503, Code: "CM00-02"},
	ErrIndexNowReconciliationInvalid:    {Title: "Invalid Publication Window", Detail: "Provide a valid bounded publication window and scan limit", StatusCode: 400, Code: "CM00-03"},
	ErrIndexNowReconciliationIncomplete: {Title: "Reconciliation Incomplete", Detail: "Publication notifications could not be confirmed; retain the previous checkpoint", StatusCode: 503, Code: "CM00-04"},
}
