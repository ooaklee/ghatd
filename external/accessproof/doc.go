// Package accessproof evaluates request-local, credential-free evidence against
// compiled alternative admission requirements. Hosts authenticate the evidence
// and recheck durable authority before commands or replay; this package does not
// verify tokens, persist grants, or replace resource-relationship checks.
package accessproof
