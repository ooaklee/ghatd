// Package contentmanager composes CMS post operations, verified request actors,
// publication-safe public projections and administrator-protected ingestion.
//
// NewService accepts optional ServiceOptions. WithIndexNow adds post-commit URL
// change notifications and safe receipts while preserving disabled mutation I/O;
// hosts supply canonical paths and storage policy. Scheduled publication uses an
// explicit bounded reconciliation window, not an implicit distributed scheduler.
// IsPublicPostAt shares publication eligibility with host renderers and filters.
// See README.md for composition, failure semantics and checkpoint responsibilities.
package contentmanager
