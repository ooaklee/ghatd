# Partners composition

`NewRuntime` binds current user, group, policy, session and account-admission
owners to the existing Partners runtime, prepares its native storage and builds
its API, financial execution and optional lifecycle owners. Supply a resolved
configuration, explicit system and account identity values, and a fail-closed
account admission function. `BindDependencies` performs the identity and policy
binding without storage I/O. Optional paid acquisition uses `PaidAcquisition`
with explicit eligible billing intervals and existing provider evidence.
Neither construction nor disabled paid acquisition starts a worker.

`BuildHTTP` reuses the host's shared browser-security guard and a principal
resolver configured with live member sessions, verified token owner, cookie,
native audiences and transport identity binding. `MountRoutes` registers the
API or an explicit no-store 503 JSON response when unavailable, preventing a
fallback to the SPA. `NeverCache` bypasses the host cache for Partners and
referral routes. Existing `partnermanager/helper.NewHTTPObserver` accepts
host-selected metric names and logger.

Call `Runtime.Start` only after configuration and routing succeed. Each enabled
worker starts an immediate sequential pass and waits for its interval after a
pass completes. Lifecycle passes also have their configured deadlines. Pass
logs contain only bounded counts and opaque correlation. Call `Close` with a
bounded context before closing borrowed stores; on drain failure leave those
stores open. For example, register `Runtime.Close` as a drain for the Mongo
resource using the parent `serverhelpers.Lifecycle`. Workers do not own Mongo.

`NewPreparationCommand` and `NewRefundRecoveryCommand` supply bounded flags,
validation and safe errors while preserving wrapped error classification. The
host supplies executor callbacks that load configuration, authenticate the
operator and own the actual storage and provider composition. Preparation
requires all three explicit acknowledgements; they are not proof of the stated
conditions. Recovery retains the original snapshot and validates the returned
resolution. `ReadRefundSnapshot` delegates bounded file screening to the
existing provider helper. Neither command runs during server startup.
