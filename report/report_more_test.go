package report

import "testing"

func TestReportersClose(t *testing.T) {
	if err := NewTextReporter(nil, VerboseDefault).Close(); err != nil {
		t.Fatalf("TextReporter.Close: %v", err)
	}
	if err := NewNDJSONReporter(nil, VerboseDefault).Close(); err != nil {
		t.Fatalf("NDJSONReporter.Close: %v", err)
	}
}
