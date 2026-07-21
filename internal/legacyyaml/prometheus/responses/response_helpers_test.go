package responses

import (
	"strings"
	"testing"
)

func TestParseQueryOutput(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantResult int
		wantErr    string
	}{
		{
			name: "valid response",
			body: `{
				"status": "success",
				"data": {
					"resultType": "vector",
					"result": [{
						"metric": {"__name__": "up", "job": "oats"},
						"value": [1700000000, "1"]
					}]
				}
			}`,
			wantResult: 1,
		},
		{
			name:    "malformed response",
			body:    `{"status":`,
			wantErr: "decoding Prometheus response:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseQueryOutput([]byte(tt.body))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseQueryOutput error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseQueryOutput: %v", err)
			}
			if len(got) != tt.wantResult {
				t.Fatalf("result count = %d, want %d", len(got), tt.wantResult)
			}
			if got[0].Metric["job"] != "oats" {
				t.Errorf("job metric = %q, want %q", got[0].Metric["job"], "oats")
			}
			if len(got[0].Value) != 2 || got[0].Value[1] != "1" {
				t.Errorf("value = %#v, want timestamp and sample value", got[0].Value)
			}
		})
	}
}
