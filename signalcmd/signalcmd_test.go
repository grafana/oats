package signalcmd

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/v2case"
)

func TestTraces(t *testing.T) {
	got := Traces(v2case.TraceAssertion{TraceQL: `{ span.http.route = "/x" }`}, 0)
	want := []string{"traces", "search", "--since", "10m0s", `{ span.http.route = "/x" }`}
	if !equal(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestTraces_WithMatchAsksForJSON(t *testing.T) {
	got := Traces(v2case.TraceAssertion{
		TraceQL: `{ span.http.route = "/x" }`,
		AssertionCommon: v2case.AssertionCommon{
			Match: []v2case.MatchEntry{{Name: strPtr("GET /x")}},
		},
	}, 0)
	if !contains(got, "-o", "json") {
		t.Errorf("expected -o json in: %v", got)
	}
}

func TestLogs(t *testing.T) {
	got := Logs(v2case.LogAssertion{LogQL: `{service_name="x"}`}, 5*time.Minute)
	want := []string{"logs", "query", "--since", "5m0s", `{service_name="x"}`}
	if !equal(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestLogs_WithMatchAsksForJSON(t *testing.T) {
	got := Logs(v2case.LogAssertion{
		LogQL: `{service_name="x"}`,
		AssertionCommon: v2case.AssertionCommon{
			Match: []v2case.MatchEntry{{Name: strPtr("line")}},
		},
	}, 5*time.Minute)
	if !contains(got, "-o", "json") {
		t.Errorf("expected -o json in: %v", got)
	}
}

func TestMetrics_PromQLOnly(t *testing.T) {
	got := Metrics(v2case.MetricAssertion{PromQL: "up"}, time.Minute)
	want := []string{"metrics", "query", "--since", "1m0s", "up"}
	if !equal(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestMetrics_WithValueAsksForJSON(t *testing.T) {
	got := Metrics(v2case.MetricAssertion{PromQL: "rate(x[1m])", Value: ">= 0"}, time.Minute)
	// JSON output flag must appear before the positional PromQL.
	if !contains(got, "-o", "json") {
		t.Errorf("expected -o json in: %v", got)
	}
	if got[len(got)-1] != "rate(x[1m])" {
		t.Errorf("PromQL should be last positional: %v", got)
	}
}

func TestProfiles(t *testing.T) {
	got := Profiles(v2case.ProfileAssertion{Query: "process_cpu:cpu:nanoseconds:cpu:nanoseconds{}"}, 0)
	if got[0] != "profiles" || got[1] != "query" {
		t.Errorf("wrong verb chain: %v", got)
	}
}

func TestProfiles_WithMatchAsksForJSON(t *testing.T) {
	got := Profiles(v2case.ProfileAssertion{
		Query: "process_cpu:cpu:nanoseconds:cpu:nanoseconds{}",
		AssertionCommon: v2case.AssertionCommon{
			Match: []v2case.MatchEntry{{Name: strPtr("main")}},
		},
	}, 0)
	if !contains(got, "-o", "json") {
		t.Errorf("expected -o json in: %v", got)
	}
}

func TestRender_QuotesSpecialChars(t *testing.T) {
	args := []string{"traces", "search", `{ span.http.route = "/x" }`}
	rendered := Render(args)
	if !strings.HasPrefix(rendered, "gcx traces search '{") {
		t.Errorf("complex arg should be single-quoted: %s", rendered)
	}
}

func TestRender_LeavesBoringArgsBare(t *testing.T) {
	args := []string{"metrics", "query", "--since", "10m"}
	rendered := Render(args)
	if rendered != "gcx metrics query --since 10m" {
		t.Errorf("boring args should not be quoted: %s", rendered)
	}
}

func TestRender_EscapesSingleQuote(t *testing.T) {
	rendered := Render([]string{"echo", "it's"})
	// Embedded single quote escaped as '\''
	if !strings.Contains(rendered, `'\''`) {
		t.Errorf("missing quote escape: %s", rendered)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(haystack []string, needles ...string) bool {
	for i := 0; i+len(needles) <= len(haystack); i++ {
		match := true
		for j, n := range needles {
			if haystack[i+j] != n {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }
