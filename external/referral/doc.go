// Package referral owns stable case-sensitive links, signed browser evidence,
// immutable signup ownership revisions and frozen economic-payment bindings.
// It accepts trusted owning-service identity/policy facts and performs no billing
// or earnings I/O. Event-time binding selection and ownership correction use one
// customer-scoped repository transaction. Reviewed prospective corrections
// retain all existing bindings and recover an immutable actor/key receipt.
// Lifetime memberships retain former owners' visibility; snapshot reads project
// only the selected partner's owned periods after validating complete history.
// Optional complete relationship evidence validates all memberships, histories
// and immutable bindings in one bounded read, without correction guard writes.
// Its private canonical revision supports global partner reporting, independent
// of traffic collection and list pagination.
// Optional combined conversion evidence adds complete traffic to that native
// snapshot, retaining canonical revisions and exposing no transport fields.
// Opt-in consented visit measurement freezes a keyed, link-scoped origin;
// explicit approved retention expires raw rows while anonymous daily counts and
// signed original times preserve history. Complete owning analytics separate visit cohorts, signup periods and retained
// memberships. Optional analytics never gate signup or financial acceptance.
// See README.md for evidence lifetime,
// cutover semantics, correction preconditions and recovery contracts.
package referral
