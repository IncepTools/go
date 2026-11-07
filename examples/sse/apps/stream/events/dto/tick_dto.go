// Package dto holds the events module's payload shapes.
package dto

import "time"

// Tick is one streamed update.
//
// Sequence is the client's resume point: SSE replays it back in the
// Last-Event-ID header after a dropped connection, so a stream that does not
// number its events cannot be resumed, only restarted.
type Tick struct {
	Sequence int       `json:"sequence"`
	Symbol   string    `json:"symbol"`
	Price    int64     `json:"price_cents"`
	At       time.Time `json:"at"`
}
