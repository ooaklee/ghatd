# Error Manifest Bundles

`external/errormanifest/bundles` contains named, reusable collections of
cross-package `reply.ErrorManifest` values for common GHATD application wiring.

The bundle helpers live in a subpackage to avoid import cycles: domain packages
can keep importing `external/errormanifest` for `Composer`, while application
composition layers such as `external/starter/v0` can import these bundles.

Each helper returns copied map entries so caller-side entry changes do not mutate
package-level maps. Referenced metadata is shallow-copied and must remain immutable.

```go
errorMaps := errormanifest.NewComposer().
	Add(bundles.AccessManager()...).
	Build()
```

Handler-level bundles leave out the handler package's own error map because
handlers add their package-local base maps internally. For example,
`bundles.AccessManager()` intentionally excludes
`accessmanager.AccessmanagerErrorMap`.

The Access, User, Content and Billing Manager handlers also add their built-in
dependency maps automatically. Their `DependencyErrorMaps()` functions own these
inventories; the matching bundle functions delegate to them for compatibility.
Existing explicit bundle injection still works, and caller overrides remain
last-wins. New hosts need only supply application-specific maps and overrides,
not repeat these managers' built-in dependencies.

`bundles.UserManager()` includes cross-package maps for services surfaced
through UMS, including `reminder.ReminderErrorMap` and `streaker.StreakErrorMap`
for the reminder and streak endpoints. The usermanager handler still adds its own
`UsermanagerErrorMap` internally.

Middleware-level bundles are different. `bundles.AuthMiddleware()` includes
`accessmanager.AccessmanagerErrorMap` because access middleware uses the
provided map set directly.
