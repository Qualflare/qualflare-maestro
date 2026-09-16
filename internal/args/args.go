// Package args splits the command line between the reporter and Maestro and
// builds the maestro invocation.
package args

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Qualflare/qualflare-maestro/internal/redact"
)

// OwnedFlags are added by the reporter. A user passing one would move or
// suppress the files the report is built from, so they are refused.
var OwnedFlags = []string{"--format", "--output", "--debug-output", "--flatten-debug-output"}

// ErrNothingToRun means no flow file or folder was given.
var ErrNothingToRun = errors.New("nothing to run: pass a flow file or folder, e.g. qualflare-maestro -- maestro test .maestro/")

// OwnedFlagError reports a Maestro flag the reporter manages itself.
type OwnedFlagError struct{ Flag string }

func (e OwnedFlagError) Error() string {
	return fmt.Sprintf("%s is set by qualflare-maestro, which needs Maestro's JUnit and debug output in a place it controls; remove it (use -output-dir to choose where the report goes)", e.Flag)
}

// NotTestError reports a Maestro subcommand that cannot be reported on.
type NotTestError struct{ Subcommand string }

func (e NotTestError) Error() string {
	return fmt.Sprintf("only `maestro test` can be reported on, got `maestro %s`", e.Subcommand)
}

// Invocation is the maestro command to run and the paths it will write.
type Invocation struct {
	Argv     []string
	JUnit    string
	DebugDir string
}

// Split separates reporter flags from the Maestro command. Reporter flags are
// recognised only at the front: the first token that is `--`, not a flag, or
// not a known reporter flag starts the Maestro arguments.
func Split(argv []string, valueFlags, boolFlags map[string]bool) (reporter, maestro []string) {
	for i := 0; i < len(argv); i++ {
		tok := argv[i]
		if tok == "--" {
			return argv[:i], argv[i+1:]
		}
		name, attached := flagName(tok)
		switch {
		case name != "" && boolFlags[name]:
		case name != "" && valueFlags[name]:
			if !attached {
				i++ // the value is the next token
			}
		default:
			return argv[:i], argv[i:]
		}
	}
	return argv, nil
}

// Build validates the Maestro arguments and returns the command to run, with
// the owned flags placed right after `test`.
func Build(maestroArgs []string, defaultBin, workDir string) (Invocation, error) {
	bin := defaultBin
	var globals, testArgs []string
	switch {
	case len(maestroArgs) > 0 && (maestroArgs[0] == "maestro" || isExplicitMaestroPath(maestroArgs[0])):
		if isExplicitMaestroPath(maestroArgs[0]) {
			bin = maestroArgs[0]
		}
		rest := maestroArgs[1:]
		i := indexOf(rest, "test")
		if i < 0 {
			if len(rest) == 0 {
				return Invocation{}, ErrNothingToRun
			}
			return Invocation{}, NotTestError{Subcommand: firstPositional(rest)}
		}
		globals, testArgs = rest[:i], rest[i+1:]
	case len(maestroArgs) > 0 && maestroArgs[0] == "test":
		testArgs = maestroArgs[1:]
	default:
		testArgs = maestroArgs
	}
	if len(testArgs) == 0 {
		return Invocation{}, ErrNothingToRun
	}
	for _, a := range append(append([]string{}, globals...), testArgs...) {
		name, _ := flagName(a)
		for _, owned := range OwnedFlags {
			if name != "" && "--"+name == owned {
				return Invocation{}, OwnedFlagError{Flag: owned}
			}
		}
	}

	inv := Invocation{
		JUnit:    filepath.Join(workDir, "report.xml"),
		DebugDir: filepath.Join(workDir, "debug"),
	}
	argv := append([]string{bin}, globals...)
	argv = append(argv, "test",
		"--format", "junit", "--output", inv.JUnit,
		"--debug-output", inv.DebugDir, "--flatten-debug-output")
	inv.Argv = append(argv, testArgs...)
	return inv, nil
}

// Passthrough returns the command exactly as the user asked for it, for a run
// with the reporter disabled.
func Passthrough(maestroArgs []string, defaultBin string) []string {
	switch {
	case len(maestroArgs) == 0:
		return nil
	case maestroArgs[0] == "maestro":
		return append([]string{defaultBin}, maestroArgs[1:]...)
	case isExplicitMaestroPath(maestroArgs[0]):
		return append([]string{}, maestroArgs...)
	case maestroArgs[0] == "test":
		return append([]string{defaultBin}, maestroArgs...)
	default:
		return append([]string{defaultBin, "test"}, maestroArgs...)
	}
}

func isExplicitMaestroPath(arg string) bool {
	return filepath.Base(arg) == "maestro" &&
		(strings.ContainsRune(arg, filepath.Separator) || strings.ContainsRune(arg, '/'))
}

// EnvValues returns the KEY=VALUE pairs passed with --env or -e.
func EnvValues(maestroArgs []string) []redact.Pair {
	var out []redact.Pair
	add := func(kv string) {
		if key, value, ok := strings.Cut(kv, "="); ok && key != "" {
			out = append(out, redact.Pair{Key: key, Value: value})
		}
	}
	for i := 0; i < len(maestroArgs); i++ {
		a := maestroArgs[i]
		switch {
		case (a == "--env" || a == "-e") && i+1 < len(maestroArgs):
			i++
			add(maestroArgs[i])
		case strings.HasPrefix(a, "--env="):
			add(strings.TrimPrefix(a, "--env="))
		case strings.HasPrefix(a, "-e") && !strings.HasPrefix(a, "--") && len(a) > 2:
			value := strings.TrimPrefix(a, "-e")
			value = strings.TrimPrefix(value, "=")
			if strings.Contains(value, "=") {
				add(value)
			}
		}
	}
	return out
}

// flagName returns a flag's name without dashes and whether its value is
// attached with '='. It returns "" for a token that is not a flag.
func flagName(tok string) (string, bool) {
	if !strings.HasPrefix(tok, "-") || tok == "-" || tok == "--" {
		return "", false
	}
	s := strings.TrimLeft(tok, "-")
	if name, _, ok := strings.Cut(s, "="); ok {
		return name, true
	}
	return s, false
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

func firstPositional(xs []string) string {
	for _, x := range xs {
		if !strings.HasPrefix(x, "-") {
			return x
		}
	}
	return ""
}
