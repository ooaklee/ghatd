// Package billinglifecyclehelper explicitly composes the native lifecycle
// pipeline over prepared owners and current bound worker authority. Startup
// performs bounded read-only admission/discovery probes, never preparation,
// remote provider lookup or scheduler startup. Hosts own pass deadlines,
// scheduling and resource drain. The separate explicit PrepareNative operation
// validates current preparation authority before additive storage preparation
// and a bounded owning sweep; it is never automatic startup. See README.md.
package billinglifecyclehelper
