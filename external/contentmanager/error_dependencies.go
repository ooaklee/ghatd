package contentmanager

import (
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// DependencyErrorMaps supplies the lower-domain response contracts exposed by
// Content Manager. The handler includes them after its own map and before host
// overrides, even when no bundle is injected. Map order matches legacy bundles;
// returned maps are copied, with immutable referenced metadata as documented by
// errormanifest.CloneManifests.
func DependencyErrorMaps() []reply.ErrorManifest {
	return errormanifest.CloneManifests(post.PostErrorMap, toolbox.ToolboxErrorMap, user.UserErrorMap)
}
