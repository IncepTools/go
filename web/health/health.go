// Package health answers the two questions an orchestrator asks, which are
// not the same question.
//
//	liveness  — is this process broken beyond recovery? Restart me if not.
//	readiness — can this process serve traffic right now? Route to me if so.
//
// Conflating them is the classic Kubernetes outage: a liveness probe that
// pings the database means a database blip restarts every replica
// simultaneously, turning a recoverable dependency failure into a total one.
// So Live here checks NOTHING but the process being able to answer, and Ready
// is where dependencies belong.
package health

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Status is the outcome of a check.
type Status string

const (
	// StatusUp means working.
	StatusUp Status = "UP"
	// StatusDegraded means working with reduced capability — a cache that is
	// down but is only a cache. A degraded service still takes traffic.
	StatusDegraded Status = "DEGRADED"
	// StatusDown means not working. A down service is removed from the load
	// balancer.
	StatusDown Status = "DOWN"
)

// Severity says what a failing check means for the service as a whole.
type Severity int

const (
	// Critical means this failing takes the service out of rotation.
	Critical Severity = iota
	// Optional means this failing degrades the service but it keeps serving.
	//
	// Most caches, most third-party APIs and every analytics sink belong
	// here. A service that reports itself DOWN because an analytics endpoint
	// is slow has made an outage out of a nuisance.
	Optional
)

// Check is one dependency's result.
type Check struct {
	Name      string         `json:"name"`
	Status    Status         `json:"status"`
	Message   string         `json:"message,omitempty"`
	LatencyMS int64          `json:"latencyMs"`
	Details   map[string]any `json:"details,omitempty"`
	Severity  string         `json:"severity,omitempty"`
}

// Checker probes one dependency.
//
// An implementation MUST honour ctx: the registry gives each check a deadline,
// and a check that ignores it holds up the whole report — which is how a
// readiness endpoint ends up timing out and taking every replica out of
// rotation at once.
type Checker interface {
	Name() string
	Severity() Severity
	Check(ctx context.Context) error
}

type fnChecker struct {
	name string
	sev  Severity
	fn   func(ctx context.Context) error
}

func (f fnChecker) Name() string                    { return f.name }
func (f fnChecker) Severity() Severity              { return f.sev }
func (f fnChecker) Check(ctx context.Context) error { return f.fn(ctx) }

// Of builds a Checker from a function.
func Of(name string, sev Severity, fn func(ctx context.Context) error) Checker {
	return fnChecker{name: name, sev: sev, fn: fn}
}

// Report is the aggregate.
type Report struct {
	Status  Status           `json:"status"`
	Service string           `json:"service,omitempty"`
	Version string           `json:"version,omitempty"`
	Uptime  string           `json:"uptime,omitempty"`
	Checks  map[string]Check `json:"checks,omitempty"`
}

// Registry holds the checks a service runs. The zero value is usable.
type Registry struct {
	mu       sync.RWMutex
	checkers []Checker
	// Timeout bounds each individual check. Defaults to 2s.
	Timeout time.Duration
	Service string
	Version string
	started time.Time
}

// New returns a registry stamped with the service identity.
func New(service, version string) *Registry {
	return &Registry{Service: service, Version: version, started: time.Now(), Timeout: 2 * time.Second}
}

// Register adds a checker.
func (r *Registry) Register(c ...Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started.IsZero() {
		r.started = time.Now()
	}
	r.checkers = append(r.checkers, c...)
}

// Run probes every dependency CONCURRENTLY and aggregates.
//
// Concurrently because sequentially is how a readiness endpoint with six
// two-second checks takes twelve seconds and gets killed by the probe timeout
// it was meant to satisfy.
func (r *Registry) Run(ctx context.Context) Report {
	r.mu.RLock()
	checkers := append([]Checker(nil), r.checkers...)
	timeout := r.Timeout
	service, version, started := r.Service, r.Version, r.started
	r.mu.RUnlock()

	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	results := make([]Check, len(checkers))
	var wg sync.WaitGroup
	for i, c := range checkers {
		wg.Add(1)
		go func(i int, c Checker) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			start := time.Now()
			err := safeCheck(cctx, c)
			res := Check{
				Name:      c.Name(),
				Status:    StatusUp,
				LatencyMS: time.Since(start).Milliseconds(),
			}
			if c.Severity() == Optional {
				res.Severity = "optional"
			}
			if err != nil {
				res.Message = err.Error()
				if c.Severity() == Optional {
					res.Status = StatusDegraded
				} else {
					res.Status = StatusDown
				}
			}
			results[i] = res
		}(i, c)
	}
	wg.Wait()

	report := Report{
		Status:  StatusUp,
		Service: service,
		Version: version,
		Checks:  make(map[string]Check, len(results)),
	}
	if !started.IsZero() {
		report.Uptime = time.Since(started).Round(time.Second).String()
	}
	for _, res := range results {
		report.Checks[res.Name] = res
		switch res.Status {
		case StatusDown:
			report.Status = StatusDown
		case StatusDegraded:
			if report.Status == StatusUp {
				report.Status = StatusDegraded
			}
		}
	}
	return report
}

func safeCheck(ctx context.Context, c Checker) (err error) {
	// A panicking health check must not take the process down with it: the
	// endpoint exists to report trouble, not to become it.
	defer func() {
		if r := recover(); r != nil {
			err = &panicErr{v: r}
		}
	}()
	return c.Check(ctx)
}

type panicErr struct{ v any }

func (p *panicErr) Error() string { return "health check panicked" }

// LiveHandler answers liveness. It checks nothing: if this code runs, the
// process is alive. Point the liveness probe here and nowhere else.
func LiveHandler() fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": StatusUp})
	}
}

// ReadyHandler answers readiness: 200 while the service can serve, 503 once a
// critical dependency is down. A DEGRADED report still returns 200 — that is
// what Optional means.
func ReadyHandler(r *Registry) fiber.Handler {
	return func(c fiber.Ctx) error {
		report := r.Run(c.Context())
		status := fiber.StatusOK
		if report.Status == StatusDown {
			status = fiber.StatusServiceUnavailable
		}
		return c.Status(status).JSON(report)
	}
}

// DetailHandler returns the full report with per-check latency, always 200.
// It is for humans and dashboards, not for probes.
//
// Put it behind authentication: the check names and messages describe your
// internal topology.
func DetailHandler(r *Registry) fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(r.Run(c.Context()))
	}
}

// PingChecker adapts anything with a context ping — a *sql.DB, a redis
// client, an HTTP probe — to a Checker.
func PingChecker(name string, sev Severity, ping func(ctx context.Context) error) Checker {
	return Of(name, sev, ping)
}
