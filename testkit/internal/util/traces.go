package util

import (
	"bufio"
	"embed"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

// ReadTraces unmarshals trace data from the filesystem and returns the
// resulting []ptrace.Traces. The traceFS argument should represent an
// embedded data directory containing traces in JSON format under
// testdata/traces/*.json rooted in the test suite directory. The JSON files
// should include a single serialized ptrace.Trace per line.
func ReadTraces(traceFS *embed.FS, name string) ([]ptrace.Traces, error) {
	// trace input for test
	f, err := traceFS.Open("testdata/traces/" + name + ".json")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var traces []ptrace.Traces
	scanner := bufio.NewScanner(f)
	u := ptrace.JSONUnmarshaler{}
	for scanner.Scan() {
		t, err := u.UnmarshalTraces(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		traces = append(traces, t)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return traces, nil
}
