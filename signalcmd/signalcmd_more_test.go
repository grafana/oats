package signalcmd

import (
	"testing"
	"time"
)

func TestTraceGet(t *testing.T) {
	got := TraceGet("abc123", time.Minute)
	want := []string{"traces", "get", "--since", "1m0s", "-o", "json", "abc123"}
	if len(got) != len(want) {
		t.Fatalf("TraceGet = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TraceGet = %#v, want %#v", got, want)
		}
	}
}
