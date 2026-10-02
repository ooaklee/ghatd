package errormanifest

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// ResponseErrors resolves an error tree to registered manifest identities in
// first-seen order, deduplicating repeated identities. Ordinary wrappers and
// joins are traversed structurally, never by their diagnostic text. Every join
// branch must resolve; an unknown branch invalidates the entire collection.
//
// An exact registered node or an unambiguous custom Is match is an explicit
// domain classification: its underlying diagnostic cause is not a separate
// public failure. Custom Is methods must compare only their own classification,
// not recursively classify independent joined failures. As with errors.Is,
// custom Is and Unwrap implementations must terminate and be safe to call.
//
// Nil, malformed, ambiguous, cyclic or over-64-node trees return one opaque,
// unmapped error for reply's generic 500 response. This is an error-response
// helper, not a success detector. It does not fill gaps in domain manifests;
// callers must compose all expected errors and keep those maps immutable while
// serving requests. Keep the original error for retries and redacted diagnostics.
func ResponseErrors(err error, manifests []reply.ErrorManifest) []error {
	keys, known := reply.NewReplier(manifests).ResolveErrors(err)
	if !known {
		return []error{unmappedError}
	}
	return keys
}

// WriteHTTPError writes a manifest-driven reply error response, including mapped
// validation joins and wrapped errors. Later maps override fields for the same
// identity; reply retains its first-status rule and 5xx dominance. Each write
// owns a fresh replier, so concurrent calls do not share mutable response state.
//
// Attributes may supply headers and metadata, which callers must redact. The
// original writer and error roots are pinned after attributes run: an
// attribute cannot substitute a raw error, writer, data or token response.
// Manifest status codes take precedence over an attribute's status. Nil errors
// fail closed rather than producing a successful response. Writer/encoding
// failures are returned; this helper neither retries nor writes a second body.
func WriteHTTPError(w http.ResponseWriter, err error, manifests []reply.ErrorManifest, attributes ...reply.ResponseAttributes) error {
	response := &reply.NewResponseRequest{Writer: w, Error: err}
	for _, attribute := range attributes {
		if attribute != nil {
			attribute(response)
		}
	}
	response.Writer = w
	response.Error = nil
	// Reply owns structural resolution. An explicit root also makes nil fail
	// closed for this error-only adapter, unlike reply's generic blank response.
	response.Errors = []error{err}
	response.Data = nil
	response.TokenOne = ""
	response.TokenTwo = ""
	response.Message = ""
	response.StatusCode = 0
	return reply.NewReplier(manifests).NewHTTPResponse(response)
}
