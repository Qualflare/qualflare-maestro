# qualflare-maestro Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go binary, `qualflare-maestro`, that runs `maestro test`, reads the JUnit report and debug output Maestro leaves behind, and writes a native Qualflare report with steps, screenshots, metadata and infrastructure failures.

**Architecture:** Maestro has no reporter API, so the binary wraps the process the way `qualflare-go` wraps `go test`: it adds four output flags, streams Maestro's output, then builds the report from files on disk and exits with Maestro's exit code. Two debug-output layouts exist (flat on 2.6.x, per-flow bundle on 2.10+); the reader decides by what is on disk. All report rules live in a pure `internal/build` package tested against real captured Maestro output.

**Tech Stack:** Go (module floor 1.21), standard library only. goreleaser, GitHub Actions (Linux for unit tests, macOS + iOS simulator for real-Maestro tests).

**Spec:** `docs/superpowers/specs/2026-09-16-qualflare-maestro-design.md`

## Global Constraints

- Module path `github.com/Qualflare/qualflare-maestro`; `go 1.21`; **no dependencies outside the standard library**.
- Every `duration` on the wire is an `int64` of **nanoseconds**.
- Case and step statuses only: `passed` `failed` `skipped` `error` `timeout` `aborted`.
- `framework` is `maestro`; `metadata.cliName` is `qualflare-maestro`; suite `category` is `e2e`.
- Step cap **300** per case (`constants.MaxStepsPerTestAttempt`); at most **50** attachments, **64** tags, **100** labels, **20** links per case.
- `defineVariablesCommand` and `applyConfigurationCommand` are **never** steps, nor is anything nested under them.
- Never read or serialize `evaluatedCommand`, `hierarchyRoot`, `debugMessage`, `stackTrace`, `screen-hierarchy/`, device logs or `manifest.json` contents. Step names come from the **raw** command only.
- Maestro `WARNED` → step status `skipped` with error exactly `optional command did not succeed`.
- Owned Maestro flags `--format` `--output` `--debug-output` `--flatten-debug-output`: refuse with exit `2`, never override.
- Exit codes: `2` usage error or owned flag; `127` maestro not found; `1` maestro could not start or the report could not be written; otherwise **Maestro's own exit code**.
- Report file `<outputDir>/qualflare-maestro-<pid>-<runID>.json`; screenshots copied to `<outputDir>/attachments/<runID>-<n>.png` and referenced by `localImagePath` relative to the report.
- Redaction replaces known variable values of **4 or more characters** with `${KEY}`; IDs, flow names and paths are never rewritten.
- The reporter makes **no network calls**.
- Tests use the committed captures in `test/captures/` for anything Maestro produced. Synthetic input is allowed only for cases the captures cannot show (step cap, duplicate names, malformed files, redaction).
- Commit messages use conventional prefixes and carry **no** Claude/Anthropic attribution or `Co-Authored-By`/`Claude-Session` trailers.

## File map

| File | Responsibility | Task |
|---|---|---|
| `go.mod`, `LICENSE`, `.gitignore` | module and repo basics | 1 |
| `internal/wire`, `constants`, `textutil`, `version`, `gitdetect`, `cidetect` | ported verbatim from `qualflare-go` | 1 |
| `internal/gitdetect/reporoot.go` | git repository root | 1 |
| `internal/config/config.go` | flags → env → CI → git → default | 2 |
| `internal/redact/redact.go` | replace variable values with `${KEY}` | 3 |
| `internal/args/args.go` | split argv, validate, inject owned flags, `--env` values | 4 |
| `internal/runner/runner.go` | run maestro, tee output, keep tails, forward signals | 5 |
| `internal/junit/junit.go` | read Maestro's JUnit report | 6 |
| `internal/debugdir/debugdir.go` | detect layout, read commands and screenshots | 7 |
| `internal/steps/steps.go` | entries → steps, rendering, nesting | 8 |
| `internal/build/build.go` | assemble the Collect report | 9 |
| `cmd/qualflare-maestro/main.go` | the binary | 10 |
| `test/integration/…` | real Maestro on a simulator | 11 |
| `e2e/…`, `.github/workflows/ci.yml`, `e2e.yml` | dogfood and CI | 12 |
| `.goreleaser.yml`, `release.yml`, docs | release and documentation | 13 |

Captures used throughout (already committed):

- `test/captures/maestro-2.6.1-ios26.5/` — flat layout: `report.xml`, `commands-(Settings opens).json`, `commands-(Settings fails on purpose).json`, one `screenshot-❌-…-(Settings fails on purpose).png`.
- `test/captures/maestro-2.10.0-ios26.5/` — bundle layout: `report.xml`, `debug/<flow>/commands.json`, `debug/<flow>/screenshots/*.png` for `Settings opens`, `Settings fails on purpose`, `Settings nested commands`.

---

### Task 1: Module scaffold and ported packages

**Files:**
- Create: `go.mod`, `.gitignore`, `LICENSE` (copied)
- Create (copied): `internal/{wire,constants,textutil,version,gitdetect,cidetect}/*.go`
- Create: `internal/gitdetect/reporoot.go`, `internal/gitdetect/reporoot_test.go`

**Interfaces:**
- Produces (ported, unchanged API): `wire.Collect`, `wire.Suite`, `wire.Case`, `wire.Step`, `wire.Attachment`, `wire.Label`, `wire.Link`, `wire.Metadata`, `wire.NewSuite(name, category string) Suite`, `wire.IntPtr`, `wire.StringPtr`; `constants.MaxStepsPerTestAttempt` (300), `MaxAttachmentsPerCase` (50), `MaxTagsPerCase` (64), `MaxTagLength` (255), `MaxLabelsPerCase` (100), `MaxLinksPerCase` (20), `MaxCaseErrorRunes` (65536), `MaxAttemptMessageRunes` (8192); `textutil.Truncate(s string, maxRunes int) string`, `textutil.TruncateOr(s string, maxRunes int, alt string) string`; `version.String() string`, `version.Full() string`; `gitdetect.Detect() Info`, `gitdetect.SetRunnerForTest(t, fn)`; `cidetect.Detect() Info`.
- Produces (new): `gitdetect.RepoRoot() string`.

- [ ] **Step 1: Create the module and copy the shared packages**

Run from `/Users/ibrahim/Astrais/frameworks/qualflare-maestro`:

```bash
cat > go.mod <<'EOF'
module github.com/Qualflare/qualflare-maestro

go 1.21
EOF

Q=../qualflare-go
cp "$Q/LICENSE" LICENSE
for p in wire constants textutil version gitdetect cidetect; do
  mkdir -p "internal/$p"
  cp "$Q/internal/$p/"*.go "internal/$p/"
done
grep -rl 'github.com/Qualflare/qualflare-go' internal | xargs sed -i '' 's#github.com/Qualflare/qualflare-go#github.com/Qualflare/qualflare-maestro#g'

cat > .gitignore <<'EOF'
qualflare-results/
e2e/e2e-results/
/qualflare-maestro
dist/
EOF
```

The copied tests keep `"golang"` and `Qualflare/qualflare-go` as test data. They assert wire behaviour, not Go-specific values, so leave them as they are.

- [ ] **Step 2: Confirm the copy builds and its tests pass**

Run: `go vet ./... && go test ./...`
Expected: PASS for `internal/cidetect`, `constants`, `gitdetect`, `textutil`, `version`, `wire`.

- [ ] **Step 3: Write the failing test for `RepoRoot`**

`internal/gitdetect/reporoot_test.go`:

```go
package gitdetect

import (
	"errors"
	"strings"
	"testing"
)

func TestRepoRoot_ReturnsTheWorkingTreeGitReports(t *testing.T) {
	SetRunnerForTest(t, func(args ...string) (string, error) {
		if got := strings.Join(args, " "); got != "rev-parse --show-toplevel" {
			t.Fatalf("unexpected git call %q", got)
		}
		return "/work/app\n", nil
	})
	if got := RepoRoot(); got != "/work/app" {
		t.Fatalf("RepoRoot() = %q, want /work/app", got)
	}
}

func TestRepoRoot_IsEmptyOutsideARepository(t *testing.T) {
	SetRunnerForTest(t, func(args ...string) (string, error) {
		return "", errors.New("fatal: not a git repository")
	})
	if got := RepoRoot(); got != "" {
		t.Fatalf("RepoRoot() = %q, want empty", got)
	}
}
```

- [ ] **Step 4: Run it to see it fail**

Run: `go test ./internal/gitdetect/ -run RepoRoot`
Expected: FAIL — `undefined: RepoRoot`.

- [ ] **Step 5: Implement `RepoRoot`**

`internal/gitdetect/reporoot.go`:

```go
package gitdetect

// RepoRoot returns the top of the git working tree containing the current
// directory, or "" outside a repository.
//
// Maestro writes each flow's path relative to whatever directory it ran in.
// Case ids are built from paths relative to this root instead, so running the
// same flows from a different directory does not give them new ids and split
// their history.
func RepoRoot() string {
	return run("rev-parse", "--show-toplevel")
}
```

- [ ] **Step 6: Run the package tests**

Run: `go test ./internal/gitdetect/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod .gitignore LICENSE internal
git commit -m "chore: scaffold the module and port shared packages from qualflare-go"
```

---

### Task 2: Configuration

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `cidetect.Detect() cidetect.Info` (fields `Provider, BuildNumber, RunURL string; PRNumber *int; Branch, Commit, RunID string`), `gitdetect.Detect() gitdetect.Info` (`Branch, Commit string`).
- Produces:
  ```go
  const DefaultOutputDir = "qualflare-results"
  type Config struct {
      OutputDir, Environment, Language string
      Platform   string // "" means: detect from the device
      Milestone  *int
      Branch, Commit, RunID string
      ShardIndex *int
      Enabled    bool
      MaestroBin string
      CIProvider, CIBuildNumber, CIRunURL string
      CIPRNumber *int
  }
  type Flags struct {
      OutputDir, Environment, Language, Platform, Milestone,
      Branch, Commit, RunID, ShardIndex, Enabled string
  }
  func Resolve(f Flags, newUUID func() string) Config
  ```

- [ ] **Step 1: Write the failing tests**

`internal/config/config_test.go`:

```go
package config

import (
	"errors"
	"testing"

	"github.com/Qualflare/qualflare-maestro/internal/gitdetect"
)

var errNoGit = errors.New("no git")

// Every variable detection reads. Tests clear them so a CI runner's own
// environment cannot decide the result.
var ciVars = []string{
	"BUILD_NUMBER", "BUILD_TAG", "BUILD_URL", "CI_COMMIT_REF_NAME", "CI_COMMIT_SHA",
	"CI_MERGE_REQUEST_IID", "CI_PIPELINE_ID", "CI_PIPELINE_IID", "CI_PIPELINE_URL",
	"CIRCLE_BRANCH", "CIRCLE_BUILD_NUM", "CIRCLE_BUILD_URL", "CIRCLE_WORKFLOW_ID", "CIRCLECI",
	"GIT_BRANCH", "GIT_COMMIT", "GITHUB_ACTIONS", "GITHUB_HEAD_REF", "GITHUB_REF",
	"GITHUB_REF_NAME", "GITHUB_REPOSITORY", "GITHUB_RUN_ATTEMPT", "GITHUB_RUN_ID",
	"GITHUB_RUN_NUMBER", "GITHUB_SHA", "GITLAB_CI", "JENKINS_URL",
	"QUALFLARE_OUTPUT_DIR", "QUALFLARE_ENVIRONMENT", "QUALFLARE_LANGUAGE", "QUALFLARE_PLATFORM",
	"QUALFLARE_MILESTONE", "QUALFLARE_BRANCH", "QUALFLARE_COMMIT", "QUALFLARE_RUN_ID",
	"QUALFLARE_SHARD_INDEX", "QUALFLARE_ENABLED", "QUALFLARE_MAESTRO_BIN",
}

func clean(t *testing.T) {
	t.Helper()
	for _, name := range ciVars {
		t.Setenv(name, "")
	}
	gitdetect.SetRunnerForTest(t, func(args ...string) (string, error) { return "", errNoGit })
}

func fixedID() string { return "generated-id" }

func TestDefaults(t *testing.T) {
	clean(t)
	c := Resolve(Flags{}, fixedID)
	checks := map[string][2]string{
		"OutputDir":   {c.OutputDir, "qualflare-results"},
		"Environment": {c.Environment, "development"},
		"Language":    {c.Language, "en-US"},
		"Platform":    {c.Platform, ""},
		"MaestroBin":  {c.MaestroBin, "maestro"},
		"RunID":       {c.RunID, "generated-id"},
		"Branch":      {c.Branch, ""},
		"Commit":      {c.Commit, ""},
	}
	for field, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", field, v[0], v[1])
		}
	}
	if !c.Enabled {
		t.Error("Enabled = false, want true by default")
	}
}

func TestFlagBeatsEnvBeatsDefault(t *testing.T) {
	clean(t)
	t.Setenv("QUALFLARE_OUTPUT_DIR", "from-env")
	if got := Resolve(Flags{}, fixedID).OutputDir; got != "from-env" {
		t.Errorf("env: OutputDir = %q", got)
	}
	if got := Resolve(Flags{OutputDir: "from-flag"}, fixedID).OutputDir; got != "from-flag" {
		t.Errorf("flag: OutputDir = %q", got)
	}
}

func TestPlatformIsLeftForDetectionUnlessGiven(t *testing.T) {
	clean(t)
	t.Setenv("QUALFLARE_PLATFORM", "android")
	if got := Resolve(Flags{}, fixedID).Platform; got != "android" {
		t.Errorf("Platform = %q, want android", got)
	}
}

func TestMaestroBinFromEnv(t *testing.T) {
	clean(t)
	t.Setenv("QUALFLARE_MAESTRO_BIN", "/opt/maestro/bin/maestro")
	if got := Resolve(Flags{}, fixedID).MaestroBin; got != "/opt/maestro/bin/maestro" {
		t.Errorf("MaestroBin = %q", got)
	}
}

func TestEnabledSpellings(t *testing.T) {
	clean(t)
	for raw, want := range map[string]bool{"true": true, "1": true, "false": false, "0": false, "no": false} {
		if got := Resolve(Flags{Enabled: raw}, fixedID).Enabled; got != want {
			t.Errorf("Enabled(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestCIDetectionFillsBranchCommitAndRunID(t *testing.T) {
	clean(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REF_NAME", "main")
	t.Setenv("GITHUB_SHA", "abc123")
	t.Setenv("GITHUB_RUN_ID", "99")
	c := Resolve(Flags{}, fixedID)
	if c.Branch != "main" || c.Commit != "abc123" {
		t.Errorf("branch/commit = %q/%q", c.Branch, c.Commit)
	}
	if c.RunID == "" || c.RunID == "generated-id" {
		t.Errorf("RunID = %q, want one derived from the CI run", c.RunID)
	}
}

func TestUnparseableNumbersDegradeToUnset(t *testing.T) {
	clean(t)
	if got := Resolve(Flags{ShardIndex: "x"}, fixedID).ShardIndex; got != nil {
		t.Errorf("ShardIndex = %v, want nil", *got)
	}
	if got := Resolve(Flags{ShardIndex: "0"}, fixedID).ShardIndex; got == nil || *got != 0 {
		t.Error("ShardIndex 0 must be kept, not dropped")
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/config/`
Expected: FAIL — `undefined: Resolve` (and the other identifiers).

- [ ] **Step 3: Implement**

`internal/config/config.go`:

```go
// Package config resolves the reporter's configuration.
//
// Precedence, highest first: command-line flag -> QUALFLARE_* environment ->
// CI detection -> git -> a default. The names match qualflare-go so one
// configuration table covers the reporter family.
//
// There is deliberately no token option: this binary makes no network calls,
// and `qf login` holds the credential.
package config

import (
	"os"
	"strconv"

	"github.com/Qualflare/qualflare-maestro/internal/cidetect"
	"github.com/Qualflare/qualflare-maestro/internal/gitdetect"
)

const DefaultOutputDir = "qualflare-results"

// Config is the resolved configuration for one run.
type Config struct {
	OutputDir   string
	Environment string
	Language    string
	// Platform is "" unless the user set one; the report then detects it from
	// the device Maestro ran on.
	Platform   string
	Milestone  *int
	Branch     string
	Commit     string
	RunID      string
	ShardIndex *int
	Enabled    bool
	MaestroBin string

	CIProvider    string
	CIBuildNumber string
	CIRunURL      string
	CIPRNumber    *int
}

// Flags carries command-line values. An empty string means "not given" and
// falls through to the next tier.
type Flags struct {
	OutputDir   string
	Environment string
	Language    string
	Platform    string
	Milestone   string
	Branch      string
	Commit      string
	RunID       string
	ShardIndex  string
	Enabled     string
}

// Resolve applies the precedence chain. newUUID supplies a fallback run id and
// is a parameter so tests are deterministic.
func Resolve(f Flags, newUUID func() string) Config {
	ci := cidetect.Detect()

	// git shells out, so it is asked only when nothing better has an answer.
	var git gitdetect.Info
	if f.Branch == "" && env("QUALFLARE_BRANCH") == "" && ci.Branch == "" {
		git = gitdetect.Detect()
	}
	if f.Commit == "" && env("QUALFLARE_COMMIT") == "" && ci.Commit == "" && git.Commit == "" {
		git.Commit = gitdetect.Detect().Commit
	}

	c := Config{
		OutputDir:   pick(f.OutputDir, env("QUALFLARE_OUTPUT_DIR"), DefaultOutputDir),
		Environment: pick(f.Environment, env("QUALFLARE_ENVIRONMENT"), "development"),
		Language:    pick(f.Language, env("QUALFLARE_LANGUAGE"), "en-US"),
		Platform:    pick(f.Platform, env("QUALFLARE_PLATFORM")),
		Branch:      pick(f.Branch, env("QUALFLARE_BRANCH"), ci.Branch, git.Branch),
		Commit:      pick(f.Commit, env("QUALFLARE_COMMIT"), ci.Commit, git.Commit),
		Enabled:     boolOr(pick(f.Enabled, env("QUALFLARE_ENABLED")), true),
		MaestroBin:  pick(env("QUALFLARE_MAESTRO_BIN"), "maestro"),

		CIProvider:    ci.Provider,
		CIBuildNumber: ci.BuildNumber,
		CIRunURL:      ci.RunURL,
		CIPRNumber:    ci.PRNumber,
	}

	// One run id per launch, shared by every shard so `qf collect` can group
	// the files. In CI it comes from the build; locally it is random per run.
	c.RunID = pick(f.RunID, env("QUALFLARE_RUN_ID"), ci.RunID)
	if c.RunID == "" {
		c.RunID = newUUID()
	}

	// A typo must not fail the run: an unparseable number is simply unset.
	c.Milestone = intOrNil(pick(f.Milestone, env("QUALFLARE_MILESTONE")))
	c.ShardIndex = intOrNil(pick(f.ShardIndex, env("QUALFLARE_SHARD_INDEX")))
	return c
}

func env(name string) string { return os.Getenv(name) }

func pick(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func boolOr(raw string, def bool) bool {
	switch raw {
	case "":
		return def
	case "1", "true", "yes", "on", "TRUE", "True", "On", "Yes":
		return true
	default:
		return false
	}
}

func intOrNil(raw string) *int {
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &n
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat(config): resolve flags, environment, CI and git in the family's precedence"
```

---

### Task 3: Redaction

**Files:**
- Create: `internal/redact/redact.go`
- Test: `internal/redact/redact_test.go`

**Interfaces:**
- Produces:
  ```go
  const MinLength = 4
  type Pair struct{ Key, Value string }
  type Redactor struct{ /* unexported */ }
  func New(pairs []Pair) *Redactor
  func (r *Redactor) String(s string) string // nil-safe
  func FromEnviron(environ []string) []Pair  // MAESTRO_* entries only
  ```

- [ ] **Step 1: Write the failing tests**

`internal/redact/redact_test.go`:

```go
package redact

import (
	"reflect"
	"testing"
)

func TestReplacesValuesWithTheirVariableName(t *testing.T) {
	r := New([]Pair{{Key: "PASSWORD", Value: "hunter22"}})
	got := r.String(`Assertion is false: "hunter22" is visible`)
	if want := `Assertion is false: "${PASSWORD}" is visible`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLongerValuesAreReplacedFirst(t *testing.T) {
	// If "secret" went first, "secret-longer" would become "${A}-longer" and the
	// longer value would never be recognised.
	r := New([]Pair{{Key: "A", Value: "secret"}, {Key: "B", Value: "secret-longer"}})
	if got, want := r.String("secret-longer and secret"), "${B} and ${A}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShortValuesAreNotRedacted(t *testing.T) {
	// Replacing "1" or "on" everywhere would mangle the report.
	r := New([]Pair{{Key: "FLAG", Value: "on"}, {Key: "N", Value: "123"}})
	if got := r.String("on 123"); got != "on 123" {
		t.Fatalf("got %q", got)
	}
}

func TestANilRedactorChangesNothing(t *testing.T) {
	var r *Redactor
	if got := r.String("untouched"); got != "untouched" {
		t.Fatalf("got %q", got)
	}
}

func TestFromEnvironKeepsOnlyMaestroVariables(t *testing.T) {
	got := FromEnviron([]string{"HOME=/Users/x", "MAESTRO_TOKEN=abc=def", "MAESTRO_EMPTY=", "PATH=/bin"})
	want := []Pair{{Key: "MAESTRO_TOKEN", Value: "abc=def"}, {Key: "MAESTRO_EMPTY", Value: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/redact/`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Implement**

`internal/redact/redact.go`:

```go
// Package redact keeps variable values out of the report.
//
// Maestro evaluates variables before it reports an error, so a flow failing on
// `assertVisible: ${SECRET}` produces a failure message holding the real value.
// The reporter knows two sources of values -- `--env KEY=VALUE` arguments and
// MAESTRO_* variables in its own environment, which Maestro copies into every
// flow -- and replaces each occurrence with ${KEY}.
package redact

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// MinLength is the shortest value that is redacted. Shorter values such as "1"
// or "on" would match all over an unrelated report.
const MinLength = 4

// Pair is one variable and its value.
type Pair struct {
	Key   string
	Value string
}

// Redactor replaces known values. The zero value and nil redact nothing.
type Redactor struct {
	pairs []Pair
}

// New builds a redactor from variable pairs.
func New(pairs []Pair) *Redactor {
	r := &Redactor{}
	for _, p := range pairs {
		if utf8.RuneCountInString(p.Value) >= MinLength {
			r.pairs = append(r.pairs, p)
		}
	}
	// Longest value first, so a value containing another is replaced whole.
	sort.SliceStable(r.pairs, func(i, j int) bool {
		if len(r.pairs[i].Value) != len(r.pairs[j].Value) {
			return len(r.pairs[i].Value) > len(r.pairs[j].Value)
		}
		return r.pairs[i].Key < r.pairs[j].Key
	})
	return r
}

// String returns s with every known value replaced by ${KEY}.
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	for _, p := range r.pairs {
		s = strings.ReplaceAll(s, p.Value, "${"+p.Key+"}")
	}
	return s
}

// FromEnviron returns the MAESTRO_* entries of an os.Environ()-style list.
func FromEnviron(environ []string) []Pair {
	var out []Pair
	for _, kv := range environ {
		key, value, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(key, "MAESTRO_") {
			out = append(out, Pair{Key: key, Value: value})
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/redact/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/redact
git commit -m "feat(redact): replace --env and MAESTRO_* values with their names"
```

---

### Task 4: Maestro arguments

**Files:**
- Create: `internal/args/args.go`
- Test: `internal/args/args_test.go`

**Interfaces:**
- Consumes: `redact.Pair`.
- Produces:
  ```go
  var OwnedFlags = []string{"--format", "--output", "--debug-output", "--flatten-debug-output"}
  var ErrNothingToRun error
  type OwnedFlagError struct{ Flag string }
  type NotTestError struct{ Subcommand string }
  type Invocation struct {
      Argv     []string // Argv[0] is the maestro executable
      JUnit    string   // where --output points
      DebugDir string   // where --debug-output points
  }
  func Split(argv []string, valueFlags, boolFlags map[string]bool) (reporter, maestro []string)
  func Build(maestroArgs []string, defaultBin, workDir string) (Invocation, error)
  func Passthrough(maestroArgs []string, defaultBin string) []string
  func EnvValues(maestroArgs []string) []redact.Pair
  ```

- [ ] **Step 1: Write the failing tests**

`internal/args/args_test.go`:

```go
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
	want := []string{"maestro", "test",
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
		"long":      {{"maestro", "test", "--format", "html", "flows/"}, {"maestro", "test", "--format", "html", "flows/"}},
		"shorthand": {{"flows/"}, {"bin", "test", "flows/"}},
		"test":      {{"test", "flows/"}, {"bin", "test", "flows/"}},
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
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/args/`
Expected: FAIL — `undefined: Split`.

- [ ] **Step 3: Implement**

`internal/args/args.go`:

```go
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
	case len(maestroArgs) > 0 && filepath.Base(maestroArgs[0]) == "maestro":
		bin = maestroArgs[0]
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
	case filepath.Base(maestroArgs[0]) == "maestro":
		return append([]string{}, maestroArgs...)
	case maestroArgs[0] == "test":
		return append([]string{defaultBin}, maestroArgs...)
	default:
		return append([]string{defaultBin, "test"}, maestroArgs...)
	}
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
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/args/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/args
git commit -m "feat(args): split the command line, refuse owned flags, inject output flags"
```

---

### Task 5: Running Maestro

**Files:**
- Create: `internal/runner/runner.go`
- Test: `internal/runner/runner_test.go` (build tag `!windows`)

**Interfaces:**
- Produces:
  ```go
  type Result struct {
      ExitCode    int
      StdoutTail  string
      StderrTail  string
      Interrupted bool // SIGINT or SIGTERM reached the reporter while maestro ran
  }
  const TailBytes = 64 << 10
  func Run(argv []string, stdout, stderr io.Writer) (Result, error) // error only if maestro could not start
  ```

- [ ] **Step 1: Write the failing tests**

`internal/runner/runner_test.go`:

```go
//go:build !windows

package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRun_PassesTheExitCodeThrough(t *testing.T) {
	res, err := Run([]string{"sh", "-c", "exit 3"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", res.ExitCode)
	}
}

func TestRun_EchoesOutputAndKeepsItsTail(t *testing.T) {
	var out, errOut bytes.Buffer
	res, err := Run([]string{"sh", "-c", "echo to-stdout; echo to-stderr 1>&2"}, &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "to-stdout\n" || errOut.String() != "to-stderr\n" {
		t.Fatalf("echoed %q / %q", out.String(), errOut.String())
	}
	if res.StdoutTail != "to-stdout\n" || res.StderrTail != "to-stderr\n" {
		t.Fatalf("tails %q / %q", res.StdoutTail, res.StderrTail)
	}
}

func TestRun_TailIsBoundedAndKeepsTheEnd(t *testing.T) {
	script := `i=0; while [ $i -lt 20000 ]; do echo line-$i 1>&2; i=$((i+1)); done; echo LAST 1>&2`
	res, err := Run([]string{"sh", "-c", script}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.StderrTail) > TailBytes {
		t.Fatalf("tail is %d bytes, cap is %d", len(res.StderrTail), TailBytes)
	}
	if !strings.HasSuffix(res.StderrTail, "LAST\n") {
		t.Fatalf("tail does not end with the last line: ...%q", res.StderrTail[len(res.StderrTail)-20:])
	}
}

func TestRun_AMissingProgramIsAnError(t *testing.T) {
	if _, err := Run([]string{"/nonexistent/maestro"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("want an error")
	}
}

func TestRun_ForwardsSIGTERMToMaestro(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	got := filepath.Join(dir, "got-term")
	script := `trap 'touch "` + got + `"; exit 42' TERM; touch "` + ready + `"; while true; do sleep 0.1; done`

	done := make(chan Result, 1)
	go func() {
		res, _ := Run([]string{"sh", "-c", script}, &bytes.Buffer{}, &bytes.Buffer{})
		done <- res
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Signal the test process itself: Run has registered for SIGTERM, so it is
	// caught and forwarded rather than killing the test binary.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-done:
		if res.ExitCode != 42 || !res.Interrupted {
			t.Fatalf("ExitCode = %d, Interrupted = %v; want 42, true", res.ExitCode, res.Interrupted)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("maestro stand-in did not exit after SIGTERM")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatal("the child never saw SIGTERM")
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/runner/`
Expected: FAIL — `undefined: Run`.

- [ ] **Step 3: Implement**

`internal/runner/runner.go`:

```go
// Package runner runs maestro, echoing its output and keeping the end of it.
package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
)

// TailBytes is how much of each stream is kept. The end of the output is what
// explains a run that died before writing a report.
const TailBytes = 64 << 10

// Result is what maestro did.
type Result struct {
	ExitCode    int
	StdoutTail  string
	StderrTail  string
	Interrupted bool
}

// Run starts argv with stdin inherited, copies its stdout and stderr to the
// given writers while keeping the last TailBytes of each, forwards SIGINT and
// SIGTERM to it, and waits. The error is non-nil only if maestro could not be
// started or waited for.
func Run(argv []string, stdout, stderr io.Writer) (Result, error) {
	if len(argv) == 0 {
		return Result{}, errors.New("runner: nothing to run")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	outTail := &tail{max: TailBytes}
	errTail := &tail{max: TailBytes}
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(stdout, outTail)
	cmd.Stderr = io.MultiWriter(stderr, errTail)

	// Registered before Start so no signal can slip past between the two.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	var interrupted atomic.Bool
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				interrupted.Store(true)
				// A terminal's Ctrl-C already reaches maestro through the process
				// group, but CI cancellation and `kill` reach only this process.
				// Forwarding lets maestro end its device session either way, and
				// the reporter stays alive long enough to write what happened.
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	waitErr := cmd.Wait()
	close(done)

	res := Result{
		StdoutTail:  outTail.String(),
		StderrTail:  errTail.String(),
		Interrupted: interrupted.Load(),
	}
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
	case errors.As(waitErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		if res.ExitCode < 0 { // killed by a signal
			res.ExitCode = 1
			if res.Interrupted {
				res.ExitCode = 130
			}
		}
	default:
		return res, waitErr
	}
	return res, nil
}

// tail keeps the last max bytes written to it.
type tail struct {
	max int
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/runner/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner
git commit -m "feat(runner): run maestro, keep the tail of its output, forward signals"
```

---

### Task 6: Maestro's JUnit report

**Files:**
- Create: `internal/junit/junit.go`
- Test: `internal/junit/junit_test.go`

**Interfaces:**
- Produces:
  ```go
  type Property struct{ Name, Value string }
  type Case struct {
      ID, Name, ClassName, File, Status, Timestamp string
      Seconds    float64
      Failure    string // text of <failure>, "" when absent
      Failed     bool   // a <failure> element was present
      Properties []Property
  }
  type Suite struct {
      Name, Device, Timestamp string
      Cases []Case
  }
  type Report struct{ Suites []Suite }
  func Parse(r io.Reader) (Report, error)
  func ParseFile(path string) (Report, error) // a missing file returns an error matching os.ErrNotExist
  ```

- [ ] **Step 1: Write the failing tests**

`internal/junit/junit_test.go`:

```go
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
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/junit/`
Expected: FAIL — `undefined: ParseFile`.

- [ ] **Step 3: Implement**

`internal/junit/junit.go`:

```go
// Package junit reads the JUnit XML `maestro test --format junit` writes.
package junit

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Property is a flow property, including the comma-joined `tags` Maestro adds.
type Property struct {
	Name  string
	Value string
}

// Case is one flow.
type Case struct {
	ID         string
	Name       string
	ClassName  string
	File       string // relative to the directory maestro ran in; empty before Maestro 2.6.0
	Status     string // SUCCESS, ERROR, ...
	Timestamp  string // e.g. 2026-09-16T19:26:50, no zone; empty on older Maestro
	Seconds    float64
	Failure    string
	Failed     bool
	Properties []Property
}

// Suite is one <testsuite>; --shard-all produces one per device.
type Suite struct {
	Name      string
	Device    string
	Timestamp string
	Cases     []Case
}

// Report is the whole file.
type Report struct {
	Suites []Suite
}

type xmlReport struct {
	XMLName xml.Name   `xml:"testsuites"`
	Suites  []xmlSuite `xml:"testsuite"`
}

type xmlSuite struct {
	Name      string    `xml:"name,attr"`
	Device    string    `xml:"device,attr"`
	Timestamp string    `xml:"timestamp,attr"`
	Cases     []xmlCase `xml:"testcase"`
}

type xmlCase struct {
	ID         string        `xml:"id,attr"`
	Name       string        `xml:"name,attr"`
	ClassName  string        `xml:"classname,attr"`
	File       string        `xml:"file,attr"`
	Time       string        `xml:"time,attr"`
	Timestamp  string        `xml:"timestamp,attr"`
	Status     string        `xml:"status,attr"`
	Properties []xmlProperty `xml:"properties>property"`
	Failure    *xmlFailure   `xml:"failure"`
}

type xmlProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type xmlFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// ParseFile reads a report from disk. A missing file returns the os error
// unwrapped so callers can tell "not written" from "unreadable".
func ParseFile(path string) (Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a report.
func Parse(r io.Reader) (Report, error) {
	var doc xmlReport
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return Report{}, fmt.Errorf("junit: %w", err)
	}
	var out Report
	for _, s := range doc.Suites {
		suite := Suite{Name: s.Name, Device: s.Device, Timestamp: s.Timestamp}
		for _, c := range s.Cases {
			jc := Case{
				ID: c.ID, Name: c.Name, ClassName: c.ClassName, File: c.File,
				Status: c.Status, Timestamp: c.Timestamp,
			}
			if secs, err := strconv.ParseFloat(strings.TrimSpace(c.Time), 64); err == nil {
				jc.Seconds = secs
			}
			if c.Failure != nil {
				jc.Failed = true
				jc.Failure = strings.TrimSpace(c.Failure.Text)
				if jc.Failure == "" {
					jc.Failure = c.Failure.Message
				}
			}
			for _, p := range c.Properties {
				jc.Properties = append(jc.Properties, Property{Name: p.Name, Value: p.Value})
			}
			suite.Cases = append(suite.Cases, jc)
		}
		out.Suites = append(out.Suites, suite)
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/junit/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/junit
git commit -m "feat(junit): read Maestro's JUnit report"
```

---

### Task 7: Reading the debug directory

**Files:**
- Create: `internal/debugdir/debugdir.go`
- Test: `internal/debugdir/debugdir_test.go`

**Interfaces:**
- Produces:
  ```go
  type Layout int
  const ( LayoutNone Layout = iota; LayoutFlat; LayoutBundle )
  func (l Layout) String() string
  type Screenshot struct { Path string; Failed bool } // Failed: flat-layout filename carries ❌
  type Entry struct {
      Kind         string          // union key, e.g. "tapOnElement"
      Raw          json.RawMessage // the raw command body; never the evaluated one
      Status       string
      TimestampMs  int64
      DurationMs   *int64
      ErrorMessage string
      Sequence     int
      Depth        int
      Screenshots  []string // bundle layout: existing screenshot files this entry lists
  }
  type Flow struct {
      Name        string       // applyConfigurationCommand config.name, else FileKey
      FileKey     string       // name Maestro used in the file or folder name
      Entries     []Entry      // sorted by Sequence
      Screenshots []Screenshot // flat: this flow's files; bundle: files in screenshots/ no entry lists
  }
  type Result struct { Layout Layout; Flows []Flow; MaestroLog string; Warnings []string }
  func Read(dir string) Result
  ```

- [ ] **Step 1: Write the failing tests**

`internal/debugdir/debugdir_test.go`:

```go
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
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/debugdir/`
Expected: FAIL — `undefined: Read`.

- [ ] **Step 3: Implement**

`internal/debugdir/debugdir.go`:

```go
// Package debugdir reads what `maestro test --debug-output X
// --flatten-debug-output` leaves in X. Two layouts exist, and the reader picks
// by what is on disk, never by Maestro version:
//
//	flat   (measured on 2.6.1):  X/commands-(<flow>).json, X/screenshot-<emoji>-<ms>-(<flow>).png
//	bundle (measured on 2.10.0): X/<flow>/commands.json,   X/<flow>/screenshots/*.png
//
// Only what the report needs is decoded. evaluatedCommand and the error's
// hierarchyRoot are never loaded into a struct field, so they cannot leak.
package debugdir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Layout is how Maestro arranged its debug output.
type Layout int

const (
	LayoutNone Layout = iota
	LayoutFlat
	LayoutBundle
)

func (l Layout) String() string {
	switch l {
	case LayoutFlat:
		return "flat"
	case LayoutBundle:
		return "bundle"
	}
	return "none"
}

// Screenshot is an image file belonging to a flow.
type Screenshot struct {
	Path   string
	Failed bool
}

// Entry is one executed command.
type Entry struct {
	Kind         string
	Raw          json.RawMessage
	Status       string
	TimestampMs  int64
	DurationMs   *int64
	ErrorMessage string
	Sequence     int
	Depth        int
	Screenshots  []string
}

// Flow is one flow's commands and screenshots.
type Flow struct {
	Name        string
	FileKey     string
	Entries     []Entry
	Screenshots []Screenshot
}

// Result is everything read from the directory.
type Result struct {
	Layout     Layout
	Flows      []Flow
	MaestroLog string
	Warnings   []string
}

type rawEntry struct {
	Command  map[string]json.RawMessage `json:"command"`
	Metadata struct {
		Status         string          `json:"status"`
		Timestamp      int64           `json:"timestamp"`
		Duration       *int64          `json:"duration"`
		SequenceNumber int             `json:"sequenceNumber"`
		Depth          int             `json:"depth"`
		Error          json.RawMessage `json:"error"`
		Artifacts      []struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"artifacts"`
	} `json:"metadata"`
}

// Read inspects dir. It never fails: anything unreadable becomes a warning, and
// the affected flow keeps its JUnit result without steps.
func Read(dir string) Result {
	var res Result
	if isFile(filepath.Join(dir, "maestro.log")) {
		res.MaestroLog = filepath.Join(dir, "maestro.log")
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return res
	}
	var bundles, flats []string
	for _, it := range items {
		name := it.Name()
		switch {
		case it.IsDir() && isFile(filepath.Join(dir, name, "commands.json")):
			bundles = append(bundles, name)
		case !it.IsDir() && strings.HasPrefix(name, "commands-") && strings.HasSuffix(name, ".json"):
			flats = append(flats, name)
		}
	}
	switch {
	case len(bundles) > 0:
		res.Layout = LayoutBundle
		for _, b := range bundles {
			readBundle(&res, filepath.Join(dir, b))
		}
	case len(flats) > 0:
		res.Layout = LayoutFlat
		for _, f := range flats {
			readFlat(&res, dir, f, items)
		}
	}
	return res
}

func readBundle(res *Result, flowDir string) {
	key := filepath.Base(flowDir)
	entries, name, err := readEntries(filepath.Join(flowDir, "commands.json"), flowDir)
	if err != nil {
		res.Warnings = append(res.Warnings, unreadable(filepath.Join(flowDir, "commands.json"), err))
		return
	}
	referenced := map[string]bool{}
	for i := range entries {
		for _, p := range entries[i].Screenshots {
			referenced[p] = true
		}
	}
	flow := Flow{Name: nameOr(name, key), FileKey: key, Entries: entries}
	if shots, err := os.ReadDir(filepath.Join(flowDir, "screenshots")); err == nil {
		for _, s := range shots {
			p := filepath.Join(flowDir, "screenshots", s.Name())
			if !s.IsDir() && strings.HasSuffix(s.Name(), ".png") && !referenced[p] {
				flow.Screenshots = append(flow.Screenshots, Screenshot{Path: p})
			}
		}
	}
	res.Flows = append(res.Flows, flow)
}

func readFlat(res *Result, dir, file string, items []os.DirEntry) {
	key := flatKey(file)
	entries, name, err := readEntries(filepath.Join(dir, file), "")
	if err != nil {
		res.Warnings = append(res.Warnings, unreadable(filepath.Join(dir, file), err))
		return
	}
	flow := Flow{Name: nameOr(name, key), FileKey: key, Entries: entries}
	suffix := "-(" + key + ").png"
	for _, it := range items {
		n := it.Name()
		if !it.IsDir() && strings.HasPrefix(n, "screenshot-") && strings.HasSuffix(n, suffix) {
			flow.Screenshots = append(flow.Screenshots, Screenshot{
				Path:   filepath.Join(dir, n),
				Failed: strings.Contains(n, "❌"),
			})
		}
	}
	res.Flows = append(res.Flows, flow)
}

// readEntries decodes a commands file. flowDir is set for the bundle layout,
// where artifact paths are relative to it; it is "" for the flat layout.
func readEntries(file, flowDir string) ([]Entry, string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, "", err
	}
	var raw []rawEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", err
	}
	var entries []Entry
	configName := ""
	for _, r := range raw {
		if len(r.Command) != 1 {
			continue
		}
		var kind string
		var body json.RawMessage
		for k, v := range r.Command {
			kind, body = k, v
		}
		e := Entry{
			Kind:        kind,
			Raw:         body,
			Status:      r.Metadata.Status,
			TimestampMs: r.Metadata.Timestamp,
			DurationMs:  r.Metadata.Duration,
			Sequence:    r.Metadata.SequenceNumber,
			Depth:       r.Metadata.Depth,
		}
		if len(r.Metadata.Error) > 0 {
			var em struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(r.Metadata.Error, &em) == nil {
				e.ErrorMessage = em.Message
			}
		}
		if flowDir != "" {
			for _, a := range r.Metadata.Artifacts {
				if a.Type != "SCREENSHOT" {
					continue
				}
				if p, ok := within(flowDir, a.Path); ok && isFile(p) {
					e.Screenshots = append(e.Screenshots, p)
				}
			}
		}
		if kind == "applyConfigurationCommand" && configName == "" {
			var cfg struct {
				Config struct {
					Name string `json:"name"`
				} `json:"config"`
			}
			if json.Unmarshal(body, &cfg) == nil {
				configName = cfg.Config.Name
			}
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
	return entries, configName, nil
}

// flatKey extracts <flow> from commands-(<flow>).json or
// commands-shard-<n>-(<flow>).json.
func flatKey(file string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(file, "commands-"), ".json")
	if i := strings.Index(s, "("); i >= 0 && strings.HasSuffix(s, ")") {
		return s[i+1 : len(s)-1]
	}
	return s
}

// within joins rel onto base and refuses a result outside base.
func within(base, rel string) (string, bool) {
	p := filepath.Join(base, filepath.FromSlash(rel))
	r, err := filepath.Rel(base, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

func nameOr(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func unreadable(file string, err error) string {
	return fmt.Sprintf("could not read %s (%v); that flow keeps its JUnit result but has no steps or screenshots", file, err)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/debugdir/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/debugdir
git commit -m "feat(debugdir): read Maestro's flat and bundle debug layouts"
```

---

### Task 8: Commands to steps

**Files:**
- Create: `internal/steps/steps.go`
- Test: `internal/steps/steps_test.go`

**Interfaces:**
- Consumes: `debugdir.Entry`, `debugdir.Read`, `wire.Step`, `wire.IntPtr`, `constants.MaxStepsPerTestAttempt`, `constants.MaxAttemptMessageRunes`, `textutil.Truncate`.
- Produces:
  ```go
  const WarnedMessage = "optional command did not succeed"
  var Dropped map[string]bool // defineVariablesCommand, applyConfigurationCommand
  type Result struct {
      Steps      []wire.Step
      StepOfShot map[string]int // screenshot path -> index in Steps
      Truncated  int            // entries left out by the step cap
  }
  func Convert(entries []debugdir.Entry, nested bool) Result
  func Render(kind string, raw json.RawMessage) string
  func Status(maestroStatus string) string
  ```

- [ ] **Step 1: Write the failing tests**

`internal/steps/steps_test.go`:

```go
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
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/steps/`
Expected: FAIL — `undefined: Render`.

- [ ] **Step 3: Implement**

`internal/steps/steps.go`:

```go
// Package steps turns Maestro command entries into report steps.
package steps

import (
	"encoding/json"
	"strings"

	"github.com/Qualflare/qualflare-maestro/internal/constants"
	"github.com/Qualflare/qualflare-maestro/internal/debugdir"
	"github.com/Qualflare/qualflare-maestro/internal/textutil"
	"github.com/Qualflare/qualflare-maestro/internal/wire"
)

// WarnedMessage is the error on a step whose optional command did not succeed.
const WarnedMessage = "optional command did not succeed"

// Dropped commands are Maestro bookkeeping, not steps anyone wrote. The first
// also holds real variable values in its raw form -- --env values and MAESTRO_*
// variables from the environment -- so it must never reach a report.
var Dropped = map[string]bool{
	"defineVariablesCommand":    true,
	"applyConfigurationCommand": true,
}

// Result is the converted steps.
type Result struct {
	Steps      []wire.Step
	StepOfShot map[string]int
	Truncated  int
}

// Convert builds steps from entries already sorted by sequence. nested is true
// for the bundle layout, whose entries carry a depth.
func Convert(entries []debugdir.Entry, nested bool) Result {
	res := Result{StepOfShot: map[string]int{}}
	dropDepth := -1
	var parents []int // parents[d] is the latest kept step at depth d
	for _, e := range entries {
		if dropDepth >= 0 {
			if e.Depth > dropDepth {
				continue
			}
			dropDepth = -1
		}
		if Dropped[e.Kind] {
			dropDepth = e.Depth
			continue
		}
		if len(res.Steps) >= constants.MaxStepsPerTestAttempt {
			res.Truncated++
			continue
		}

		step := wire.Step{
			Name:   Render(e.Kind, e.Raw),
			Status: Status(e.Status),
			Error:  textutil.Truncate(e.ErrorMessage, constants.MaxAttemptMessageRunes),
		}
		if e.DurationMs != nil && *e.DurationMs > 0 {
			step.Duration = *e.DurationMs * 1_000_000
		}
		if e.Status == "WARNED" {
			step.Error = WarnedMessage
		}

		idx := len(res.Steps)
		if nested {
			d := e.Depth
			if d < 0 {
				d = 0
			}
			if d > 0 && d <= len(parents) {
				step.ParentIndex = wire.IntPtr(parents[d-1])
			}
			if d <= len(parents) {
				parents = append(parents[:d], idx)
			}
		}
		res.Steps = append(res.Steps, step)
		for _, p := range e.Screenshots {
			res.StepOfShot[p] = idx
		}
	}
	return res
}

// Status maps a Maestro command status to a report status.
func Status(s string) string {
	switch s {
	case "COMPLETED":
		return "passed"
	case "FAILED":
		return "failed"
	case "RUNNING":
		return "aborted"
	}
	return "skipped" // WARNED, SKIPPED, PENDING, and anything Maestro adds later
}

// Render names a step from its raw command, in Maestro's YAML vocabulary. It
// never reads the evaluated command, so ${VARS} stay placeholders.
func Render(kind string, raw json.RawMessage) string {
	body := object(raw)
	name := render(kind, body)
	if boolean(body["optional"]) || boolean(object(body["selector"])["optional"]) {
		name += " (optional)"
	}
	return textutil.Truncate(name, 255)
}

func render(kind string, body map[string]json.RawMessage) string {
	switch kind {
	case "launchAppCommand":
		return join("launchApp", text(body["appId"]))
	case "assertConditionCommand":
		cond := object(body["condition"])
		if v, ok := cond["visible"]; ok {
			return join("assertVisible", selector(v))
		}
		if v, ok := cond["notVisible"]; ok {
			return join("assertNotVisible", selector(v))
		}
		return "assertCondition"
	case "tapOnElement":
		return join("tapOn", selector(body["selector"]))
	case "inputTextCommand":
		return join("inputText", text(body["text"]))
	case "repeatCommand":
		if t := text(body["times"]); t != "" {
			return "repeat: " + t + " times"
		}
		return "repeat"
	case "retryCommand":
		switch m := text(body["maxRetries"]); m {
		case "":
			return "retry"
		case "1":
			return "retry: up to 1 retry"
		default:
			return "retry: up to " + m + " retries"
		}
	case "runFlowCommand":
		return join("runFlow", text(body["sourceDescription"]))
	}
	// A command not listed above is named without its arguments: they might
	// hold a value that has no business in a report.
	return strings.TrimSuffix(kind, "Command")
}

func selector(raw json.RawMessage) string {
	s := object(raw)
	if v := text(s["textRegex"]); v != "" {
		return v
	}
	if v := text(s["idRegex"]); v != "" {
		return "id: " + v
	}
	return ""
}

func join(name, arg string) string {
	if arg == "" {
		return name
	}
	return name + ": " + arg
}

func object(raw json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	return m
}

func text(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func boolean(raw json.RawMessage) bool {
	var b bool
	return len(raw) > 0 && json.Unmarshal(raw, &b) == nil && b
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/steps/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/steps
git commit -m "feat(steps): name steps from raw commands and rebuild nesting from depth"
```

---

### Task 9: Assembling the report

**Files:**
- Create: `internal/build/build.go`
- Test: `internal/build/build_test.go`

**Interfaces:**
- Consumes: `config.Config`; `junit.Report`, `junit.Case`, `junit.Property`, `junit.ParseFile`; `debugdir.Result`, `debugdir.Flow`, `debugdir.Entry`, `debugdir.LayoutBundle`, `debugdir.Read`; `steps.Convert`, `steps.Result`, `steps.WarnedMessage`; `redact.New`, `redact.Pair`, `*redact.Redactor`; `wire.*`; `constants.*`; `textutil.*`.
- Produces:
  ```go
  const (Framework = "maestro"; CLIName = "qualflare-maestro"; Category = "e2e"; FallbackPlatform = "ios")
  type Input struct {
      Cfg         config.Config
      JUnit       *junit.Report // nil when the file was not written or could not be read
      Debug       debugdir.Result
      ExitCode    int
      Interrupted bool
      StderrTail  string
      LogTail     string // last lines of maestro.log
      WorkingDir  string // absolute; JUnit `file` is relative to it
      RepoRoot    string // absolute git root, "" outside a repository
      Redactor    *redact.Redactor
      Version     string
      Now         time.Time
  }
  type Copy struct{ From, To string } // To is relative to the output directory, slash-separated
  type Output struct {
      Report   wire.Collect
      Copies   []Copy
      Warnings []string
  }
  func Collect(in Input) Output
  func DropAttachment(c *wire.Collect, localImagePath string)
  func FileSafe(s string) string
  ```

- [ ] **Step 1: Write the failing tests**

`internal/build/build_test.go`:

```go
package build

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Qualflare/qualflare-maestro/internal/config"
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
		"iPhone 17 - iOS 26.5 - 089E029C":         "ios",
		"Pixel 7 - Android 14 - emulator-5554":    "android",
		"emulator-5554":                           "android",
		"Chromium Desktop Browser":                "web",
		"Studio Display":                          "",
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
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/build/`
Expected: FAIL — `undefined: Collect`.

- [ ] **Step 3: Implement**

`internal/build/build.go`:

```go
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
		for _, js := range in.JUnit.Suites {
			s := wire.NewSuite(suiteName(js, multi), Category)
			for _, jc := range js.Cases {
				wc := b.buildCase(jc, osFromDevice(js.Device), multi, flows, ambiguous)
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

func suiteName(js junit.Suite, multi bool) string {
	name := js.Name
	if name == "" {
		name = "Maestro"
	}
	if multi {
		name += " (" + osFromDevice(js.Device) + ")"
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

func (b *builder) buildCase(jc junit.Case, osName string, multi bool, flows map[string]*debugdir.Flow, ambiguous map[string]bool) wire.Case {
	r := b.in.Redactor
	path := b.flowPath(jc.File)
	id := jc.Name
	if path != "" {
		id = path + "#" + jc.Name
	}
	if multi {
		id += "@" + osName
	}
	wc := wire.Case{
		ID:         id,
		Name:       textutil.TruncateOr(jc.Name, 255, "(unnamed flow)"),
		ClassName:  textutil.Truncate(path, 255),
		Status:     caseStatus(jc),
		Error:      textutil.Truncate(r.String(jc.Failure), constants.MaxCaseErrorRunes),
		Duration:   int64(math.Round(jc.Seconds * 1e9)),
		ShardIndex: b.in.Cfg.ShardIndex,
	}
	b.applyProperties(&wc, jc.Properties)

	if f, ok := flows[jc.Name]; ok && !ambiguous[jc.Name] {
		conv := steps.Convert(f.Entries, b.in.Debug.Layout == debugdir.LayoutBundle)
		for i := range conv.Steps {
			conv.Steps[i].Name = r.String(conv.Steps[i].Name)
			conv.Steps[i].Error = r.String(conv.Steps[i].Error)
		}
		wc.Steps = conv.Steps
		if conv.Truncated > 0 {
			b.warn("flow %q has more than %d steps; the last %d were left out", jc.Name, constants.MaxStepsPerTestAttempt, conv.Truncated)
		}
		if d, start, ok := span(f.Entries); ok {
			wc.Duration, wc.StartedAt = d, start
		}
		wc.Attachments = b.attachments(f, conv)
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
		rel := fmt.Sprintf("attachments/%s-%d.png", FileSafe(b.in.Cfg.RunID), b.shots)
		b.out.Copies = append(b.out.Copies, Copy{From: path, To: rel})
		out = append(out, wire.Attachment{
			Name:           textutil.Truncate(filepath.Base(path), 255),
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
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/build/`
Expected: PASS.

- [ ] **Step 5: Run everything so far**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: all packages PASS. (`gofmt -w` first: hand-aligned literals in the tests above are normalised rather than reported as a failure.)

- [ ] **Step 6: Commit**

```bash
git add internal/build
git commit -m "feat(build): assemble the native report from JUnit and debug output"
```

---

### Task 10: The binary

**Files:**
- Create: `cmd/qualflare-maestro/main.go`
- Test: `cmd/qualflare-maestro/main_test.go` (build tag `!windows`)

**Interfaces:**
- Consumes: `args.Split`, `args.Build`, `args.Passthrough`, `args.EnvValues`, `args.ErrNothingToRun`; `config.Resolve`, `config.Flags`; `runner.Run`; `junit.ParseFile`; `debugdir.Read`; `gitdetect.RepoRoot`; `redact.New`, `redact.FromEnviron`; `build.Collect`, `build.Input`, `build.DropAttachment`, `build.FileSafe`; `version.String`, `version.Full`; `wire.Collect`.
- Produces: `func run(argv []string, stdout, stderr io.Writer) int` (the whole binary minus `os.Exit`).

- [ ] **Step 1: Write the failing tests**

The tests stand in for Maestro with a shell script named `maestro` that copies a committed capture into the paths it is given, so the whole binary runs end to end without a device.

`cmd/qualflare-maestro/main_test.go`:

```go
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
```

Why the last test needs code of its own: `args.Split` stops at the first token it does not recognise, so on its own a mistyped reporter flag such as `-nope=1` would be treated as the start of Maestro's arguments and the flows would run unreported. `run` therefore rejects any unknown flag that appears before a `--` (`unknownBeforeDoubleDash` in Step 3).

- [ ] **Step 2: Run to see it fail**

Run: `go test ./cmd/qualflare-maestro/`
Expected: FAIL — `undefined: run`.

- [ ] **Step 3: Implement**

`cmd/qualflare-maestro/main.go`:

```go
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
		return passthrough(maestroArgs, cfg.MaestroBin, stdout, errOut)
	}

	workDir := filepath.Join(cfg.OutputDir, ".work-"+build.FileSafe(cfg.RunID)+"-"+strconv.Itoa(os.Getpid()))
	inv, err := args.Build(maestroArgs, cfg.MaestroBin, workDir)
	if err != nil {
		fmt.Fprintf(errOut, "%s %v\n", prefix, err)
		return 2
	}
	if _, err := exec.LookPath(inv.Argv[0]); err != nil {
		fmt.Fprintf(errOut, "%s cannot run %q: install Maestro (https://docs.maestro.dev) or point QUALFLARE_MAESTRO_BIN at it\n", prefix, inv.Argv[0])
		return 127
	}
	if err := os.MkdirAll(inv.DebugDir, 0o755); err != nil {
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

	path, err := writeReport(out.Report, cfg)
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

func writeReport(c wire.Collect, cfg config.Config) (string, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return "", err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	path := filepath.Join(cfg.OutputDir, fmt.Sprintf("qualflare-maestro-%d-%s.json", os.Getpid(), build.FileSafe(cfg.RunID)))
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
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	const maxBytes = 256 << 10
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
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
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./cmd/qualflare-maestro/`
Expected: PASS.

- [ ] **Step 5: Try it by hand against the bundle capture**

```bash
go build -o /tmp/qfm ./cmd/qualflare-maestro
/tmp/qfm -version
/tmp/qfm -- maestro test --output x.xml flows/; echo "exit=$?"
```
Expected: `qualflare-maestro devel`, then the `--output` refusal and `exit=2`.

- [ ] **Step 6: Commit**

```bash
git add cmd
git commit -m "feat: the qualflare-maestro binary"
```

---

### Task 11: Real Maestro on a simulator

These tests are the only ones that run real Maestro, so they are what catches a Maestro release changing its output. They are excluded from `go test ./...` by a build tag and need macOS with Maestro installed.

**Files:**
- Create: `test/integration/flows/config.yaml`, `passes.yaml`, `fails.yaml`, `optional.yaml`, `nested.yaml`, `secret.yaml`
- Create: `test/integration/badyaml/broken.yaml`
- Create: `test/integration/pick-simulator.py`
- Create: `test/integration/integration_test.go` (build tag `integration`)

**Interfaces:**
- Consumes: the built binary (`cmd/qualflare-maestro`), `wire.Collect`, `steps.WarnedMessage`.
- Produces: nothing other tasks import.

- [ ] **Step 1: Write the flows**

`test/integration/flows/config.yaml`:
```yaml
flows:
  - "*"
```

`test/integration/flows/passes.yaml`:
```yaml
appId: com.apple.Preferences
name: Passes
tags:
  - integration
properties:
  qualflare.priority: critical
  qualflare.link.issue: https://example.com/IT-1
  qualflare.label.team: mobile
---
- launchApp
- assertVisible: General
```

`test/integration/flows/fails.yaml`:
```yaml
appId: com.apple.Preferences
name: Fails
---
- launchApp
- assertVisible: "No Such Row 98765"
```

`test/integration/flows/optional.yaml`:
```yaml
appId: com.apple.Preferences
name: Optional miss
---
- launchApp
- tapOn:
    text: "No Such Row 12345"
    optional: true
- assertVisible: General
```

`test/integration/flows/nested.yaml`:
```yaml
appId: com.apple.Preferences
name: Nested
---
- launchApp
- repeat:
    times: 2
    commands:
      - assertVisible: General
- retry:
    maxRetries: 1
    commands:
      - assertVisible: General
- runFlow:
    commands:
      - assertVisible: General
```

`test/integration/flows/secret.yaml` — fails on purpose on a selector built from a variable, so Maestro's evaluated failure message contains the value:
```yaml
appId: com.apple.Preferences
name: Secret selector
---
- launchApp
- assertVisible: ${IT_SECRET}
```

`test/integration/badyaml/broken.yaml`:
```yaml
appId: com.apple.Preferences
---
- launchApp
- tapOn: [this is not closed
```

- [ ] **Step 2: Write the simulator picker**

`test/integration/pick-simulator.py`:
```python
#!/usr/bin/env python3
"""Print the UDID of an available iPhone simulator on the newest iOS runtime."""
import json
import subprocess
import sys

devices = json.loads(subprocess.check_output(
    ["xcrun", "simctl", "list", "devices", "available", "-j"]))["devices"]
for runtime in sorted(devices, reverse=True):
    if "iOS" not in runtime:
        continue
    for device in devices[runtime]:
        if device["name"].startswith("iPhone"):
            print(device["udid"])
            sys.exit(0)
sys.exit("no available iPhone simulator")
```

- [ ] **Step 3: Write the tests**

`test/integration/integration_test.go`:

```go
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
		t.Errorf("exit = %d, want maestro's 1 (two flows fail on purpose)", res.exit)
	}
	for _, leak := range []string{secret, token, "MAESTRO_QF_IT_TOKEN", "hierarchyRoot", "debugMessage"} {
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
		"Nested": "passed", "Secret selector": "failed",
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

```

- [ ] **Step 4: Check it compiles without running**

Run: `go vet -tags integration ./test/integration/`
Expected: no output.

- [ ] **Step 5: Run the no-device tests (no simulator booted)**

```bash
xcrun simctl shutdown all
go test -tags integration ./test/integration/ -run 'TestInvalidYAML|TestNoDevice' -v -count=1
```
Expected: PASS for both.

If `TestNoDevice` instead shows Maestro starting a simulator by itself or waiting for input until the timeout, that is Maestro behaviour this reporter cannot change: delete `TestNoDevice`, delete its step in Task 12's `ci.yml`, and add the observed behaviour to `docs/LIMITATIONS.md` in Task 13.

- [ ] **Step 6: Run the flows on a booted simulator**

```bash
udid=$(python3 test/integration/pick-simulator.py)
xcrun simctl boot "$udid"; xcrun simctl bootstatus "$udid" -b
go test -tags integration ./test/integration/ -run TestFlows -v -count=1 -timeout 30m
xcrun simctl shutdown "$udid"
```
Expected: PASS, logging the Maestro version. Run it on the locally installed Maestro, and — if a second version is at hand, as with the 2.10.0 zip used for the captures — put that one first on `PATH` and run it again.

- [ ] **Step 7: Commit**

```bash
git add test/integration
git commit -m "test: run the reporter against real Maestro on an iOS simulator"
```

---

### Task 12: Dogfood suite and CI

**Files:**
- Create: `e2e/flows/config.yaml`, `settings-opens.yaml`, `nested-commands.yaml`, `optional-miss.yaml`
- Create: `e2e/verify/main.go`
- Create: `e2e/assert-launch-landed.py` (copied byte for byte)
- Create: `.github/workflows/ci.yml`, `.github/workflows/e2e.yml`

**Interfaces:**
- Consumes: `wire.Collect`, `steps.WarnedMessage`, the binary.
- Produces: nothing other tasks import.

**Before this can go green in GitHub:** the repository needs a `QF_TOKEN` secret and a public Qualflare project with the slug `qualflare-maestro`. Setting those up is the maintainer's job; the workflow fails loudly naming `QF_TOKEN` until they exist.

- [ ] **Step 1: Write the dogfood flows**

Every flow passes by construction; deliberate failures live only in `test/integration/`, which is never uploaded. Every assertion reads the label from `${DOGFOOD_LABEL}`, so the value (`General`) appearing anywhere in the report is a redaction failure the verifier can detect.

`e2e/flows/config.yaml`:
```yaml
flows:
  - "*"
```

`e2e/flows/settings-opens.yaml`:
```yaml
appId: com.apple.Preferences
name: Settings opens
tags:
  - dogfood
properties:
  qualflare.priority: high
  qualflare.description: Opens the iOS Settings app and checks a row is visible.
  qualflare.link.custom.repository: https://github.com/Qualflare/qualflare-maestro
  qualflare.label.surface: settings
---
- launchApp
- assertVisible: ${DOGFOOD_LABEL}
```

`e2e/flows/nested-commands.yaml`:
```yaml
appId: com.apple.Preferences
name: Nested commands
tags:
  - dogfood
---
- launchApp
- repeat:
    times: 2
    commands:
      - assertVisible: ${DOGFOOD_LABEL}
- retry:
    maxRetries: 1
    commands:
      - assertVisible: ${DOGFOOD_LABEL}
- runFlow:
    commands:
      - assertVisible: ${DOGFOOD_LABEL}
```

`e2e/flows/optional-miss.yaml`:
```yaml
appId: com.apple.Preferences
name: An optional command that misses
tags:
  - dogfood
---
- launchApp
- tapOn:
    text: "No Such Row 12345"
    optional: true
- assertVisible: ${DOGFOOD_LABEL}
```

- [ ] **Step 2: Copy the landing check**

```bash
mkdir -p e2e
cp ../qualflare-go/e2e/assert-launch-landed.py e2e/assert-launch-landed.py
cmp ../qualflare-go/e2e/assert-launch-landed.py e2e/assert-launch-landed.py && echo identical
```
Expected: `identical`. It must stay byte-identical across reporter repos.

- [ ] **Step 3: Write the verifier**

`e2e/verify/main.go`:

```go
// Command verify checks the dogfood report before it is uploaded.
//
// A report can be valid JSON and still wrong: the upload succeeds and nobody
// notices a missing step or a leaked value. This asserts the things the dogfood
// flows exist to demonstrate, against an explicit list rather than whatever the
// report happens to contain.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Qualflare/qualflare-maestro/internal/steps"
	"github.com/Qualflare/qualflare-maestro/internal/wire"
)

func main() { os.Exit(run()) }

func run() int {
	dir := os.Getenv("QUALFLARE_OUTPUT_DIR")
	if dir == "" {
		dir = "e2e-results"
	}
	files, _ := filepath.Glob(filepath.Join(dir, "qualflare-maestro-*.json"))
	if len(files) != 1 {
		fmt.Printf("want exactly one report in %s, found %v\n", dir, files)
		return 1
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		fmt.Println(err)
		return 1
	}
	var c wire.Collect
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Println("report is not valid JSON:", err)
		return 1
	}

	var failures []string
	check := func(ok bool, format string, a ...any) {
		if !ok {
			failures = append(failures, fmt.Sprintf(format, a...))
		}
	}

	check(c.Framework == "maestro", "framework = %q", c.Framework)
	check(c.Metadata.CLIName == "qualflare-maestro", "cliName = %q", c.Metadata.CLIName)
	check(strings.HasPrefix(c.Metadata.Version, "e2e-"), "version = %q; the workflow builds with -X version=e2e-<sha>, so anything else means the version is not reaching the report", c.Metadata.Version)
	check(c.Platform == "ios", "platform = %q", c.Platform)
	check(!bytes.Contains(raw, []byte("General")), "the value of DOGFOOD_LABEL reached the report")
	check(!bytes.Contains(raw, []byte("MAESTRO_")), "a MAESTRO_* variable name reached the report")

	cases := map[string]wire.Case{}
	for _, s := range c.Suites {
		for _, cs := range s.Cases {
			_, dup := cases[cs.Name]
			check(!dup, "case %q appears twice", cs.Name)
			cases[cs.Name] = cs
		}
	}
	want := []string{"Settings opens", "Nested commands", "An optional command that misses"}
	check(len(cases) == len(want), "got %d cases, want %d", len(cases), len(want))
	for _, name := range want {
		cs, ok := cases[name]
		check(ok, "no case %q", name)
		if ok {
			check(cs.Status == "passed", "%q is %q; every dogfood flow passes by construction", name, cs.Status)
			check(len(cs.Steps) > 0, "%q has no steps", name)
		}
		for _, a := range cs.Attachments {
			_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(a.LocalImagePath)))
			check(err == nil, "%q: attachment %s is not on disk", name, a.LocalImagePath)
		}
	}

	opens := cases["Settings opens"]
	check(opens.Priority == "high", "Settings opens priority = %q", opens.Priority)
	check(len(opens.Links) == 1 && opens.Links[0].Type == "custom" && opens.Links[0].Name == "repository", "Settings opens links = %+v", opens.Links)
	check(len(opens.Labels) == 1 && opens.Labels[0] == (wire.Label{Name: "surface", Value: "settings"}), "Settings opens labels = %+v", opens.Labels)
	check(hasStep(opens, func(s wire.Step) bool { return s.Name == "assertVisible: ${DOGFOOD_LABEL}" }), "no step named from the raw command")

	check(hasStep(cases["An optional command that misses"], func(s wire.Step) bool {
		return s.Status == "skipped" && s.Error == steps.WarnedMessage
	}), "the optional miss has no skipped step")

	if os.Getenv("EXPECT_NESTING") == "true" {
		check(hasStep(cases["Nested commands"], func(s wire.Step) bool { return s.ParentIndex != nil }), "Nested commands has no nested steps")
	}

	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Println("FAIL", f)
		}
		fmt.Printf("%d check(s) failed -- not uploading a report that misrepresents the suite\n", len(failures))
		return 1
	}
	fmt.Printf("all checks passed (%d cases)\n", len(cases))
	return 0
}

func hasStep(c wire.Case, match func(wire.Step) bool) bool {
	for _, s := range c.Steps {
		if match(s) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Check it builds**

Run: `go vet ./e2e/verify/`
Expected: no output.

- [ ] **Step 5: Write `ci.yml`**

`.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  unit:
    name: Lint, vet, unit test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: stable
          cache: true
      - name: gofmt
        run: |
          unformatted=$(gofmt -l $(git ls-files '*.go'))
          if [ -n "$unformatted" ]; then
            echo "::error::gofmt would rewrite:"; echo "$unformatted"
            exit 1
          fi
      - run: go vet ./... && go vet -tags integration ./test/integration/
      - run: go test -race ./...

  floor:
    name: Go 1.21 (module floor)
    needs: unit
    runs-on: ubuntu-latest
    env:
      # Without this a newer toolchain is downloaded silently and the floor is
      # never actually tested.
      GOTOOLCHAIN: local
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: '1.21'
      - run: go build ./... && go test ./...

  no-deps:
    name: Zero external dependencies
    needs: unit
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: stable
      - run: |
          modules=$(go list -m all | grep -v '^github.com/Qualflare/qualflare-maestro$' || true)
          if [ -n "$modules" ]; then
            echo "::error::unexpected module dependencies:"; echo "$modules"; exit 1
          fi

  maestro:
    name: Real Maestro ${{ matrix.maestro }} on an iOS simulator
    needs: unit
    runs-on: macos-15
    timeout-minutes: 60
    strategy:
      fail-fast: false
      matrix:
        # 2.6.1 writes the flat debug layout, latest the per-flow bundle. Both
        # must keep working; latest is also what catches Maestro's next change.
        maestro: ['2.6.1', 'latest']
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: stable
          cache: true
      - uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: '17'
      - name: Install Maestro
        run: |
          if [ "${{ matrix.maestro }}" != "latest" ]; then export MAESTRO_VERSION="${{ matrix.maestro }}"; fi
          curl -Ls "https://get.maestro.mobile.dev" | bash
          echo "$HOME/.maestro/bin" >> "$GITHUB_PATH"
      - run: maestro --version
      # Before any simulator boots: these cases need no device.
      - name: Invalid YAML and no device
        run: go test -tags integration ./test/integration/ -run 'TestInvalidYAML|TestNoDevice' -v -count=1 -timeout 30m
      - name: Boot an iPhone simulator
        run: |
          udid=$(python3 test/integration/pick-simulator.py)
          xcrun simctl boot "$udid"
          xcrun simctl bootstatus "$udid" -b
      - name: Flows against the Settings app
        run: go test -tags integration ./test/integration/ -run TestFlows -v -count=1 -timeout 50m
```

- [ ] **Step 6: Write `e2e.yml`**

`.github/workflows/e2e.yml`:

```yaml
# Dogfood: qualflare-maestro reports on flows against the iOS Settings app, and
# the PUBLISHED CLI uploads the report to a public Qualflare project.
#
# The CLI is deliberately unpinned: a CLI release that stops understanding this
# report should turn this red.
#
# The flows pass by construction. Failures, invalid YAML and missing devices are
# test/integration's job, which is never uploaded -- do not add a failing flow here.
name: E2E

on:
  push:
    branches: [main]
  workflow_dispatch:

concurrency:
  group: e2e-${{ github.ref }}
  # Every push to main should produce its own launch.
  cancel-in-progress: false

permissions:
  contents: read

jobs:
  e2e:
    name: Dogfood flows + upload to Qualflare
    runs-on: macos-15
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: stable
          cache: true
      - uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: '17'

      - name: Install Maestro (latest)
        run: |
          curl -Ls "https://get.maestro.mobile.dev" | bash
          echo "$HOME/.maestro/bin" >> "$GITHUB_PATH"

      - name: Build the reporter
        run: |
          go build -ldflags "-X github.com/Qualflare/qualflare-maestro/internal/version.Version=e2e-${GITHUB_SHA::7}" \
            -o "$RUNNER_TEMP/qualflare-maestro" ./cmd/qualflare-maestro

      - name: Boot an iPhone simulator
        run: |
          udid=$(python3 test/integration/pick-simulator.py)
          xcrun simctl boot "$udid"
          xcrun simctl bootstatus "$udid" -b

      # continue-on-error on the next two, gated at the end: a red run is the
      # one the dashboard most needs to receive.
      - name: Run the dogfood flows
        id: suite
        continue-on-error: true
        working-directory: e2e
        run: |
          "$RUNNER_TEMP/qualflare-maestro" -output-dir e2e-results -environment production \
            -- maestro test --env DOGFOOD_LABEL=General flows/

      - name: Verify the report matches what the flows declare
        id: verify
        continue-on-error: true
        working-directory: e2e
        env:
          QUALFLARE_OUTPUT_DIR: e2e-results
          EXPECT_NESTING: 'true'
        run: go run ./verify

      - name: Install the published CLI
        run: |
          npm install -g @qualflare/cli
          qf version

      - name: Record the project's run count before uploading
        id: runs_before
        run: echo "count=$(python3 e2e/assert-launch-landed.py qualflare-maestro --print)" >> "$GITHUB_OUTPUT"

      - name: Upload to Qualflare
        id: upload
        if: hashFiles('e2e/e2e-results/*.json') != ''
        env:
          QF_TOKEN: ${{ secrets.QF_TOKEN }}
          QF_BRANCH: ${{ github.ref_name }}
          QF_COMMIT: ${{ github.sha }}
        run: |
          if [ -z "$QF_TOKEN" ]; then
            echo "::error::QF_TOKEN is not set on this repository."
            exit 1
          fi
          qf login qualflare-maestro --force
          qf qualflare-maestro collect ./e2e/e2e-results

      - name: Assert the launch reached the project
        if: steps.upload.outcome == 'success'
        run: python3 e2e/assert-launch-landed.py qualflare-maestro --before "${{ steps.runs_before.outputs.count }}"

      - name: Keep the report when something went wrong
        if: steps.suite.outcome != 'success' || steps.verify.outcome != 'success'
        uses: actions/upload-artifact@v4
        with:
          name: e2e-results
          path: e2e/e2e-results/
          retention-days: 7
          if-no-files-found: warn

      - name: Fail if the flows or the verifier were red
        if: steps.suite.outcome != 'success' || steps.verify.outcome != 'success'
        run: |
          echo "::error::suite=${{ steps.suite.outcome }} verify=${{ steps.verify.outcome }}"
          exit 1
```

- [ ] **Step 7: Run the dogfood locally once**

```bash
udid=$(python3 test/integration/pick-simulator.py)
xcrun simctl boot "$udid"; xcrun simctl bootstatus "$udid" -b
go build -ldflags "-X github.com/Qualflare/qualflare-maestro/internal/version.Version=e2e-local" -o /tmp/qfm ./cmd/qualflare-maestro
(cd e2e && /tmp/qfm -output-dir e2e-results -environment local -- maestro test --env DOGFOOD_LABEL=General flows/)
(cd e2e && QUALFLARE_OUTPUT_DIR=e2e-results EXPECT_NESTING=true go run ./verify)
xcrun simctl shutdown "$udid"; rm -rf e2e/e2e-results
```
Expected: Maestro exits 0, then `all checks passed (3 cases)`. On Maestro older than 2.10 leave `EXPECT_NESTING` unset.

- [ ] **Step 8: Commit**

```bash
git add e2e .github/workflows/ci.yml .github/workflows/e2e.yml
git commit -m "ci: unit, floor and real-Maestro jobs, plus the dogfood upload"
```

---

### Task 13: Release configuration and documentation

**Files:**
- Create: `.goreleaser.yml`, `.github/workflows/release.yml`
- Create: `README.md`, `CHANGELOG.md`, `RELEASING.md`, `docs/CONFIGURATION.md`, `docs/LIMITATIONS.md`

**Interfaces:** none.

- [ ] **Step 1: Port the goreleaser configuration**

```bash
cp ../qualflare-go/.goreleaser.yml .goreleaser.yml
sed -i '' 's#qualflare-go#qualflare-maestro#g' .goreleaser.yml
```

Then edit `.goreleaser.yml` by hand:

1. First comment line → `# Release configuration for qualflare-maestro.`; delete the rest of that header comment's second paragraph about npm and container images, keeping the supply-chain sentence.
2. `homebrew_casks[0].homepage` → `https://qualflare.com/maestro-test-reporting/`
3. `homebrew_casks[0].description` → `Native Maestro reporter for Qualflare`
4. In `release.footer`, delete the whole `### The metadata library` section (this repo has no library), and change the go install line to `go install github.com/Qualflare/qualflare-maestro/cmd/qualflare-maestro@{{ .Tag }}`.

Check: `grep -n 'qualflare-go\|metadata library\|go-test-reporting' .goreleaser.yml` prints nothing.

- [ ] **Step 2: Port the release workflow**

```bash
mkdir -p .github/workflows
cp ../qualflare-go/.github/workflows/release.yml .github/workflows/release.yml
sed -i '' 's#qualflare-go#qualflare-maestro#g' .github/workflows/release.yml
```

Then in `.github/workflows/release.yml` replace the gofmt step's command line

```
          unformatted=$(gofmt -l $(git ls-files '*.go' | grep -v '^test/integration/fixtures/buildfail/'))
```

with

```
          unformatted=$(gofmt -l $(git ls-files '*.go'))
```

Check: `grep -n 'buildfail\|qualflare-go' .github/workflows/release.yml` prints nothing.

- [ ] **Step 3: Write `docs/CONFIGURATION.md`**

````markdown
# Configuration

Every option can be a flag or an environment variable. Precedence, highest first:
flag → environment variable → CI detection → git → default.

Reporter flags go **before** Maestro's arguments. The `--` form keeps the two apart:

```bash
qualflare-maestro -environment staging -- maestro test --env USER=ci .maestro/
```

| Flag | Environment variable | Default | Meaning |
|---|---|---|---|
| `-output-dir` | `QUALFLARE_OUTPUT_DIR` | `qualflare-results` | Where the report and its screenshots are written |
| `-environment` | `QUALFLARE_ENVIRONMENT` | `development` | Environment the run belongs to |
| `-language` | `QUALFLARE_LANGUAGE` | `en-US` | Report language |
| `-platform` | `QUALFLARE_PLATFORM` | detected | `ios`, `android` or `web`; detected from the device Maestro ran on |
| `-milestone` | `QUALFLARE_MILESTONE` | — | Milestone sequence number |
| `-branch` | `QUALFLARE_BRANCH` | CI, then git | Branch name |
| `-commit` | `QUALFLARE_COMMIT` | CI, then git | Commit SHA |
| `-run-id` | `QUALFLARE_RUN_ID` | CI run, else random | Groups one launch's report files; every shard must share it |
| `-shard-index` | `QUALFLARE_SHARD_INDEX` | — | Which shard produced these cases |
| `-enabled` | `QUALFLARE_ENABLED` | `true` | `false` runs Maestro exactly as given and writes no report |
| — | `QUALFLARE_MAESTRO_BIN` | `maestro` on `PATH` | The Maestro executable to run |
| `-version` | — | — | Print the version and exit |

## Flags the reporter owns

`--format`, `--output`, `--debug-output` and `--flatten-debug-output` are set by the reporter,
because it reads Maestro's JUnit report and debug output from a place it controls. Passing any of
them exits with code `2`. Every other Maestro flag passes through unchanged.

## Exit codes

| Code | Meaning |
|---|---|
| Maestro's | Maestro ran; its own exit code is returned unchanged |
| `2` | A usage error, or one of the owned flags was passed |
| `127` | `maestro` could not be found |
| `1` | Maestro could not be started, or the report could not be written |

CI detection covers GitHub Actions, GitLab CI, CircleCI and Jenkins.
````

- [ ] **Step 4: Write `docs/LIMITATIONS.md`**

````markdown
# Known limitations

- **Maestro 2.6.0 or newer.** Older releases write no `file` attribute in JUnit, which case ids
  depend on.
- **Only `maestro test`.** `maestro cloud`, `maestro record` and `--continuous` runs are not wrapped.
- **No retry history per case.** Maestro has no flow-level retry for local runs. Retries done by the
  YAML `retry` command appear as steps on Maestro 2.10+, where each attempt is recorded; on older
  releases each attempt overwrites the last.
- **Nesting needs Maestro 2.10+.** Older releases record no depth, so `runFlow`, `repeat` and `retry`
  children are shown as top-level steps.
- **Screenshots.** Maestro 2.10+ takes one for failed and warned steps and links it to the step.
  Older releases take one for a failed step only, matched to the flow by file name.
- **Flows need unique names.** Maestro names its debug output after the flow. When two flows in one
  run share a name — or one flow runs on several devices with `--shard-all` — their steps cannot be
  told apart, so those cases keep their result but get no steps or screenshots.
- **Variable values.** Values passed with `--env` and `MAESTRO_*` environment variables are replaced
  by `${NAME}` in errors, descriptions, properties, labels and step text. Values shorter than 4
  characters are not replaced, and values set any other way — a flow's own `env:` block,
  `evalScript`, a file loaded by a script — are unknown to the reporter and are not protected.
- **Platform detection** reads Maestro's device name. If it cannot tell, the report says `ios` and a
  warning asks for `-platform`.
- **Only iOS has been measured.** Android output is expected to match, but has not been captured yet.
- **Screenshots need `qf` 0.1.24 or newer** to upload.
````

- [ ] **Step 5: Write `README.md`**

````markdown
# qualflare-maestro

[![CI](https://github.com/Qualflare/qualflare-maestro/actions/workflows/ci.yml/badge.svg)](https://github.com/Qualflare/qualflare-maestro/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

A native [Maestro](https://maestro.dev) reporter for [Qualflare](https://qualflare.com). It runs
`maestro test` and turns what Maestro leaves behind into a report with a step for every command,
screenshots tied to the step that took them, real tags, and Qualflare metadata written in the flow's
own YAML.

Without it, Maestro results reach Qualflare as JUnit XML: one line per flow, with a status, a
duration and a failure message. No steps and no screenshots. And when a run dies before reporting —
invalid YAML, no device — there is no file at all, so the failure never shows up.

The reporter makes **no network calls**. It writes a report directory, and
[`qualflare-cli`](https://github.com/Qualflare/qualflare-cli) uploads it.

## Install

```bash
brew install qualflare/tap/qualflare-maestro
# or
go install github.com/Qualflare/qualflare-maestro/cmd/qualflare-maestro@latest
```

Binaries for macOS, Linux and Windows are on the
[releases page](https://github.com/Qualflare/qualflare-maestro/releases).

Requires **Maestro 2.6.0 or newer**. Screenshots need `qf` **0.1.24 or newer** to upload.

## Quickstart

```bash
qualflare-maestro -- maestro test .maestro/
qf my-project collect ./qualflare-results
```

`qualflare-maestro .maestro/` is shorthand for the first line. Maestro's exit code is returned
unchanged, so the command drops into CI in place of `maestro test`:

```yaml
- name: Run Maestro flows
  run: qualflare-maestro -- maestro test .maestro/

- name: Upload results to Qualflare
  if: always()
  run: qf my-project collect ./qualflare-results
```

## Why it wraps Maestro instead of plugging in

Maestro has no reporter or listener API: the report formats are a fixed list, and nothing lets you
load code into a run. So the reporter runs `maestro test` itself, adds `--format junit`, `--output`,
`--debug-output` and `--flatten-debug-output` so Maestro's output lands somewhere it controls, and
reads it when Maestro exits. Passing one of those four flags yourself is an error; everything else
goes straight to Maestro.

## What ends up in the report

- **A case per flow**, identified by its path in your repository and its name.
- **A step per command**, named from the flow's YAML (`tapOn: Login`, `assertVisible: ${USER}`).
  An optional command that did not succeed is a skipped step. On Maestro 2.10+, commands inside
  `runFlow`, `repeat` and `retry` are nested under them.
- **Screenshots** Maestro takes, attached to the step that took them.
- **Durations** measured from the commands themselves, not rounded to whole seconds.
- **Tags** from the flow's `tags`.
- **Infrastructure failures.** If Maestro exits without writing results — invalid YAML, no device —
  the report holds a failed `[unattributed failure]` case with the end of Maestro's log, rather than
  nothing.

## Metadata from YAML

Flows cannot call an API, so Qualflare metadata goes in the flow's `properties`:

```yaml
appId: com.example.app
name: Checkout with a saved card
tags:
  - checkout
  - smoke
properties:
  qualflare.priority: high
  qualflare.description: Pays with the first saved card and checks the receipt.
  qualflare.link.issue: https://example.atlassian.net/browse/PAY-142
  qualflare.link.custom.runbook: https://wiki.example.com/payments
  qualflare.label.team: payments
  owner: checkout-squad
---
- launchApp
```

| Property | Becomes |
|---|---|
| `qualflare.priority` | priority: `low`, `medium`, `high` or `critical` |
| `qualflare.description` | description |
| `qualflare.link.<issue\|tms\|custom>[.<name>]` | a link, optionally named |
| `qualflare.label.<name>` | a label |
| anything else | a case property |

## Keeping secrets out

Step names come from the YAML as written, so `inputText: ${PASSWORD}` stays a placeholder. Maestro's
error messages do contain real values, so values passed with `--env` and `MAESTRO_*` environment
variables are also replaced by `${NAME}` wherever they appear in the report. See
[docs/LIMITATIONS.md](docs/LIMITATIONS.md) for what that does not cover.

## Configuration

Output directory, environment, platform and more: [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## Known limitations

[docs/LIMITATIONS.md](docs/LIMITATIONS.md)

## Development

```bash
go test ./...
```

Tests run against real Maestro output committed under `test/captures/`. The tests that run Maestro
itself need macOS, Maestro and an iOS simulator:

```bash
go test -tags integration ./test/integration/ -run 'TestInvalidYAML|TestNoDevice' -v   # no simulator booted
go test -tags integration ./test/integration/ -run TestFlows -v                       # simulator booted
```

## License

Apache-2.0
````

Do **not** add a Qualflare badge or banner yet: until the first dogfood launch lands, they would
show "no runs". Add them once `e2e.yml` has uploaded successfully.

- [ ] **Step 6: Write `CHANGELOG.md`**

```markdown
# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- First release. Wraps `maestro test` and writes a native Qualflare report: a case per flow, a step
  per command named from the flow's YAML, nesting and screenshots linked to steps on Maestro 2.10+,
  metadata from `qualflare.*` properties, and an `[unattributed failure]` case when Maestro exits
  without results.
- Supports both Maestro debug-output layouts (2.6.x flat files and the 2.10+ per-flow bundle),
  chosen by what is on disk.
- Replaces `--env` and `MAESTRO_*` values with `${NAME}` in report text.
```

- [ ] **Step 7: Write `RELEASING.md`**

````markdown
# Releasing

There is no version file. The version comes from the git tag, through `-ldflags` for release builds
and through the module version for `go install`.

1. Make sure `main` is green, including both Maestro jobs in CI and the latest E2E run.
2. Move the `Unreleased` entries in `CHANGELOG.md` under the new version with today's date, commit,
   and push.
3. Tag and push:

   ```bash
   git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0
   ```

4. `release.yml` runs the full gate, checks the built binary reports the tag, then goreleaser
   publishes the archives, checksums, SBOMs, cosign signature, GitHub release and Homebrew cask.
   Without a `HOMEBREW_TAP_TOKEN` secret the cask is skipped rather than failing the release.
5. Verify the result, not the green check:

   ```bash
   brew install qualflare/tap/qualflare-maestro && qualflare-maestro -version
   go install github.com/Qualflare/qualflare-maestro/cmd/qualflare-maestro@v0.1.0 && qualflare-maestro -version
   ```

   Both should print the tag's version.
````

- [ ] **Step 8: Final gate**

```bash
gofmt -l . ; go vet ./... && go vet -tags integration ./test/integration/ && go test -race ./...
go list -m all
```
Expected: no gofmt output, all tests PASS, and `go list -m all` prints only `github.com/Qualflare/qualflare-maestro`.

- [ ] **Step 9: Commit**

```bash
git add .goreleaser.yml .github/workflows/release.yml README.md CHANGELOG.md RELEASING.md docs/CONFIGURATION.md docs/LIMITATIONS.md
git commit -m "docs: README, configuration, limitations, and release setup"
```

---

## After the plan

Outside this repository, and not part of any task above:

- Create the GitHub repository `Qualflare/qualflare-maestro`, push, add the `QF_TOKEN` secret, and
  create the public Qualflare project `qualflare-maestro`, so `e2e.yml` can upload.
- `landing-fe/src/pages/maestro-test-reporting.astro`: lead with the reporter, and fix the claim that
  a Maestro-looking filename "may suffice" for detection.
- `qualflare-cli/internal/core/services/report_service_parse_test.go:598`: stale "sibling
  commands.json" comment.
