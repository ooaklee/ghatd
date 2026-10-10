// Package teleprovider supplies stateless, explicitly configured messaging and
// registration ports with an optional OpenWA HTTP adapter. Construction performs
// no I/O. Registration is availability evidence; message receipts prove provider
// acceptance, never delivery or destination authority. Hosts retain consent,
// durable work, reply deduplication and reconciliation of uncertain mutations.
package teleprovider
