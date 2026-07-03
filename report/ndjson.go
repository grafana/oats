package report

import (
	"encoding/json"
	"io"
)

// NDJSONReporter emits one JSON object per Event on its own line. Suitable
// for tooling that wants to consume events structurally. Pass and gcx.exec
// events are filtered by Verbosity in the same spirit as TextReporter so the
// token cost of "everything went fine" remains zero.
type NDJSONReporter struct {
	w   io.Writer
	enc *json.Encoder
	v   Verbosity
}

func NewNDJSONReporter(w io.Writer, v Verbosity) *NDJSONReporter {
	enc := json.NewEncoder(w)
	// Compact form: no HTML escaping, no extra whitespace beyond the
	// trailing newline json.Encoder emits per Encode.
	enc.SetEscapeHTML(false)
	return &NDJSONReporter{w: w, enc: enc, v: v}
}

func (r *NDJSONReporter) Emit(e Event) {
	if !r.shouldEmit(e) {
		return
	}
	// Failure to write the stream is silent on purpose: a downstream pipe
	// that died should not crash the runner. The terminal exit status still
	// reflects the test outcome.
	_ = r.enc.Encode(e)
}

func (r *NDJSONReporter) Close() error { return nil }

func (r *NDJSONReporter) shouldEmit(e Event) bool {
	switch e.Type {
	case EventCasePass:
		return r.v >= VerbosePasses
	case EventGCXExec:
		return r.v >= VerboseCmd
	case EventFixtureStart, EventFixtureReady, EventFixtureTeardown:
		return r.v >= VerboseAll
	}
	return true
}
