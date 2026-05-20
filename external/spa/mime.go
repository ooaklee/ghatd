package spa

import "mime"

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}
