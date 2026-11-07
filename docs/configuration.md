# Configuration

`core/config` loads the environment into a struct, once, at boot — and fails
the boot when something required is missing.

```go
package appconfig

import "time"

type Config struct {
	Port           int           `env:"PORT" default:"8080"`
	LogLevel       string        `env:"LOG_LEVEL" default:"info"`
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" default:"30s"`
	Origins        []string      `env:"CORS_ORIGINS" default:"https://app.example.com"`

	DatabaseURL string `env:"DATABASE_URL" required:"true" secret:"true"`

	Redis RedisConfig `envPrefix:"REDIS_"`
}

type RedisConfig struct {
	Addr string `env:"ADDR" default:"localhost:6379"`
	DB   int    `env:"DB" default:"0"`
}
```

```go
var cfg appconfig.Config
if err := config.Load(&cfg); err != nil {
	return err
}
```

## The failure this kills

`os.Getenv` scattered through the code, read lazily, so a missing variable is
discovered by the first request that needs it — at 2am, by a user. Declare
the whole surface as a struct and a misconfigured deployment cannot come up.

Every problem is reported in **one pass**:

```
config: 2 problem(s):
  - DATABASE_URL is required but not set
  - REQUEST_TIMEOUT: "thirty" is not a duration (try 30s, 5m)
```

Fixing configuration one restart at a time wastes a deploy each round.

## Tags

| Tag | |
|---|---|
| `env:"NAME"` | The variable name. Without it the field is skipped. |
| `default:"x"` | Used when unset **or empty**. |
| `required:"true"` | Fail the load when unset and no default. |
| `envPrefix:"REDIS_"` | Prefix for a nested struct's variables. |
| `secret:"true"` | Masked by `config.Describe`. |
| `sep:";"` | Slice and map separator; default `,`. |

## Types

`string`, `bool`, all integer and float widths, `time.Duration`, slices,
`map[string]string`, nested structs, pointers, and anything implementing
`encoding.TextUnmarshaler`.

`time.Duration` parses as a duration, not a count of nanoseconds, because
`30s` is what a human writes in a deployment manifest.

```go
Origins []string          `env:"CORS_ORIGINS"`   // "a.com,b.com"
Labels  map[string]string `env:"LABELS"`         // "team=core,tier=1"
Level   slog.Level        `env:"LOG_LEVEL"`      // TextUnmarshaler
```

## Dotenv

```go
_ = config.LoadDotEnv(".env")
```

Sets only variables that are **not already** in the environment. The
precedence is deliberate: a real environment variable always wins, so a
committed `.env` cannot override what a deployment sets. A missing file is not
an error — that is the normal production case.

## Logging what was read

```go
logger.Info("configuration", "values", config.Describe(&cfg))
```

```
DATABASE_URL=po******db
LOG_LEVEL=info
PORT=8080
REDIS_ADDR=localhost:6379
```

Fields tagged `secret:"true"` are masked. Log this at boot: knowing what the
process actually read beats guessing which config map won.

## Testing

```go
err := config.LoadFrom(&cfg, config.MapLookup(map[string]string{
	"DATABASE_URL": "postgres://localhost/test",
	"PORT":         "9090",
}))
```

No environment mutation, so tests stay parallel-safe.

## Conventions

**One struct per service**, in `internal/appconfig`. A second configuration
source somewhere else is the thing this package exists to prevent.

**Mark every credential `secret:"true"`.** The cost is a tag; the cost of
forgetting is a password in a log aggregator with a long retention.

**Default generously, require sparingly.** A variable with a sensible default
is one fewer thing a deployment has to get right. Reserve `required` for the
ones with no safe default — a DSN, a signing key.

**Do not put a DSN in a file.** `icpt.json` names the *variable*, never the
value. See [`icpt.json`](project-config.md).

**Validate beyond the types.** If two fields must agree, check it after
`Load` and return an error. A boot-time check is free; a runtime surprise is
not.

```go
if cfg.ReadReplicaURL != "" && cfg.ReadReplicaURL == cfg.DatabaseURL {
	return errors.New("READ_REPLICA_URL points at the primary")
}
```

## Next

- [First steps](first-steps.md) — configuration in a working `main.go`.
- [Logging](logging.md) — the logger built from it.
