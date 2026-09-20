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
	Layout Layout
	Flows  []Flow
	// Platform is "android", "ios" or "" -- what the bundle manifests attest to,
	// not a guess. Maestro's JUnit `device` attribute cannot answer this: on
	// Android it is the AVD name or the adb serial (DeviceService's
	// listAndroidDevices sets description to `avdName ?: connection.serial`), so
	// `qualflare_probe_api34` and `R5CT30ABCDE` are both normal, and neither says
	// android. The manifests do, structurally: a DEVICE_LOG entry sourced from
	// logcat exists only on Android, and one sourced from xctest only on Apple
	// platforms. The flat 2.6.x layout writes no manifest, so it stays "".
	Platform   string
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
		case it.Type().IsDir() && isFile(filepath.Join(dir, name, "commands.json")):
			bundles = append(bundles, name)
		case it.Type().IsRegular() && strings.HasPrefix(name, "commands-") && strings.HasSuffix(name, ".json"):
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

// manifest is the subset of artifact-manifest/v1 that says which platform ran.
type manifest struct {
	Entries []struct {
		Kind         string `json:"kind"`
		RelativePath string `json:"relativePath"`
		Metadata     struct {
			Source string `json:"source"`
		} `json:"metadata"`
	} `json:"entries"`
}

// platformFromManifest reads one flow's manifest.json. An unreadable or absent
// manifest is not a warning: it costs nothing but the hint, and -platform and the
// device string still answer.
func platformFromManifest(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	for _, e := range m.Entries {
		if !strings.EqualFold(e.Kind, "DEVICE_LOG") {
			continue
		}
		hay := strings.ToLower(e.RelativePath + " " + e.Metadata.Source)
		switch {
		case strings.Contains(hay, "logcat"):
			return "android"
		case strings.Contains(hay, "xctest"), strings.Contains(hay, "simulator"):
			return "ios"
		}
	}
	return ""
}

func readBundle(res *Result, flowDir string) {
	key := filepath.Base(flowDir)
	if res.Platform == "" {
		res.Platform = platformFromManifest(filepath.Join(flowDir, "manifest.json"))
	}
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
			if s.Type().IsRegular() && strings.HasSuffix(s.Name(), ".png") && !referenced[p] {
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
		if it.Type().IsRegular() && strings.HasPrefix(n, "screenshot-") && strings.HasSuffix(n, suffix) {
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
	st, err := os.Lstat(p)
	return err == nil && st.Mode().IsRegular()
}

func unreadable(file string, err error) string {
	return fmt.Sprintf("could not read %s (%v); that flow keeps its JUnit result but has no steps or screenshots", file, err)
}
