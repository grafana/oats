// Package discovery turns an oats.toml + a collection of case yamls into a
// concrete run plan.
//
// In OATS v1, the runner walked the file system for any yaml carrying
// "oats-schema-version" and ran whatever it found. v2 declares the plan up
// front: oats.toml lists suites, each suite lists cases (path globs) and the
// fixture they share. "oats list" prints the plan before "oats run" executes
// it.
package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/grafana/oats/v2case"
)

// RootConfig is the parsed oats.toml file. Field names mirror the TOML keys
// directly so a misnamed key surfaces as a "field not defined" error from
// the toml decoder rather than a silent miss.
type RootConfig struct {
	Meta    Meta                      `toml:"meta"`
	Suites  []SuiteConfig             `toml:"suite"`
	Fixture map[string]FixtureConfig  `toml:"fixture"`
	Cache   CacheConfig               `toml:"cache,omitempty"`

	// SourceDir is the directory of the loaded oats.toml. Case glob
	// expressions resolve relative to it.
	SourceDir string `toml:"-"`
}

type Meta struct {
	Version int `toml:"version"`
}

type SuiteConfig struct {
	Name    string   `toml:"name"`
	Cases   []string `toml:"cases"` // path globs, relative to oats.toml dir
	Fixture string   `toml:"fixture,omitempty"`
	Tags    []string `toml:"tags,omitempty"`
}

type FixtureConfig struct {
	Type        string `toml:"type"` // "compose" | "k3d" | "remote"
	Template    string `toml:"template,omitempty"`
	ComposeFile string `toml:"compose_file,omitempty"`
	PoolSize    int    `toml:"pool_size,omitempty"`
	Endpoint    string `toml:"endpoint,omitempty"` // remote only
}

type CacheConfig struct {
	TTLDays int `toml:"ttl_days,omitempty"` // zero → use runtime default
}

// SupportedVersion is the value of [meta].version that this binary parses.
const SupportedVersion = 2

// Load reads an oats.toml from disk.
func Load(path string) (*RootConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("discovery load %s: %w", path, err)
	}
	var cfg RootConfig
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("discovery: %s contains unknown keys: %v", path, undecoded)
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
	if len(c.Suites) == 0 {
		return fmt.Errorf("at least one [[suite]] required")
	}
	for i, s := range c.Suites {
		if s.Name == "" {
			return fmt.Errorf("suite[%d].name: required", i)
		}
		if len(s.Cases) == 0 {
			return fmt.Errorf("suite[%d] (%q): cases is required and non-empty", i, s.Name)
		}
		if s.Fixture != "" {
			if _, ok := c.Fixture[s.Fixture]; !ok {
				return fmt.Errorf("suite[%d] (%q): fixture %q not defined", i, s.Name, s.Fixture)
			}
		}
	}
	for name, f := range c.Fixture {
		switch f.Type {
		case "compose":
			if f.Template == "" && f.ComposeFile == "" {
				return fmt.Errorf("fixture %q: type=compose requires template or compose_file", name)
			}
		case "k3d":
			// PoolSize=0 means "single ephemeral cluster" — valid.
		case "remote":
			if f.Endpoint == "" {
				return fmt.Errorf("fixture %q: type=remote requires endpoint", name)
			}
		case "":
			return fmt.Errorf("fixture %q: type is required (compose | k3d | remote)", name)
		default:
			return fmt.Errorf("fixture %q: unknown type %q", name, f.Type)
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
}

// Plan is one suite plus the cases it expanded to.
type Plan struct {
	Suite   SuiteConfig
	Fixture FixtureConfig
	Cases   []*v2case.Case
}

// PlanRun expands globs and applies the filter against the loaded config.
// Returns plans in oats.toml order; cases within a plan are sorted by
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
	for _, suite := range c.Suites {
		if !wantSuite(suite.Name) {
			continue
		}
		if !wantTag(suite.Tags) {
			continue
		}

		cases, err := c.loadSuiteCases(suite)
		if err != nil {
			return nil, fmt.Errorf("suite %q: %w", suite.Name, err)
		}

		plans = append(plans, Plan{
			Suite:   suite,
			Fixture: c.Fixture[suite.Fixture],
			Cases:   cases,
		})
	}
	return plans, nil
}

func (c *RootConfig) loadSuiteCases(suite SuiteConfig) ([]*v2case.Case, error) {
	seen := make(map[string]struct{}) // dedupe overlapping globs
	var cases []*v2case.Case
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
			tc, loadErr := v2case.Load(m)
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

// Summary renders a single line per plan suitable for `oats list`. It does
// not load any cases — useful for a dry-run before deciding to expand globs.
func (c *RootConfig) Summary() string {
	var out string
	for _, s := range c.Suites {
		out += fmt.Sprintf("suite=%s fixture=%s tags=%v cases=%v\n", s.Name, s.Fixture, s.Tags, s.Cases)
	}
	return out
}
