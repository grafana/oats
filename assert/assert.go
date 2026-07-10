// Package assert holds the vocabulary OATS uses to check gcx output.
//
// Each function takes the captured stdout (or a parsed structure derived from
// it) and an expectation, and returns a slice of failures — empty if the
// expectation held. Functions never panic; they never short-circuit on the
// first failure within a group. A case yaml that declares five Contains
// substrings reports all five misses in one run, not just the first.
//
// The shape mirrors the case yaml keys documented in docs/case-reference.md:
// contains, not_contains, regex, value, count, absent.
package assert

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/grafana/oats/casefile"
)

// Failure carries enough context to render a compact "FAIL <case>  <source>"
// block. The Detail is the message body; the package never formats source
// pointers itself — that's the renderer's job.
type Failure struct {
	Rule   string // "contains", "regex", "value", ...
	Detail string
}

func (f Failure) Error() string { return f.Rule + ": " + f.Detail }

// Row is the normalized structural unit used by collector-style `match`
// assertions. Depending on the signal type, Name is the primary field
// (`name` for traces, log body for logs, metric name for metrics) and
// Attributes carries labels/attributes associated with that row.
type Row struct {
	Name       string
	Attributes map[string]string
}

// Contains checks that each substring appears at least once in stdout.
func Contains(stdout string, substrings []string) []Failure {
	var fails []Failure
	for _, s := range substrings {
		if !strings.Contains(stdout, s) {
			fails = append(fails, Failure{
				Rule:   "contains",
				Detail: fmt.Sprintf("substring %q not found in stdout", s),
			})
		}
	}
	return fails
}

// NotContains checks that none of the substrings appear in stdout.
func NotContains(stdout string, substrings []string) []Failure {
	var fails []Failure
	for _, s := range substrings {
		if strings.Contains(stdout, s) {
			fails = append(fails, Failure{
				Rule:   "not_contains",
				Detail: fmt.Sprintf("substring %q unexpectedly present in stdout", s),
			})
		}
	}
	return fails
}

// Regex checks that each pattern matches stdout at least once. An invalid
// pattern is itself reported as a failure — case yamls don't get to fail
// silently on a bad regex.
func Regex(stdout string, patterns []string) []Failure {
	var fails []Failure
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			fails = append(fails, Failure{
				Rule:   "regex",
				Detail: fmt.Sprintf("invalid pattern %q: %v", p, err),
			})
			continue
		}
		if !re.MatchString(stdout) {
			fails = append(fails, Failure{
				Rule:   "regex",
				Detail: fmt.Sprintf("pattern %q did not match stdout", p),
			})
		}
	}
	return fails
}

// Value parses a numeric comparison expression ("> 0", ">= 1.5", "== 42",
// "< 100", "<= 0", "!= 0") and checks it against the supplied actual value.
// Used by metrics assertions; the actual value comes from parsing gcx
// metrics query's JSON output upstream of this function.
func Value(actual float64, expr string) []Failure {
	op, threshold, err := parseValueExpr(expr)
	if err != nil {
		return []Failure{{Rule: "value", Detail: err.Error()}}
	}
	if !applyComparison(actual, op, threshold) {
		return []Failure{{
			Rule:   "value",
			Detail: fmt.Sprintf("expected value %s, got %v", expr, actual),
		}}
	}
	return nil
}

// Count is Value's sibling for integer cardinality. Same operators, integer
// threshold only — "== 0", ">= 1", "< 10".
func Count(actual int, expr string) []Failure {
	// Delegate to Value via float64 — the parser is the same and integer
	// comparisons through float64 are exact for any count we'd plausibly
	// assert on.
	return retag(Value(float64(actual), expr), "count")
}

// Absent is a convenience over Count: it asserts the count is exactly zero.
// It is the spelling case-yaml authors use when "no traces matched" is the
// expectation.
func Absent(actual int) []Failure {
	if actual != 0 {
		return []Failure{{
			Rule:   "absent",
			Detail: fmt.Sprintf("expected zero results, got %d", actual),
		}}
	}
	return nil
}

// MatchRows checks that each collector-style match entry is satisfied by at
// least one row. Each entry is independent: one entry may match one row and
// the next entry a different row.
func MatchRows(rows []Row, entries []casefile.MatchEntry) []Failure {
	var fails []Failure
	for _, entry := range entries {
		if !anyRowMatches(rows, entry) {
			fails = append(fails, Failure{
				Rule:   "match",
				Detail: fmt.Sprintf("no row matched %s", describeMatch(entry)),
			})
		}
	}
	return fails
}

func anyRowMatches(rows []Row, entry casefile.MatchEntry) bool {
	for _, row := range rows {
		if rowMatches(row, entry) {
			return true
		}
	}
	return false
}

func rowMatches(row Row, entry casefile.MatchEntry) bool {
	matchType := entry.EffectiveMatchType()
	if entry.Name != nil {
		if !matchesValue(row.Name, *entry.Name, matchType) {
			return false
		}
	}
	for _, expected := range entry.Attributes {
		actual, ok := row.Attributes[expected.Key]
		if expected.Value == nil {
			if !ok {
				return false
			}
			continue
		}
		if !ok {
			return false
		}
		if !matchesValue(actual, *expected.Value, matchType) {
			return false
		}
	}
	// All specified constraints held. This is never vacuously true: casefile's
	// validateMatchEntries rejects an entry with no name and no attributes, so
	// a row reaching here has matched at least one real constraint.
	return true
}

func matchesValue(actual, expected string, matchType casefile.MatchType) bool {
	switch matchType {
	case casefile.MatchTypeRegexp:
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(actual)
	default:
		return actual == expected
	}
}

func describeMatch(entry casefile.MatchEntry) string {
	var parts []string
	if entry.MatchType != "" {
		parts = append(parts, fmt.Sprintf("match_type=%s", entry.MatchType))
	}
	if entry.Name != nil {
		parts = append(parts, fmt.Sprintf("name=%q", *entry.Name))
	}
	for _, expected := range entry.Attributes {
		switch expected.Value {
		case nil:
			parts = append(parts, fmt.Sprintf("attribute %s present", expected.Key))
		default:
			parts = append(parts, fmt.Sprintf("attribute %s=%q", expected.Key, *expected.Value))
		}
	}
	if len(parts) == 0 {
		return "empty match entry"
	}
	return strings.Join(parts, ", ")
}

// retag relabels a failure set's Rule field. Count and Absent delegate their
// numeric comparison to Value, then retag the resulting "value" failures as
// "count"/"absent" so the reported rule matches the assertion the author wrote.
func retag(fails []Failure, rule string) []Failure {
	for i := range fails {
		fails[i].Rule = rule
	}
	return fails
}

func parseValueExpr(expr string) (op string, threshold float64, err error) {
	expr = strings.TrimSpace(expr)
	// Longest operators first so ">=" is matched before ">".
	for _, candidate := range []string{">=", "<=", "==", "!=", ">", "<"} {
		if strings.HasPrefix(expr, candidate) {
			numStr := strings.TrimSpace(strings.TrimPrefix(expr, candidate))
			n, parseErr := strconv.ParseFloat(numStr, 64)
			if parseErr != nil {
				return "", 0, fmt.Errorf("invalid numeric threshold in %q: %v", expr, parseErr)
			}
			return candidate, n, nil
		}
	}
	return "", 0, fmt.Errorf("expected comparison operator (>=, <=, ==, !=, >, <) at start of %q", expr)
}

func applyComparison(actual float64, op string, threshold float64) bool {
	switch op {
	case ">=":
		return actual >= threshold
	case "<=":
		return actual <= threshold
	case "==":
		return actual == threshold
	case "!=":
		return actual != threshold
	case ">":
		return actual > threshold
	case "<":
		return actual < threshold
	}
	return false
}
