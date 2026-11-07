// Package dto holds the jobs module's request and response shapes.
package dto

// SubmitJob is the enqueue request.
//
// Validation happens at the edge, before the job is durable. A queue is a bad
// place to discover that a payload was never going to work: the worker fails,
// retries the same bad payload, and eventually dead-letters it.
type SubmitJob struct {
	Kind    string `json:"kind" validate:"required,oneof=resize report export"`
	Subject string `json:"subject" validate:"required,max=200"`
	// Fail makes the handler return an error, so the retry and dead-letter
	// paths can be watched without editing code.
	Fail bool `json:"fail"`
}

// Normalize implements binding.Normalizer: it runs before validation, so the
// oneof rule sees the value that will actually be enqueued.
func (s *SubmitJob) Normalize() { s.Kind = lower(s.Kind) }

// JobReceipt is what the client gets back.
//
// The id is the queue's, not the job's position: a queue offers no ordering
// guarantee worth exposing, and a client that is handed a position will
// eventually depend on it.
type JobReceipt struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

func lower(s string) string {
	out := []byte(s)
	for i, b := range out {
		if b >= 'A' && b <= 'Z' {
			out[i] = b + 32
		}
	}
	return string(out)
}
