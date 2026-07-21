package runner

import (
	"strings"
	"testing"

	"github.com/grafana/oats/casefile"
)

func TestExtractMetricRowsBranches(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		wantErr string
		wantVal float64
		wantN   int
	}{
		{
			name:    "empty",
			wantErr: "empty result",
		},
		{
			name:    "malformed JSON",
			stdout:  "not json",
			wantErr: "metric JSON parse",
		},
		{
			name:    "empty vector",
			stdout:  `{"data":{"result":[]}}`,
			wantErr: "empty result",
		},
		{
			name:    "vector",
			stdout:  `{"data":{"result":[{"metric":{"__name__":"up","job":"oats"},"value":[1,"2.5"]}]}}`,
			wantVal: 2.5,
			wantN:   1,
		},
		{
			name:    "matrix fallback",
			stdout:  `{"data":{"result":[{"metric":{},"values":[[1,"3"]]}]}}`,
			wantVal: 3,
			wantN:   1,
		},
		{
			name:    "missing scalar",
			stdout:  `{"data":{"result":[{"metric":{}}]}}`,
			wantErr: "no scalar value",
			wantN:   1,
		},
		{
			name:    "invalid scalar",
			stdout:  `{"data":{"result":[{"metric":{},"value":[1,"wat"]}]}}`,
			wantErr: "is not a number",
			wantN:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, count, value, err := extractMetricRows(tt.stdout)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("extractMetricRows: %v", err)
			}
			if count != tt.wantN {
				t.Errorf("count = %d, want %d", count, tt.wantN)
			}
			if tt.wantErr == "" && value != tt.wantVal {
				t.Errorf("value = %v, want %v", value, tt.wantVal)
			}
			if tt.wantN > 0 && len(rows) != tt.wantN {
				t.Errorf("rows = %d, want %d", len(rows), tt.wantN)
			}
		})
	}
}

func TestExtractLogRowsBranches(t *testing.T) {
	rows, count, err := extractLogRows("")
	if err != nil || rows != nil || count != 0 {
		t.Fatalf("empty logs = %#v, %d, %v", rows, count, err)
	}

	rows, count, err = extractLogRows(`{"data":{"result":[{"stream":{"service":"api"},"values":[["1","ready"],["2"]]}]}}`)
	if err != nil {
		t.Fatalf("extractLogRows: %v", err)
	}
	if count != 2 || rows[0].Name != "ready" || rows[1].Name != "" || rows[0].Attributes["service"] != "api" {
		t.Fatalf("unexpected log rows: %#v count=%d", rows, count)
	}
	if _, _, err := extractLogRows("not json"); err == nil || !strings.Contains(err.Error(), "log JSON parse") {
		t.Fatalf("malformed log error = %v", err)
	}
}

func TestExtractTraceRowsFallbacks(t *testing.T) {
	rows, count, err := extractTraceRows("")
	if err != nil || rows != nil || count != 0 {
		t.Fatalf("empty traces = %#v, %d, %v", rows, count, err)
	}

	stdout := `{"data":{"result":[{"spanName":"operation","attributes":{"http.method":"GET"},"resource":{"attributes":{"service.name":"api"}},"rootServiceName":"api","traceID":"abc"}]}}`
	rows, count, err = extractTraceRows(stdout)
	if err != nil {
		t.Fatalf("extractTraceRows: %v", err)
	}
	if count != 1 || len(rows) == 0 {
		t.Fatalf("fallback rows = %#v count=%d", rows, count)
	}
	if rows[0].Name != "operation" || rows[0].Attributes["service.name"] != "api" || rows[0].Attributes["trace_id"] != "abc" {
		t.Fatalf("unexpected fallback row: %#v", rows[0])
	}
	if _, _, err := extractTraceRows("not json"); err == nil || !strings.Contains(err.Error(), "trace JSON parse") {
		t.Fatalf("malformed trace error = %v", err)
	}
}

func TestExtractTraceIDs(t *testing.T) {
	ids, count, err := extractTraceIDs("")
	if err != nil || ids != nil || count != 0 {
		t.Fatalf("empty trace IDs = %#v, %d, %v", ids, count, err)
	}
	ids, count, err = extractTraceIDs(`{"traces":[{"traceID":"a"},{"traceID":""},{"traceID":"b"}]}`)
	if err != nil || strings.Join(ids, ",") != "a,b" || count != 3 {
		t.Fatalf("trace IDs = %#v, %d, %v", ids, count, err)
	}
	if _, _, err := extractTraceIDs("not json"); err == nil || !strings.Contains(err.Error(), "trace JSON parse") {
		t.Fatalf("malformed trace IDs error = %v", err)
	}
}

func TestExtractProfileRowsBranches(t *testing.T) {
	for name, input := range map[string]string{
		"direct flamegraph": `{"flamegraph":{"names":["root","worker",""]}}`,
		"nested flamegraph": `{"data":{"flamegraph":{"names":["root"]}}}`,
		"named rows":        `{"rows":[{"name":"profile-row"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			rows, count, err := extractProfileRows(input)
			if err != nil || count != len(rows) || count == 0 {
				t.Fatalf("rows = %#v count=%d err=%v", rows, count, err)
			}
		})
	}
	rows, count, err := extractProfileRows("")
	if err != nil || rows != nil || count != 0 {
		t.Fatalf("empty profiles = %#v, %d, %v", rows, count, err)
	}
	if _, _, err := extractProfileRows("not json"); err == nil || !strings.Contains(err.Error(), "profile JSON parse") {
		t.Fatalf("malformed profile error = %v", err)
	}
}

func TestParseOTelHelpers(t *testing.T) {
	root := map[string]any{
		"trace": map[string]any{
			"batches": []any{
				map[string]any{
					"resource": map[string]any{"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "api"}},
					}},
					"scopeSpans": []any{map[string]any{
						"scope": map[string]any{"name": "instrumentation"},
						"spans": []any{map[string]any{
							"name": "operation",
							"kind": 2,
							"attributes": []any{
								map[string]any{"key": "count", "value": map[string]any{"intValue": 3}},
								map[string]any{"key": "flags", "value": map[string]any{"arrayValue": map[string]any{"values": []any{
									map[string]any{"boolValue": true},
									map[string]any{"doubleValue": 1.5},
								}}}},
							},
						}},
					}},
				},
			},
		},
	}
	rows, ok := extractOTLPTraceRows(root)
	if !ok || len(rows) != 1 {
		t.Fatalf("extractOTLPTraceRows = %#v, %v", rows, ok)
	}
	if rows[0].Attributes["service.name"] != "api" || rows[0].Attributes["count"] != "3" || rows[0].Attributes["flags"] != "true,1.5" || rows[0].Attributes["kind"] != "2" {
		t.Fatalf("OTLP attributes = %#v", rows[0].Attributes)
	}

	if got := stringifyMapAny("not a map"); len(got) != 0 {
		t.Fatalf("stringifyMapAny non-map = %#v", got)
	}
	if rows, ok := extractOTLPTraceRows(map[string]any{"resourceSpans": []any{}}); ok || rows != nil {
		t.Fatalf("empty OTLP rows = %#v, %v", rows, ok)
	}
}

func TestToSeedPayload(t *testing.T) {
	payload, err := toSeedPayload(casefile.Seed{
		Traces:  []casefile.SeedTrace{{Service: "api", Spans: []casefile.SeedSpan{{Name: "op", Duration: "2ms"}}}},
		Logs:    []casefile.SeedLog{{Service: "api", Body: "ready", SeverityNumber: 9, SeverityText: "INFO"}},
		Metrics: []casefile.SeedMetric{{Service: "api", Name: "requests", Value: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Traces) != 1 || payload.Traces[0].Span.Duration.String() != "2ms" || len(payload.Logs) != 1 || len(payload.Metrics) != 1 {
		t.Fatalf("unexpected seed payload: %+v", payload)
	}
	if _, err := toSeedPayload(casefile.Seed{Traces: []casefile.SeedTrace{{Spans: []casefile.SeedSpan{{Name: "op", Duration: "bad"}}}}}); err == nil || !strings.Contains(err.Error(), "invalid duration") {
		t.Fatalf("invalid duration error = %v", err)
	}
}
