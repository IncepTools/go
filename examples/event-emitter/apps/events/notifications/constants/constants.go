// Package constants holds the notifications module's fixed strings.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "notifications"

// Subscriber names. The bus uses them in logs and in its subscriber table,
// so a vague one ("handler") is a trace you cannot read later.
const (
	SubAudit  = "orders.audit"
	SubNotify = "orders.notify-customer"
)

// Messages returned to clients.
const (
	MsgPlaced = "order placed"
)
