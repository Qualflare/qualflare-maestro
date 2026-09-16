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
