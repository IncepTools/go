package config_test

import (
	"fmt"
	"time"

	"github.com/IncepTools/go/src/core/config"
)

type redisConfig struct {
	Addr string `env:"ADDR" default:"localhost:6379"`
	DB   int    `env:"DB" default:"0"`
}

type appConfig struct {
	Port           int           `env:"PORT" default:"8080"`
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" default:"30s"`
	Origins        []string      `env:"CORS_ORIGINS" default:"https://app.example.com"`
	DatabaseURL    string        `env:"DATABASE_URL" required:"true" secret:"true"`
	Redis          redisConfig   `envPrefix:"REDIS_"`
}

// The whole configuration surface is one struct, loaded once. LoadFrom
// takes a lookup so a test needs no environment mutation.
func Example() {
	var cfg appConfig
	err := config.LoadFrom(&cfg, config.MapLookup(map[string]string{
		"DATABASE_URL": "postgres://user:hunter2@db/shop",
		"REDIS_ADDR":   "redis.internal:6379",
	}))
	if err != nil {
		panic(err)
	}

	fmt.Println(cfg.Port)           // from the default
	fmt.Println(cfg.RequestTimeout) // parsed as a duration, not nanoseconds
	fmt.Println(cfg.Origins)
	fmt.Println(cfg.Redis.Addr) // the prefixed variable
	fmt.Println(cfg.Redis.DB)
	// Output:
	// 8080
	// 30s
	// [https://app.example.com]
	// redis.internal:6379
	// 0
}

// Every problem is reported in one pass. Fixing configuration one restart
// at a time wastes a deploy each round.
func ExampleLoad_reportsEveryProblemAtOnce() {
	type cfg struct {
		DSN  string `env:"DATABASE_URL" required:"true"`
		Port int    `env:"PORT"`
	}
	var c cfg
	err := config.LoadFrom(&c, config.MapLookup(map[string]string{
		"PORT": "eighty",
	}))

	fmt.Println(err)
	// Output:
	// config: 2 problem(s):
	//   - DATABASE_URL is required but not set
	//   - PORT: "eighty" is not an integer
}

// Describe is safe to log at boot: anything tagged secret is masked.
func ExampleDescribe() {
	cfg := appConfig{
		Port:        8080,
		DatabaseURL: "postgres://user:hunter2@db/shop",
	}
	for _, line := range config.Describe(&cfg) {
		fmt.Println(line)
	}
	// Output:
	// CORS_ORIGINS=[]
	// DATABASE_URL=po********op
	// PORT=8080
	// REDIS_ADDR=
	// REDIS_DB=0
	// REQUEST_TIMEOUT=0s
}
