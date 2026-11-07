// Package constants holds the tasks module's fixed strings.
//
// They live here rather than inline so that the same sentence is not spelled
// three different ways across the handlers, and so a wording change can be
// made in one place.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "tasks"

// EntityName is the noun used in error messages: "task not found".
const EntityName = "task"

// Messages returned to clients.
const (
	MsgListed    = "tasks retrieved"
	MsgFetched   = "task retrieved"
	MsgCreated   = "task created"
	MsgUpdated   = "task updated"
	MsgCompleted = "task completed"
	MsgDeleted   = "task deleted"
)

// Sortable is the allow-list of columns a client may sort by.
//
// It is an allow-list because a sort key is interpolated into an ORDER BY
// clause. Anything not named here is refused at the edge.
var Sortable = []string{"created_at", "title", "due_at"}
