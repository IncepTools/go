// Package constants holds the quotes module's fixed strings.
package constants

// ModuleName is the route segment and the registry key.
const ModuleName = "quotes"

// EnvConfigPath names the file the module re-reads on every reload. A real
// deployment points this at a mounted secret or a parameter-store cache.
const EnvConfigPath = "QUOTES_CONFIG"

// DefaultConfigPath is used when the environment says nothing. A missing file
// is not an error: the module starts on its defaults.
const DefaultConfigPath = "quotes.json"

// DefaultCurrency is used when the file says nothing.
const DefaultCurrency = "USD"

// Messages returned to clients.
const (
	MsgQuoted = "quote rendered"
)
