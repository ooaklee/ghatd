// Package billinglifecyclehelper explicitly composes the native lifecycle
// pipeline over prepared owners and current bound worker authority. Startup
// performs bounded read-only admission/discovery probes, never preparation,
// remote provider lookup or scheduler startup. Hosts own pass deadlines,
// scheduling and resource drain. See README.md for the complete contract.
package billinglifecyclehelper
