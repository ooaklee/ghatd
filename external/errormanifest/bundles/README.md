# Error Manifest Bundles

`external/errormanifest/bundles` contains named, reusable collections of
cross-package `reply.ErrorManifest` values for common GHATD application wiring.

The bundle helpers live in a subpackage to avoid import cycles: domain packages
can keep importing `external/errormanifest` for `Composer`, while application
composition layers such as `external/starter/v0` can import these bundles.

Each helper returns copied manifests so caller-side changes do not mutate the
package-level error maps owned by individual GHATD packages.

```go
errorMaps := errormanifest.NewComposer().
	Add(bundles.AccessManager()...).
	Build()
```

Handler-level bundles leave out the handler package's own error map because
handlers add their package-local base maps internally. For example,
`bundles.AccessManager()` intentionally excludes
`accessmanager.AccessmanagerErrorMap`.

`bundles.UserManager()` includes cross-package maps for services surfaced
through UMS, including `reminder.ReminderErrorMap` for the `/api/v1/ums`
reminder endpoints. The usermanager handler still adds its own
`UsermanagerErrorMap` internally.

Middleware-level bundles are different. `bundles.AuthMiddleware()` includes
`accessmanager.AccessmanagerErrorMap` because access middleware uses the
provided map set directly.
