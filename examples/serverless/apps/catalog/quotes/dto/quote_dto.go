// Package dto holds the quotes module's response shapes.
package dto

// Quote is what the API returns.
//
// Currency comes from configuration the module reloads in place, so the same
// symbol can answer in a different currency without a restart — which is the
// thing a serverless deployment cannot do with SIGHUP.
type Quote struct {
	Symbol   string `json:"symbol"`
	Currency string `json:"currency"`
	Price    int64  `json:"price_cents"`
}
