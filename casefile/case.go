// Package casefile is the in-memory representation of an OATS case yaml.
//
// A case yaml describes one observable behaviour to verify: how to seed data
// into the stack, and what gcx queries should return. Cases declare what
// they expect, not how to run gcx — the runner picks gcx args from the
// expectation block.
//
// The struct shape mirrors the case yaml documented in the OATS v2
// implementation plan (internal-docs #14). Field tags follow the yaml.v3
// convention.
package casefile

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// SchemaVersion is the value of the top-level `oats-schema-version` field
// that this loader understands. Cases with any other value are rejected at
// parse time.
const SchemaVersion = 3

// Case is one entry point yaml file. Cases are independently runnable;
// suites group them via oats.toml.
type Case struct {
	OatsSchemaVersion int            `yaml:"oats-schema-version"`
	Name              string         `yaml:"name"`
	Tags              []string       `yaml:"tags,omitempty"`
	Interval          time.Duration  `yaml:"interval,omitempty"`
	Fixture           *FixtureConfig `yaml:"fixture,omitempty"`

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

// FixtureConfig declares how a suite stands up the backends its cases run
// against. Exactly one of the type-specific blocks is set; the block that is
// present selects the fixture kind. Dual toml/yaml tags let the same struct
// load from oats.toml (BurntSushi/toml) and from a case yaml (yaml.v3).
type FixtureConfig struct {
	Compose *ComposeFixture `toml:"compose,omitempty" yaml:"compose,omitempty"`
	K3D     *K3DFixture     `toml:"k3d,omitempty" yaml:"k3d,omitempty"`
	Remote  *RemoteFixture  `toml:"remote,omitempty" yaml:"remote,omitempty"`
}

// ComposeFixture boots a docker-compose stack. template selects a built-in
// stack ("lgtm"); file/files point at compose files relative to the fixture
// source dir. app_service + app_port let OATS publish and discover an
// ephemeral host port for the application under test.
type ComposeFixture struct {
	Template   string   `toml:"template,omitempty" yaml:"template,omitempty"`
	File       string   `toml:"file,omitempty" yaml:"file,omitempty"`
	Files      []string `toml:"files,omitempty" yaml:"files,omitempty"`
	Env        []string `toml:"env,omitempty" yaml:"env,omitempty"`
	AppService string   `toml:"app_service,omitempty" yaml:"app_service,omitempty"`
	AppPort    int      `toml:"app_port,omitempty" yaml:"app_port,omitempty"`
}

// K3DFixture boots a k3d cluster and builds/imports the application image.
type K3DFixture struct {
	K8sDir           string   `toml:"k8s_dir,omitempty" yaml:"k8s_dir,omitempty"`
	AppService       string   `toml:"app_service,omitempty" yaml:"app_service,omitempty"`
	AppDockerFile    string   `toml:"app_docker_file,omitempty" yaml:"app_docker_file,omitempty"`
	AppDockerContext string   `toml:"app_docker_context,omitempty" yaml:"app_docker_context,omitempty"`
	AppDockerTag     string   `toml:"app_docker_tag,omitempty" yaml:"app_docker_tag,omitempty"`
	AppPort          int      `toml:"app_port,omitempty" yaml:"app_port,omitempty"`
	ImportImages     []string `toml:"import_images,omitempty" yaml:"import_images,omitempty"`
	PoolSize         int      `toml:"pool_size,omitempty" yaml:"pool_size,omitempty"`
}

// RemoteFixture points at an already-running stack; OATS boots nothing.
type RemoteFixture struct {
	Endpoint string `toml:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}

// Kind returns "compose"/"k3d"/"remote", or "" when no block is set. Exactly
// one block should be set (enforced by Validate).
func (f FixtureConfig) Kind() string {
	switch {
	case f.Compose != nil:
		return "compose"
	case f.K3D != nil:
		return "k3d"
	case f.Remote != nil:
		return "remote"
	default:
		return ""
	}
}

// HasManagedApp reports whether OATS manages the compose app endpoint (so it
// can publish + discover an ephemeral host port): a compose block with
// app_service + app_port. It drives both the parallel-safety gate and app-port
// discovery, so the two stay in lockstep.
func (f FixtureConfig) HasManagedApp() bool {
	return f.Compose != nil && f.Compose.AppService != "" && f.Compose.AppPort > 0
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
	Traces      []TraceAssertion   `yaml:"traces,omitempty"`
	Metrics     []MetricAssertion  `yaml:"metrics,omitempty"`
	Logs        []LogAssertion     `yaml:"logs,omitempty"`
	Profiles    []ProfileAssertion `yaml:"profiles,omitempty"`
	ComposeLogs []string           `yaml:"compose-logs,omitempty"`
	Custom      []CustomCheck      `yaml:"custom-checks,omitempty"`
}

type CustomCheck struct {
	Script string `yaml:"script"`
}

// AssertionCommon holds the keys every signal-type assertion supports.
// Embedded into each concrete assertion struct so a case author can mix and
// match contains / regex / count / absent on any signal.
type AssertionCommon struct {
	Contains    StringList   `yaml:"contains,omitempty"`
	NotContains StringList   `yaml:"not_contains,omitempty"`
	Regex       StringList   `yaml:"regex,omitempty"`
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
	MatchType  MatchType         `yaml:"match_type,omitempty"`
	Name       *string           `yaml:"name,omitempty"`
	Attributes AttributeMatchers `yaml:"attributes,omitempty"`
}

func (m MatchEntry) EffectiveMatchType() MatchType {
	if m.MatchType == "" {
		return MatchTypeStrict
	}
	return m.MatchType
}

type StringList []string

func (s *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case 0:
		*s = nil
		return nil
	case yaml.ScalarNode:
		var v string
		if err := node.Decode(&v); err != nil {
			return err
		}
		*s = []string{v}
		return nil
	case yaml.SequenceNode:
		var v []string
		if err := node.Decode(&v); err != nil {
			return err
		}
		*s = v
		return nil
	default:
		return fmt.Errorf("expected string or list of strings")
	}
}

func (s StringList) MarshalYAML() (any, error) {
	switch len(s) {
	case 0:
		return nil, nil
	case 1:
		return s[0], nil
	default:
		return []string(s), nil
	}
}

type AttributeMatcher struct {
	Key   string
	Value *string
}

type AttributeMatchers []AttributeMatcher

func (a *AttributeMatchers) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		type attrDoc struct {
			Key   string  `yaml:"key"`
			Value *string `yaml:"value,omitempty"`
		}
		var docs []attrDoc
		if err := node.Decode(&docs); err != nil {
			return err
		}
		out := make([]AttributeMatcher, 0, len(docs))
		for _, doc := range docs {
			out = append(out, AttributeMatcher(doc))
		}
		*a = out
		return nil
	case yaml.MappingNode:
		// Backward-compatible input: attributes: {key: value} or
		// attributes: {key: {present: true}}.
		out := make([]AttributeMatcher, 0, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode, valueNode := node.Content[i], node.Content[i+1]
			attr := AttributeMatcher{Key: keyNode.Value}
			switch valueNode.Kind {
			case yaml.ScalarNode:
				v := valueNode.Value
				attr.Value = &v
			case yaml.MappingNode:
				var aux struct {
					Present *bool `yaml:"present"`
				}
				if err := valueNode.Decode(&aux); err != nil {
					return err
				}
				if aux.Present == nil || !*aux.Present {
					return fmt.Errorf("expected scalar value or {present: true}")
				}
			default:
				return fmt.Errorf("expected scalar value or {present: true}")
			}
			out = append(out, attr)
		}
		*a = out
		return nil
	default:
		return fmt.Errorf("expected list of {key, value?} or legacy mapping")
	}
}

func (a AttributeMatchers) MarshalYAML() (any, error) {
	if len(a) == 0 {
		return nil, nil
	}
	type attrDoc struct {
		Key   string  `yaml:"key"`
		Value *string `yaml:"value,omitempty"`
	}
	out := make([]attrDoc, 0, len(a))
	for _, attr := range a {
		out = append(out, attrDoc(attr))
	}
	return out, nil
}

type TraceAssertion struct {
	TraceQL         string       `yaml:"traceql"`
	MatchSpans      []MatchEntry `yaml:"match_spans,omitempty"`
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
		return nil, fmt.Errorf("casefile load %s: %w", path, err)
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("casefile parse %s: %w", path, err)
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
	if c.OatsSchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported oats-schema-version '%d' required version is '%d'",
			c.OatsSchemaVersion, SchemaVersion)
	}
	if c.Name == "" {
		return fmt.Errorf("name: required, non-empty")
	}
	if c.Interval < 0 {
		return fmt.Errorf("interval: must be >= 0")
	}
	if c.Fixture != nil {
		if err := c.Fixture.Validate("fixture"); err != nil {
			return err
		}
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
		for i, tr := range c.Seed.Traces {
			for j, sp := range tr.Spans {
				if sp.Duration == "" {
					continue
				}
				if _, err := time.ParseDuration(sp.Duration); err != nil {
					return fmt.Errorf("seed.traces[%d].spans[%d].duration: invalid duration %q: %v", i, j, sp.Duration, err)
				}
			}
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
		if err := validateTraceAssertion(i, c.Expected.Traces[i]); err != nil {
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

// Validate enforces that exactly one fixture block is set and that the set
// block carries the fields its kind requires. label names the fixture in
// error messages (the fixture name from oats.toml, or "fixture" for a
// case-level block).
func (f FixtureConfig) Validate(label string) error {
	set := 0
	for _, present := range []bool{f.Compose != nil, f.K3D != nil, f.Remote != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("fixture %q: set exactly one of compose/k3d/remote", label)
	}
	switch {
	case f.Compose != nil:
		c := f.Compose
		if c.Template == "" && c.File == "" && len(c.Files) == 0 {
			return fmt.Errorf("fixture %q: compose requires template, file, or files", label)
		}
		if c.File != "" && len(c.Files) > 0 {
			return fmt.Errorf("fixture %q: compose sets file or files, not both", label)
		}
	case f.K3D != nil:
		k := f.K3D
		if k.K8sDir == "" || k.AppService == "" || k.AppDockerFile == "" || k.AppDockerTag == "" || k.AppPort == 0 {
			return fmt.Errorf("fixture %q: k3d requires k8s_dir, app_service, app_docker_file, app_docker_tag, and app_port", label)
		}
	case f.Remote != nil:
		if f.Remote.Endpoint == "" {
			return fmt.Errorf("fixture %q: remote requires endpoint", label)
		}
	}
	return nil
}

func validateTraceAssertion(idx int, a TraceAssertion) error {
	if err := validateMatchEntries(fmt.Sprintf("expected.traces[%d].match_spans", idx), a.MatchSpans); err != nil {
		return err
	}
	return validateAssertionCommon("expected.traces", idx, a.AssertionCommon)
}

func validateAssertionCommon(path string, idx int, a AssertionCommon) error {
	for j, p := range a.Regex {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("%s[%d].regex[%d]: invalid regexp %q: %v", path, idx, j, p, err)
		}
	}
	if err := validateMatchEntries(fmt.Sprintf("%s[%d].match", path, idx), a.Match); err != nil {
		return err
	}
	return nil
}

func validateMatchEntries(path string, entries []MatchEntry) error {
	for j, m := range entries {
		matchPath := fmt.Sprintf("%s[%d]", path, j)
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
		seenKeys := map[string]struct{}{}
		for i, attr := range m.Attributes {
			attrPath := fmt.Sprintf("%s.attributes[%d]", matchPath, i)
			if strings.TrimSpace(attr.Key) == "" {
				return fmt.Errorf("%s.key: required", attrPath)
			}
			if _, ok := seenKeys[attr.Key]; ok {
				return fmt.Errorf("%s.key: duplicate key %q", attrPath, attr.Key)
			}
			seenKeys[attr.Key] = struct{}{}
			if m.EffectiveMatchType() == MatchTypeRegexp && attr.Value != nil {
				if _, err := regexp.Compile(*attr.Value); err != nil {
					return fmt.Errorf("%s.value: invalid regexp %q: %v", attrPath, *attr.Value, err)
				}
			}
		}
	}
	return nil
}
