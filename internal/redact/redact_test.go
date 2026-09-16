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

func TestLongerValuesClaimOverlappingTextFirst(t *testing.T) {
	r := New([]Pair{{Key: "A", Value: "abcd1234-long"}, {Key: "B", Value: "xxab"}})
	if got, want := r.String("xxabcd1234-long"), "xx${A}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDifferentValuesAtDisjointPositionsAreBothReplaced(t *testing.T) {
	r := New([]Pair{{Key: "A", Value: "alpha-secret"}, {Key: "B", Value: "beta-secret"}})
	if got, want := r.String("alpha-secret then beta-secret"), "${A} then ${B}"; got != want {
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

func TestBooleanValuesAreNotRedacted(t *testing.T) {
	r := New([]Pair{
		{Key: "LOWER_TRUE", Value: "true"},
		{Key: "UPPER_TRUE", Value: "TRUE"},
		{Key: "LOWER_FALSE", Value: "false"},
		{Key: "UPPER_FALSE", Value: "FALSE"},
	})
	if got := r.String("true TRUE false FALSE"); got != "true TRUE false FALSE" {
		t.Fatalf("got %q", got)
	}
}

func TestReplacementsHappenInOnePass(t *testing.T) {
	r := New([]Pair{{Key: "A", Value: "secret-value"}, {Key: "B", Value: "${A}-x"}})
	if got, want := r.String("secret-value-x"), "${A}-x"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestANilRedactorChangesNothing(t *testing.T) {
	var r *Redactor
	if got := r.String("untouched"); got != "untouched" {
		t.Fatalf("got %q", got)
	}
}

func TestFromEnvironKeepsOnlyMaestroVariables(t *testing.T) {
	got := FromEnviron([]string{
		"HOME=/Users/x",
		"MAESTRO_TOKEN=abc=def",
		"MAESTRO_EMPTY=",
		"MAESTRO_CLI_NO_ANALYTICS=true",
		"MAESTRO_DRIVER_STARTUP_TIMEOUT=120000",
		"MAESTRO_USE_GRAALJS=false",
		"MAESTRO_DISABLE_UPDATE_CHECK=true",
		"MAESTRO_VERSION=2.10.0",
		"PATH=/bin",
	})
	want := []Pair{{Key: "MAESTRO_TOKEN", Value: "abc=def"}, {Key: "MAESTRO_EMPTY", Value: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
