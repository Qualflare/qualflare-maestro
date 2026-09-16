package args

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Qualflare/qualflare-maestro/internal/redact"
)

var (
	valueFlags = map[string]bool{"output-dir": true, "environment": true}
	boolFlags  = map[string]bool{"version": true}
)

func TestSplit(t *testing.T) {
	cases := []struct {
		name              string
		argv              []string
		reporter, maestro []string
	}{
		{"double dash", []string{"-environment", "ci", "--", "maestro", "test", "flows/"},
			[]string{"-environment", "ci"}, []string{"maestro", "test", "flows/"}},
		{"shorthand stops at the first unknown flag", []string{"-output-dir", "out", "--env", "X=1", "flows/"},
			[]string{"-output-dir", "out"}, []string{"--env", "X=1", "flows/"}},
		{"attached value", []string{"-output-dir=out", "flows/"},
			[]string{"-output-dir=out"}, []string{"flows/"}},
		{"bool flag takes no value", []string{"-version"},
			[]string{"-version"}, nil},
		{"nothing for the reporter", []string{"flows/"},
			[]string{}, []string{"flows/"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, m := Split(c.argv, valueFlags, boolFlags)
			if !reflect.DeepEqual(r, c.reporter) || !reflect.DeepEqual(m, c.maestro) {
				t.Fatalf("Split(%q) = %q, %q; want %q, %q", c.argv, r, m, c.reporter, c.maestro)
			}
		})
	}
}

func TestBuild_LongFormInjectsTheOwnedFlagsRightAfterTest(t *testing.T) {
	work := filepath.Join("out", ".work")
	inv, err := Build([]string{"maestro", "test", "--env", "X=1", "flows/"}, "ignored", work)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ignored", "test",
		"--format", "junit", "--output", filepath.Join(work, "report.xml"),
		"--debug-output", filepath.Join(work, "debug"), "--flatten-debug-output",
		"--env", "X=1", "flows/"}
	if !reflect.DeepEqual(inv.Argv, want) {
		t.Fatalf("Argv = %q\nwant   %q", inv.Argv, want)
	}
	if inv.JUnit != filepath.Join(work, "report.xml") || inv.DebugDir != filepath.Join(work, "debug") {
		t.Fatalf("paths = %q, %q", inv.JUnit, inv.DebugDir)
	}
}

func TestBuild_ShorthandUsesTheDefaultBinary(t *testing.T) {
	for _, in := range [][]string{{"flows/"}, {"test", "flows/"}} {
		inv, err := Build(in, "/opt/maestro", "w")
		if err != nil {
			t.Fatal(err)
		}
		if inv.Argv[0] != "/opt/maestro" || inv.Argv[1] != "test" || inv.Argv[len(inv.Argv)-1] != "flows/" {
			t.Fatalf("Build(%q).Argv = %q", in, inv.Argv)
		}
	}
}

func TestBuild_AnExplicitMaestroPathWins(t *testing.T) {
	inv, err := Build([]string{"/custom/bin/maestro", "test", "flows/"}, "maestro", "w")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Argv[0] != "/custom/bin/maestro" {
		t.Fatalf("Argv[0] = %q", inv.Argv[0])
	}
}

func TestBuild_KeepsGlobalOptionsBeforeTest(t *testing.T) {
	inv, err := Build([]string{"maestro", "--udid", "ABC", "test", "flows/"}, "maestro", "w")
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Argv[:4]; !reflect.DeepEqual(got, []string{"maestro", "--udid", "ABC", "test"}) {
		t.Fatalf("Argv starts %q", got)
	}
}

func TestBuild_RefusesEveryOwnedFlag(t *testing.T) {
	for _, flag := range []string{"--format", "--output", "--debug-output", "--flatten-debug-output",
		"--format=junit", "--output=x.xml"} {
		_, err := Build([]string{"maestro", "test", flag, "flows/"}, "maestro", "w")
		var owned OwnedFlagError
		if !errors.As(err, &owned) {
			t.Errorf("%s: err = %v, want OwnedFlagError", flag, err)
		}
	}
}

func TestBuild_DoesNotMistakeTestOutputDirForOutput(t *testing.T) {
	if _, err := Build([]string{"maestro", "test", "--test-output-dir", "shots", "flows/"}, "maestro", "w"); err != nil {
		t.Fatalf("--test-output-dir must pass through: %v", err)
	}
}

func TestBuild_OnlyWrapsTheTestSubcommand(t *testing.T) {
	_, err := Build([]string{"maestro", "cloud", "app.apk", "flows/"}, "maestro", "w")
	var notTest NotTestError
	if !errors.As(err, &notTest) || notTest.Subcommand != "cloud" {
		t.Fatalf("err = %v, want NotTestError{cloud}", err)
	}
}

func TestBuild_NothingToRun(t *testing.T) {
	for _, in := range [][]string{nil, {"maestro", "test"}, {"test"}} {
		if _, err := Build(in, "maestro", "w"); !errors.Is(err, ErrNothingToRun) {
			t.Errorf("Build(%q) err = %v, want ErrNothingToRun", in, err)
		}
	}
}

func TestPassthroughAddsNothing(t *testing.T) {
	cases := map[string][2][]string{
		"long":          {{"maestro", "test", "--format", "html", "flows/"}, {"bin", "test", "--format", "html", "flows/"}},
		"explicit path": {{"/custom/bin/maestro", "test", "flows/"}, {"/custom/bin/maestro", "test", "flows/"}},
		"shorthand":     {{"flows/"}, {"bin", "test", "flows/"}},
		"test":          {{"test", "flows/"}, {"bin", "test", "flows/"}},
	}
	for name, c := range cases {
		if got := Passthrough(c[0], "bin"); !reflect.DeepEqual(got, c[1]) {
			t.Errorf("%s: Passthrough = %q, want %q", name, got, c[1])
		}
	}
	if got := Passthrough(nil, "bin"); got != nil {
		t.Errorf("Passthrough(nil) = %q, want nil", got)
	}
}

func TestEnvValues(t *testing.T) {
	got := EnvValues([]string{"maestro", "test", "--env", "USER=alice99", "-e", "PASS=hunter22",
		"--env=TOKEN=tok=123", "--env", "NOEQUALS", "flows/"})
	want := []redact.Pair{{Key: "USER", Value: "alice99"}, {Key: "PASS", Value: "hunter22"}, {Key: "TOKEN", Value: "tok=123"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
