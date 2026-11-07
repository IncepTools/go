// Package constants holds the reports module's fixed strings.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "reports"

// Job names. They are the scheduler's keys and the singleton lock's keys, so
// one spelling matters: two names for the same job means two replicas both
// think they hold the lock.
const (
	JobDaily   = "reports.daily"
	JobCleanup = "reports.cleanup"
)

// Messages returned to clients.
const (
	MsgStatus = "report status retrieved"
)
