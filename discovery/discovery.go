// Package discovery turns an oats-config.yaml + a collection of case yamls into a
// concrete run plan.
//
// In OATS v1, the runner walked the file system for any yaml carrying
// "oats-schema-version" and ran whatever it found. The current format
// declares the plan up front: oats-config.yaml lists suites, each suite lists cases
// (path globs) and the fixture they share. "oats list" prints the plan
// before "oats run" executes it.
package discovery

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/grafana/oats/casefile"
	"go.yaml.in/yaml/v3"
)

// RootConfig is the parsed oats-config.yaml file. Field names mirror the YAML keys
// directly so a misnamed key surfaces as a "field not defined" error from
// the yaml decoder rather than a silent miss.
type RootConfig struct {
	Meta    Meta                              `yaml:"meta"`
	Cases   []string                          `yaml:"cases,omitempty"`
	Suites  []SuiteConfig                     `yaml:"suites,omitempty"`
	Fixture map[string]casefile.FixtureConfig `yaml:"fixture,omitempty"`
	Cache   CacheConfig                       `yaml:"cache,omitempty"`

	// SourceDir is the directory of the loaded oats-config.yaml. Case glob
	// expressions resolve relative to it.
	SourceDir string `yaml:"-"`
}

type Meta struct {
	Version int `yaml:"version"`
}

type SuiteConfig struct {
	Name    string   `yaml:"name"`
	Cases   []string `yaml:"cases"` // path globs, relative to oats-config.yaml dir
	Fixture string   `yaml:"fixture,omitempty"`
	Tags    []string `yaml:"tags,omitempty"`
}

type CacheConfig struct {
	TTLDays int `yaml:"ttl_days,omitempty"` // zero → use runtime default
}

// SupportedVersion is the value of meta.version that this binary parses. It is
// the single schema version for a v3 project: cases carry no version field of
// their own.
const SupportedVersion = 3

// Load reads an oats-config.yaml from disk.
func Load(path string) (*RootConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("discovery load %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown keys
	var cfg RootConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("discovery parse %s: %w", path, err)
	}
	cfg.SourceDir = filepath.Dir(path)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks structural rules a toml parser cannot. Called by Load;
// exposed for tests that construct RootConfigs in memory.
func (c *RootConfig) Validate() error {
	if c.Meta.Version != SupportedVersion {
		return fmt.Errorf("meta.version: expected %d, got %d", SupportedVersion, c.Meta.Version)
	}
	if len(c.Cases) > 0 && len(c.Suites) > 0 {
		return fmt.Errorf("use top-level cases or suites, not both")
	}
	if len(c.Cases) == 0 && len(c.Suites) == 0 {
		return fmt.Errorf("at least one top-level case or suites entry required")
	}
	if len(c.Cases) > 0 {
		for i, path := range c.Cases {
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("cases[%d]: path is required and non-empty", i)
			}
		}
	}
	for i, s := range c.Suites {
		if len(s.Cases) == 0 {
			return fmt.Errorf("suite[%d]: cases is required and non-empty", i)
		}
		if s.Fixture != "" {
			if _, ok := c.Fixture[s.Fixture]; !ok {
				return fmt.Errorf("suite[%d] (%q): fixture %q not defined", i, suiteLabel(s), s.Fixture)
			}
		}
	}
	for name, f := range c.Fixture {
		if err := f.Validate(name); err != nil {
			return err
		}
	}
	return nil
}

// Filter describes which suites and cases to include in a Plan.
// Empty fields impose no restriction. Tag filtering uses any-match semantics:
// a suite passes if any of its tags appears in the filter list.
type Filter struct {
	Suites []string // exact suite names; empty = all suites
	Tags   []string // any-match
	// Paths restricts the run to cases at (or under) these absolute file/dir
	// paths. Empty = no path restriction. Backs the positional `oats <path>…`
	// scoping, which selects which cases run without changing where the config
	// is loaded from.
	Paths []string
}

// keepCasesUnderPaths returns the cases whose source file is one of paths or
// lives under one of them (dir scope). paths must be absolute and cleaned.
func keepCasesUnderPaths(cases []*casefile.Case, paths []string) []*casefile.Case {
	if len(paths) == 0 {
		return cases
	}
	var kept []*casefile.Case
	for _, tc := range cases {
		abs, err := filepath.Abs(tc.SourcePath)
		if err != nil {
			continue
		}
		for _, p := range paths {
			if abs == p || strings.HasPrefix(abs, p+string(filepath.Separator)) {
				kept = append(kept, tc)
				break
			}
		}
	}
	return kept
}

// Plan is one suite plus the cases it expanded to.
type Plan struct {
	Suite            SuiteConfig
	Fixture          casefile.FixtureConfig
	FixtureSourceDir string
	Cases            []*casefile.Case
}

func (c *RootConfig) effectiveSuites() []SuiteConfig {
	if len(c.Suites) > 0 {
		return c.Suites
	}
	suites := make([]SuiteConfig, 0, len(c.Cases))
	for _, path := range c.Cases {
		suites = append(suites, SuiteConfig{Cases: []string{path}})
	}
	return suites
}

// PlanRun expands globs and applies the filter against the loaded config.
// Returns plans in oats-config.yaml order; cases within a plan are sorted by
// SourcePath for stable test ordering.
func (c *RootConfig) PlanRun(f Filter) ([]Plan, error) {
	wantSuite := func(name string) bool {
		if len(f.Suites) == 0 {
			return true
		}
		for _, s := range f.Suites {
			if s == name {
				return true
			}
		}
		return false
	}
	wantTag := func(tags []string) bool {
		if len(f.Tags) == 0 {
			return true
		}
		for _, want := range f.Tags {
			for _, has := range tags {
				if want == has {
					return true
				}
			}
		}
		return false
	}

	var plans []Plan
	for _, suite := range c.effectiveSuites() {
		cases, err := c.loadSuiteCases(suite)
		if err != nil {
			return nil, fmt.Errorf("suite %q: %w", suiteLabel(suite), err)
		}
		cases = keepCasesUnderPaths(cases, f.Paths)
		if len(cases) == 0 {
			// No cases in this suite fall under the requested path scope.
			continue
		}
		suite = materializeSuite(suite, cases)
		if !wantSuite(suite.Name) {
			continue
		}
		if !wantTag(suite.Tags) {
			continue
		}
		fixture, fixtureSourceDir, err := c.resolveSuiteFixture(suite, cases)
		if err != nil {
			return nil, fmt.Errorf("suite %q: %w", suite.Name, err)
		}

		plans = append(plans, Plan{
			Suite:            suite,
			Fixture:          fixture,
			FixtureSourceDir: fixtureSourceDir,
			Cases:            cases,
		})
	}
	return plans, nil
}

func (c *RootConfig) loadSuiteCases(suite SuiteConfig) ([]*casefile.Case, error) {
	seen := make(map[string]struct{}) // dedupe overlapping globs
	var cases []*casefile.Case
	for _, pattern := range suite.Cases {
		abs := filepath.Join(c.SourceDir, pattern)
		matches, err := filepath.Glob(abs)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("glob %q matched zero files", pattern)
		}
		for _, m := range matches {
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			tc, loadErr := casefile.Load(m)
			if loadErr != nil {
				return nil, loadErr
			}
			cases = append(cases, tc)
		}
	}
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].SourcePath < cases[j].SourcePath
	})
	return cases, nil
}

func suiteLabel(s SuiteConfig) string {
	if s.Name != "" {
		return s.Name
	}
	if len(s.Cases) == 1 {
		p := filepath.Clean(s.Cases[0])
		if base := dirLabel(filepath.Dir(p)); base != "" {
			return base
		}
		return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	}
	return fmt.Sprintf("suite[%d cases]", len(s.Cases))
}

func materializeSuite(s SuiteConfig, cases []*casefile.Case) SuiteConfig {
	if s.Name == "" {
		s.Name = deriveSuiteName(s, cases)
	}
	if len(s.Tags) == 0 {
		seen := make(map[string]struct{})
		for _, c := range cases {
			for _, tag := range c.Tags {
				if _, ok := seen[tag]; ok {
					continue
				}
				seen[tag] = struct{}{}
				s.Tags = append(s.Tags, tag)
			}
		}
		sort.Strings(s.Tags)
	}
	return s
}

func deriveSuiteName(s SuiteConfig, cases []*casefile.Case) string {
	if len(cases) == 1 {
		if strings.TrimSpace(cases[0].Name) != "" {
			return cases[0].Name
		}
		if base := dirLabel(filepath.Dir(cases[0].SourcePath)); base != "" {
			return base
		}
	}
	if len(s.Cases) == 1 {
		p := filepath.Clean(s.Cases[0])
		if base := dirLabel(filepath.Dir(p)); base != "" {
			return base
		}
		return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	}
	return fmt.Sprintf("suite-%d-cases", len(cases))
}

func dirLabel(dir string) string {
	base := filepath.Base(dir)
	if base == "oats" {
		parent := filepath.Base(filepath.Dir(dir))
		if parent != "." && parent != string(filepath.Separator) && parent != "" {
			return parent
		}
	}
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func (c *RootConfig) resolveSuiteFixture(suite SuiteConfig, cases []*casefile.Case) (casefile.FixtureConfig, string, error) {
	if suite.Fixture != "" {
		return c.Fixture[suite.Fixture], c.SourceDir, nil
	}
	var (
		fixture   casefile.FixtureConfig
		sourceDir string
		seen      bool
	)
	for _, tc := range cases {
		if tc.Fixture == nil {
			continue
		}
		next := *tc.Fixture
		nextSourceDir := filepath.Dir(tc.SourcePath)
		if !seen {
			fixture, sourceDir, seen = next, nextSourceDir, true
			continue
		}
		if !reflect.DeepEqual(fixture, next) || sourceDir != nextSourceDir {
			return casefile.FixtureConfig{}, "", fmt.Errorf("suite omits fixture but cases do not agree on one shared fixture")
		}
	}
	if !seen {
		return casefile.FixtureConfig{}, "", nil
	}
	return fixture, sourceDir, nil
}

// Summary renders a single line per plan suitable for `oats list`. It does
// not load any cases — useful for a dry-run before deciding to expand globs.
func (c *RootConfig) Summary() string {
	var out string
	for _, s := range c.effectiveSuites() {
		label := s.Name
		if label == "" {
			label = suiteLabel(s)
		}
		out += fmt.Sprintf("suite=%s fixture=%s tags=%v cases=%v\n", label, s.Fixture, s.Tags, s.Cases)
	}
	return out
}
