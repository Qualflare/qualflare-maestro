// Package build turns Maestro's JUnit report and debug output into a native
// Collect report. It performs no file I/O: the caller reads the inputs and
// carries out the screenshot copies returned, so every rule here is tested
// against the committed captures.
package build

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Qualflare/qualflare-maestro/internal/config"
	"github.com/Qualflare/qualflare-maestro/internal/constants"
	"github.com/Qualflare/qualflare-maestro/internal/debugdir"
	"github.com/Qualflare/qualflare-maestro/internal/junit"
	"github.com/Qualflare/qualflare-maestro/internal/redact"
	"github.com/Qualflare/qualflare-maestro/internal/steps"
	"github.com/Qualflare/qualflare-maestro/internal/textutil"
	"github.com/Qualflare/qualflare-maestro/internal/wire"
)

const (
	Framework        = "maestro"
	CLIName          = "qualflare-maestro"
	Category         = "e2e"
	FallbackPlatform = "ios"
)

// Input is everything a report is built from.
type Input struct {
	Cfg         config.Config
	FileToken   string
	JUnit       *junit.Report
	Debug       debugdir.Result
	ExitCode    int
	Interrupted bool
	StderrTail  string
	LogTail     string
	WorkingDir  string
	RepoRoot    string
	Redactor    *redact.Redactor
	Version     string
	Now         time.Time
}

// Copy is a screenshot to copy next to the report.
type Copy struct {
	From string
	To   string
}

// Output is the report plus the work and warnings it implies.
type Output struct {
	Report   wire.Collect
	Copies   []Copy
	Warnings []string
}

// Collect builds the report.
func Collect(in Input) Output {
	b := &builder{in: in}
	b.collect()
	return b.out
}

// DropAttachment removes every attachment pointing at localImagePath, for a
// screenshot whose copy failed.
func DropAttachment(c *wire.Collect, localImagePath string) {
	for si := range c.Suites {
		for ci := range c.Suites[si].Cases {
			cs := &c.Suites[si].Cases[ci]
			kept := cs.Attachments[:0]
			for _, a := range cs.Attachments {
				if a.LocalImagePath != localImagePath {
					kept = append(kept, a)
				}
			}
			cs.Attachments = kept
		}
	}
}

// FileSafe makes s usable inside a file name.
func FileSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', ' ', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		return r
	}, s)
}

type builder struct {
	in         Input
	out        Output
	shots      int
	pathWarned bool
}

func (b *builder) warn(format string, a ...any) {
	b.out.Warnings = append(b.out.Warnings, fmt.Sprintf(format, a...))
}

func (b *builder) collect() {
	in := b.in
	device := ""
	if in.JUnit != nil && len(in.JUnit.Suites) > 0 {
		device = in.JUnit.Suites[0].Device
	}
	c := wire.Collect{
		Framework:     Framework,
		Platform:      b.platform(device),
		OS:            osFromDevice(device),
		Environment:   in.Cfg.Environment,
		Language:      in.Cfg.Language,
		Metadata:      wire.Metadata{Version: in.Version, Timestamp: in.Now.UTC().Format(time.RFC3339), CLIName: CLIName, RunID: in.Cfg.RunID},
		Suites:        []wire.Suite{},
		Milestone:     in.Cfg.Milestone,
		CIProvider:    in.Cfg.CIProvider,
		CIBuildNumber: in.Cfg.CIBuildNumber,
		CIRunURL:      in.Cfg.CIRunURL,
		CIPRNumber:    in.Cfg.CIPRNumber,
	}
	if in.Cfg.Branch != "" {
		c.Branch = wire.StringPtr(in.Cfg.Branch)
	}
	if in.Cfg.Commit != "" {
		c.Commit = wire.StringPtr(in.Cfg.Commit)
	}

	cases := 0
	if in.JUnit != nil {
		flows, ambiguous := b.flowIndex()
		multi := len(in.JUnit.Suites) > 1
		duplicateOS := hasDuplicateOS(in.JUnit.Suites)
		identities := b.caseIdentities()
		if in.Debug.Layout == debugdir.LayoutNone {
			for _, suite := range in.JUnit.Suites {
				if len(suite.Cases) > 0 {
					b.warn("maestro's debug output had no command data, so cases have no steps or screenshots")
					break
				}
			}
		}
		for i, js := range in.JUnit.Suites {
			s := wire.NewSuite(suiteName(js, multi, duplicateOS, i+1), Category)
			for j, jc := range js.Cases {
				wc := b.buildCase(jc, identities[i][j], flows, ambiguous)
				s.Duration += wc.Duration
				s.Cases = append(s.Cases, wc)
			}
			cases += len(s.Cases)
			c.Suites = append(c.Suites, s)
		}
	}

	switch {
	case cases == 0 && (in.ExitCode != 0 || in.Interrupted):
		c.Suites = []wire.Suite{b.unattributed()}
	case cases == 0:
		c.Suites = []wire.Suite{}
		b.warn("maestro exited 0 but reported no flows; check the flow path and any --include-tags/--exclude-tags filters")
	}
	b.out.Report = c
}

type caseIdentity struct {
	id   string
	path string
}

func (b *builder) caseIdentities() [][]caseIdentity {
	identities := make([][]caseIdentity, len(b.in.JUnit.Suites))
	baseSuites := map[string]map[int]bool{}
	for i, suite := range b.in.JUnit.Suites {
		identities[i] = make([]caseIdentity, len(suite.Cases))
		for j, jc := range suite.Cases {
			path := b.flowPath(jc.File)
			base := jc.Name
			if path != "" {
				base = path + "#" + jc.Name
			}
			identities[i][j] = caseIdentity{id: base, path: path}
			if baseSuites[base] == nil {
				baseSuites[base] = map[int]bool{}
			}
			baseSuites[base][i] = true
		}
	}

	qualifiedCounts := map[string]int{}
	for i, suite := range b.in.JUnit.Suites {
		for _, identity := range identities[i] {
			if len(baseSuites[identity.id]) > 1 {
				qualifiedCounts[identity.id+"@"+osFromDevice(suite.Device)]++
			}
		}
	}
	for i, suite := range b.in.JUnit.Suites {
		for j := range identities[i] {
			identity := &identities[i][j]
			if len(baseSuites[identity.id]) <= 1 {
				continue
			}
			qualified := identity.id + "@" + osFromDevice(suite.Device)
			identity.id = qualified
			if qualifiedCounts[qualified] > 1 {
				identity.id += fmt.Sprintf("#%d", i+1)
			}
		}
	}
	return identities
}

var iosPattern = regexp.MustCompile(`(?i)\biOS\b`)

func (b *builder) platform(device string) string {
	switch p := strings.ToLower(b.in.Cfg.Platform); p {
	case "ios", "android", "web":
		return p
	case "":
	default:
		b.warn("ignoring -platform %q: expected ios, android or web", b.in.Cfg.Platform)
	}
	if p := detectPlatform(device); p != "" {
		return p
	}
	b.warn("could not tell the platform from the device %q; reporting %q, pass -platform to set it", device, FallbackPlatform)
	return FallbackPlatform
}

func detectPlatform(device string) string {
	d := strings.ToLower(device)
	switch {
	case iosPattern.MatchString(device):
		return "ios"
	case strings.Contains(d, "android"), strings.Contains(d, "emulator"):
		return "android"
	case strings.Contains(d, "chrom"), strings.Contains(d, "browser"):
		return "web"
	}
	return ""
}

// osFromDevice drops the trailing " - <UDID>" from "iPhone 17 - iOS 26.5 - <UDID>".
func osFromDevice(device string) string {
	parts := strings.Split(device, " - ")
	if len(parts) >= 3 {
		parts = parts[:len(parts)-1]
	}
	if s := strings.TrimSpace(strings.Join(parts, " - ")); s != "" {
		return textutil.Truncate(s, 100)
	}
	return "unknown"
}

func hasDuplicateOS(suites []junit.Suite) bool {
	seen := map[string]bool{}
	for _, suite := range suites {
		osName := osFromDevice(suite.Device)
		if seen[osName] {
			return true
		}
		seen[osName] = true
	}
	return false
}

func suiteName(js junit.Suite, multi, duplicateOS bool, position int) string {
	name := js.Name
	if name == "" {
		name = "Maestro"
	}
	if multi {
		osName := osFromDevice(js.Device)
		if duplicateOS {
			osName += fmt.Sprintf(" #%d", position)
		}
		name += " (" + osName + ")"
	}
	return textutil.Truncate(name, 255)
}

// flowIndex maps flow names to their debug output, and marks names that are
// not unique -- in the debug output or in the JUnit report -- as ambiguous.
func (b *builder) flowIndex() (map[string]*debugdir.Flow, map[string]bool) {
	flows := map[string]*debugdir.Flow{}
	ambiguous := map[string]bool{}
	for i := range b.in.Debug.Flows {
		f := &b.in.Debug.Flows[i]
		if _, seen := flows[f.Name]; seen {
			ambiguous[f.Name] = true
		}
		flows[f.Name] = f
	}
	seen := map[string]bool{}
	for _, s := range b.in.JUnit.Suites {
		for _, c := range s.Cases {
			if seen[c.Name] {
				ambiguous[c.Name] = true
			}
			seen[c.Name] = true
		}
	}
	var names []string
	for name := range ambiguous {
		if _, ok := flows[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b.warn("more than one flow ran as %q, so their steps and screenshots cannot be told apart and are left out; give each flow a unique name", name)
	}
	return flows, ambiguous
}

func (b *builder) buildCase(jc junit.Case, identity caseIdentity, flows map[string]*debugdir.Flow, ambiguous map[string]bool) wire.Case {
	r := b.in.Redactor
	wc := wire.Case{
		ID:         identity.id,
		Name:       textutil.TruncateOr(jc.Name, 255, "(unnamed flow)"),
		ClassName:  textutil.Truncate(identity.path, 255),
		Status:     caseStatus(jc),
		Error:      textutil.Truncate(r.String(jc.Failure), constants.MaxCaseErrorRunes),
		Duration:   int64(math.Round(jc.Seconds * 1e9)),
		ShardIndex: b.in.Cfg.ShardIndex,
	}
	b.applyProperties(&wc, jc.Properties)

	f, matched := flows[jc.Name]
	if matched && !ambiguous[jc.Name] {
		entries := append([]debugdir.Entry(nil), f.Entries...)
		for i := range entries {
			entries[i].ErrorMessage = r.String(entries[i].ErrorMessage)
		}
		conv := steps.Convert(entries, b.in.Debug.Layout == debugdir.LayoutBundle)
		for i := range conv.Steps {
			conv.Steps[i].Name = textutil.Truncate(r.String(conv.Steps[i].Name), 255)
			conv.Steps[i].Error = textutil.Truncate(conv.Steps[i].Error, constants.MaxAttemptMessageRunes)
		}
		wc.Steps = conv.Steps
		if conv.Truncated > 0 {
			b.warn("flow %q has more than %d steps; the last %d were left out", jc.Name, constants.MaxStepsPerTestAttempt, conv.Truncated)
		}
		if d, start, ok := span(f.Entries); ok {
			wc.Duration, wc.StartedAt = d, start
		}
		wc.Attachments = b.attachments(f, conv)
	} else if b.in.Debug.Layout != debugdir.LayoutNone && !matched && !ambiguous[jc.Name] {
		b.warn("flow %q: no command data matched it, so it has no steps or screenshots", jc.Name)
	}
	if wc.StartedAt == "" && jc.Timestamp != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04:05", jc.Timestamp, time.Local); err == nil {
			wc.StartedAt = t.UTC().Format(time.RFC3339)
		}
	}
	return wc
}

func caseStatus(jc junit.Case) string {
	switch jc.Status {
	case "SUCCESS", "WARNING":
		return "passed"
	case "ERROR":
		return "failed"
	case "CANCELED", "STOPPED":
		return "aborted"
	}
	if jc.Failed {
		return "failed"
	}
	if jc.Status != "" {
		return "aborted"
	}
	return "passed"
}

func (b *builder) flowPath(file string) string {
	if file == "" {
		return ""
	}
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(b.in.WorkingDir, file)
	}
	if b.in.RepoRoot != "" {
		if rel, err := filepath.Rel(b.in.RepoRoot, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	if !b.pathWarned {
		b.pathWarned = true
		b.warn("the flows are not inside a git repository, so case ids use paths relative to the working directory; run from the same directory every time to keep each flow's history together")
	}
	return filepath.ToSlash(file)
}

var (
	priorities = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
	linkTypes  = map[string]bool{"issue": true, "tms": true, "custom": true}
)

func (b *builder) applyProperties(wc *wire.Case, props []junit.Property) {
	r := b.in.Redactor
	for _, p := range props {
		value := r.String(p.Value)
		switch {
		case p.Name == "tags":
			for _, tag := range strings.Split(p.Value, ",") {
				if tag = strings.TrimSpace(tag); tag != "" && len(wc.Tags) < constants.MaxTagsPerCase {
					wc.Tags = append(wc.Tags, textutil.Truncate(tag, constants.MaxTagLength))
				}
			}
		case p.Name == "junitId", p.Name == "junitClassname":
			// JUnit's own naming; case ids deliberately ignore it.
		case p.Name == "qualflare.priority":
			if v := strings.ToLower(strings.TrimSpace(p.Value)); priorities[v] {
				wc.Priority = v
			} else {
				b.warn("flow %q: qualflare.priority %q is not low, medium, high or critical; ignored", wc.Name, p.Value)
			}
		case p.Name == "qualflare.description":
			wc.Description = textutil.Truncate(value, 10000)
		case strings.HasPrefix(p.Name, "qualflare.link."):
			typ, name, _ := strings.Cut(strings.TrimPrefix(p.Name, "qualflare.link."), ".")
			if !linkTypes[typ] || value == "" {
				b.warn("flow %q: %s is not a link this reporter understands (qualflare.link.issue, .tms or .custom, optionally .<name>); ignored", wc.Name, p.Name)
				continue
			}
			if len(wc.Links) < constants.MaxLinksPerCase {
				wc.Links = append(wc.Links, wire.Link{URL: value, Type: typ, Name: textutil.Truncate(name, 255)})
			}
		case strings.HasPrefix(p.Name, "qualflare.label."):
			name := strings.TrimPrefix(p.Name, "qualflare.label.")
			if name == "" {
				b.warn("flow %q: qualflare.label. needs a name after the dot; ignored", wc.Name)
				continue
			}
			if len(wc.Labels) < constants.MaxLabelsPerCase {
				wc.Labels = append(wc.Labels, wire.Label{Name: textutil.Truncate(name, 128), Value: textutil.Truncate(value, 512)})
			}
		case strings.HasPrefix(p.Name, "qualflare."):
			b.warn("flow %q: unknown property %s; the qualflare.* names are priority, description, link.<type>[.<name>] and label.<name>", wc.Name, p.Name)
		default:
			if wc.Properties == nil {
				wc.Properties = map[string]string{}
			}
			wc.Properties[p.Name] = value
		}
	}
}

// span is the time from the first command's start to the last command's end.
func span(entries []debugdir.Entry) (int64, string, bool) {
	var first, last int64
	found := false
	for _, e := range entries {
		if e.TimestampMs <= 0 {
			continue
		}
		end := e.TimestampMs
		if e.DurationMs != nil {
			end += *e.DurationMs
		}
		if !found || e.TimestampMs < first {
			first = e.TimestampMs
		}
		if !found || end > last {
			last = end
		}
		found = true
	}
	if !found {
		return 0, "", false
	}
	return (last - first) * int64(time.Millisecond), time.UnixMilli(first).UTC().Format(time.RFC3339), true
}

func (b *builder) attachments(f *debugdir.Flow, conv steps.Result) []wire.Attachment {
	var out []wire.Attachment
	add := func(path string, step *int) {
		if len(out) >= constants.MaxAttachmentsPerCase {
			return
		}
		b.shots++
		fileToken := b.in.FileToken
		if fileToken == "" {
			fileToken = FileSafe(b.in.Cfg.RunID)
		}
		rel := fmt.Sprintf("attachments/%s-%d.png", fileToken, b.shots)
		b.out.Copies = append(b.out.Copies, Copy{From: path, To: rel})
		out = append(out, wire.Attachment{
			Name:           textutil.Truncate(filepath.Base(rel), 255),
			MimeType:       "image/png",
			LocalImagePath: rel,
			StepIndex:      step,
		})
	}
	for _, e := range f.Entries {
		for _, p := range e.Screenshots {
			if idx, ok := conv.StepOfShot[p]; ok {
				add(p, wire.IntPtr(idx))
			} else {
				add(p, nil)
			}
		}
	}
	failed := -1
	for i, s := range conv.Steps {
		if s.Status == "failed" {
			failed = i
			break
		}
	}
	for _, s := range f.Screenshots {
		if s.Failed && failed >= 0 {
			add(s.Path, wire.IntPtr(failed))
		} else {
			add(s.Path, nil)
		}
	}
	return out
}

func (b *builder) unattributed() wire.Suite {
	in := b.in
	s := wire.NewSuite("[unattributed]", Category)
	name, status := "[unattributed failure]", "error"
	if in.Interrupted {
		name, status = "[interrupted run]", "aborted"
	}
	msg := strings.TrimSpace(in.LogTail)
	if msg == "" {
		msg = strings.TrimSpace(in.StderrTail)
	}
	if msg == "" {
		msg = fmt.Sprintf("maestro exited with code %d without writing a JUnit report, so no flow results are available", in.ExitCode)
	}
	s.Cases = append(s.Cases, wire.Case{
		ID:     name,
		Name:   name,
		Status: status,
		Error:  textutil.Truncate(in.Redactor.String(msg), constants.MaxCaseErrorRunes),
	})
	return s
}
