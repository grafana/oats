// Package report carries OATS v2's observation channel.
//
// Every interesting moment in a run — a case started, an assertion failed, a
// fixture became ready — is published as an Event. A Reporter renders the
// stream for a particular audience: the compact text format for humans and
// agents alike (the default), or NDJSON for tooling that wants to consume
// events structurally.
//
// The same Event vocabulary feeds both renderers. The CI-annotation emitter
// is a thin layer over the text renderer that fires when GITHUB_ACTIONS=true.
package report

import "time"

// EventType is the canonical lifecycle vocabulary. New types should be added
// here and then handled by all renderers — emitting an unknown type must
// remain safe (renderers either drop it or render a neutral line).
type EventType string

const (
	EventRunStart        EventType = "run.start"
	EventRunEnd          EventType = "run.end"
	EventSuiteStart      EventType = "suite.start"
	EventSuiteEnd        EventType = "suite.end"
	EventFixtureStart    EventType = "fixture.start"
	EventFixtureReady    EventType = "fixture.ready"
	EventFixtureTeardown EventType = "fixture.teardown"
	EventCaseStart       EventType = "case.start"
	EventCasePass        EventType = "case.pass"
	EventCaseFail        EventType = "case.fail"
	EventCaseSkip        EventType = "case.skip"
	EventAssertFail      EventType = "assert.fail"
	EventGCXExec         EventType = "gcx.exec"
)

// SchemaVersion travels with each run.start event. Consumers pin to a
// version; we bump on any breaking change (key removed, key semantics
// changed). Additive changes are not breaking.
const SchemaVersion = 1

// Event is the single payload type that crosses the Reporter boundary.
// Fields are sparse on purpose — each event type populates only what it
// needs, and Reporters MUST tolerate missing values for fields outside an
// event's natural set.
//
// JSON tags drive NDJSON output. The TextReporter uses the same fields but
// formats them prose-side.
type Event struct {
	Type EventType `json:"event"`
	Ts   time.Time `json:"ts,omitempty"`

	// Set on run.start only.
	OatsVersion   string `json:"oats_version,omitempty"`
	SchemaVersion int    `json:"schema_version,omitempty"`

	// Identifying context.
	Suite       string `json:"suite,omitempty"`
	FixtureType string `json:"fixture_type,omitempty"`
	Case        string `json:"case,omitempty"`
	Source      string `json:"source,omitempty"`

	// Per-assertion / per-failure detail.
	Msg           string `json:"msg,omitempty"`
	Cmd           string `json:"cmd,omitempty"`
	StdoutExcerpt string `json:"stdout_excerpt,omitempty"`

	// Timing and aggregate counts.
	DurationMs int64 `json:"duration_ms,omitempty"`
	CaseCount  int   `json:"case_count,omitempty"`
	Pass       int   `json:"pass,omitempty"`
	Fail       int   `json:"fail,omitempty"`
	Skip       int   `json:"skip,omitempty"`
}

// Reporter consumes Events as they happen and flushes its final output when
// Close is called. Concrete implementations must be safe for sequential use
// (no concurrent Emit calls — the runner serialises them).
type Reporter interface {
	Emit(e Event)
	Close() error
}

// Verbosity controls how much detail the renderers emit. TextReporter uses
// it for pass/cmd/lifecycle chatter; NDJSON applies the same gates to
// case.pass, gcx.exec, and fixture lifecycle events.
type Verbosity int

const (
	// VerboseDefault prints failures plus the final summary. Pass events,
	// fixture lifecycle, and gcx exec details are silent.
	VerboseDefault Verbosity = iota
	// VerbosePasses adds one line per passing case.
	VerbosePasses
	// VerboseCmd adds the gcx invocation behind each assertion (pass or fail).
	VerboseCmd
	// VerboseAll adds fixture lifecycle and full gcx stdout / per-phase timing.
	VerboseAll
)
