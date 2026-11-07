// The examples are a SEPARATE module on purpose.
//
// They need dependencies the framework itself does not — a GORM dialector,
// a database driver — and anything listed in the root go.mod becomes a line
// in the go.sum of every project that imports this library. Keeping them
// here costs one `replace` directive and keeps the library's dependency
// surface honest.
//
// The trade: `go build ./...` from the repository root does NOT cover this
// directory. `make examples` does, and so does CI.
module github.com/IncepTools/go/examples

go 1.26.0

replace github.com/IncepTools/go => ../

require (
	github.com/IncepTools/go v0.0.0
	github.com/glebarez/sqlite v1.11.0
	github.com/go-playground/validator/v10 v10.30.5
	github.com/gofiber/fiber/v3 v3.6.1
	github.com/google/uuid v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/glebarez/go-sqlite v1.21.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/gofiber/schema v1.8.8 // indirect
	github.com/gofiber/utils/v2 v2.6.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/leodido/go-urn v1.5.0 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/molecule-man/go-brrr v1.3.0 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/redis/go-redis/v9 v9.22.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/tinylib/msgp v1.6.5 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.75.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.61.1 // indirect
)
