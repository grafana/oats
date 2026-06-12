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

	"go.yaml.in/yaml/v3"
)

// SchemaVersion is the value of the top-level `oats:` field that this loader
// understands. Cases with any other value are rejected at parse time.
const SchemaVersion = 2

// Case is one entry point yaml file. Cases are independently runnable;
// suites group them via oats.toml.
type Case struct {
	OatsVersion int      `yaml:"oats"`
	Name        string   `yaml:"name"`
	Tags        []string `yaml:"tags,omitempty"`
	Hermetic    *bool    `yaml:"hermetic,omitempty"` // pointer: distinguish unset vs explicit false

	Seed     Seed     `yaml:"seed"`
	Expected Expected `yaml:"expected"`

	// SourcePath is filled by the loader; not part of the yaml surface.
	SourcePath string `yaml:"-"`
}

// Seed declares how a case populates the stack before assertions run.
// Exactly one of the fields beyond Type is meaningful per Type value:
//
//	type: app          → Compose is required
//	type: inline-otlp  → Traces/Logs/Metrics describe the payload to push
type Seed struct {
	Type    string         `yaml:"type"` // "app" or "inline-otlp"
	Compose string         `yaml:"compose,omitempty"`
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

// Expected groups per-signal assertion blocks. A case may omit any signal it
// does not care about; an empty Expected makes the case a no-op (rejected at
// validation).
type Expected struct {
	Traces   []TraceAssertion   `yaml:"traces,omitempty"`
	Metrics  []MetricAssertion  `yaml:"metrics,omitempty"`
	Logs     []LogAssertion     `yaml:"logs,omitempty"`
	Profiles []ProfileAssertion `yaml:"profiles,omitempty"`
}

// AssertionCommon holds the keys every signal-type assertion supports.
// Embedded into each concrete assertion struct so a case author can mix and
// match contains / regex / count / absent on any signal.
type AssertionCommon struct {
	Contains    []string `yaml:"contains,omitempty"`
	NotContains []string `yaml:"not_contains,omitempty"`
	Regex       []string `yaml:"regex,omitempty"`
	Count       string   `yaml:"count,omitempty"` // ">= 1", "== 0", ...
	Absent      bool     `yaml:"absent,omitempty"`
}

type TraceAssertion struct {
	TraceQL string `yaml:"traceql"`
	AssertionCommon `yaml:",inline"`
}

type MetricAssertion struct {
	PromQL string `yaml:"promql"`
	Value  string `yaml:"value,omitempty"` // ">= 0", "== 42", ...
	AssertionCommon `yaml:",inline"`
}

type LogAssertion struct {
	LogQL string `yaml:"logql"`
	AssertionCommon `yaml:",inline"`
}

type ProfileAssertion struct {
	Query string `yaml:"query"`
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
	switch c.Seed.Type {
	case "app":
		if c.Seed.Compose == "" {
			return fmt.Errorf("seed.compose: required when seed.type = app")
		}
	case "inline-otlp":
		if len(c.Seed.Traces)+len(c.Seed.Logs)+len(c.Seed.Metrics) == 0 {
			return fmt.Errorf("seed: inline-otlp must declare at least one trace, log, or metric")
		}
	case "":
		return fmt.Errorf("seed.type: required (app | inline-otlp)")
	default:
		return fmt.Errorf("seed.type: unknown value %q (expected app or inline-otlp)", c.Seed.Type)
	}
	if len(c.Expected.Traces)+len(c.Expected.Metrics)+len(c.Expected.Logs)+len(c.Expected.Profiles) == 0 {
		return fmt.Errorf("expected: at least one signal assertion required (a case with no expectations cannot fail)")
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
