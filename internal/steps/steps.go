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
	return name
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
