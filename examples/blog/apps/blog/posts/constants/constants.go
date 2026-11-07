// Package constants holds the posts module's fixed strings.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "posts"

// EntityName is the noun used in error messages: "post not found".
const EntityName = "post"

// TopicPublished is the outbox topic for a published post.
const TopicPublished = "post.published"

// Messages returned to clients.
const (
	MsgListed    = "posts retrieved"
	MsgFetched   = "post retrieved"
	MsgCreated   = "post created"
	MsgUpdated   = "post updated"
	MsgPublished = "post published"
	MsgDeleted   = "post deleted"
)

// Sortable is the allow-list of columns a client may sort by.
var Sortable = []string{"created_at", "title", "published_at"}
