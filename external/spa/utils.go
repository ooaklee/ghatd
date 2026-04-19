package spa

import (
	"fmt"
	"net/http"
	"path"
	"strings"
)

// PathOption configures behaviour for NewHandleUpdatePathToIndex.
type PathOption func(*PathUpdateOptions)

// PathUpdateOptions defines options for NewHandleUpdatePathToIndex.
type PathUpdateOptions struct {
	bypassFileExtensions map[string]struct{}
}

// DefaultBypassExtensions defines the default file extensions that bypass SPA index rewriting.
var DefaultBypassExtensions = []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".txt", ".woff", ".woff2", ".traineddata", ".xml", ".avif"}

// DefaultPathUpdateOptions returns the default options for NewHandleUpdatePathToIndex
func DefaultPathUpdateOptions() *PathUpdateOptions {

	result := &PathUpdateOptions{bypassFileExtensions: map[string]struct{}{}}
	for _, extension := range DefaultBypassExtensions {
		result.bypassFileExtensions[extension] = struct{}{}
	}

	return result
}

// NormaliseFileExtension ensures the file extension starts with a dot.
// If the input is empty, it returns an empty string.
func NormaliseFileExtension(fileExtension string) string {
	if fileExtension == "" {
		return ""
	}

	if !strings.HasPrefix(fileExtension, ".") {
		return fmt.Sprintf(".%s", fileExtension)
	}

	return fileExtension
}

// BypassWithFileExtension allows a file extension to bypass SPA index rewriting.
// The extension may be passed with or without a leading dot, for example "webp" or ".webp".
func BypassWithFileExtension(fileExtension string) PathOption {
	normalisedFileExtension := NormaliseFileExtension(fileExtension)

	return func(options *PathUpdateOptions) {
		if normalisedFileExtension == "" {
			return
		}
		options.bypassFileExtensions[normalisedFileExtension] = struct{}{}
	}
}

// IgnoreFileExtension ensures a file extension is ignored for bypassing SPA index rewriting,
// meaning requests with this extension will be rewritten to "/".
// The extension may be passed with or without a leading dot, for example "js" or ".js".
func IgnoreFileExtension(fileExtension string) PathOption {
	normalisedFileExtension := NormaliseFileExtension(fileExtension)

	return func(options *PathUpdateOptions) {
		delete(options.bypassFileExtensions, normalisedFileExtension)
	}
}

// IgnoreDefaultFileExtensions removes all default bypass extensions from the
// path update options.
//
// When passed to NewHandleUpdatePathToIndex, built-in static extensions
// (for example .js, .css, .png) will no longer bypass SPA rewriting and will
// be rewritten to "/" unless explicitly re-added with BypassWithFileExtension.
func IgnoreDefaultFileExtensions() PathOption {

	return func(options *PathUpdateOptions) {

		for _, extension := range DefaultBypassExtensions {
			if extension != "" {
				delete(options.bypassFileExtensions, extension)
			}
		}
	}
}

// NewHandleUpdatePathToIndex returns a request path updater for SPA routing.
//
// By default, common static file extensions are bypassed so those requests are
// not rewritten to "/". Additional rules can be added with PathOption, for
// example BypassWithFileExtension("webp") to allow an extension through
// or IgnoreFileExtension("js") to force rewriting that extension to "/".
//
// Option order matters for default extensions:
//
// - IgnoreDefaultFileExtensions() then BypassWithFileExtension("js"): ".js" is added back and bypassed.
// - BypassWithFileExtension("js") then IgnoreDefaultFileExtensions(): ".js" is removed again and rewritten.
func NewHandleUpdatePathToIndex(options ...PathOption) func(r *http.Request) *http.Request {
	pathOptions := DefaultPathUpdateOptions()
	for _, option := range options {
		option(pathOptions)
	}

	return func(r *http.Request) *http.Request {
		if _, exists := pathOptions.bypassFileExtensions[path.Ext(r.URL.Path)]; !exists {
			r.URL.Path = "/"
		}
		return r
	}
}
