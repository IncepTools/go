// Package dto holds the notifications module's request and response shapes.
package dto

// PlaceOrder is the create request.
type PlaceOrder struct {
	Reference  string `json:"reference" validate:"required,max=64"`
	Email      string `json:"email" validate:"required,email"`
	TotalCents int64  `json:"total_cents" validate:"required,gt=0"`
}

// OrderReceipt is what the API returns.
//
// It reports what is true when the response is written. The async
// subscribers have not finished, so claiming "notified" here would be a lie
// the client has no way to check.
type OrderReceipt struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
}
