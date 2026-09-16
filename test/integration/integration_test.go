//go:build integration

// Package integration runs the reporter against real Maestro.
//
//	go test -tags integration ./test/integration/ -run 'TestInvalidYAML|TestNoDevice' -v   # no simulator booted
//	go test -tags integration ./test/integration/ -run TestFlows -v                       # an iOS simulator booted
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Qualflare/qualflare-maestro/internal/steps"
	"github.com/Qualflare/qualflare-maestro/internal/wire"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "qfm-it-")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "qualflare-maestro")
	build := exec.Command("go", "build", "-o", binary, "github.com/Qualflare/qualflare-maestro/cmd/qualflare-maestro")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Println("building the reporter failed:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	exit   int
	report wire.Collect
	raw    []byte
	outDir string
}

func maestroVersion(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("maestro"); err != nil {
		t.Skip("maestro is not on PATH")
	}
	out, err := exec.Command("maestro", "--version").Output()
	if err != nil {
		t.Fatalf("maestro --version: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func atLeast(version string, major, minor int) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	return err1 == nil && err2 == nil && (ma > major || (ma == major && mi >= minor))
}

func simulatorBooted() bool {
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "booted").Output()
	return err == nil && strings.Contains(string(out), "(Booted)")
}

func runReporter(t *testing.T, env []string, argv ...string) result {
	t.Helper()
	outDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"-output-dir", outDir, "-environment", "integration"}, argv...)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Stdin stays unset (/dev/null): a Maestro prompt must fail, not wait forever.
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("the reporter did not finish within 20 minutes; if Maestro was waiting for input, record that in docs/LIMITATIONS.md")
	}
	res := result{outDir: outDir}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		res.exit = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("running the reporter: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(outDir, "qualflare-maestro-*.json"))
	if len(files) != 1 {
		t.Fatalf("want one report in %s, found %v", outDir, files)
	}
	res.raw, _ = os.ReadFile(files[0])
	if err := json.Unmarshal(res.raw, &res.report); err != nil {
		t.Fatalf("the report is not valid JSON: %v", err)
	}
	return res
}

func byName(c wire.Collect) map[string]wire.Case {
	out := map[string]wire.Case{}
	for _, s := range c.Suites {
		for _, cs := range s.Cases {
			out[cs.Name] = cs
		}
	}
	return out
}

func TestFlows(t *testing.T) {
	version := maestroVersion(t)
	if !simulatorBooted() {
		t.Skip("needs a booted iOS simulator")
	}
	t.Logf("maestro %s", version)

	const secret = "Integration Secret Row 55555"
	const token = "qf-it-token-7a1c9e"
	res := runReporter(t, []string{"MAESTRO_QF_IT_TOKEN=" + token},
		"--", "maestro", "test", "--env", "IT_SECRET="+secret, "flows/")

	if res.exit != 1 {
		t.Errorf("exit = %d, want maestro's 1 (three flows fail on purpose)", res.exit)
	}

	// Variable names may appear as ${NAME} placeholders, so only ban their JSON object-key form.
	for _, leak := range []string{secret, token, "\"MAESTRO_QF_IT_TOKEN\":", "hierarchyRoot", "debugMessage"} {
		if bytes.Contains(res.raw, []byte(leak)) {
			t.Errorf("the report contains %q", leak)
		}
	}
	if res.report.Platform != "ios" {
		t.Errorf("platform = %q", res.report.Platform)
	}

	cases := byName(res.report)
	for name, status := range map[string]string{
		"Passes": "passed", "Fails": "failed", "Optional miss": "passed",
		"Nested": "passed", "Secret selector": "failed", "Maestro env selector": "failed",
	} {
		c, ok := cases[name]
		if !ok {
			t.Errorf("no case %q", name)
			continue
		}
		if c.Status != status || len(c.Steps) == 0 {
			t.Errorf("%q: status %q with %d steps, want %q with steps", name, c.Status, len(c.Steps), status)
		}
	}

	if e := cases["Secret selector"].Error; !strings.Contains(e, "${IT_SECRET}") {
		t.Errorf("secret flow error = %q, want the variable named instead of its value", e)
	}
	if e := cases["Maestro env selector"].Error; !strings.Contains(e, "${MAESTRO_QF_IT_TOKEN}") {
		t.Errorf("maestro env flow error = %q, want the MAESTRO_* variable named instead of its value", e)
	}

	passes := cases["Passes"]
	if passes.Priority != "critical" || len(passes.Links) != 1 || len(passes.Labels) != 1 {
		t.Errorf("Passes metadata: priority %q, links %v, labels %v", passes.Priority, passes.Links, passes.Labels)
	}

	var warned bool
	for _, s := range cases["Optional miss"].Steps {
		warned = warned || (s.Status == "skipped" && s.Error == steps.WarnedMessage)
	}
	if !warned {
		t.Errorf("Optional miss has no skipped optional step: %+v", cases["Optional miss"].Steps)
	}

	fails := cases["Fails"]
	var linked bool
	for _, a := range fails.Attachments {
		if a.StepIndex != nil && fails.Steps[*a.StepIndex].Status == "failed" {
			linked = true
		}
		if _, err := os.Stat(filepath.Join(res.outDir, filepath.FromSlash(a.LocalImagePath))); err != nil {
			t.Errorf("attachment %s is not on disk", a.LocalImagePath)
		}
	}
	if !linked {
		t.Errorf("Fails has no screenshot tied to its failed step: %+v", fails.Attachments)
	}

	if atLeast(version, 2, 10) {
		var parented bool
		for _, s := range cases["Nested"].Steps {
			parented = parented || s.ParentIndex != nil
		}
		if !parented {
			t.Errorf("maestro %s records depth, but Nested has no nested steps", version)
		}
	}

	if work, _ := filepath.Glob(filepath.Join(res.outDir, ".work-*")); len(work) != 0 {
		t.Errorf("work directory left behind: %v", work)
	}
}

func TestInvalidYAML(t *testing.T) {
	maestroVersion(t)
	res := runReporter(t, nil, "--", "maestro", "test", "badyaml/")
	assertUnattributed(t, res)
}

func TestNoDevice(t *testing.T) {
	maestroVersion(t)
	if simulatorBooted() {
		t.Skip("needs no booted simulator; run it before booting one")
	}
	res := runReporter(t, nil, "--", "maestro", "test", "flows/")
	assertUnattributed(t, res)
}

func assertUnattributed(t *testing.T, res result) {
	t.Helper()
	if res.exit == 0 {
		t.Fatal("exit = 0, want maestro's failure passed through")
	}
	if len(res.report.Suites) != 1 || len(res.report.Suites[0].Cases) != 1 {
		t.Fatalf("suites = %+v", res.report.Suites)
	}
	c := res.report.Suites[0].Cases[0]
	if c.Name != "[unattributed failure]" || c.Status != "error" || strings.TrimSpace(c.Error) == "" {
		t.Errorf("case = %+v", c)
	}
}
