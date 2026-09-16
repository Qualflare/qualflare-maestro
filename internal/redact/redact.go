// Package redact keeps variable values out of the report.
//
// Maestro evaluates variables before it reports an error, so a flow failing on
// `assertVisible: ${SECRET}` produces a failure message holding the real value.
// The reporter knows two sources of values -- `--env KEY=VALUE` arguments and
// MAESTRO_* variables in its own environment, which Maestro copies into every
// flow -- and replaces each occurrence with ${KEY}.
package redact

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// MinLength is the shortest value that is redacted. Shorter values such as "1"
// or "on" would match all over an unrelated report.
const MinLength = 4

// Pair is one variable and its value.
type Pair struct {
	Key   string
	Value string
}

// Redactor replaces known values. The zero value and nil redact nothing.
type Redactor struct {
	replacer *strings.Replacer
}

// New builds a redactor from variable pairs.
func New(pairs []Pair) *Redactor {
	var kept []Pair
	for _, p := range pairs {
		if utf8.RuneCountInString(p.Value) >= MinLength &&
			!strings.EqualFold(p.Value, "true") && !strings.EqualFold(p.Value, "false") {
			kept = append(kept, p)
		}
	}
	// Longest value first, so a value containing another is replaced whole.
	sort.SliceStable(kept, func(i, j int) bool {
		if len(kept[i].Value) != len(kept[j].Value) {
			return len(kept[i].Value) > len(kept[j].Value)
		}
		return kept[i].Key < kept[j].Key
	})
	r := &Redactor{}
	if len(kept) > 0 {
		replacements := make([]string, 0, len(kept)*2)
		for _, p := range kept {
			replacements = append(replacements, p.Value, "${"+p.Key+"}")
		}
		r.replacer = strings.NewReplacer(replacements...)
	}
	return r
}

// String returns s with every known value replaced by ${KEY}.
func (r *Redactor) String(s string) string {
	if r == nil || r.replacer == nil {
		return s
	}
	return r.replacer.Replace(s)
}

// FromEnviron returns the MAESTRO_* entries of an os.Environ()-style list.
func FromEnviron(environ []string) []Pair {
	var out []Pair
	for _, kv := range environ {
		key, value, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(key, "MAESTRO_") && !maestroSetting(key) {
			out = append(out, Pair{Key: key, Value: value})
		}
	}
	return out
}

func maestroSetting(key string) bool {
	if key == "MAESTRO_VERSION" {
		return true
	}
	for _, prefix := range []string{"MAESTRO_CLI_", "MAESTRO_DRIVER_", "MAESTRO_USE_", "MAESTRO_DISABLE_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
