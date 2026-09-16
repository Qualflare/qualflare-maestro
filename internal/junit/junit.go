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
