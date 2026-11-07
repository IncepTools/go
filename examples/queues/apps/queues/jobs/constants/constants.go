// Package constants holds the jobs module's fixed strings.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "jobs"

// QueueName is the queue this module enqueues onto. The worker logs it, so
// one spelling matters.
const QueueName = "jobs"

// Messages returned to clients.
const (
	MsgEnqueued = "job enqueued"
)
