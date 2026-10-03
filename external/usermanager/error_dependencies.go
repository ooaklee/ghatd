package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/streaker"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/reply/v2"
)

// DependencyErrorMaps supplies the lower-domain response contracts exposed by
// User Manager. The handler always includes them after its own map and before
// caller overrides; hosts need not repeat the dependency inventory. Results
// preserve existing precedence and include surfaced streak errors. Referenced
// metadata remains immutable, as described by errormanifest.CloneManifests.
func DependencyErrorMaps() []reply.ErrorManifest {
	return errormanifest.CloneManifests(
		user.UserErrorMap, contacter.ContacterErrorMap, toolbox.ToolboxErrorMap,
		group.GroupErrorMap, notifier.NotifierErrorMap, reminder.ReminderErrorMap,
		streaker.StreakErrorMap, vision.VisionErrorMap, router.PolicyErrorManifest(),
	)
}
