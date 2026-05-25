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
	bypassFileNames      map[string]struct{}
	ignoredFileNames     map[string]struct{}
}

// DefaultBypassExtensions defines the default file extensions that bypass SPA index rewriting.
var DefaultBypassExtensions = []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".txt", ".woff", ".woff2", ".traineddata", ".xml", ".avif", ".webmanifest"}

// DefaultPathUpdateOptions returns the default options for NewHandleUpdatePathToIndex
func DefaultPathUpdateOptions() *PathUpdateOptions {
	result := &PathUpdateOptions{
		bypassFileExtensions: map[string]struct{}{},
		bypassFileNames:      map[string]struct{}{},
		ignoredFileNames:     map[string]struct{}{},
	}
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

// normaliseFileName trims file name input and keeps only the final path segment.
func normaliseFileName(fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return ""
	}

	fileName = path.Base(fileName)
	if fileName == "." || fileName == "/" {
		return ""
	}

	return fileName
}

// BypassWithFileName allows a file name to bypass SPA index rewriting.
// It matches against the URL path filename segment only, not the full route path.
func BypassWithFileName(fileName string) PathOption {
	normalisedFileName := normaliseFileName(fileName)

	return func(options *PathUpdateOptions) {
		if normalisedFileName == "" {
			return
		}
		delete(options.ignoredFileNames, normalisedFileName)
		options.bypassFileNames[normalisedFileName] = struct{}{}
	}
}

// IgnoreFileName ensures a file name is ignored for bypassing SPA index rewriting,
// meaning requests with this file name will be rewritten to "/".
// It matches against the URL path filename segment only, not the full route path.
func IgnoreFileName(fileName string) PathOption {
	normalisedFileName := normaliseFileName(fileName)

	return func(options *PathUpdateOptions) {
		if normalisedFileName == "" {
			return
		}
		delete(options.bypassFileNames, normalisedFileName)
		options.ignoredFileNames[normalisedFileName] = struct{}{}
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
// or BypassWithFileName("beacon-example.html") to allow one specific file
// through without bypassing every file with the same extension. IgnoreFileName
// can force one specific file to be rewritten even when its extension bypasses.
//
// Option order matters for default extensions:
//
// - IgnoreDefaultFileExtensions() then BypassWithFileExtension("js"): ".js" is added back and bypassed.
// - BypassWithFileExtension("js") then IgnoreDefaultFileExtensions(): ".js" is removed again and rewritten.
// - IgnoreFileName("loader.js") then BypassWithFileName("loader.js"): "loader.js" is added back and bypassed.
// - BypassWithFileName("loader.js") then IgnoreFileName("loader.js"): "loader.js" is removed again and rewritten.
func NewHandleUpdatePathToIndex(options ...PathOption) func(r *http.Request) *http.Request {
	pathOptions := DefaultPathUpdateOptions()
	for _, option := range options {
		option(pathOptions)
	}

	return func(r *http.Request) *http.Request {
		fileName := path.Base(r.URL.Path)
		if _, fileNameIgnored := pathOptions.ignoredFileNames[fileName]; fileNameIgnored {
			r.URL.Path = "/"
			return r
		}
		if _, fileNameBypass := pathOptions.bypassFileNames[fileName]; fileNameBypass {
			return r
		}
		if _, exists := pathOptions.bypassFileExtensions[path.Ext(r.URL.Path)]; !exists {
			r.URL.Path = "/"
		}
		return r
	}
}
