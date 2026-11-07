package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IncepTools/go/core/config"
)

type redisCfg struct {
	Addr string `env:"ADDR" default:"localhost:6379"`
	DB   int    `env:"DB" default:"0"`
}

type testCfg struct {
	Port    int           `env:"PORT" default:"8080"`
	DSN     string        `env:"DATABASE_URL" required:"true" secret:"true"`
	Timeout time.Duration `env:"TIMEOUT" default:"30s"`
	Debug   bool          `env:"DEBUG" default:"false"`
	Origins []string      `env:"ORIGINS" default:"a.com,b.com"`
	Ratio   float64       `env:"RATIO" default:"0.5"`
	Redis   redisCfg      `envPrefix:"REDIS_"`
	Ignored string
}

func TestLoadAppliesDefaultsAndParsesTypes(t *testing.T) {
	var cfg testCfg
	err := config.LoadFrom(&cfg, config.MapLookup(map[string]string{
		"DATABASE_URL": "postgres://u:p@host/db",
		"PORT":         "9090",
		"REDIS_ADDR":   "redis:6379",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s — a duration must parse as 30s, not nanoseconds", cfg.Timeout)
	}
	if len(cfg.Origins) != 2 || cfg.Origins[0] != "a.com" {
		t.Errorf("Origins = %v", cfg.Origins)
	}
	if cfg.Ratio != 0.5 {
		t.Errorf("Ratio = %v", cfg.Ratio)
	}
	if cfg.Redis.Addr != "redis:6379" {
		t.Errorf("Redis.Addr = %q, want the prefixed variable to win", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 0 {
		t.Errorf("Redis.DB = %d", cfg.Redis.DB)
	}
}

func TestMissingRequiredIsReportedAllAtOnce(t *testing.T) {
	type cfg struct {
		A string `env:"A" required:"true"`
		B string `env:"B" required:"true"`
	}
	var c cfg
	err := config.LoadFrom(&c, config.MapLookup(nil))
	if err == nil {
		t.Fatal("missing required variables were accepted")
	}
	var cerr *config.Error
	if !as(err, &cerr) {
		t.Fatalf("err is %T, want *config.Error", err)
	}
	if len(cerr.Problems) != 2 {
		t.Fatalf("problems = %v, want both A and B reported in one pass", cerr.Problems)
	}
}

func TestBadValueNamesTheVariable(t *testing.T) {
	type cfg struct {
		Port int `env:"PORT"`
	}
	var c cfg
	err := config.LoadFrom(&c, config.MapLookup(map[string]string{"PORT": "eighty"}))
	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("err = %v; it must name PORT", err)
	}
}

func TestDescribeMasksSecrets(t *testing.T) {
	cfg := testCfg{DSN: "postgres://user:hunter2@host/db", Port: 8080}
	for _, line := range config.Describe(&cfg) {
		if strings.Contains(line, "hunter2") {
			t.Fatalf("Describe leaked a secret: %s", line)
		}
	}
}

func as(err error, target **config.Error) bool { return errors.As(err, target) }
