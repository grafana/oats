// Package discovery turns an oats-config.yaml + a collection of case yamls into
// concrete run plans.
//
// In OATS v1, the runner walked the file system for any yaml carrying
// "oats-schema-version" and ran whatever it found. The current format declares
// the cases up front: oats-config.yaml lists case files (path globs). Each case
// carries its own fixture; discovery groups cases that share one fixture into a
// single plan so the (often expensive) fixture boots once and every case in the
// group runs against it. Distinct plans are independent and may run in parallel
// where fixture isolation allows. "oats list" prints the plans before "oats run"
// executes them.
package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/grafana/oats/casefile"
	"go.yaml.in/yaml/v3"
)

// RootConfig is the parsed oats-config.yaml file. Field names mirror the YAML keys
// directly so a misnamed key surfaces as a "field not defined" error from
// the yaml decoder rather than a silent miss.
type RootConfig struct {
	Meta  Meta        `yaml:"meta"`
	Cases []string    `yaml:"cases"` // path globs, relative to oats-config.yaml dir
	Cache CacheConfig `yaml:"cache,omitempty"`

	// SourceDir is the directory of the loaded oats-config.yaml. Case glob
	// expressions resolve relative to it.
	SourceDir string `yaml:"-"`
}

type Meta struct {
	Version int `yaml:"version"`
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

// Validate checks structural rules the yaml parser cannot. Called by Load;
// exposed for tests that construct RootConfigs in memory.
func (c *RootConfig) Validate() error {
	if c.Meta.Version != SupportedVersion {
		return fmt.Errorf("meta.version: expected %d, got %d", SupportedVersion, c.Meta.Version)
	}
	if len(c.Cases) == 0 {
		return fmt.Errorf("cases: at least one case glob is required")
	}
	for i, path := range c.Cases {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("cases[%d]: path is required and non-empty", i)
		}
	}
	return nil
}

// Filter describes which cases to include in the run. Empty fields impose no
// restriction. Tag filtering uses any-match semantics: a case passes if any of
// its tags appears in the filter list.
type Filter struct {
	Tags []string // any-match against each case's tags
	// Paths restricts the run to cases at (or under) these absolute file/dir
	// paths. Empty = no path restriction. Backs the positional `oats <path>…`
	// scoping, which selects which cases run without changing where the config
	// is loaded from.
	Paths []string
}

// Plan is a fixture-boot group: one fixture plus the cases that share it. The
// fixture boots once and the plan's cases run serially against it; distinct
// plans are independent and may run in parallel where fixture isolation allows.
type Plan struct {
	Name             string   // derived label for reporting/filtering output
	Tags             []string // union of the member cases' tags (sorted)
	Fixture          casefile.FixtureConfig
	FixtureSourceDir string
	Cases            []*casefile.Case
}

// PlanRun loads the configured cases, applies the filter, and groups the
// survivors into plans by fixture identity. Plans are returned in the order
// their fixtures first appear; cases are sorted by SourcePath for stable
// ordering, so a plan's cases keep that order too.
func (c *RootConfig) PlanRun(f Filter) ([]Plan, error) {
	cases, err := c.loadCases()
	if err != nil {
		return nil, err
	}
	cases = keepCasesUnderPaths(cases, f.Paths)
	cases = keepCasesWithTags(cases, f.Tags)
	if len(cases) == 0 {
		return nil, nil
	}

	// Group by fixture identity, preserving first-appearance order.
	var order []string
	groups := map[string][]*casefile.Case{}
	fixtures := map[string]casefile.FixtureConfig{}
	dirs := map[string]string{}
	for _, tc := range cases {
		fx, dir := c.fixtureFor(tc)
		key := groupKey(fx, dir)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
			fixtures[key] = fx
			dirs[key] = dir
		}
		groups[key] = append(groups[key], tc)
	}

	plans := make([]Plan, 0, len(order))
	for _, key := range order {
		gcs := groups[key]
		plans = append(plans, Plan{
			Name:             deriveGroupName(fixtures[key], gcs),
			Tags:             unionTags(gcs),
			Fixture:          fixtures[key],
			FixtureSourceDir: dirs[key],
			Cases:            gcs,
		})
	}
	return plans, nil
}

// loadCases expands every glob in Cases (relative to SourceDir), dedupes
// overlapping matches, loads each case, and returns them sorted by SourcePath.
func (c *RootConfig) loadCases() ([]*casefile.Case, error) {
	seen := make(map[string]struct{})
	var cases []*casefile.Case
	for _, pattern := range c.Cases {
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

// fixtureFor returns the case's fixture (or the default lgtm compose when the
// case declares none) and the directory its relative paths resolve against.
func (c *RootConfig) fixtureFor(tc *casefile.Case) (casefile.FixtureConfig, string) {
	if tc.Fixture == nil {
		// No fixture: default to a compose fixture with the builtin lgtm
		// template (an empty ComposeFixture resolves to template=lgtm). This
		// boots just the lgtm stack, which is handy for inline-otlp cases.
		// Temp compose files land next to the config.
		return casefile.FixtureConfig{Compose: &casefile.ComposeFixture{}}, c.SourceDir
	}
	return *tc.Fixture, filepath.Dir(tc.SourcePath)
}

// groupKey identifies the fixture-boot group a case belongs to. Cases with
// deep-equal fixtures share one boot; a fixture that resolves files relative to
// its directory (compose file/files, k3d manifests) additionally keys on that
// directory, since the same relative path means different files in different
// directories. A path-less fixture (remote, or a template-only compose) keys on
// the fixture alone, so identical copies in different directories share one boot.
func groupKey(f casefile.FixtureConfig, dir string) string {
	b, _ := json.Marshal(f)
	key := string(b)
	if f.UsesRelativePaths() {
		key += "\x00" + dir
	}
	return key
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

// keepCasesWithTags returns the cases with at least one tag in tags (any-match).
func keepCasesWithTags(cases []*casefile.Case, tags []string) []*casefile.Case {
	if len(tags) == 0 {
		return cases
	}
	var kept []*casefile.Case
	for _, tc := range cases {
		for _, want := range tags {
			if slices.Contains(tc.Tags, want) {
				kept = append(kept, tc)
				break
			}
		}
	}
	return kept
}

func unionTags(cases []*casefile.Case) []string {
	seen := make(map[string]struct{})
	var tags []string
	for _, tc := range cases {
		for _, t := range tc.Tags {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			tags = append(tags, t)
		}
	}
	sort.Strings(tags)
	return tags
}

// deriveGroupName produces a readable label for a plan. A single case uses its
// name (or its directory); multiple cases use their common directory, falling
// back to the fixture kind and count.
func deriveGroupName(f casefile.FixtureConfig, cases []*casefile.Case) string {
	if len(cases) == 1 {
		if n := strings.TrimSpace(cases[0].Name); n != "" {
			return n
		}
		if lbl := dirLabel(filepath.Dir(cases[0].SourcePath)); lbl != "" {
			return lbl
		}
	}
	if lbl := dirLabel(commonDir(cases)); lbl != "" {
		return lbl
	}
	kind := f.Kind()
	if kind == "" {
		kind = "cases"
	}
	return fmt.Sprintf("%s (%d cases)", kind, len(cases))
}

// commonDir returns the longest directory prefix shared by every case's source
// file, or "" if they share none.
func commonDir(cases []*casefile.Case) string {
	if len(cases) == 0 {
		return ""
	}
	parts := strings.Split(filepath.Dir(cases[0].SourcePath), string(filepath.Separator))
	for _, tc := range cases[1:] {
		other := strings.Split(filepath.Dir(tc.SourcePath), string(filepath.Separator))
		n := len(parts)
		if len(other) < n {
			n = len(other)
		}
		i := 0
		for i < n && parts[i] == other[i] {
			i++
		}
		parts = parts[:i]
	}
	return strings.Join(parts, string(filepath.Separator))
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

// Summary renders one line per plan for `oats list`.
func Summary(plans []Plan) string {
	var out strings.Builder
	for _, p := range plans {
		names := make([]string, len(p.Cases))
		for i, tc := range p.Cases {
			names[i] = tc.Name
		}
		fmt.Fprintf(&out, "plan=%s fixture=%s tags=%v cases=%v\n", p.Name, p.Fixture.Kind(), p.Tags, names)
	}
	return out.String()
}
