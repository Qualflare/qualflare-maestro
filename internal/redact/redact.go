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
	pairs []Pair
}

// New builds a redactor from variable pairs.
func New(pairs []Pair) *Redactor {
	r := &Redactor{}
	for _, p := range pairs {
		if utf8.RuneCountInString(p.Value) >= MinLength {
			r.pairs = append(r.pairs, p)
		}
	}
	// Longest value first, so a value containing another is replaced whole.
	sort.SliceStable(r.pairs, func(i, j int) bool {
		if len(r.pairs[i].Value) != len(r.pairs[j].Value) {
			return len(r.pairs[i].Value) > len(r.pairs[j].Value)
		}
		return r.pairs[i].Key < r.pairs[j].Key
	})
	return r
}

// String returns s with every known value replaced by ${KEY}.
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	for _, p := range r.pairs {
		s = strings.ReplaceAll(s, p.Value, "${"+p.Key+"}")
	}
	return s
}

// FromEnviron returns the MAESTRO_* entries of an os.Environ()-style list.
func FromEnviron(environ []string) []Pair {
	var out []Pair
	for _, kv := range environ {
		key, value, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(key, "MAESTRO_") {
			out = append(out, Pair{Key: key, Value: value})
		}
	}
	return out
}
