// Package observability provides the compatibility HTTP metrics collector.
// New services should prefer core/telemetry, which emits OpenTelemetry
// metrics and spans through pluggable providers. This package remains useful
// for a service that explicitly wants a local Prometheus text endpoint without
// configuring an OpenTelemetry exporter.
package observability

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/web/response"
)

// Metrics is an in-process HTTP request collector.
//
// Labels are method, route template, and status class. The route comes from
// Fiber's matched route, not the raw URL, so an endpoint such as /users/:id
// cannot create an unbounded time-series for every user id.
type Metrics struct {
	mu       sync.RWMutex
	requests map[label]*counter
}

type label struct {
	method string
	route  string
	status string
}

type counter struct {
	count   uint64
	seconds float64
}

// New returns an empty request collector.
func New() *Metrics {
	return &Metrics{requests: make(map[label]*counter)}
}

// Middleware records every request, including failures returned by handlers.
func (m *Metrics) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		// Take the status from the error when there is one: the exception
		// filter has not run yet, so the response still carries the default.
		status := c.Response().StatusCode()
		if err != nil {
			status = response.StatusForError(err)
		}
		m.record(c.Method(), routeName(c), status, time.Since(start))
		return err
	}
}

func (m *Metrics) record(method, route string, status int, elapsed time.Duration) {
	if route == "" {
		route = "<unmatched>"
	}
	key := label{method: method, route: route, status: fmt.Sprintf("%dxx", status/100)}
	m.mu.Lock()
	row := m.requests[key]
	if row == nil {
		row = &counter{}
		m.requests[key] = row
	}
	row.count++
	row.seconds += elapsed.Seconds()
	m.mu.Unlock()
}

// Handler exposes the collector in Prometheus' text exposition format.
func (m *Metrics) Handler(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
	return c.SendString(m.Prometheus())
}

// Prometheus returns a stable snapshot suitable for scraping or testing.
func (m *Metrics) Prometheus() string {
	m.mu.RLock()
	rows := make([]struct {
		key label
		row counter
	}, 0, len(m.requests))
	for key, row := range m.requests {
		rows = append(rows, struct {
			key label
			row counter
		}{key: key, row: *row})
	}
	m.mu.RUnlock()
	sort.Slice(rows, func(i, j int) bool {
		return strings.Join([]string{rows[i].key.method, rows[i].key.route, rows[i].key.status}, "\x00") <
			strings.Join([]string{rows[j].key.method, rows[j].key.route, rows[j].key.status}, "\x00")
	})

	var b strings.Builder
	b.WriteString("# HELP inceptools_http_requests_total Total HTTP requests.\n")
	b.WriteString("# TYPE inceptools_http_requests_total counter\n")
	b.WriteString("# HELP inceptools_http_request_duration_seconds_sum Sum of HTTP request durations.\n")
	b.WriteString("# TYPE inceptools_http_request_duration_seconds_sum counter\n")
	for _, item := range rows {
		labels := fmt.Sprintf("method=%q,route=%q,status_class=%q", item.key.method, item.key.route, item.key.status)
		fmt.Fprintf(&b, "inceptools_http_requests_total{%s} %d\n", labels, item.row.count)
		fmt.Fprintf(&b, "inceptools_http_request_duration_seconds_sum{%s} %g\n", labels, item.row.seconds)
	}
	return b.String()
}

func routeName(c fiber.Ctx) string {
	if route := c.Route(); route != nil && route.Path != "" {
		return route.Path
	}
	return c.Path()
}
