// Package dto holds the hello module's response shapes.
package dto

// Greeting is what the API returns.
//
// It is a declared type rather than a map because the route table records it:
// `Returns(dto.Greeting{})` is what makes the response shape appear in the
// generated documentation instead of being discoverable only by calling it.
type Greeting struct {
	Hello string `json:"hello"`
}
