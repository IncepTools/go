// Package constants holds the incoming module's fixed strings.
//
// They live here rather than inline so the same sentence is not spelled three
// different ways across the handlers, and so a wording change is made once.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "incoming"

// SignatureHeader is the header the sender puts the HMAC in. GitHub spells it
// this way; Stripe and Shopify use different names and the same scheme.
const SignatureHeader = "X-Hub-Signature-256"

// Messages returned to the sender.
const (
	MsgAccepted = "webhook accepted"
)
