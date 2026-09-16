package steps

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-maestro/internal/debugdir"
)

func flow(t *testing.T, name string, parts ...string) debugdir.Flow {
	t.Helper()
	r := debugdir.Read(filepath.Join(append([]string{"..", "..", "test", "captures"}, parts...)...))
	for _, f := range r.Flows {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no flow %q", name)
	return debugdir.Flow{}
}

func TestRender(t *testing.T) {
	cases := []struct{ kind, raw, want string }{
		{"launchAppCommand", `{"appId":"com.apple.Preferences","optional":false}`, "launchApp: com.apple.Preferences"},
		{"assertConditionCommand", `{"condition":{"visible":{"textRegex":"${LABEL}","optional":false}},"optional":false}`, "assertVisible: ${LABEL}"},
		{"assertConditionCommand", `{"condition":{"notVisible":{"idRegex":"spinner"}}}`, "assertNotVisible: id: spinner"},
		{"tapOnElement", `{"selector":{"textRegex":"Definitely Not A Real Row","optional":true},"retryIfNoChange":false}`, "tapOn: Definitely Not A Real Row (optional)"},
		{"tapOnElement", `{"selector":{}}`, "tapOn"},
		{"inputTextCommand", `{"text":"${PASSWORD}"}`, "inputText: ${PASSWORD}"},
		{"repeatCommand", `{"times":"2","commands":[]}`, "repeat: 2 times"},
		{"retryCommand", `{"maxRetries":"1","commands":[]}`, "retry: up to 1 retry"},
		{"retryCommand", `{"maxRetries":3}`, "retry: up to 3 retries"},
		{"runFlowCommand", `{"commands":[]}`, "runFlow"},
		// Unknown commands are named without arguments: they may carry values.
		{"evalScriptCommand", `{"scriptString":"output.token = 'abc'"}`, "evalScript"},
		{"scrollCommand", `not json`, "scroll"},
	}
	for _, c := range cases {
		if got := Render(c.kind, json.RawMessage(c.raw)); got != c.want {
			t.Errorf("Render(%s, %s) = %q, want %q", c.kind, c.raw, got, c.want)
		}
	}
}

func TestStatus(t *testing.T) {
	for in, want := range map[string]string{
		"COMPLETED": "passed", "FAILED": "failed", "WARNED": "skipped",
		"SKIPPED": "skipped", "PENDING": "skipped", "RUNNING": "aborted", "SOMETHING_NEW": "skipped",
	} {
		if got := Status(in); got != want {
			t.Errorf("Status(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestConvert_BundleNestingBecomesParentIndex(t *testing.T) {
	f := flow(t, "Settings nested commands", "maestro-2.10.0-ios26.5", "debug")
	res := Convert(f.Entries, true)

	wantNames := []string{
		"launchApp: com.apple.Preferences", "repeat: 2 times",
		"assertVisible: ${LABEL}", "assertVisible: ${LABEL}",
		"retry: up to 1 retry", "assertVisible: ${LABEL}",
		"runFlow", "assertVisible: ${LABEL}",
	}
	wantParents := []string{"-", "-", "1", "1", "-", "4", "-", "6"}
	if len(res.Steps) != len(wantNames) {
		t.Fatalf("got %d steps, want %d: %+v", len(res.Steps), len(wantNames), res.Steps)
	}
	for i, s := range res.Steps {
		parent := "-"
		if s.ParentIndex != nil {
			parent = fmt.Sprint(*s.ParentIndex)
		}
		if s.Name != wantNames[i] || parent != wantParents[i] {
			t.Errorf("step %d = %q parent %s, want %q parent %s", i, s.Name, parent, wantNames[i], wantParents[i])
		}
	}
}

func TestConvert_BundleWarnedStepAndItsScreenshot(t *testing.T) {
	f := flow(t, "Settings opens", "maestro-2.10.0-ios26.5", "debug")
	res := Convert(f.Entries, true)
	if len(res.Steps) != 3 {
		t.Fatalf("got %d steps, want launchApp, assertVisible, tapOn", len(res.Steps))
	}
	tap := res.Steps[2]
	if tap.Status != "skipped" || tap.Error != WarnedMessage || tap.Duration != 3237*1_000_000 {
		t.Errorf("tap step = %+v", tap)
	}
	if len(res.StepOfShot) != 1 {
		t.Fatalf("StepOfShot = %v, want one screenshot", res.StepOfShot)
	}
	for path, idx := range res.StepOfShot {
		if idx != 2 || !strings.Contains(path, "step-005-tapOnElement") {
			t.Errorf("screenshot %s -> step %d, want the tapOn step (2)", path, idx)
		}
	}
}

func TestConvert_FlatHasNoNestingAndNullDurationIsZero(t *testing.T) {
	f := flow(t, "Settings opens", "maestro-2.6.1-ios26.5")
	res := Convert(f.Entries, false)
	for i, s := range res.Steps {
		if s.ParentIndex != nil {
			t.Errorf("step %d has a parent in the flat layout", i)
		}
	}
	if last := res.Steps[len(res.Steps)-1]; last.Status != "skipped" || last.Duration != 0 {
		t.Errorf("warned step = %+v, want skipped with duration 0", last)
	}
}

func TestConvert_FailedStepCarriesOnlyTheMessage(t *testing.T) {
	f := flow(t, "Settings fails on purpose", "maestro-2.6.1-ios26.5")
	res := Convert(f.Entries, false)
	last := res.Steps[len(res.Steps)-1]
	if want := `Assertion is false: "This Text Does Not Exist 12345" is visible`; last.Status != "failed" || last.Error != want {
		t.Errorf("failed step = %+v", last)
	}
}

func TestConvert_NoVariableValuesInAnyStep(t *testing.T) {
	for _, parts := range [][]string{{"maestro-2.6.1-ios26.5"}, {"maestro-2.10.0-ios26.5", "debug"}} {
		r := debugdir.Read(filepath.Join(append([]string{"..", "..", "test", "captures"}, parts...)...))
		for _, f := range r.Flows {
			data, _ := json.Marshal(Convert(f.Entries, r.Layout == debugdir.LayoutBundle).Steps)
			for _, leak := range []string{"General", "MAESTRO_", "089E029C-523A-4F81-8558-F0297DC8FF47", "hierarchyRoot"} {
				if strings.Contains(string(data), leak) {
					t.Errorf("%s / %s: steps contain %q", parts[0], f.Name, leak)
				}
			}
		}
	}
}

func TestConvert_DropsBookkeepingAndEverythingNestedUnderIt(t *testing.T) {
	entries := []debugdir.Entry{
		{Kind: "defineVariablesCommand", Raw: json.RawMessage(`{"env":{"K":"v"}}`), Status: "COMPLETED", Depth: 0},
		{Kind: "launchAppCommand", Raw: json.RawMessage(`{}`), Status: "COMPLETED", Depth: 1},
		{Kind: "applyConfigurationCommand", Raw: json.RawMessage(`{}`), Status: "COMPLETED", Depth: 0},
		{Kind: "launchAppCommand", Raw: json.RawMessage(`{"appId":"kept"}`), Status: "COMPLETED", Depth: 0},
	}
	res := Convert(entries, true)
	if len(res.Steps) != 1 || res.Steps[0].Name != "launchApp: kept" {
		t.Fatalf("steps = %+v", res.Steps)
	}
}

func TestConvert_StepCap(t *testing.T) {
	var entries []debugdir.Entry
	for i := 0; i < 305; i++ {
		entries = append(entries, debugdir.Entry{Kind: "scrollCommand", Raw: json.RawMessage(`{}`), Status: "COMPLETED", Sequence: i})
	}
	res := Convert(entries, true)
	if len(res.Steps) != 300 || res.Truncated != 5 {
		t.Fatalf("steps %d, truncated %d; want 300, 5", len(res.Steps), res.Truncated)
	}
}
