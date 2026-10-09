// Package partnerruntime composes Partners owners over borrowed encrypted Mongo
// storage from explicit trusted config and dependency ports. NewRuntime binds
// services without database I/O; Prepare explicitly creates indexes and probes
// transaction readiness before the host admits requests or workers. See README.md
// for key separation, ownership, scheduling and activation requirements.
package partnerruntime
