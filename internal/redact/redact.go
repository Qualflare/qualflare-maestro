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
	return &Redactor{pairs: kept}
}

// String returns s with every known value replaced by ${KEY}.
func (r *Redactor) String(s string) string {
	if r == nil || len(r.pairs) == 0 {
		return s
	}

	// Longer values claim their occurrences first, and a shorter value is never
	// replaced inside or overlapping text a longer one already claimed -- so a
	// value that ends where another begins cannot leave half of the longer one
	// behind. r.pairs is sorted longest-first by New.
	//
	// `taken` is what keeps that check cheap: comparing each candidate against
	// every existing claim was quadratic, and measured 30 ms for a 64 KiB tail in
	// which a 4-character value recurred -- and quadratic in the count means the
	// 256 KiB tail lastLines can produce was heading for half a second.
	taken := make([]bool, len(s))
	free := func(from, to int) bool {
		for i := from; i < to; i++ {
			if taken[i] {
				return false
			}
		}
		return true
	}

	type claim struct {
		start, end int
		key        string
	}
	var claims []claim
	for _, p := range r.pairs {
		for from := 0; from <= len(s)-len(p.Value); {
			rel := strings.Index(s[from:], p.Value)
			if rel < 0 {
				break
			}
			start := from + rel
			end := start + len(p.Value)
			if free(start, end) {
				for i := start; i < end; i++ {
					taken[i] = true
				}
				claims = append(claims, claim{start: start, end: end, key: p.Key})
			}
			from = end
		}
	}
	if len(claims) == 0 {
		return s
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].start < claims[j].start })

	var out strings.Builder
	out.Grow(len(s))
	from := 0
	for _, claimed := range claims {
		out.WriteString(s[from:claimed.start])
		out.WriteString("${")
		out.WriteString(claimed.key)
		out.WriteByte('}')
		from = claimed.end
	}
	out.WriteString(s[from:])
	return out.String()
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
