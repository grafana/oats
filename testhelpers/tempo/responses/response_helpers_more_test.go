package responses

import "testing"

func TestParseTempoSearchResult(t *testing.T) {
	result, err := ParseTempoSearchResult([]byte(`{"traces":[{"traceID":"abc","rootServiceName":"api"}]}`))
	if err != nil {
		t.Fatalf("ParseTempoSearchResult: %v", err)
	}
	if len(result.Traces) != 1 || result.Traces[0].TraceID != "abc" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := ParseTempoSearchResult([]byte("not json")); err == nil {
		t.Fatal("malformed result parsed successfully")
	}
}
