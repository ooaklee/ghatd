# Communications

`contacter` manages communication records, configured communication types and
aggregate statistics. Its handler uses the package error manifest for failures.

## HTTP routes

`AttachRoutes` registers these exact routes through the shared route registry:

| Path | Methods | Access |
| --- | --- | --- |
| `/api/v1/comms/types` | GET, OPTIONS | Public capability labels and accepted values. |
| `/api/v1/comms/stats` | GET, OPTIONS | Administrator middleware required. |

Supply `AdminOnlyMiddleware` when attaching routes. `AdminAccess` defaults to
`router.AdminSession`; use `router.AdminSessionOrAPI` for an administrator
API-token-or-session adapter. Other modes are rejected. Metadata describes the
adapter; it does not authenticate callers or replace the middleware.

Configure any policy authorizer before attachment. Check
`Router.ValidateRoutePolicies()` after **all** route registration and refuse
startup on error. Missing admin middleware or an invalid mode invalidates the
registry. Its runtime backstop then returns `ROUTE_CONFIGURATION` (503) on every
descriptor, including public type discovery. With valid configuration, type
discovery does not run admin middleware; a configured policy authorizer still
receives its public descriptor. Existing methods and paths are unchanged.

See the [router guide](../router/README.md#declarative-route-policies) for
registration order, inventory and policy enforcement. Record creation, delivery
and user-facing communication workflows remain service/manager responsibilities;
these two routes do not expose arbitrary communication records publicly.
