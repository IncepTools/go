// Package service holds the quotes module's business rules.
//
// Note what it does NOT import: nothing from web/, and nothing that knows
// whether the process is a server or a function. Hosting is the host's
// concern; this is the same code either way.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync/atomic"

	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/constants"
	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/dto"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
)

// settings is the reloadable half of the configuration.
type settings struct {
	Currency string `json:"currency"`
}

// QuoteService answers quotes using configuration it can refresh in place.
type QuoteService struct {
	path string
	// An atomic value, not a plain field: Reload can run while requests are
	// in flight, and a torn read of a string under a concurrent write is a
	// data race, not merely a stale answer. Swapping the whole struct at once
	// also means no request ever sees half of a new configuration.
	current atomic.Pointer[settings]
	reloads atomic.Int64
}

// NewQuoteService builds the service with the configuration present at cold
// start.
func NewQuoteService() *QuoteService {
	path := os.Getenv(constants.EnvConfigPath)
	if path == "" {
		path = constants.DefaultConfigPath
	}
	s := &QuoteService{path: path}
	loaded, err := read(path)
	if err != nil {
		// Starting on defaults is the deliberate choice here, because the
		// file is optional. Configuration that is genuinely required should
		// fail the boot instead: a service that silently starts without its
		// real settings is worse than one that refuses to start.
		loaded = &settings{Currency: constants.DefaultCurrency}
	}
	s.current.Store(loaded)
	return s
}

// Reload re-reads the file without restarting anything.
//
// On failure it returns the error and leaves the previous configuration in
// place. That is the contract that matters: a reload must never half-apply,
// and a bad file must not take a healthy process down with it.
func (s *QuoteService) Reload(ctx context.Context) error {
	next, err := read(s.path)
	if err != nil {
		return err
	}
	s.current.Store(next)
	s.reloads.Add(1)
	logx.C(ctx).Info("quote configuration reloaded",
		"path", s.path, "currency", next.Currency, "reloads", s.reloads.Load())
	return nil
}

// Quote prices one symbol.
func (s *QuoteService) Quote(_ context.Context, symbol string) (dto.Quote, error) {
	if symbol == "unknown" {
		return dto.Quote{}, errs.NotFound("no quote for %q", symbol).
			WithCode("NO_SUCH_SYMBOL")
	}
	return dto.Quote{
		Symbol:   symbol,
		Currency: s.current.Load().Currency,
		Price:    10_000 + int64(len(symbol))*125,
	}, nil
}

// read parses the file, treating absence as "use the defaults" and a
// malformed file as an error worth reporting.
func read(path string) (*settings, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // an operator-supplied config path
	if errors.Is(err, fs.ErrNotExist) {
		return &settings{Currency: constants.DefaultCurrency}, nil
	}
	if err != nil {
		return nil, errs.Unavailable("reading %s failed", path).WithCause(err)
	}
	var out settings
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errs.Invalid("%s is not valid JSON", path).WithCause(err)
	}
	if out.Currency == "" {
		out.Currency = constants.DefaultCurrency
	}
	return &out, nil
}
