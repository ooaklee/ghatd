# Access composition

`access` connects existing `accesspolicy`, `accessmanager` and
`accesspolicymanager` owners without opening another database pool or copying
authorization rules into a host. Call it from the explicit composition root.

1. Resolve one stable authority `System` from trusted application policy. It is
   not a deployment/component name, claim or request field.
2. Call `NewRuntime(ctx, RuntimeConfig{System: system, Core: mongo.CoreRepository,
   Routes: routes})` **before attaching any routes**. It prepares transactional
   policy storage and installs the route-policy guard. Startup defaults to a
   30-second bound, configurable with `StartupTimeout`.
3. Initialize API-token inventory with the owning repository's
   `InitializeTokenInventory` under the host's bounded startup context. Supply
   `runtime.Service()` to `accesspolicy.TokenPolicy` and other live enforcement
   consumers using that same system.
4. After Access Manager and its middleware exist, call `AttachManagement` with
   the existing session service, user repository, token inventory and **real
   explicit-bearer middleware**. `Capabilities` explicitly enables scope and
   permission management; false retains token-limit management only.
5. Validate all route policies before serving. Any construction or attachment
   error aborts startup; route changes are not transactional.

Every management operation rechecks the current session and ACTIVE administrator
through the Access Manager authorizer. Cookie-only callers, API credentials and
forged pass-through bearer middleware cannot provision a grant. The selected
target stays separate from the verified actor. Startup never creates a user,
credential, default grant or capability.

`AdminAccess: nil` disables the browser proof bridge. To enable it, supply
`AdminAccessConfig` with an exact origin, exact access-cookie name, existing
Redis connection, purpose namespace and existing email sender. The absolute
proof window must be one to five minutes. Delivery must be enabled outside the
exact `local` environment. Configuration is checked before route attachment.
Redis and Mongo remain caller-owned; this package has no shutdown function.

Canonical domain contracts: [policy](../../accesspolicy/README.md),
[management](../../accesspolicymanager/README.md),
[browser proof bridge](../../accesspolicy/adminaccess/README.md).

## Validation

`go test ./external/serverhelpers/access` covers dependency failures, optional
surfaces, cancellation and denial of a forged bearer adapter. Set
`GHATD_TEST_MONGO_URI` to an isolated replica set to run the native provisioning
cases. Those create unique case-owned databases and remove only those databases;
they exercise actual policy, inventory and user persistence while substituting
signature/session storage. They do not prove an external identity-provider flow.
