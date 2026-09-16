package debugdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureDir(t *testing.T, parts ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "test", "captures"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func flowNamed(t *testing.T, r Result, name string) Flow {
	t.Helper()
	for _, f := range r.Flows {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no flow %q in %d flows", name, len(r.Flows))
	return Flow{}
}

func kinds(f Flow) []string {
	var out []string
	for _, e := range f.Entries {
		out = append(out, e.Kind)
	}
	return out
}

func TestRead_FlatLayout(t *testing.T) {
	r := Read(captureDir(t, "maestro-2.6.1-ios26.5"))
	if r.Layout != LayoutFlat || len(r.Flows) != 2 {
		t.Fatalf("layout %v with %d flows, want flat with 2", r.Layout, len(r.Flows))
	}

	opens := flowNamed(t, r, "Settings opens")
	if got := strings.Join(kinds(opens), ","); got != "defineVariablesCommand,applyConfigurationCommand,launchAppCommand,assertConditionCommand,tapOnElement" {
		t.Errorf("entries out of sequence order: %s", got)
	}
	if !strings.Contains(string(opens.Entries[3].Raw), `${LABEL}`) {
		t.Errorf("raw assertion lost its placeholder: %s", opens.Entries[3].Raw)
	}
	if warned := opens.Entries[4]; warned.Status != "WARNED" || warned.DurationMs != nil {
		t.Errorf("warned entry = %+v, want WARNED with nil duration", warned)
	}
	if len(opens.Screenshots) != 0 {
		t.Errorf("opens screenshots = %v, want none", opens.Screenshots)
	}

	fails := flowNamed(t, r, "Settings fails on purpose")
	failed := fails.Entries[len(fails.Entries)-1]
	if want := `Assertion is false: "This Text Does Not Exist 12345" is visible`; failed.Status != "FAILED" || failed.ErrorMessage != want {
		t.Errorf("failed entry = %s %q", failed.Status, failed.ErrorMessage)
	}
	if len(fails.Screenshots) != 1 || !fails.Screenshots[0].Failed {
		t.Errorf("fails screenshots = %+v, want one failure screenshot", fails.Screenshots)
	}
}

func TestRead_BundleLayout(t *testing.T) {
	r := Read(captureDir(t, "maestro-2.10.0-ios26.5", "debug"))
	if r.Layout != LayoutBundle || len(r.Flows) != 3 {
		t.Fatalf("layout %v with %d flows, want bundle with 3", r.Layout, len(r.Flows))
	}

	nested := flowNamed(t, r, "Settings nested commands")
	var depths []int
	for _, e := range nested.Entries {
		depths = append(depths, e.Depth)
	}
	if got, want := depths, []int{0, 0, 0, 0, 1, 1, 0, 1, 0, 1}; !equalInts(got, want) {
		t.Errorf("depths = %v, want %v", got, want)
	}

	opens := flowNamed(t, r, "Settings opens")
	tap := opens.Entries[4]
	if tap.Kind != "tapOnElement" || tap.DurationMs == nil || len(tap.Screenshots) != 1 {
		t.Fatalf("tap entry = %+v", tap)
	}
	if !strings.HasSuffix(tap.Screenshots[0], filepath.Join("Settings opens", "screenshots", "step-005-tapOnElement-Definitely_Not_A_Real_Row.png")) {
		t.Errorf("screenshot path = %q", tap.Screenshots[0])
	}
	if len(opens.Screenshots) != 0 {
		t.Errorf("unreferenced screenshots = %v, want none (the step lists it)", opens.Screenshots)
	}
}

func TestRead_EmptyDirectoryHasNoLayout(t *testing.T) {
	if r := Read(t.TempDir()); r.Layout != LayoutNone || len(r.Flows) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestRead_MissingDirectoryHasNoLayout(t *testing.T) {
	if r := Read(filepath.Join(t.TempDir(), "absent")); r.Layout != LayoutNone {
		t.Fatalf("got %+v", r)
	}
}

func TestRead_MalformedCommandsFileIsAWarningNotAFailure(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Broken flow", "commands.json"), "[{not json")
	r := Read(dir)
	if r.Layout != LayoutBundle || len(r.Flows) != 0 || len(r.Warnings) != 1 {
		t.Fatalf("got layout %v, %d flows, warnings %v", r.Layout, len(r.Flows), r.Warnings)
	}
}

func TestRead_ArtifactPathsCannotEscapeTheFlowFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "outside.png"), "png")
	write(t, filepath.Join(dir, "Flow", "commands.json"),
		`[{"command":{"launchAppCommand":{"appId":"x"}},"metadata":{"status":"COMPLETED","timestamp":1,"sequenceNumber":0,"artifacts":[{"type":"SCREENSHOT","path":"../outside.png"}]}}]`)
	r := Read(dir)
	if got := r.Flows[0].Entries[0].Screenshots; len(got) != 0 {
		t.Fatalf("screenshots = %v, want the escaping path ignored", got)
	}
}

func TestRead_SymlinkedArtifactIsIgnored(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside.png")
	write(t, outside, "png")
	write(t, filepath.Join(dir, "Flow", "commands.json"),
		`[{"command":{"launchAppCommand":{"appId":"x"}},"metadata":{"status":"COMPLETED","timestamp":1,"sequenceNumber":0,"artifacts":[{"type":"SCREENSHOT","path":"screenshots/escape.png"}]}}]`)
	if err := os.MkdirAll(filepath.Join(dir, "Flow", "screenshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "Flow", "screenshots", "escape.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	r := Read(dir)
	flow := flowNamed(t, r, "Flow")
	if got := flow.Entries[0].Screenshots; len(got) != 0 {
		t.Errorf("entry screenshots = %v, want none", got)
	}
	if got := flow.Screenshots; len(got) != 0 {
		t.Errorf("unreferenced screenshots = %v, want none", got)
	}
}

func TestRead_SymlinkedFlowFolderIsNotABundle(t *testing.T) {
	dir := t.TempDir()
	realFlow := t.TempDir()
	write(t, filepath.Join(realFlow, "commands.json"),
		`[{"command":{"launchAppCommand":{"appId":"x"}},"metadata":{"status":"COMPLETED","timestamp":1,"sequenceNumber":0}}]`)
	if err := os.Symlink(realFlow, filepath.Join(dir, "Linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if r := Read(dir); len(r.Flows) != 0 {
		t.Fatalf("flows = %+v, want none", r.Flows)
	}
}

func TestRead_FlatShardPrefixAndMaestroLog(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "maestro.log"), "log")
	write(t, filepath.Join(dir, "commands-shard-2-(Login).json"),
		`[{"command":{"launchAppCommand":{"appId":"x"}},"metadata":{"status":"COMPLETED","timestamp":1,"sequenceNumber":0}}]`)
	r := Read(dir)
	if r.Layout != LayoutFlat || r.Flows[0].FileKey != "Login" || r.Flows[0].Name != "Login" {
		t.Fatalf("got %+v", r)
	}
	if r.MaestroLog != filepath.Join(dir, "maestro.log") {
		t.Fatalf("MaestroLog = %q", r.MaestroLog)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
