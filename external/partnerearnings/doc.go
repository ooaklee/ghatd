// Package partnerearnings owns the financial ledger for the Partners program:
// commission accrual and maturity, refund reversals, payout claims and manual
// payment recording. It is the sole writer of earnings and never reads another
// domain's store. Service depends on a driver-free Repository port and computes
// every business rule itself; the host adapter owns datastore I/O, retry and the
// ledger write guard. All mutating operations run inside Repository.WithTransaction
// and fail closed on currency mismatch, changed idempotent payloads, or unavailable
// wiring. Statements derive current balances and bounded journal pages from one
// owning snapshot; host transport must project permitted fields.
// Payment cohort reports conserve original per-payment payout/return backing
// independently of current global balance and original-payment date filters.
// Grouped retained-referral amounts use the same snapshot and full immutable
// revision lists; missing acceptance is not a source-complete zero entitlement.
// Financial metrics separate original-payment cohorts, original economic-date
// commission movements and unfiltered balances/claim exposure, with frozen
// plan provenance and bounded breakdown pages whose totals are cursor-independent.
// Unfiltered current maturity reports due ledger work including zero/reversed
// credit, with net pending amounts and overlapping holds, without executing it.
// Required private maturity sources commit with accrual/completion, preserving
// original deadlines and accepted journal receipts for bounded owning discovery.
// Source reads verify complete financial evidence; they do not start a worker.
// See README for composition, accounting and complete-snapshot cost limitations.
package partnerearnings
