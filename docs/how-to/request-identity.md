# Request identity: actors and targets

Use `ActorID` for the verified caller of a manager operation. Use `UserID`,
`TargetUserID` or a resource-specific ID for the subject being read or changed.
An administrator acting on another account has **two different identities**;
permission checks use the actor, while persistence selects the target.

This convention does not turn an ID into a credential or permission. HTTP
middleware verifies credentials and applies route policy. A request mapper binds
the resulting actor, and a service applies its domain authorization. Trusted
in-process callers must establish authority before constructing these commands.
They do not need to simulate an HTTP request.

## User Manager migration

The direct caller fields in `external/usermanager/request.go` are renamed:

| Previous field | Replacement | Meaning |
| --- | --- | --- |
| Manager-owned `UserId` or `UserID` | `ActorID` | Verified caller |
| `GetGroupsByUserIDRequest.ID` | `ActorID` | Caller of the group lookup |
| Embedded lower-domain `UserID`/`ID` | Unchanged | Target account or resource |
| `FilterUserID`, `FilterUserIDs` | Unchanged | Requested target filters, subject to authorization |
| `MyHandleRequest.ActorID` | Unchanged | Verified session caller |

This is a breaking Go source change; there are no deprecated actor aliases.
Update manager struct literals, selectors, fixtures and adapters. Do not perform
a global text replacement: a promoted `UserID` may now refer to an embedded
**target** field, so stale selectors can still compile with the wrong meaning.
Qualify embedded target accesses where that makes the distinction clearer.

For example, a trusted administrative workflow can request another user's groups:

```go
request := &usermanager.GetGroupsByUserIDRequest{
    ActorID: verifiedAdministratorID,
    GetGroupsByUserIDRequest: &group.GetGroupsByUserIDRequest{
        UserID: selectedUserID,
    },
}
response, err := services.UserManager.GetGroupsByUserID(ctx, request)
```

The service checks the actor's administrative authority when the IDs differ.
The target account being an administrator does not authorize the caller.

For `/me` operations, the actor is also the target by definition.
`GetUserGroupMembershipsRequest{ActorID: verifiedCallerID}` requests that caller's
memberships. Use the authorized cross-user group lookup above for another user;
do not substitute a target ID for the caller. Internal enrichment keeps its
already-authorized target queries separate from the self-service actor contract.

This User Manager migration changes only manager-owned request fields.
Billing Manager's separate migration is described below; other managers and
delegated lower-domain requests retain their current APIs.
Contact creation retains the lower-domain `UserId` attribution field, which its
HTTP mapper overwrites after decoding; anonymous contact submissions keep an
empty attribution. Stored IDs, database schemas, routes, JSON target parameters
and response envelopes are unchanged. Migrating a request field does not rename
ownership, authorship or proof-subject fields in a stored record.

## Billing Manager migration

Billing Manager now uses `ActorID` for checkout, portal and optional catalogue
callers, and in place of `RequestingUserID` on its three private billing reads.
The reads' `UserID` remains the selected account. Checkout and portal are
self-service: the actor owns the provider customer. Lower billing/provider
ownership fields and webhook account resolution are unchanged.

Private billing reads reject an empty actor, including trusted in-process calls.
Cross-account reads look up the actor's administrative role, not the target's;
missing or inconsistent authority fails closed. Public catalogue requests still
work without authentication, using an empty actor and public-only projections.
An anonymous rate-limit placeholder cannot enable administrative pricing.
See [Billing Manager](../../external/billingmanager/README.md#actorid-migration)
for the exact source changes and error behavior.

## Transport binding

The [Blueprint reference template](../../internal/blueprint/README.md#actorid-migration)
uses `ActorID` for create, get-by-ID, update and delete requests. Its actor-bearing
HTTP mappers require verified authentication, while lower services accept trusted
in-process composition and reject contradictory published identity. Stored audit
fields and route admission remain unchanged; copying the template does not supply
a product's resource-ownership policy.

Vision's nine mutation commands use `ActorID`, including deletion. Stored
authorship and vote ownership remain distinct from request identity. Its lower
service accepts trusted in-process calls but never derives permissions from an
actor string; User Manager retains owner/administrator authorization. See the
[Vision migration](../../external/vision/README.md#actorid-migration).

Pricer's eight mutation commands also use `ActorID`. Its HTTP mappers require
explicit authentication and reject full plan/feature replacement objects.
Trusted in-process replacements must agree with the selected resource and retain
stored audit history. Pricer remains a lower-domain service: the caller must
preserve the administrator authorization enforced by its HTTP route middleware.
See [Pricer migration](../../external/pricer/README.md#actorid-migration).

Content Manager also uses `ActorID` for its eight reader fields and the four
mutation fields embedded from `post`. Stored author IDs and selected post IDs
remain separate. Its services reject contradictory context evidence rather than
silently using a different caller. HTTP mutation payloads cannot supply complete
post replacements. See the [Content Manager migration](../../external/contentmanager/README.md#actorid-migration)
and [Post domain contract](../../external/post/README.md#actorid-migration) for
trusted composition, public projections and compatibility details.

Actor fields use `json:"-"` and have no query or path tag. The current query
decoder ignores fields without a query tag but interprets `query:"-"` as a
literal `-` parameter; it is **not** a skip marker. Test the actual codec rather
than relying on tag conventions from another decoder.

All actor-bearing mappers in User Manager's `fender.go` now use
`AcquireAuthenticatedUserIDFrom`. Both the authenticated flag and nonempty
caller ID must be present. An anonymous rate-limit placeholder, or a custom
adapter that only calls `TransitWith`, is insufficient. Built-in access
middleware publishes both values. Custom authentication adapters must publish
their verified result with
[`ContextWithAuthentication`](../../external/accessmanager/middleware/context.go)
only after credential verification; never manufacture it from request input.
The display-handle mapper retains its additional active-session restrictions.

Mapper verification is not a substitute for administrative middleware. For
example, notification delivery remains an admin/service route even though its
mapper separates `ActorID` from the recipient's `UserID`. Direct service callers
must preserve any route-owned authorization they bypass.

## Verification checklist

- Test ordinary, administrative and anonymous callers with distinct target IDs.
- Reject missing authentication state even when a context ID is nonempty.
- Verify body/query actor fields, case variants, nulls and a literal `-` query
  cannot overwrite the actor. Keep legitimate target parameters working.
- Assert that the lower domain receives the target, not the actor, where they
  differ; self-service operations must remain bound to the actor.
- Check embedded fields and compile host adapters after changing exported names.
- Use named table-driven cases with independent fixtures and test denied paths
  without invoking lower-domain mutations.

See the [User Manager guide](../../external/usermanager/README.md#actorid-migration)
for the migrated surface and [Starter](../../external/starter/v0/README.md)
for composition.
