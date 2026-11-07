// Package constants holds the events module's fixed strings.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "events"

// EventTick is the SSE event name clients listen for.
//
// It is named rather than left blank because a client registers listeners per
// event name. An unnamed event only reaches onmessage, so adding a second
// kind later would silently change what existing clients receive.
const EventTick = "tick"

// Interval names in the query string.
const (
	QueryInterval = "interval"
)
