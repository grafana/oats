package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTextReporter_SilentOnAllPass(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, VerboseDefault)
	r.Emit(Event{Type: EventRunStart, OatsVersion: "test"})
	r.Emit(Event{Type: EventCasePass, Case: "a"})
	r.Emit(Event{Type: EventCasePass, Case: "b"})
	r.Emit(Event{Type: EventRunEnd, DurationMs: 100})

	out := buf.String()
	if strings.Contains(out, "FAIL") {
		t.Errorf("no FAIL block expected on all-pass run:\n%s", out)
	}
	if !strings.Contains(out, "PASS 2/2") {
		t.Errorf("summary line missing:\n%s", out)
	}
}

func TestTextReporter_FailureBlockHasSourceAndCmd(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, VerboseDefault)
	r.Emit(Event{Type: EventRunStart})
	r.Emit(Event{Type: EventCaseStart, Case: "rolldice", Source: "examples/nodejs/oats.yaml:1"})
	r.Emit(Event{
		Type:   EventAssertFail,
		Case:   "rolldice",
		Source: "examples/nodejs/oats.yaml:8",
		Msg:    "TraceQL returned no results",
		Cmd:    "gcx traces search '{ span.http.route = \"/rolldice\" }'",
	})
	r.Emit(Event{Type: EventCaseFail, Case: "rolldice"})
	r.Emit(Event{Type: EventRunEnd})

	out := buf.String()
	if !strings.Contains(out, "FAIL rolldice  examples/nodejs/oats.yaml:8") {
		t.Errorf("FAIL header missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "TraceQL returned no results") {
		t.Errorf("failure message missing:\n%s", out)
	}
	if !strings.Contains(out, "gcx traces search") {
		t.Errorf("command missing:\n%s", out)
	}
	if !strings.Contains(out, "FAIL 0/1 (1 failed") {
		t.Errorf("summary line missing or wrong:\n%s", out)
	}
}

func TestTextReporter_GHAAnnotationsWhenEnabled(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	var buf bytes.Buffer
	r := NewTextReporter(&buf, VerboseDefault)
	r.Emit(Event{Type: EventRunStart})
	r.Emit(Event{
		Type:   EventAssertFail,
		Case:   "x",
		Source: "examples/nodejs/oats.yaml:8",
		Msg:    "oops",
	})
	// Same source twice — only one annotation should appear.
	r.Emit(Event{
		Type:   EventAssertFail,
		Case:   "x",
		Source: "examples/nodejs/oats.yaml:8",
		Msg:    "again",
	})
	r.Emit(Event{Type: EventCaseFail, Case: "x"})
	r.Emit(Event{Type: EventRunEnd})

	out := buf.String()
	expectedAnnotation := "::error file=examples/nodejs/oats.yaml,line=8::oops"
	if !strings.Contains(out, expectedAnnotation) {
		t.Errorf("expected annotation missing:\n%s", out)
	}
	if strings.Count(out, "::error file=") != 1 {
		t.Errorf("duplicate annotations not deduplicated:\n%s", out)
	}
}

func TestTextReporter_VerbosePassPrintsPasses(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, VerbosePasses)
	r.Emit(Event{Type: EventRunStart})
	r.Emit(Event{Type: EventCasePass, Case: "a"})
	r.Emit(Event{Type: EventRunEnd})

	if !strings.Contains(buf.String(), "PASS a\n") {
		t.Errorf("expected per-case PASS line:\n%s", buf.String())
	}
}

func TestTextReporter_VerboseAllPrintsFixtureLifecycle(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, VerboseAll)
	r.Emit(Event{Type: EventFixtureStart, Fixture: "local", DurationMs: 1})
	r.Emit(Event{Type: EventFixtureReady, Fixture: "local", DurationMs: 12})
	r.Emit(Event{Type: EventFixtureTeardown, Fixture: "local", DurationMs: 3})

	out := buf.String()
	for _, want := range []string{
		"[fixture local] fixture.start",
		"[fixture local] fixture.ready",
		"[fixture local] fixture.teardown",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected fixture lifecycle line %q in:\n%s", want, out)
		}
	}
}

func TestNDJSONReporter_EmitsOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	r := NewNDJSONReporter(&buf, VerboseDefault)
	r.Emit(Event{Type: EventRunStart, OatsVersion: "x", SchemaVersion: 1, Ts: time.Now()})
	r.Emit(Event{Type: EventCaseFail, Case: "rolldice", DurationMs: 1234})
	r.Emit(Event{Type: EventRunEnd, Pass: 0, Fail: 1})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), buf.String())
	}
	for _, line := range lines {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("invalid JSON line %q: %v", line, err)
		}
	}
}

func TestNDJSONReporter_FiltersPassByDefault(t *testing.T) {
	var buf bytes.Buffer
	r := NewNDJSONReporter(&buf, VerboseDefault)
	r.Emit(Event{Type: EventCasePass, Case: "a"})
	r.Emit(Event{Type: EventCaseFail, Case: "b"})

	if strings.Contains(buf.String(), `"case.pass"`) {
		t.Errorf("pass event leaked through default verbosity:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `"case.fail"`) {
		t.Errorf("fail event missing:\n%s", buf.String())
	}
}

func TestNDJSONReporter_EmitsFixtureLifecycleAtVerboseAll(t *testing.T) {
	var buf bytes.Buffer
	r := NewNDJSONReporter(&buf, VerboseAll)
	r.Emit(Event{Type: EventFixtureStart, Fixture: "local"})
	r.Emit(Event{Type: EventFixtureReady, Fixture: "local"})
	r.Emit(Event{Type: EventFixtureTeardown, Fixture: "local"})

	out := buf.String()
	for _, want := range []string{`"fixture.start"`, `"fixture.ready"`, `"fixture.teardown"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %s in NDJSON output:\n%s", want, out)
		}
	}
}

func TestSplitSource(t *testing.T) {
	cases := []struct {
		in       string
		wantFile string
		wantLine int
	}{
		{"a.yaml:42", "a.yaml", 42},
		{"a.yaml", "a.yaml", 0},
		{"path/with/colon:x/a.yaml:7", "path/with/colon:x/a.yaml", 7},
		{"a.yaml:notanint", "a.yaml:notanint", 0},
		{"", "", 0},
	}
	for _, tc := range cases {
		f, n := splitSource(tc.in)
		if f != tc.wantFile || n != tc.wantLine {
			t.Errorf("splitSource(%q): got (%q, %d), want (%q, %d)", tc.in, f, n, tc.wantFile, tc.wantLine)
		}
	}
}
