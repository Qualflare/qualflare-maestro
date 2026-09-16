package junit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capture(name string) string {
	return filepath.Join("..", "..", "test", "captures", name, "report.xml")
}

func caseNamed(t *testing.T, r Report, name string) Case {
	t.Helper()
	for _, s := range r.Suites {
		for _, c := range s.Cases {
			if c.Name == name {
				return c
			}
		}
	}
	t.Fatalf("no case named %q", name)
	return Case{}
}

func TestParse_Maestro261(t *testing.T) {
	r, err := ParseFile(capture("maestro-2.6.1-ios26.5"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Suites) != 1 || len(r.Suites[0].Cases) != 2 {
		t.Fatalf("got %d suites / %d cases, want 1 / 2", len(r.Suites), len(r.Suites[0].Cases))
	}
	if got := r.Suites[0].Device; got != "iPhone 17 - iOS 26.5 - 089E029C-523A-4F81-8558-F0297DC8FF47" {
		t.Errorf("Device = %q", got)
	}

	fails := caseNamed(t, r, "Settings fails on purpose")
	if fails.Status != "ERROR" || !fails.Failed || fails.Seconds != 23 {
		t.Errorf("fails = %+v", fails)
	}
	if want := `Assertion is false: "This Text Does Not Exist 12345" is visible`; fails.Failure != want {
		t.Errorf("Failure = %q, want %q", fails.Failure, want)
	}

	opens := caseNamed(t, r, "Settings opens")
	if opens.File != "flows/settings-opens.yaml" || opens.Failed {
		t.Errorf("opens = %+v", opens)
	}
	props := map[string]string{}
	for _, p := range opens.Properties {
		props[p.Name] = p.Value
	}
	if props["qualflare.priority"] != "high" || props["tags"] != "smoke, probe" || props["team"] != "mobile" {
		t.Errorf("properties = %v", props)
	}
}

func TestParse_Maestro2100HasMillisecondsAndTimestamps(t *testing.T) {
	r, err := ParseFile(capture("maestro-2.10.0-ios26.5"))
	if err != nil {
		t.Fatal(err)
	}
	nested := caseNamed(t, r, "Settings nested commands")
	if nested.Seconds != 8.57 || nested.Timestamp != "2026-09-16T19:26:50" || nested.Status != "SUCCESS" {
		t.Errorf("nested = %+v", nested)
	}
}

func TestParseFile_MissingFileIsNotExist(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "report.xml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestParse_MalformedXMLIsAnError(t *testing.T) {
	if _, err := Parse(strings.NewReader("<testsuites><testsuite")); err == nil {
		t.Fatal("want an error")
	}
}
