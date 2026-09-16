package build

import (
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Qualflare/qualflare-maestro/internal/config"
	"github.com/Qualflare/qualflare-maestro/internal/constants"
	"github.com/Qualflare/qualflare-maestro/internal/debugdir"
	"github.com/Qualflare/qualflare-maestro/internal/junit"
	"github.com/Qualflare/qualflare-maestro/internal/redact"
	"github.com/Qualflare/qualflare-maestro/internal/steps"
	"github.com/Qualflare/qualflare-maestro/internal/wire"
)

const (
	flat   = "maestro-2.6.1-ios26.5"
	bundle = "maestro-2.10.0-ios26.5"
)

// load builds an Input from a committed capture, as if maestro had just run
// in the capture's own folder inside a git repository rooted there.
func load(t *testing.T, capture string) Input {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "test", "captures", capture))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := junit.ParseFile(filepath.Join(root, "report.xml"))
	if err != nil {
		t.Fatal(err)
	}
	debug := root
	if capture == bundle {
		debug = filepath.Join(root, "debug")
	}
	return Input{
		Cfg:        config.Config{Environment: "ci", Language: "en-US", RunID: "run-1"},
		JUnit:      &rep,
		Debug:      debugdir.Read(debug),
		ExitCode:   1,
		WorkingDir: root,
		RepoRoot:   root,
		Version:    "1.2.3",
		Now:        time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	}
}

func caseNamed(t *testing.T, c wire.Collect, name string) wire.Case {
	t.Helper()
	for _, s := range c.Suites {
		for _, cs := range s.Cases {
			if cs.Name == name {
				return cs
			}
		}
	}
	t.Fatalf("no case %q", name)
	return wire.Case{}
}

func TestCollect_TopLevelFields(t *testing.T) {
	c := Collect(load(t, bundle)).Report
	if c.Framework != "maestro" || c.Platform != "ios" || c.OS != "iPhone 17 - iOS 26.5" {
		t.Errorf("framework/platform/os = %q/%q/%q", c.Framework, c.Platform, c.OS)
	}
	if c.Metadata.CLIName != "qualflare-maestro" || c.Metadata.Version != "1.2.3" || c.Metadata.RunID != "run-1" {
		t.Errorf("metadata = %+v", c.Metadata)
	}
	if len(c.Suites) != 1 || c.Suites[0].Category != "e2e" || len(c.Suites[0].Cases) != 3 {
		t.Fatalf("suites = %+v", c.Suites)
	}
}

func TestCollect_CaseIdentityStatusAndError(t *testing.T) {
	c := Collect(load(t, bundle)).Report
	opens := caseNamed(t, c, "Settings opens")
	if opens.ID != "flows/settings-opens.yaml#Settings opens" || opens.ClassName != "flows/settings-opens.yaml" || opens.Status != "passed" {
		t.Errorf("opens = id %q class %q status %q", opens.ID, opens.ClassName, opens.Status)
	}
	fails := caseNamed(t, c, "Settings fails on purpose")
	if want := `Assertion is false: "This Text Does Not Exist 12345" is visible`; fails.Status != "failed" || fails.Error != want {
		t.Errorf("fails = status %q error %q", fails.Status, fails.Error)
	}
}

func TestCaseStatus_UnknownStatusesFailClosed(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status string
		failed bool
		want   string
	}{
		{name: "pending without failure", status: "PENDING", want: "aborted"},
		{name: "pending with failure", status: "PENDING", failed: true, want: "failed"},
		{name: "running without failure", status: "RUNNING", want: "aborted"},
		{name: "running with failure", status: "RUNNING", failed: true, want: "failed"},
		{name: "empty without failure", want: "passed"},
		{name: "empty with failure", failed: true, want: "failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := caseStatus(junit.Case{Status: tt.status, Failed: tt.failed}); got != tt.want {
				t.Fatalf("caseStatus(%q, failed=%v) = %q, want %q", tt.status, tt.failed, got, tt.want)
			}
		})
	}
}

func TestCollect_IDsAreRelativeToTheRepositoryNotTheWorkingDirectory(t *testing.T) {
	in := load(t, bundle)
	in.RepoRoot = filepath.Dir(filepath.Dir(in.WorkingDir)) // two levels above where maestro ran
	opens := caseNamed(t, Collect(in).Report, "Settings opens")
	if want := "captures/" + bundle + "/flows/settings-opens.yaml#Settings opens"; opens.ID != want {
		t.Errorf("ID = %q, want %q", opens.ID, want)
	}
}

func TestCollect_OutsideARepositoryWarnsOnce(t *testing.T) {
	in := load(t, bundle)
	in.RepoRoot = ""
	out := Collect(in)
	if n := countContaining(out.Warnings, "not inside a git repository"); n != 1 {
		t.Errorf("got %d repository warnings, want 1: %v", n, out.Warnings)
	}
}

func TestCollect_MetadataFromProperties(t *testing.T) {
	opens := caseNamed(t, Collect(load(t, flat)).Report, "Settings opens")
	if opens.Priority != "high" {
		t.Errorf("Priority = %q", opens.Priority)
	}
	if len(opens.Links) != 1 || opens.Links[0] != (wire.Link{URL: "https://example.com/QF-1", Type: "issue"}) {
		t.Errorf("Links = %+v", opens.Links)
	}
	if strings.Join(opens.Tags, ",") != "smoke,probe" {
		t.Errorf("Tags = %v", opens.Tags)
	}
	if len(opens.Properties) != 1 || opens.Properties["team"] != "mobile" {
		t.Errorf("Properties = %v, want only team=mobile", opens.Properties)
	}
}

func TestCollect_LabelsDescriptionAndBadValues(t *testing.T) {
	in := load(t, flat)
	in.JUnit.Suites[0].Cases[1].Properties = []junit.Property{
		{Name: "qualflare.label.team", Value: "payments"},
		{Name: "qualflare.description", Value: "Checks checkout."},
		{Name: "qualflare.link.custom.runbook", Value: "https://example.com/rb"},
		{Name: "qualflare.priority", Value: "urgent"},
		{Name: "qualflare.link.jira", Value: "https://example.com/x"},
		{Name: "qualflare.colour", Value: "red"},
	}
	out := Collect(in)
	opens := caseNamed(t, out.Report, "Settings opens")
	if len(opens.Labels) != 1 || opens.Labels[0] != (wire.Label{Name: "team", Value: "payments"}) {
		t.Errorf("Labels = %+v", opens.Labels)
	}
	if opens.Description != "Checks checkout." {
		t.Errorf("Description = %q", opens.Description)
	}
	if len(opens.Links) != 1 || opens.Links[0] != (wire.Link{URL: "https://example.com/rb", Type: "custom", Name: "runbook"}) {
		t.Errorf("Links = %+v", opens.Links)
	}
	if opens.Priority != "" || len(opens.Properties) != 0 {
		t.Errorf("bad values leaked: priority %q, properties %v", opens.Priority, opens.Properties)
	}
	for _, key := range []string{"urgent", "qualflare.link.jira", "qualflare.colour"} {
		if countContaining(out.Warnings, key) != 1 {
			t.Errorf("no warning mentioning %q in %v", key, out.Warnings)
		}
	}
}

func TestCollect_DurationComesFromCommandTimestamps(t *testing.T) {
	in := load(t, flat)
	in.JUnit = &junit.Report{Suites: []junit.Suite{{Name: "S", Cases: []junit.Case{{Name: "F", Status: "SUCCESS", Seconds: 13}}}}}
	d500, d250 := int64(500), int64(250)
	in.Debug = debugdir.Result{Layout: debugdir.LayoutFlat, Flows: []debugdir.Flow{{Name: "F", Entries: []debugdir.Entry{
		{Kind: "launchAppCommand", Status: "COMPLETED", TimestampMs: 1000, DurationMs: &d500},
		{Kind: "tapOnElement", Status: "COMPLETED", TimestampMs: 2000, DurationMs: &d250, Sequence: 1},
	}}}}
	f := caseNamed(t, Collect(in).Report, "F")
	if f.Duration != 1_250_000_000 || f.StartedAt != "1970-01-01T00:00:01Z" {
		t.Errorf("duration %d startedAt %q, want 1.25s from 1970-01-01T00:00:01Z", f.Duration, f.StartedAt)
	}
}

func TestCollect_BundleScreenshotIsLinkedToItsStep(t *testing.T) {
	out := Collect(load(t, bundle))
	opens := caseNamed(t, out.Report, "Settings opens")
	if len(opens.Steps) != 3 || opens.Steps[2].Error != steps.WarnedMessage {
		t.Fatalf("steps = %+v", opens.Steps)
	}
	if len(opens.Attachments) != 1 {
		t.Fatalf("attachments = %+v", opens.Attachments)
	}
	a := opens.Attachments[0]
	if a.StepIndex == nil || *a.StepIndex != 2 || a.MimeType != "image/png" || !strings.HasPrefix(a.LocalImagePath, "attachments/run-1-") {
		t.Errorf("attachment = %+v", a)
	}
	if a.Name != path.Base(a.LocalImagePath) {
		t.Errorf("attachment name = %q, want copied file name %q", a.Name, path.Base(a.LocalImagePath))
	}
	if strings.Contains(a.Name, "Definitely") {
		t.Errorf("attachment name contains command argument: %q", a.Name)
	}
	var copied bool
	for _, cp := range out.Copies {
		if cp.To == a.LocalImagePath && strings.HasSuffix(cp.From, "step-005-tapOnElement-Definitely_Not_A_Real_Row.png") {
			copied = true
		}
	}
	if !copied {
		t.Errorf("no copy for %s in %+v", a.LocalImagePath, out.Copies)
	}
}

func TestCollect_AttachmentNamesCarryTheFileToken(t *testing.T) {
	in := load(t, bundle)
	in.FileToken = "run-1-4242"
	out := Collect(in)

	var attachments int
	for _, suite := range out.Report.Suites {
		for _, cs := range suite.Cases {
			for _, attachment := range cs.Attachments {
				attachments++
				if !strings.HasPrefix(attachment.LocalImagePath, "attachments/run-1-4242-") {
					t.Errorf("LocalImagePath = %q, want file token prefix", attachment.LocalImagePath)
				}
			}
		}
	}
	if attachments == 0 {
		t.Fatal("bundle capture produced no attachments")
	}
}

func TestCollect_BundleNestingSurvives(t *testing.T) {
	nested := caseNamed(t, Collect(load(t, bundle)).Report, "Settings nested commands")
	if nested.Steps[2].ParentIndex == nil || *nested.Steps[2].ParentIndex != 1 {
		t.Errorf("step 2 parent = %v, want 1", nested.Steps[2].ParentIndex)
	}
}

func TestCollect_FlatFailureScreenshotPointsAtTheFailedStep(t *testing.T) {
	fails := caseNamed(t, Collect(load(t, flat)).Report, "Settings fails on purpose")
	if len(fails.Attachments) != 1 || fails.Attachments[0].StepIndex == nil {
		t.Fatalf("attachments = %+v", fails.Attachments)
	}
	if strings.Contains(fails.Attachments[0].Name, "Settings") {
		t.Errorf("attachment name contains flow name: %q", fails.Attachments[0].Name)
	}
	if i := *fails.Attachments[0].StepIndex; fails.Steps[i].Status != "failed" {
		t.Errorf("stepIndex %d is a %q step, want the failed one", i, fails.Steps[i].Status)
	}
}

func TestCollect_NoSecretsOrDeviceIDsInTheReport(t *testing.T) {
	for _, capture := range []string{flat, bundle} {
		data, err := json.Marshal(Collect(load(t, capture)).Report)
		if err != nil {
			t.Fatal(err)
		}
		for _, leak := range []string{"General", "MAESTRO_", "089E029C-523A-4F81-8558-F0297DC8FF47", "hierarchyRoot", "debugMessage"} {
			if strings.Contains(string(data), leak) {
				t.Errorf("%s: report contains %q", capture, leak)
			}
		}
	}
}

func TestCollect_RedactsKnownValuesFromFreeText(t *testing.T) {
	in := load(t, flat)
	in.JUnit.Suites[0].Cases[0].Failure = `Assertion is false: "hunter22-secret" is visible`
	in.JUnit.Suites[0].Cases[0].Properties = append(in.JUnit.Suites[0].Cases[0].Properties, junit.Property{Name: "account", Value: "hunter22-secret"})
	in.Redactor = redact.New([]redact.Pair{{Key: "PASSWORD", Value: "hunter22-secret"}})
	fails := caseNamed(t, Collect(in).Report, "Settings fails on purpose")
	if want := `Assertion is false: "${PASSWORD}" is visible`; fails.Error != want {
		t.Errorf("Error = %q, want %q", fails.Error, want)
	}
	if fails.Properties["account"] != "${PASSWORD}" {
		t.Errorf("property = %q", fails.Properties["account"])
	}
}

func TestCollect_RedactsBeforeTruncatingStepErrors(t *testing.T) {
	const secret = "hunter22-secret"
	in := Input{
		JUnit: &junit.Report{Suites: []junit.Suite{{
			Name:   "S",
			Device: "iPhone 17 - iOS 26.5 - DEVICE",
			Cases:  []junit.Case{{Name: "F", Status: "ERROR"}},
		}}},
		Debug: debugdir.Result{Layout: debugdir.LayoutBundle, Flows: []debugdir.Flow{{
			Name: "F",
			Entries: []debugdir.Entry{{
				Kind:         "tapOnElement",
				Status:       "FAILED",
				ErrorMessage: strings.Repeat("x", constants.MaxAttemptMessageRunes-6) + secret,
			}},
		}}},
		Redactor: redact.New([]redact.Pair{{Key: "PASSWORD", Value: secret}}),
	}

	got := caseNamed(t, Collect(in).Report, "F").Steps[0].Error
	if strings.Contains(got, secret[:6]) {
		t.Fatalf("step error contains partial secret %q at the truncation boundary", secret[:6])
	}
	if n := utf8.RuneCountInString(got); n > constants.MaxAttemptMessageRunes {
		t.Fatalf("step error is %d runes, want at most %d", n, constants.MaxAttemptMessageRunes)
	}
}

func TestCollect_RedactsBeforeTruncatingStepNames(t *testing.T) {
	const secret = "hunter22-secret"
	raw, err := json.Marshal(map[string]string{"text": strings.Repeat("x", 238) + secret})
	if err != nil {
		t.Fatal(err)
	}
	in := Input{
		JUnit: &junit.Report{Suites: []junit.Suite{{
			Name:   "S",
			Device: "iPhone 17 - iOS 26.5 - DEVICE",
			Cases:  []junit.Case{{Name: "F", Status: "ERROR"}},
		}}},
		Debug: debugdir.Result{Layout: debugdir.LayoutBundle, Flows: []debugdir.Flow{{
			Name: "F",
			Entries: []debugdir.Entry{{
				Kind:   "inputTextCommand",
				Raw:    raw,
				Status: "FAILED",
			}},
		}}},
		Redactor: redact.New([]redact.Pair{{Key: "PASSWORD", Value: secret}}),
	}

	got := caseNamed(t, Collect(in).Report, "F").Steps[0].Name
	if strings.Contains(got, secret[:6]) {
		t.Fatalf("step name contains partial secret %q at the truncation boundary", secret[:6])
	}
	if n := utf8.RuneCountInString(got); n > 255 {
		t.Fatalf("step name is %d runes, want at most 255", n)
	}
}

func TestCollect_UnattributedFailureWhenNothingWasReported(t *testing.T) {
	out := Collect(Input{
		Cfg: config.Config{Environment: "ci", Language: "en-US", RunID: "r"}, ExitCode: 1,
		LogTail: "No devices found", StderrTail: "ignored when the log has something",
		Redactor: redact.New(nil), Now: time.Unix(0, 0),
	})
	if len(out.Report.Suites) != 1 || out.Report.Suites[0].Name != "[unattributed]" {
		t.Fatalf("suites = %+v", out.Report.Suites)
	}
	c := out.Report.Suites[0].Cases[0]
	if c.Name != "[unattributed failure]" || c.Status != "error" || c.Error != "No devices found" {
		t.Errorf("case = %+v", c)
	}
}

func TestCollect_RedactsTheUnattributedText(t *testing.T) {
	out := Collect(Input{
		ExitCode: 1,
		LogTail:  "token=hunter22-secret",
		Redactor: redact.New([]redact.Pair{{Key: "PASSWORD", Value: "hunter22-secret"}}),
	})
	if got, want := out.Report.Suites[0].Cases[0].Error, "token=${PASSWORD}"; got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}

func TestCollect_UnattributedFallsBackToStderrThenToTheExitCode(t *testing.T) {
	c := Collect(Input{ExitCode: 3, StderrTail: "boom\n"}).Report.Suites[0].Cases[0]
	if c.Error != "boom" {
		t.Errorf("Error = %q, want stderr", c.Error)
	}
	c = Collect(Input{ExitCode: 3}).Report.Suites[0].Cases[0]
	if !strings.Contains(c.Error, "code 3") {
		t.Errorf("Error = %q, want the exit code named", c.Error)
	}
}

func TestCollect_InterruptedWithoutJUnitIsAborted(t *testing.T) {
	c := Collect(Input{ExitCode: 130, Interrupted: true}).Report.Suites[0].Cases[0]
	if c.Name != "[interrupted run]" || c.Status != "aborted" {
		t.Errorf("case = %+v", c)
	}
}

func TestCollect_ExitZeroWithNoFlowsWarns(t *testing.T) {
	out := Collect(Input{JUnit: &junit.Report{}})
	if len(out.Report.Suites) != 0 || countContaining(out.Warnings, "reported no flows") != 1 {
		t.Errorf("suites %d, warnings %v", len(out.Report.Suites), out.Warnings)
	}
}

func TestCollect_DuplicateFlowNamesSkipEnrichment(t *testing.T) {
	in := load(t, flat)
	in.JUnit.Suites[0].Cases = append(in.JUnit.Suites[0].Cases, in.JUnit.Suites[0].Cases[1])
	out := Collect(in)
	for _, c := range out.Report.Suites[0].Cases {
		if c.Name == "Settings opens" && (len(c.Steps) != 0 || len(c.Attachments) != 0) {
			t.Errorf("an ambiguous flow got steps or screenshots: %+v", c)
		}
	}
	if countContaining(out.Warnings, `"Settings opens"`) != 1 {
		t.Errorf("warnings = %v", out.Warnings)
	}
}

func TestCollect_WarnsWhenThereIsNoCommandData(t *testing.T) {
	in := load(t, bundle)
	in.Debug = debugdir.Result{}
	out := Collect(in)
	if len(out.Report.Suites) != 1 || len(out.Report.Suites[0].Cases) != 3 {
		t.Fatalf("suites = %+v, want one suite with three cases", out.Report.Suites)
	}
	want := "maestro's debug output had no command data, so cases have no steps or screenshots"
	if len(out.Warnings) != 1 || out.Warnings[0] != want {
		t.Fatalf("warnings = %v, want exactly %q", out.Warnings, want)
	}
}

func TestCollect_WarnsWhenAFlowHasNoCommands(t *testing.T) {
	in := load(t, flat)
	missing := in.Debug.Flows[0].Name
	in.Debug.Flows = in.Debug.Flows[1:]
	out := Collect(in)
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], missing) || !strings.Contains(out.Warnings[0], "no command data matched") {
		t.Fatalf("warnings = %v, want one warning naming %q", out.Warnings, missing)
	}
}

func TestCollect_Platform(t *testing.T) {
	in := load(t, bundle)
	in.Cfg.Platform = "android"
	if got := Collect(in).Report.Platform; got != "android" {
		t.Errorf("override: %q", got)
	}

	in.Cfg.Platform = "banana"
	out := Collect(in)
	if out.Report.Platform != "ios" || countContaining(out.Warnings, "banana") != 1 {
		t.Errorf("bad override: platform %q warnings %v", out.Report.Platform, out.Warnings)
	}

	out = Collect(Input{ExitCode: 1})
	if out.Report.Platform != FallbackPlatform || countContaining(out.Warnings, "-platform") != 1 {
		t.Errorf("fallback: platform %q warnings %v", out.Report.Platform, out.Warnings)
	}
}

func TestDetectPlatform(t *testing.T) {
	for device, want := range map[string]string{
		"iPhone 17 - iOS 26.5 - 089E029C":      "ios",
		"Pixel 7 - Android 14 - emulator-5554": "android",
		"emulator-5554":                        "android",
		"Chromium Desktop Browser":             "web",
		"Studio Display":                       "",
	} {
		if got := detectPlatform(device); got != want {
			t.Errorf("detectPlatform(%q) = %q, want %q", device, got, want)
		}
	}
}

func TestCollect_MultipleSuitesQualifyIDsAndNames(t *testing.T) {
	in := load(t, bundle)
	second := in.JUnit.Suites[0]
	second.Device = "iPhone Air - iOS 26.5 - AAAA"
	in.JUnit.Suites = append(in.JUnit.Suites, second)
	c := Collect(in).Report
	if c.Suites[1].Name != "Test Suite (iPhone Air - iOS 26.5)" {
		t.Errorf("suite name = %q", c.Suites[1].Name)
	}
	if id := c.Suites[1].Cases[0].ID; !strings.HasSuffix(id, "@iPhone Air - iOS 26.5") {
		t.Errorf("id = %q", id)
	}
}

func TestCollect_ShardSplitKeepsIDsStable(t *testing.T) {
	in := load(t, bundle)
	all := in.JUnit.Suites[0].Cases
	first := in.JUnit.Suites[0]
	first.Cases = append([]junit.Case(nil), all[:1]...)
	second := in.JUnit.Suites[0]
	second.Cases = append([]junit.Case(nil), all[1:]...)
	in.JUnit.Suites = []junit.Suite{first, second}

	c := Collect(in).Report
	for _, suite := range c.Suites {
		for _, cs := range suite.Cases {
			want := cs.ClassName + "#" + cs.Name
			if cs.ID != want {
				t.Errorf("id = %q, want stable unqualified id %q", cs.ID, want)
			}
		}
	}
}

func TestCollect_IdenticalShardDevicesGetDistinctIDs(t *testing.T) {
	in := load(t, bundle)
	in.JUnit.Suites = append(in.JUnit.Suites, in.JUnit.Suites[0])
	c := Collect(in).Report
	if c.Suites[0].Name == c.Suites[1].Name {
		t.Errorf("suite names are both %q", c.Suites[0].Name)
	}
	firstID := c.Suites[0].Cases[0].ID
	secondID := c.Suites[1].Cases[0].ID
	if firstID == secondID {
		t.Errorf("case ids are both %q", firstID)
	}
	if want := "@iPhone 17 - iOS 26.5#1"; !strings.HasSuffix(firstID, want) {
		t.Errorf("first case id = %q, want suffix %q", firstID, want)
	}
	if want := "@iPhone 17 - iOS 26.5#2"; !strings.HasSuffix(secondID, want) {
		t.Errorf("second case id = %q, want suffix %q", secondID, want)
	}
}

func TestDropAttachment(t *testing.T) {
	c := wire.Collect{Suites: []wire.Suite{{Cases: []wire.Case{{Attachments: []wire.Attachment{
		{LocalImagePath: "attachments/a.png"}, {LocalImagePath: "attachments/b.png"},
	}}}}}}
	DropAttachment(&c, "attachments/a.png")
	if got := c.Suites[0].Cases[0].Attachments; len(got) != 1 || got[0].LocalImagePath != "attachments/b.png" {
		t.Errorf("attachments = %+v", got)
	}
}

func countContaining(xs []string, sub string) int {
	n := 0
	for _, x := range xs {
		if strings.Contains(x, sub) {
			n++
		}
	}
	return n
}
