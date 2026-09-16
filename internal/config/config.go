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
