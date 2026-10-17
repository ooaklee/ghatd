// Package internationalisationmanager composes reference catalogue services and
// a host-secured HTTP API for currencies, phone countries, flags and timezones.
// The host supplies live authentication, CSRF and request admission.
// Optional include_flags=false list requests skip referenced-flag I/O while retaining
// flag IDs for independent SVG loading; default list enrichment is unchanged.
// Native seed-writing setup lives in helper. See README.md for wire/owner policy.
package internationalisationmanager
