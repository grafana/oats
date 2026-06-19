// Package signalcmd translates a single v2case assertion into the gcx
// command line that will execute the corresponding query.
//
// One function per signal type. Each returns []string suitable for handing
// to engine.GCX.Execute: positional args only, no --context (engine prepends
// that), no datasource flag (resolved from the gcx config context).
//
// The output format is fixed to gcx's default "agents" text mode for
// substring assertions; JSON (-o json) for assertions that need a
// structured value extracted (metrics' `value` key).
package signalcmd

import (
	"strings"
	"time"

	"github.com/grafana/oats/v2case"
)

// Defaults applied when a case does not pin its own values.
const (
	DefaultSince = 10 * time.Minute
)

// Traces builds the gcx args for a TraceAssertion.
func Traces(a v2case.TraceAssertion, since time.Duration) []string {
	if since <= 0 {
		since = DefaultSince
	}
	args := []string{
		"traces", "search",
		"--since", since.String(),
	}
	if len(a.MatchSpans) > 0 {
		args = append(args, "-o", "json")
	}
	args = append(args, a.TraceQL)
	return args
}

// Logs builds the gcx args for a LogAssertion.
func Logs(a v2case.LogAssertion, since time.Duration) []string {
	if since <= 0 {
		since = DefaultSince
	}
	args := []string{
		"logs", "query",
		"--since", since.String(),
	}
	if len(a.Match) > 0 {
		args = append(args, "-o", "json")
	}
	args = append(args, a.LogQL)
	return args
}

// Metrics builds the gcx args for a MetricAssertion. When the assertion
// declares a numeric `value` comparison we ask gcx for JSON so the runner
// can parse the actual value out; otherwise the default agent text format
// is enough for substring matching.
func Metrics(a v2case.MetricAssertion, since time.Duration) []string {
	if since <= 0 {
		since = DefaultSince
	}
	args := []string{
		"metrics", "query",
		"--since", since.String(),
	}
	if a.Value != "" || len(a.Match) > 0 {
		args = append(args, "-o", "json")
	}
	args = append(args, a.PromQL)
	return args
}

// Profiles builds the gcx args for a ProfileAssertion.
func Profiles(a v2case.ProfileAssertion, since time.Duration) []string {
	if since <= 0 {
		since = DefaultSince
	}
	profileType, expr := splitProfileQuery(a.Query)
	args := []string{
		"profiles", "query",
		"--since", since.String(),
	}
	if len(a.Match) > 0 {
		args = append(args, "-o", "json")
	}
	if profileType != "" {
		args = append(args, "--profile-type", profileType)
	}
	args = append(args, expr)
	return args
}

func splitProfileQuery(query string) (profileType string, expr string) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", "{}"
	}
	if i := strings.Index(q, "{"); i >= 0 {
		profileType = strings.TrimSpace(q[:i])
		expr = strings.TrimSpace(q[i:])
		if expr == "" {
			expr = "{}"
		}
		return profileType, expr
	}
	if strings.Contains(q, ":") {
		return q, "{}"
	}
	return "", q
}

// Render is a convenience used by the report layer to show the gcx
// invocation in a FAIL block. It mirrors what a shell would print.
func Render(args []string) string {
	out := "gcx"
	for _, a := range args {
		out += " " + shellQuote(a)
	}
	return out
}

// shellQuote is intentionally tiny: TraceQL / PromQL / LogQL strings often
// contain spaces and quotes that need to survive a copy-paste from a FAIL
// block. We single-quote anything that's not "boring."
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '=':
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	// Single-quote and escape any embedded single quotes.
	quoted := "'"
	for _, c := range s {
		if c == '\'' {
			quoted += `'\''`
		} else {
			quoted += string(c)
		}
	}
	quoted += "'"
	return quoted
}
