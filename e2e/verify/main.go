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
		fmt.Printf("%d check(s) failed -- the report does not match the flows; it is still uploaded, and this job fails\n", len(failures))
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
