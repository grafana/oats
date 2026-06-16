// Package v2case is the in-memory representation of an OATS v2 case yaml.
//
// A case yaml describes one observable behaviour to verify: how to seed data
// into the stack, and what gcx queries should return. Cases declare what
// they expect, not how to run gcx — the runner picks gcx args from the
// expectation block.
//
// The struct shape mirrors the case yaml documented in the OATS v2
// implementation plan (internal-docs #14). Field tags follow the yaml.v3
// convention.
package v2case

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// SchemaVersion is the value of the top-level `oats:` field that this loader
// understands. Cases with any other value are rejected at parse time.
const SchemaVersion = 2

// Case is one entry point yaml file. Cases are independently runnable;
// suites group them via oats.toml.
type Case struct {
	OatsVersion int           `yaml:"oats"`
	Name        string        `yaml:"name"`
	Tags        []string      `yaml:"tags,omitempty"`
	Hermetic    *bool         `yaml:"hermetic,omitempty"` // pointer: distinguish unset vs explicit false
	Interval    time.Duration `yaml:"interval,omitempty"`

	Seed     Seed     `yaml:"seed"`
	Input    []Input  `yaml:"input,omitempty"`
	Expected Expected `yaml:"expected"`

	// SourcePath is filled by the loader; not part of the yaml surface.
	SourcePath string `yaml:"-"`
}

// Seed declares how a case populates the stack before assertions run.
// Exactly one of the fields beyond Type is meaningful per Type value:
//
//	type: app          → external suite fixture boots the app; Compose is optional
//	type: inline-otlp  → Traces/Logs/Metrics describe the payload to push
type Seed struct {
	Type    string         `yaml:"type"`              // "app" or "inline-otlp"
	Compose string         `yaml:"compose,omitempty"` // optional legacy shorthand; suite fixture normally owns app boot
	Traces  []SeedTrace    `yaml:"traces,omitempty"`
	Logs    []SeedLog      `yaml:"logs,omitempty"`
	Metrics []SeedMetric   `yaml:"metrics,omitempty"`
	Vars    map[string]any `yaml:"vars,omitempty"`
}

type SeedTrace struct {
	Service string     `yaml:"service"`
	Spans   []SeedSpan `yaml:"spans"`
}

type SeedSpan struct {
	Name     string `yaml:"name"`
	Kind     int    `yaml:"kind,omitempty"`
	Duration string `yaml:"duration,omitempty"` // human duration ("200ms")
}

type SeedLog struct {
	Service        string `yaml:"service"`
	Body           string `yaml:"body"`
	SeverityNumber int    `yaml:"severity_number,omitempty"`
	SeverityText   string `yaml:"severity_text,omitempty"`
}

type SeedMetric struct {
	Service string `yaml:"service"`
	Name    string `yaml:"name"`
	Value   int64  `yaml:"value"`
}

// Input drives the application under test so telemetry is emitted before
// assertions run. It mirrors the legacy OATS HTTP request shape.
type Input struct {
	Scheme  string            `yaml:"scheme,omitempty"`
	Host    string            `yaml:"host,omitempty"`
	Method  string            `yaml:"method,omitempty"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty"`
	Status  string            `yaml:"status,omitempty"`
}

// Expected groups per-signal assertion blocks. A case may omit any signal it
// does not care about; an empty Expected makes the case a no-op (rejected at
// validation).
type Expected struct {
	Traces   []TraceAssertion   `yaml:"traces,omitempty"`
	Metrics  []MetricAssertion  `yaml:"metrics,omitempty"`
	Logs     []LogAssertion     `yaml:"logs,omitempty"`
	Profiles []ProfileAssertion `yaml:"profiles,omitempty"`
	Custom   []CustomCheck      `yaml:"custom-checks,omitempty"`
}

type CustomCheck struct {
	Script string `yaml:"script"`
}

// AssertionCommon holds the keys every signal-type assertion supports.
// Embedded into each concrete assertion struct so a case author can mix and
// match contains / regex / count / absent on any signal.
type AssertionCommon struct {
	Contains    []string     `yaml:"contains,omitempty"`
	NotContains []string     `yaml:"not_contains,omitempty"`
	Regex       []string     `yaml:"regex,omitempty"`
	Match       []MatchEntry `yaml:"match,omitempty"`
	Count       string       `yaml:"count,omitempty"` // ">= 1", "== 0", ...
	Absent      bool         `yaml:"absent,omitempty"`
}

type MatchType string

const (
	MatchTypeStrict MatchType = "strict"
	MatchTypeRegexp MatchType = "regexp"
)

type MatchEntry struct {
	MatchType  MatchType                       `yaml:"match_type,omitempty"`
	Name       *string                         `yaml:"name,omitempty"`
	Attributes map[string]AttributeExpectation `yaml:"attributes,omitempty"`
}

func (m MatchEntry) EffectiveMatchType() MatchType {
	if m.MatchType == "" {
		return MatchTypeStrict
	}
	return m.MatchType
}

type AttributeExpectation struct {
	Value   *string
	Present *bool
}

func (a *AttributeExpectation) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		v := node.Value
		a.Value = &v
		a.Present = nil
		return nil
	case yaml.MappingNode:
		var aux struct {
			Present *bool `yaml:"present"`
		}
		if err := node.Decode(&aux); err != nil {
			return err
		}
		a.Value = nil
		a.Present = aux.Present
		return nil
	default:
		return fmt.Errorf("expected scalar string or mapping {present: true}")
	}
}

func (a AttributeExpectation) MarshalYAML() (any, error) {
	switch {
	case a.Value != nil && a.Present == nil:
		return *a.Value, nil
	case a.Value == nil && a.Present != nil:
		return map[string]bool{"present": *a.Present}, nil
	case a.Value == nil && a.Present == nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("attribute expectation cannot marshal value and present simultaneously")
	}
}

type TraceAssertion struct {
	TraceQL         string `yaml:"traceql"`
	AssertionCommon `yaml:",inline"`
}

type MetricAssertion struct {
	PromQL          string `yaml:"promql"`
	Value           string `yaml:"value,omitempty"` // ">= 0", "== 42", ...
	AssertionCommon `yaml:",inline"`
}

type LogAssertion struct {
	LogQL           string `yaml:"logql"`
	AssertionCommon `yaml:",inline"`
}

type ProfileAssertion struct {
	Query           string `yaml:"query"`
	AssertionCommon `yaml:",inline"`
}

// Load reads a yaml file from disk and returns a parsed, validated Case.
// Returns an error if the file is missing, the yaml is malformed, or the
// case violates any structural rule (see Validate).
func Load(path string) (*Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("v2case load %s: %w", path, err)
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("v2case parse %s: %w", path, err)
	}
	c.SourcePath = path
	return c, nil
}

// Parse is Load's byte-slice counterpart. Useful in tests that hold yaml
// inline.
func Parse(data []byte) (*Case, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown keys

	var c Case
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks structural rules a yaml parser cannot enforce on its own.
// Called automatically by Parse; exported for tests that construct Cases
// programmatically.
func (c *Case) Validate() error {
	if c.OatsVersion != SchemaVersion {
		return fmt.Errorf("oats: expected %d, got %d (this binary parses v%d only)",
			SchemaVersion, c.OatsVersion, SchemaVersion)
	}
	if c.Name == "" {
		return fmt.Errorf("name: required, non-empty")
	}
	if c.Interval < 0 {
		return fmt.Errorf("interval: must be >= 0")
	}
	switch c.Seed.Type {
	case "app":
		// App-backed cases are normally booted by the suite fixture declared in
		// oats.toml. seed.compose remains accepted as a legacy/migration
		// shorthand, but is not required for validation or execution here.
	case "inline-otlp":
		if len(c.Seed.Traces)+len(c.Seed.Logs)+len(c.Seed.Metrics) == 0 {
			return fmt.Errorf("seed: inline-otlp must declare at least one trace, log, or metric")
		}
	case "":
		return fmt.Errorf("seed.type: required (app | inline-otlp)")
	default:
		return fmt.Errorf("seed.type: unknown value %q (expected app or inline-otlp)", c.Seed.Type)
	}
	if len(c.Expected.Traces)+len(c.Expected.Metrics)+len(c.Expected.Logs)+len(c.Expected.Profiles)+len(c.Expected.Custom) == 0 {
		return fmt.Errorf("expected: at least one assertion required (signal or custom-check)")
	}
	for i, in := range c.Input {
		if in.Path == "" {
			return fmt.Errorf("input[%d].path: required, non-empty", i)
		}
	}
	for i := range c.Expected.Traces {
		if c.Expected.Traces[i].TraceQL == "" {
			return fmt.Errorf("expected.traces[%d].traceql: required, non-empty", i)
		}
		if err := validateAssertionCommon("expected.traces", i, c.Expected.Traces[i].AssertionCommon); err != nil {
			return err
		}
	}
	for i := range c.Expected.Logs {
		if c.Expected.Logs[i].LogQL == "" {
			return fmt.Errorf("expected.logs[%d].logql: required, non-empty", i)
		}
		if err := validateAssertionCommon("expected.logs", i, c.Expected.Logs[i].AssertionCommon); err != nil {
			return err
		}
	}
	for i := range c.Expected.Metrics {
		if c.Expected.Metrics[i].PromQL == "" {
			return fmt.Errorf("expected.metrics[%d].promql: required, non-empty", i)
		}
		if err := validateAssertionCommon("expected.metrics", i, c.Expected.Metrics[i].AssertionCommon); err != nil {
			return err
		}
	}
	for i := range c.Expected.Profiles {
		if c.Expected.Profiles[i].Query == "" {
			return fmt.Errorf("expected.profiles[%d].query: required, non-empty", i)
		}
		if err := validateAssertionCommon("expected.profiles", i, c.Expected.Profiles[i].AssertionCommon); err != nil {
			return err
		}
	}
	for i := range c.Expected.Custom {
		if strings.TrimSpace(c.Expected.Custom[i].Script) == "" {
			return fmt.Errorf("expected.custom-checks[%d].script: required, non-empty", i)
		}
	}
	return nil
}

// IsHermetic reports whether this case promises not to interfere with siblings
// sharing the same fixture. Default is true; cases opt out by setting
// `hermetic: false`.
func (c *Case) IsHermetic() bool {
	if c.Hermetic == nil {
		return true
	}
	return *c.Hermetic
}

func validateAssertionCommon(path string, idx int, a AssertionCommon) error {
	for j, p := range a.Regex {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("%s[%d].regex[%d]: invalid regexp %q: %v", path, idx, j, p, err)
		}
	}
	for j, m := range a.Match {
		matchPath := fmt.Sprintf("%s[%d].match[%d]", path, idx, j)
		switch m.EffectiveMatchType() {
		case MatchTypeStrict, MatchTypeRegexp:
		default:
			return fmt.Errorf("%s.match_type: unknown value %q (expected strict or regexp)", matchPath, m.MatchType)
		}
		if m.Name == nil && len(m.Attributes) == 0 {
			return fmt.Errorf("%s: at least one of name or attributes is required", matchPath)
		}
		if m.EffectiveMatchType() == MatchTypeRegexp && m.Name != nil {
			if _, err := regexp.Compile(*m.Name); err != nil {
				return fmt.Errorf("%s.name: invalid regexp %q: %v", matchPath, *m.Name, err)
			}
		}
		for key, attr := range m.Attributes {
			attrPath := fmt.Sprintf("%s.attributes[%q]", matchPath, key)
			if attr.Value != nil && attr.Present != nil {
				return fmt.Errorf("%s: expected either scalar value or {present: true}, not both", attrPath)
			}
			if attr.Value == nil && attr.Present == nil {
				return fmt.Errorf("%s: expected scalar value or {present: true}", attrPath)
			}
			if attr.Present != nil && !*attr.Present {
				return fmt.Errorf("%s.present: only true is allowed", attrPath)
			}
			if m.EffectiveMatchType() == MatchTypeRegexp && attr.Value != nil {
				if _, err := regexp.Compile(*attr.Value); err != nil {
					return fmt.Errorf("%s: invalid regexp %q: %v", attrPath, *attr.Value, err)
				}
			}
		}
	}
	return nil
}
