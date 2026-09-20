// Command qualflare-maestro runs `maestro test` and turns what Maestro leaves
// behind into a Qualflare report.
//
//	qualflare-maestro -- maestro test .maestro/
//	qualflare-maestro .maestro/                   # shorthand for the above
//
// Maestro has no reporter or listener API, so this wraps the process: it adds
// the flags that put Maestro's JUnit report and debug output somewhere known,
// reads them afterwards, and exits with Maestro's own exit code.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Qualflare/qualflare-maestro/internal/args"
	"github.com/Qualflare/qualflare-maestro/internal/build"
	"github.com/Qualflare/qualflare-maestro/internal/config"
	"github.com/Qualflare/qualflare-maestro/internal/debugdir"
	"github.com/Qualflare/qualflare-maestro/internal/gitdetect"
	"github.com/Qualflare/qualflare-maestro/internal/junit"
	"github.com/Qualflare/qualflare-maestro/internal/redact"
	"github.com/Qualflare/qualflare-maestro/internal/runner"
	"github.com/Qualflare/qualflare-maestro/internal/version"
	"github.com/Qualflare/qualflare-maestro/internal/wire"
)

const prefix = "[qualflare-maestro]"

var (
	valueFlags = map[string]bool{
		"output-dir": true, "environment": true, "language": true, "platform": true,
		"milestone": true, "branch": true, "commit": true, "run-id": true,
		"shard-index": true, "enabled": true,
	}
	boolFlags = map[string]bool{"version": true, "help": true, "h": true}
)

const usage = `Usage:
  qualflare-maestro [flags] -- maestro test <maestro arguments>
  qualflare-maestro [flags] <maestro test arguments>

Runs maestro test, then writes a Qualflare report. Upload it with:
  qf <project> collect <output-dir>

The reporter sets --format, --output, --debug-output and --flatten-debug-output
itself; passing any of them is an error. Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(argv []string, stdout, errOut io.Writer) int {
	if bad := unknownBeforeDoubleDash(argv); bad != "" {
		fmt.Fprintf(errOut, "%s unknown flag %s before --; reporter flags are: %s\n", prefix, bad, flagList())
		return 2
	}
	reporterArgs, maestroArgs := args.Split(argv, valueFlags, boolFlags)

	flags := flag.NewFlagSet("qualflare-maestro", flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.Usage = func() { fmt.Fprint(errOut, usage); flags.PrintDefaults() }
	var f config.Flags
	showVersion := flags.Bool("version", false, "print the version and exit")
	flags.StringVar(&f.OutputDir, "output-dir", "", "where the report is written (default qualflare-results)")
	flags.StringVar(&f.Environment, "environment", "", "environment the run belongs to (default development)")
	flags.StringVar(&f.Language, "language", "", "report language (default en-US)")
	flags.StringVar(&f.Platform, "platform", "", "ios, android or web (default: detected from the device)")
	flags.StringVar(&f.Milestone, "milestone", "", "milestone sequence number")
	flags.StringVar(&f.Branch, "branch", "", "branch name (default: from CI or git)")
	flags.StringVar(&f.Commit, "commit", "", "commit sha (default: from CI or git)")
	flags.StringVar(&f.RunID, "run-id", "", "groups a launch's report files; every shard must share one")
	flags.StringVar(&f.ShardIndex, "shard-index", "", "which shard produced these cases")
	flags.StringVar(&f.Enabled, "enabled", "", "set false to run maestro without writing a report")
	if err := flags.Parse(reporterArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "qualflare-maestro "+version.Full())
		return 0
	}

	cfg := config.Resolve(f, newRunID)
	if !cfg.Enabled {
		raw := f.Enabled
		if raw == "" {
			raw = os.Getenv("QUALFLARE_ENABLED")
		}
		if raw != "" && !config.IsDisabledSpelling(raw) {
			fmt.Fprintf(errOut, "%s -enabled/QUALFLARE_ENABLED value %q is not recognised; the reporter is disabled and Maestro will still run\n", prefix, raw)
		}
		return passthrough(maestroArgs, cfg.MaestroBin, stdout, errOut)
	}

	fileToken := build.FileSafe(cfg.RunID) + "-" + strconv.Itoa(os.Getpid())
	workDir := filepath.Join(cfg.OutputDir, ".work-"+fileToken)
	inv, err := args.Build(maestroArgs, cfg.MaestroBin, workDir)
	if err != nil {
		fmt.Fprintf(errOut, "%s %v\n", prefix, err)
		return 2
	}
	if _, err := exec.LookPath(inv.Argv[0]); err != nil {
		fmt.Fprintf(errOut, "%s cannot run %q: install Maestro (https://docs.maestro.dev) or point QUALFLARE_MAESTRO_BIN at it\n", prefix, inv.Argv[0])
		return 127
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		fmt.Fprintf(errOut, "%s %v\n", prefix, err)
		return 1
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		fmt.Fprintf(errOut, "%s %v\n", prefix, err)
		return 1
	}
	if err := os.MkdirAll(inv.DebugDir, 0o700); err != nil {
		fmt.Fprintf(errOut, "%s %v\n", prefix, err)
		return 1
	}

	res, err := runner.Run(inv.Argv, stdout, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "%s could not start maestro: %v\n", prefix, err)
		return 1
	}

	var report *junit.Report
	switch r, err := junit.ParseFile(inv.JUnit); {
	case err == nil:
		report = &r
	case !errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(errOut, "%s maestro's JUnit report could not be read: %v\n", prefix, err)
	}
	debug := debugdir.Read(inv.DebugDir)

	out := build.Collect(build.Input{
		Cfg:         cfg,
		FileToken:   fileToken,
		JUnit:       report,
		Debug:       debug,
		ExitCode:    res.ExitCode,
		Interrupted: res.Interrupted,
		StderrTail:  res.StderrTail,
		LogTail:     lastLines(debug.MaestroLog, 50),
		WorkingDir:  realPath(workingDir()),
		RepoRoot:    realPath(gitdetect.RepoRoot()),
		Redactor:    redact.New(append(args.EnvValues(maestroArgs), redact.FromEnviron(os.Environ())...)),
		Version:     version.String(),
		Now:         time.Now(),
	})
	for _, cp := range out.Copies {
		if err := copyFile(cp.From, filepath.Join(cfg.OutputDir, filepath.FromSlash(cp.To))); err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("screenshot %s could not be copied and was left out: %v", filepath.Base(cp.From), err))
			build.DropAttachment(&out.Report, cp.To)
		}
	}

	path, err := writeReport(out.Report, cfg, fileToken)
	if err != nil {
		fmt.Fprintf(errOut, "%s could not write the report: %v (maestro's output is kept in %s)\n", prefix, err, workDir)
		return 1
	}
	_ = os.RemoveAll(workDir)

	for _, w := range append(debug.Warnings, out.Warnings...) {
		fmt.Fprintf(errOut, "%s %s\n", prefix, w)
	}
	fmt.Fprintf(errOut, "%s wrote %d suite(s), %d case(s) to %s\n", prefix, len(out.Report.Suites), countCases(out.Report), path)
	return res.ExitCode
}

// unknownBeforeDoubleDash returns the first flag in front of a `--` that is not
// a reporter flag. Without this, a typo such as -enviroment would slide over to
// Maestro's side and run the flows unreported.
func unknownBeforeDoubleDash(argv []string) string {
	end := -1
	for i, a := range argv {
		if a == "--" {
			end = i
			break
		}
	}
	for i := 0; i < end; i++ {
		a := argv[i]
		if !strings.HasPrefix(a, "-") {
			return ""
		}
		name, _, attached := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case boolFlags[name]:
		case valueFlags[name]:
			if !attached {
				i++
			}
		default:
			return a
		}
	}
	return ""
}

func flagList() string {
	var names []string
	for n := range valueFlags {
		names = append(names, "-"+n)
	}
	names = append(names, "-version")
	return strings.Join(sortStrings(names), ", ")
}

func sortStrings(xs []string) []string {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
	return xs
}

func passthrough(maestroArgs []string, bin string, stdout, errOut io.Writer) int {
	argv := args.Passthrough(maestroArgs, bin)
	if len(argv) == 0 {
		fmt.Fprintf(errOut, "%s %v\n", prefix, args.ErrNothingToRun)
		return 2
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		fmt.Fprintf(errOut, "%s cannot run %q: install Maestro (https://docs.maestro.dev) or point QUALFLARE_MAESTRO_BIN at it\n", prefix, argv[0])
		return 127
	}
	res, err := runner.Run(argv, stdout, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "%s could not start maestro: %v\n", prefix, err)
		return 1
	}
	return res.ExitCode
}

func writeReport(c wire.Collect, cfg config.Config, fileToken string) (string, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return "", err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	path := filepath.Join(cfg.OutputDir, fmt.Sprintf("qualflare-maestro-%s.json", fileToken))
	return path, os.WriteFile(path, data, 0o644)
}

func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// lastLines returns up to n trailing lines of a file, reading at most its last
// 256 KiB.
func lastLines(path string, n int) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	const maxBytes = 256 << 10
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return ""
	}
	if start > 0 {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			data = data[newline+1:]
		} else {
			data = nil
		}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func workingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// realPath resolves symlinks so the git root and the working directory compare
// cleanly (on macOS, /var and /private/var are the same place).
func realPath(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func countCases(c wire.Collect) int {
	n := 0
	for _, s := range c.Suites {
		n += len(s.Cases)
	}
	return n
}

func newRunID() string {
	return fmt.Sprintf("local-%d-%d", os.Getpid(), time.Now().UnixNano())
}
