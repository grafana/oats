package runner

import (
	"strings"
	"testing"
)

func TestExtractLogRows(t *testing.T) {
	stdout := `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"service_name":"svc"},"values":[{"timestamp":"1700000000","line":"first","structuredMetadata":{"level":"info"}},{"timestamp":"1700000001","line":"second","parsed":{"request_id":"abc123"}}]}]}}`

	rows, count, err := extractLogRows(stdout)
	if err != nil {
		t.Fatalf("extractLogRows: %v", err)
	}
	if count != 2 || len(rows) != 2 {
		t.Fatalf("expected 2 rows, got count=%d len=%d", count, len(rows))
	}
	if rows[0].Name != "first" || rows[0].Attributes["service_name"] != "svc" || rows[0].Attributes["level"] != "info" {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[1].Name != "second" || rows[1].Attributes["service_name"] != "svc" || rows[1].Attributes["request_id"] != "abc123" {
		t.Fatalf("unexpected second row: %#v", rows[1])
	}
	if _, ok := rows[0].Attributes["request_id"]; ok {
		t.Fatal("second row's parsed attributes leaked into the first row")
	}
}

func TestExtractLogRowsRejectsMissingResult(t *testing.T) {
	_, _, err := extractLogRows(`{"status":"success","data":{"resultType":"streams"}}`)
	if err == nil || !strings.Contains(err.Error(), "data.result is missing") {
		t.Fatalf("extractLogRows error = %v, want missing-result guidance", err)
	}
}

func TestExtractLogRowsRejectsChangedValueShape(t *testing.T) {
	stdout := `{"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[["1700000000","first"]]}]}}`
	_, _, err := extractLogRows(stdout)
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("extractLogRows error = %v, want unsupported-format guidance", err)
	}
}
