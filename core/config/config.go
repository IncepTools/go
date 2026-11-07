// Package config loads configuration from the environment into a struct, once,
// at boot — and fails the boot when something required is missing.
//
// The failure mode this exists to kill: `os.Getenv` scattered through the code,
// read lazily, so a missing variable is discovered by the first request that
// needs it at 2am rather than by the process refusing to start. Declare the
// whole surface as a struct, call Load, and a misconfigured deployment cannot
// come up.
//
//	type Config struct {
//	    Port     int           `env:"PORT" default:"8080"`
//	    DSN      string        `env:"DATABASE_URL" required:"true" secret:"true"`
//	    Timeout  time.Duration `env:"HTTP_TIMEOUT" default:"30s"`
//	    Origins  []string      `env:"CORS_ORIGINS" default:"*"`
//	    Redis    RedisConfig   `envPrefix:"REDIS_"`
//	}
package config

import (
	"bufio"
	"encoding"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tag names read by Load.
const (
	TagEnv      = "env"       // variable name
	TagDefault  = "default"   // value used when the variable is unset or empty
	TagRequired = "required"  // "true" to fail the load when unset
	TagPrefix   = "envPrefix" // prefix applied to a nested struct's variables
	TagSecret   = "secret"    // "true" to mask the value in Describe output
	TagSep      = "sep"       // slice separator, default ","
)

// Error reports every problem found in one Load, rather than the first.
// Fixing configuration one restart at a time is a waste of a deploy.
type Error struct{ Problems []string }

func (e *Error) Error() string {
	return fmt.Sprintf("config: %d problem(s):\n  - %s", len(e.Problems), strings.Join(e.Problems, "\n  - "))
}

// Lookup resolves a variable name. It exists so tests and the CLI can load a
// config from a map instead of the real environment.
type Lookup func(key string) (string, bool)

// OSLookup reads the process environment.
func OSLookup(key string) (string, bool) { return os.LookupEnv(key) }

// MapLookup reads a fixed map. Useful in tests.
func MapLookup(m map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// Load fills dst (a non-nil pointer to a struct) from the process environment.
func Load(dst any) error { return LoadFrom(dst, OSLookup) }

// LoadFrom fills dst from an arbitrary lookup.
func LoadFrom(dst any, lookup Lookup) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("config: Load expects a non-nil pointer to a struct, got %T", dst)
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("config: Load expects a pointer to a struct, got pointer to %s", elem.Kind())
	}
	var problems []string
	walk(elem, "", lookup, &problems)
	if len(problems) > 0 {
		sort.Strings(problems)
		return &Error{Problems: problems}
	}
	return nil
}

func walk(v reflect.Value, prefix string, lookup Lookup, problems *[]string) {
	t := v.Type()
	for i := range t.NumField() {
		field, value := t.Field(i), v.Field(i)
		if !field.IsExported() {
			continue
		}
		// A nested struct groups variables under a prefix. time.Time and any
		// type implementing TextUnmarshaler are leaves, not groups.
		if value.Kind() == reflect.Struct && !isLeaf(value) {
			walk(value, prefix+field.Tag.Get(TagPrefix), lookup, problems)
			continue
		}
		if value.Kind() == reflect.Pointer && value.Type().Elem().Kind() == reflect.Struct && !isLeaf(value) {
			if value.IsNil() {
				value.Set(reflect.New(value.Type().Elem()))
			}
			walk(value.Elem(), prefix+field.Tag.Get(TagPrefix), lookup, problems)
			continue
		}

		name := field.Tag.Get(TagEnv)
		if name == "" || name == "-" {
			continue
		}
		name = prefix + name

		raw, found := lookup(name)
		if !found || raw == "" {
			if def, ok := field.Tag.Lookup(TagDefault); ok {
				raw, found = def, true
			}
		}
		if !found || raw == "" {
			if field.Tag.Get(TagRequired) == "true" {
				*problems = append(*problems, name+" is required but not set")
			}
			continue
		}
		sep := field.Tag.Get(TagSep)
		if sep == "" {
			sep = ","
		}
		if err := assign(value, strings.TrimSpace(raw), sep); err != nil {
			*problems = append(*problems, fmt.Sprintf("%s: %v", name, err))
		}
	}
}

func isLeaf(v reflect.Value) bool {
	if _, ok := v.Interface().(time.Time); ok {
		return true
	}
	if v.CanAddr() {
		if _, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return true
		}
	}
	_, ok := v.Interface().(encoding.TextUnmarshaler)
	return ok
}

func assign(v reflect.Value, raw, sep string) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.CanAddr() {
		if u, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return u.UnmarshalText([]byte(raw))
		}
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("%q is not a boolean", raw)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// time.Duration is an int64; parse it as a duration, not a count of
		// nanoseconds, because "30s" is what a human writes in a deployment.
		if v.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("%q is not a duration (try 30s, 5m)", raw)
			}
			v.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q is not an integer", raw)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q is not an unsigned integer", raw)
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q is not a number", raw)
		}
		v.SetFloat(f)
	case reflect.Slice:
		parts := strings.Split(raw, sep)
		out := reflect.MakeSlice(v.Type(), 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			item := reflect.New(v.Type().Elem()).Elem()
			if err := assign(item, p, sep); err != nil {
				return err
			}
			out = reflect.Append(out, item)
		}
		v.Set(out)
	case reflect.Map:
		// "k=v,k2=v2"
		out := reflect.MakeMap(v.Type())
		for _, p := range strings.Split(raw, sep) {
			k, val, ok := strings.Cut(p, "=")
			if !ok {
				return fmt.Errorf("%q is not a k=v list", raw)
			}
			kv := reflect.New(v.Type().Key()).Elem()
			vv := reflect.New(v.Type().Elem()).Elem()
			if err := assign(kv, strings.TrimSpace(k), sep); err != nil {
				return err
			}
			if err := assign(vv, strings.TrimSpace(val), sep); err != nil {
				return err
			}
			out.SetMapIndex(kv, vv)
		}
		v.Set(out)
	default:
		return fmt.Errorf("unsupported field type %s", v.Kind())
	}
	return nil
}

// Describe returns one line per configured variable, with any field tagged
// `secret:"true"` masked. Log it at boot: knowing what the process actually
// read beats guessing which ConfigMap won.
func Describe(src any) []string {
	rv := reflect.ValueOf(src)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	describe(rv, "", &out)
	sort.Strings(out)
	return out
}

func describe(v reflect.Value, prefix string, out *[]string) {
	t := v.Type()
	for i := range t.NumField() {
		field, value := t.Field(i), v.Field(i)
		if !field.IsExported() {
			continue
		}
		if value.Kind() == reflect.Struct && !isLeaf(value) {
			describe(value, prefix+field.Tag.Get(TagPrefix), out)
			continue
		}
		name := field.Tag.Get(TagEnv)
		if name == "" || name == "-" {
			continue
		}
		shown := fmt.Sprintf("%v", value.Interface())
		if field.Tag.Get(TagSecret) == "true" {
			shown = mask(shown)
		}
		*out = append(*out, prefix+name+"="+shown)
	}
}

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", min(len(s)-4, 8)) + s[len(s)-2:]
}

// LoadDotEnv reads a .env file and sets any variable that is not ALREADY in
// the environment. The precedence is deliberate: a real environment variable
// always wins, so a committed .env cannot override what a deployment sets.
//
// A missing file is not an error — that is the normal production case.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}
