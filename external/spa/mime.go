package spa

import "mime"

// init registers the .webmanifest MIME type so the package serves web app
// manifests with the correct content type on platforms lacking a built-in
// mapping.
func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}
