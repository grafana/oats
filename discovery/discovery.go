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
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/grafana/oats/v2case"
)

// RootConfig is the parsed oats.toml file. Field names mirror the TOML keys
// directly so a misnamed key surfaces as a "field not defined" error from
// the toml decoder rather than a silent miss.
type RootConfig struct {
	Meta    Meta                     `toml:"meta"`
	Cases   []string                 `toml:"cases"`
	Suites  []SuiteConfig            `toml:"suite"`
	Fixture map[string]FixtureConfig `toml:"fixture"`
	Cache   CacheConfig              `toml:"cache,omitempty"`

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
	Type             string   `toml:"type"` // "compose" | "k3d" | "remote"
	Template         string   `toml:"template,omitempty"`
	ComposeFile      string   `toml:"compose_file,omitempty"`
	ComposeFiles     []string `toml:"compose_files,omitempty"`
	Env              []string `toml:"env,omitempty"`
	K8sDir           string   `toml:"k8s_dir,omitempty"`
	AppService       string   `toml:"app_service,omitempty"`
	AppDockerFile    string   `toml:"app_docker_file,omitempty"`
	AppDockerContext string   `toml:"app_docker_context,omitempty"`
	AppDockerTag     string   `toml:"app_docker_tag,omitempty"`
	AppPort          int      `toml:"app_port,omitempty"`
	ImportImages     []string `toml:"import_images,omitempty"`
	PoolSize         int      `toml:"pool_size,omitempty"`
	Endpoint         string   `toml:"endpoint,omitempty"` // remote only
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
	if len(c.Cases) > 0 && len(c.Suites) > 0 {
		return fmt.Errorf("use top-level cases or [[suite]], not both")
	}
	if len(c.Cases) == 0 && len(c.Suites) == 0 {
		return fmt.Errorf("at least one top-level case or [[suite]] required")
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
		switch f.Type {
		case "compose":
			if f.Template == "" && f.ComposeFile == "" && len(f.ComposeFiles) == 0 {
				return fmt.Errorf("fixture %q: type=compose requires template, compose_file, or compose_files", name)
			}
			if f.ComposeFile != "" && len(f.ComposeFiles) > 0 {
				return fmt.Errorf("fixture %q: use compose_file or compose_files, not both", name)
			}
		case "k3d":
			if f.K8sDir == "" || f.AppService == "" || f.AppDockerFile == "" || f.AppDockerTag == "" || f.AppPort == 0 {
				return fmt.Errorf("fixture %q: type=k3d requires k8s_dir, app_service, app_docker_file, app_docker_tag, and app_port", name)
			}
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
	Suite            SuiteConfig
	Fixture          FixtureConfig
	FixtureSourceDir string
	Cases            []*v2case.Case
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
	for _, suite := range c.effectiveSuites() {
		cases, err := c.loadSuiteCases(suite)
		if err != nil {
			return nil, fmt.Errorf("suite %q: %w", suiteLabel(suite), err)
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

func materializeSuite(s SuiteConfig, cases []*v2case.Case) SuiteConfig {
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

func deriveSuiteName(s SuiteConfig, cases []*v2case.Case) string {
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

func (c *RootConfig) resolveSuiteFixture(suite SuiteConfig, cases []*v2case.Case) (FixtureConfig, string, error) {
	if suite.Fixture != "" {
		return c.Fixture[suite.Fixture], c.SourceDir, nil
	}
	var (
		fixture   FixtureConfig
		sourceDir string
		seen      bool
	)
	for _, tc := range cases {
		if tc.Fixture == nil {
			continue
		}
		next := fixtureConfigFromCase(*tc.Fixture)
		nextSourceDir := filepath.Dir(tc.SourcePath)
		if !seen {
			fixture, sourceDir, seen = next, nextSourceDir, true
			continue
		}
		if !reflect.DeepEqual(fixture, next) || sourceDir != nextSourceDir {
			return FixtureConfig{}, "", fmt.Errorf("suite omits fixture but cases do not agree on one shared fixture")
		}
	}
	if !seen {
		return FixtureConfig{}, "", nil
	}
	return fixture, sourceDir, nil
}

func fixtureConfigFromCase(f v2case.FixtureConfig) FixtureConfig {
	return FixtureConfig{
		Type:             f.Type,
		Template:         f.Template,
		ComposeFile:      f.ComposeFile,
		ComposeFiles:     append([]string(nil), f.ComposeFiles...),
		Env:              append([]string(nil), f.Env...),
		K8sDir:           f.K8sDir,
		AppService:       f.AppService,
		AppDockerFile:    f.AppDockerFile,
		AppDockerContext: f.AppDockerContext,
		AppDockerTag:     f.AppDockerTag,
		AppPort:          f.AppPort,
		ImportImages:     append([]string(nil), f.ImportImages...),
		PoolSize:         f.PoolSize,
		Endpoint:         f.Endpoint,
	}
}

// Summary renders a single line per plan suitable for `oats list`. It does
// not load any cases — useful for a dry-run before deciding to expand globs.
func (c *RootConfig) Summary() string {
	var out string
	for _, s := range c.Suites {
		label := s.Name
		if label == "" {
			label = suiteLabel(s)
		}
		out += fmt.Sprintf("suite=%s fixture=%s tags=%v cases=%v\n", label, s.Fixture, s.Tags, s.Cases)
	}
	return out
}
