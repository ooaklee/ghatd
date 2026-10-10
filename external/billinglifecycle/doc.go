// Package billinglifecycle retains original billing lookup inputs and coordinates
// bounded discovery, fenced execution, completion and recovery over encrypted
// record storage. Owning billing managers validate provenance and current worker
// authority. Hosts supply explicit scopes, preparation, scheduling and lifetime;
// constructors create no grants, perform no provider calls and start no workers.
// See README.md for durable formats, uncertainty rules and host adoption.
package billinglifecycle
