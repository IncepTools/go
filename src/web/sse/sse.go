// Package sse exposes Fiber's SSE transport through the inceptools web layer.
package sse

import fibersse "github.com/gofiber/fiber/v3/middleware/sse"

// Event is a server-sent event payload.
type Event = fibersse.Event

// Stream writes server-sent events to a client connection.
type Stream = fibersse.Stream

// Handler is the Fiber handler used to serve an SSE stream.
type Handler = fibersse.Handler

// Config configures the SSE middleware.
type Config = fibersse.Config

// New creates Fiber's SSE middleware.
var New = fibersse.New
