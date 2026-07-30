package vision

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// VisionErrorMap maps vision failures to public API errors.
var VisionErrorMap = reply.ErrorManifest{
	ErrVisionError:                   {Title: "Bad Request", Detail: "The vision request is invalid.", StatusCode: http.StatusBadRequest, Code: "VIS0-001"},
	ErrVisionTitleIsRequired:         {Title: "Missing Vision Title", Detail: "Please provide a title.", StatusCode: http.StatusBadRequest, Code: "VIS0-002"},
	ErrVisionInvalidType:             {Title: "Invalid Vision Type", Detail: "Please provide a supported vision type.", StatusCode: http.StatusBadRequest, Code: "VIS0-003"},
	ErrVisionNanoIDIsRequired:        {Title: "Missing Vision NanoID", Detail: "Please provide a vision NanoID.", StatusCode: http.StatusBadRequest, Code: "VIS0-004"},
	ErrVisionResourceNotFound:        {Title: "Vision Not Found", Detail: "The requested vision could not be found.", StatusCode: http.StatusNotFound, Code: "VIS0-005"},
	ErrVisionUserIDIsRequired:        {Title: "Missing User ID", Detail: "Please authenticate before completing this action.", StatusCode: http.StatusUnauthorized, Code: "VIS0-006"},
	ErrVisionInvalidPayload:          {Title: "Invalid Vision Payload", Detail: "Please provide a valid vision payload.", StatusCode: http.StatusBadRequest, Code: "VIS0-007"},
	ErrVisionInvalidQueryParam:       {Title: "Invalid Vision Query", Detail: "Please provide valid vision query parameters.", StatusCode: http.StatusBadRequest, Code: "VIS0-008"},
	ErrVisionDatabaseError:           {Title: "Internal Error", Detail: "Unable to complete the vision operation at this time.", StatusCode: http.StatusInternalServerError, Code: "VIS0-009"},
	ErrVisionConfigNotSet:            {Title: "Vision Configuration Missing", Detail: "Vision has not been configured.", StatusCode: http.StatusInternalServerError, Code: "VIS0-010"},
	ErrVisionConfigInvalid:           {Title: "Invalid Vision Configuration", Detail: "Vision configuration is invalid.", StatusCode: http.StatusInternalServerError, Code: "VIS0-011"},
	ErrVisionInvalidStatus:           {Title: "Invalid Vision Status", Detail: "Please provide a configured roadmap status.", StatusCode: http.StatusBadRequest, Code: "VIS0-012"},
	ErrVisionInvalidStatusTransition: {Title: "Invalid Status Transition", Detail: "The requested roadmap status transition is not allowed.", StatusCode: http.StatusConflict, Code: "VIS0-013"},
	ErrVisionInvalidVote:             {Title: "Invalid Vote", Detail: "A vote must be either 0 (downvote) or 1 (upvote).", StatusCode: http.StatusBadRequest, Code: "VIS0-014"},
	ErrVisionDownvotingDisabled:      {Title: "Downvoting Disabled", Detail: "Downvoting is not enabled.", StatusCode: http.StatusBadRequest, Code: "VIS0-015"},
	ErrVisionCommentMessageIsRequired: {
		Title:      "Missing Comment",
		Detail:     "Please provide a comment message.",
		StatusCode: http.StatusBadRequest,
		Code:       "VIS0-016",
	},
	ErrVisionCommentNotFound: {
		Title:      "Comment Not Found",
		Detail:     "The requested vision comment could not be found.",
		StatusCode: http.StatusNotFound,
		Code:       "VIS0-017",
	},
}
