//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-maestro/internal/wire"
)

const fakeScript = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_ARGS_FILE"
out=""; dbg=""
while [ $# -gt 0 ]; do
  case "$1" in
    --output) out="$2"; shift ;;
    --debug-output) dbg="$2"; shift ;;
  esac
  shift
done
if [ -n "$FAKE_CAPTURE" ]; then
  cp "$FAKE_CAPTURE/report.xml" "$out"
  cp -R "$FAKE_CAPTURE/debug/." "$dbg/"
fi
if [ -n "$FAKE_LOG" ]; then printf '%s\n' "$FAKE_LOG" > "$dbg/maestro.log"; fi
exit "${FAKE_EXIT:-0}"
`

// setup installs the fake maestro and points the reporter at a fresh output
// directory. It returns the fake's path, the file its arguments land in, and
// the output directory.
func setup(t *testing.T) (bin, argsFile, outDir string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "maestro")
	if err := os.WriteFile(bin, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile = filepath.Join(dir, "args.txt")
	outDir = filepath.Join(dir, "results")
	t.Setenv("FAKE_ARGS_FILE", argsFile)
	t.Setenv("FAKE_CAPTURE", "")
	t.Setenv("FAKE_LOG", "")
	t.Setenv("FAKE_EXIT", "")
	t.Setenv("QUALFLARE_MAESTRO_BIN", bin)
	t.Setenv("QUALFLARE_OUTPUT_DIR", outDir)
	t.Setenv("QUALFLARE_RUN_ID", "t1")
	t.Setenv("QUALFLARE_ENABLED", "")
	t.Setenv("QUALFLARE_PLATFORM", "")
	return bin, argsFile, outDir
}

func capture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "test", "captures", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// stageFlat arranges the 2.6.1 capture the way the fake expects: report.xml
// beside a debug/ folder holding the flat files.
func stageFlat(t *testing.T) string {
	t.Helper()
	src := capture(t, "maestro-2.6.1-ios26.5")
	dst := t.TempDir()
	copyTo(t, filepath.Join(src, "report.xml"), filepath.Join(dst, "report.xml"))
	items, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if n := it.Name(); strings.HasPrefix(n, "commands-") || strings.HasPrefix(n, "screenshot-") {
			copyTo(t, filepath.Join(src, n), filepath.Join(dst, "debug", n))
		}
	}
	return dst
}

func copyTo(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readReport(t *testing.T, outDir string) (wire.Collect, []byte) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(outDir, "qualflare-maestro-*.json"))
	if len(files) != 1 {
		t.Fatalf("want one report in %s, found %v", outDir, files)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var c wire.Collect
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c, raw
}

func TestRun_BundleCaptureEndToEnd(t *testing.T) {
	bin, argsFile, outDir := setup(t)
	t.Setenv("FAKE_CAPTURE", capture(t, "maestro-2.10.0-ios26.5"))
	t.Setenv("FAKE_EXIT", "1")

	var stderr bytes.Buffer
	code := run([]string{"--", bin, "test", "--env", "LABEL=General", "flows/"}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want maestro's 1\n%s", code, stderr.String())
	}

	c, raw := readReport(t, outDir)
	if n := len(c.Suites[0].Cases); n != 3 {
		t.Errorf("cases = %d, want 3", n)
	}
	for _, leak := range []string{"General", "MAESTRO_", "089E029C-523A-4F81-8558-F0297DC8FF47"} {
		if bytes.Contains(raw, []byte(leak)) {
			t.Errorf("report contains %q", leak)
		}
	}
	shots, _ := filepath.Glob(filepath.Join(outDir, "attachments", "*.png"))
	if len(shots) != 2 {
		t.Errorf("attachments on disk = %v, want 2", shots)
	}
	if work, _ := filepath.Glob(filepath.Join(outDir, ".work-*")); len(work) != 0 {
		t.Errorf("work directory left behind: %v", work)
	}
	passed, _ := os.ReadFile(argsFile)
	for _, want := range []string{"--format", "junit", "--flatten-debug-output", "flows/"} {
		if !strings.Contains(string(passed), want+"\n") {
			t.Errorf("maestro was not passed %q:\n%s", want, passed)
		}
	}
	if !strings.Contains(stderr.String(), "wrote 1 suite(s), 3 case(s)") {
		t.Errorf("no summary line in:\n%s", stderr.String())
	}
}

func TestRun_FlatCaptureEndToEnd(t *testing.T) {
	_, _, outDir := setup(t)
	t.Setenv("FAKE_CAPTURE", stageFlat(t))
	t.Setenv("FAKE_EXIT", "1")
	if code := run([]string{"flows/"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	c, _ := readReport(t, outDir)
	var withShot int
	for _, cs := range c.Suites[0].Cases {
		withShot += len(cs.Attachments)
	}
	if len(c.Suites[0].Cases) != 2 || withShot != 1 {
		t.Errorf("cases %d, attachments %d; want 2, 1", len(c.Suites[0].Cases), withShot)
	}
}

func TestRun_ShorthandPrependsTest(t *testing.T) {
	_, argsFile, _ := setup(t)
	run([]string{"-environment", "ci", "flows/"}, &bytes.Buffer{}, &bytes.Buffer{})
	passed, _ := os.ReadFile(argsFile)
	if first := strings.SplitN(string(passed), "\n", 2)[0]; first != "test" {
		t.Errorf("first argument = %q, want test", first)
	}
}

func TestRun_RefusesOwnedFlagsWithoutRunningMaestro(t *testing.T) {
	bin, argsFile, outDir := setup(t)
	var stderr bytes.Buffer
	code := run([]string{"--", bin, "test", "--output", "mine.xml", "flows/"}, &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--output") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Error("maestro ran anyway")
	}
	if _, err := os.Stat(outDir); err == nil {
		t.Error("an output directory was created for a refused run")
	}
}

func TestRun_MissingMaestroIs127(t *testing.T) {
	setup(t)
	t.Setenv("QUALFLARE_MAESTRO_BIN", filepath.Join(t.TempDir(), "maestro"))
	var stderr bytes.Buffer
	if code := run([]string{"flows/"}, &bytes.Buffer{}, &stderr); code != 127 {
		t.Fatalf("exit = %d, want 127 (%s)", code, stderr.String())
	}
}

func TestRun_UnattributedFailureWhenMaestroWritesNoReport(t *testing.T) {
	_, _, outDir := setup(t)
	t.Setenv("FAKE_EXIT", "1")
	t.Setenv("FAKE_LOG", "No connected devices were found")
	if code := run([]string{"flows/"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	c, _ := readReport(t, outDir)
	cs := c.Suites[0].Cases[0]
	if cs.Name != "[unattributed failure]" || cs.Status != "error" || !strings.Contains(cs.Error, "No connected devices") {
		t.Errorf("case = %+v", cs)
	}
}

func TestRun_DisabledRunsMaestroExactlyAsGiven(t *testing.T) {
	bin, argsFile, outDir := setup(t)
	t.Setenv("QUALFLARE_ENABLED", "false")
	t.Setenv("FAKE_EXIT", "4")
	if code := run([]string{"--", bin, "test", "--format", "html", "flows/"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	passed, _ := os.ReadFile(argsFile)
	if got := strings.TrimSpace(string(passed)); got != "test\n--format\nhtml\nflows/" {
		t.Errorf("maestro got %q, want the arguments untouched", got)
	}
	if _, err := os.Stat(outDir); err == nil {
		t.Error("a disabled run wrote output")
	}
}

func TestRun_Version(t *testing.T) {
	setup(t)
	var stdout bytes.Buffer
	if code := run([]string{"-version"}, &stdout, &bytes.Buffer{}); code != 0 || !strings.HasPrefix(stdout.String(), "qualflare-maestro ") {
		t.Fatalf("exit %d stdout %q", code, stdout.String())
	}
}

func TestRun_UnknownReporterFlagBeforeDoubleDashIsAUsageError(t *testing.T) {
	setup(t)
	if code := run([]string{"-nope=1", "--", "maestro", "test", "flows/"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
