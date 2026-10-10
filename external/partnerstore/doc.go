// Package partnerstore adapts program, referral and earnings repositories to
// encrypted shared recordstore transactions. It commits durable references,
// immutable audit revisions, financial journals and receipts atomically under
// domain guards, without selecting commercial rules.
// Required private maturity sources share their journal transaction and retain
// original deadlines and accepted receipts without expiry in a scoped index.
// Link-scoped visit receipts freeze concurrent observations and anonymous day
// counts atomically. Only raw
// visit rows receive explicit expiration. Complete analytics share one bounded owning
// read snapshot. Optional conversion evidence adds complete immutable customer
// bindings to that same traffic/ownership read with a shared bounded budget.
// See README.md for startup,
// complete-history reads, uncertain commits and restore requirements.
package partnerstore
