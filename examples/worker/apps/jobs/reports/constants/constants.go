// Package constants holds the reports module's fixed strings.
package constants

// ModuleName is the registry key. The module serves no routes, so this is a
// name rather than a path — it still appears in health output and in the
// lifecycle's hook names, which is where you read it when something stalls.
const ModuleName = "reports"

// Job names. They are the scheduler's keys and the singleton lock's keys, so
// one spelling matters: two names for the same job means two replicas both
// think they hold the lock.
const (
	JobDaily = "reports.daily"
	JobSweep = "reports.sweep"
)

// HealthCheck is the name the readiness report uses for this module.
const HealthCheck = "reports"

// MaxFailedRuns is how many failures the health check tolerates before it
// reports degraded.
const MaxFailedRuns = 10
