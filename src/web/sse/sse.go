// Package sse exposes Fiber's SSE transport through the inceptools web layer.
package sse

import fibersse "github.com/gofiber/fiber/v3/middleware/sse"

type Event = fibersse.Event
type Stream = fibersse.Stream
type Handler = fibersse.Handler
type Config = fibersse.Config

var New = fibersse.New
