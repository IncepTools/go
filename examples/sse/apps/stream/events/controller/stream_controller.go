// Package controller handles the events module's HTTP surface.
//
// The controller owns the transport: it reads the interval and the resume
// point off the request, adapts the service's callback to SSE frames, and
// sets the reconnection policy. The service knows none of that.
package controller

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/sse/apps/stream/events/constants"
	"github.com/IncepTools/go/examples/sse/apps/stream/events/dto"
	"github.com/IncepTools/go/examples/sse/apps/stream/events/service"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
	"github.com/IncepTools/go/src/web/sse"
)

// StreamController serves the event stream.
type StreamController struct {
	svc *service.FeedService
}

// New builds the controller.
func New(svc *service.FeedService) *StreamController { return &StreamController{svc: svc} }

// Routes declares the endpoints.
func (c *StreamController) Routes() []router.Route {
	return []router.Route{
		router.Get("/", c.stream).
			AsPublic().
			Summary("Stream ticks as server-sent events").Tag("sse").
			QueryParam("interval", "Gap between ticks, e.g. 500ms or 2s").
			Returns(dto.Tick{}),
	}
}

func (c *StreamController) stream(ctx fiber.Ctx) error {
	var requested time.Duration
	if raw := ctx.Query(constants.QueryInterval); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return response.Fail(ctx, errs.Invalid("interval %q is not a duration", raw).
				WithCode("BAD_INTERVAL").WithCause(err))
		}
		requested = d
	}
	interval, err := c.svc.Interval(requested)
	if err != nil {
		// Fail BEFORE the stream opens. Once the first byte is written the
		// status is already 200, and an error after that can only be
		// delivered as an event the client has to know to look for.
		return response.Fail(ctx, err)
	}

	// Last-Event-ID is what the browser replays after a dropped connection.
	// Honouring it is the difference between resuming and starting over.
	after := 0
	if raw := ctx.Get("Last-Event-ID"); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil && n > 0 {
			after = n
		}
	}

	return sse.New(sse.Config{
		// Retry tells the client how long to wait before reconnecting. A
		// default of zero means browsers reconnect immediately, which turns a
		// restart into a stampede.
		Retry: 3 * time.Second,
		Handler: func(_ fiber.Ctx, stream *sse.Stream) error {
			return c.svc.Stream(stream.Context(), interval, after, func(tick dto.Tick) error {
				return stream.Event(sse.Event{
					Name: constants.EventTick,
					ID:   strconv.Itoa(tick.Sequence),
					Data: tick,
				})
			})
		},
	})(ctx)
}
