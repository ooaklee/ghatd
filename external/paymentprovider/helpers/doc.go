// Package helpers provides optional Stripe configuration, paid-period parsing
// and retained refund-file input screening. StripeProviderOptions binds explicit
// clients, revenue configuration and trusted promotion-code policy at construction.
// Hosts supply credentials, routes and private file selection; owning services
// retain financial evidence verification and current authority.
// See README.md for configuration, failure and proof boundaries.
package helpers
