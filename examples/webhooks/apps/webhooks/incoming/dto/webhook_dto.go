// Package dto holds the incoming module's request and response shapes.
package dto

// Delivery is one webhook delivery.
//
// ID is the sender's delivery id, which is what makes redelivery detectable:
// a sender that gets no 2xx retries, and the same event arrives twice.
type Delivery struct {
	ID     string `json:"id" validate:"required,max=128"`
	Action string `json:"action" validate:"required,max=64"`
}

// Receipt is what the sender gets back.
//
// Duplicate reports an already-seen delivery id. It is still a 202: the
// sender did nothing wrong and must not retry.
type Receipt struct {
	DeliveryID string `json:"delivery_id"`
	Duplicate  bool   `json:"duplicate"`
}
