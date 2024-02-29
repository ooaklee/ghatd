package rememberer

import (
	"github.com/ooaklee/reply"
)

// remembererErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
//nolint will be used later
var remembererErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyRemembererError:      {Title: "Bad Request", Detail: "Some rememberer error", StatusCode: 400},
	ErrKeyWordWithIdNotFound:   {Title: "Resource Not Found", Detail: "No word can be found matching the Id provided", StatusCode: 404, Code: "R-002"},
	ErrKeyWordWithNameNotFound: {Title: "Resource Not Found", Detail: "No word can be found matching the name provided", StatusCode: 404, Code: "R-003"},
	ErrKeyWordAlreadyExists:    {Title: "Resource Conflict", Detail: "Word already exists!", StatusCode: 429, Code: "R-004"},
	ErrKeyInvalidWordId:        {Title: "Bad Request", Detail: "Invalid or malformatted word identifier provided", StatusCode: 400, Code: "R-005"},
	ErrKeyInvalidWordBody:      {Title: "Bad Request", Detail: "Invalid or malformatted word. Check that the word is more than 2 character", StatusCode: 400, Code: "R-006"},
	ErrKeyInvalidQueryParam:    {Title: "Bad Request", Detail: "Invalid or malformatted query param values provided", StatusCode: 400, Code: "R-007"},
}
