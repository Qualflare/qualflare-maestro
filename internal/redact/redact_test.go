package redact

import (
	"reflect"
	"testing"
)

func TestReplacesValuesWithTheirVariableName(t *testing.T) {
	r := New([]Pair{{Key: "PASSWORD", Value: "hunter22"}})
	got := r.String(`Assertion is false: "hunter22" is visible`)
	if want := `Assertion is false: "${PASSWORD}" is visible`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLongerValuesAreReplacedFirst(t *testing.T) {
	// If "secret" went first, "secret-longer" would become "${A}-longer" and the
	// longer value would never be recognised.
	r := New([]Pair{{Key: "A", Value: "secret"}, {Key: "B", Value: "secret-longer"}})
	if got, want := r.String("secret-longer and secret"), "${B} and ${A}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShortValuesAreNotRedacted(t *testing.T) {
	// Replacing "1" or "on" everywhere would mangle the report.
	r := New([]Pair{{Key: "FLAG", Value: "on"}, {Key: "N", Value: "123"}})
	if got := r.String("on 123"); got != "on 123" {
		t.Fatalf("got %q", got)
	}
}

func TestANilRedactorChangesNothing(t *testing.T) {
	var r *Redactor
	if got := r.String("untouched"); got != "untouched" {
		t.Fatalf("got %q", got)
	}
}

func TestFromEnvironKeepsOnlyMaestroVariables(t *testing.T) {
	got := FromEnviron([]string{"HOME=/Users/x", "MAESTRO_TOKEN=abc=def", "MAESTRO_EMPTY=", "PATH=/bin"})
	want := []Pair{{Key: "MAESTRO_TOKEN", Value: "abc=def"}, {Key: "MAESTRO_EMPTY", Value: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
