// gcx output parsers: turn gcx JSON (and text-mode) responses for each signal
// into assert.Row slices and counts, including OTLP and Pyroscope shapes.
package runner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grafana/oats/assert"
)

// approxRowCount counts non-empty, non-banner output lines in gcx text mode.
// It is intentionally approximate — gcx's row-counting story will mature as
// we use it, and a future enhancement can swap this for a structured-output
// path. For now, "did anything come back?" is enough for absent / count.
func approxRowCount(stdout string) int {
	n := 0
	for _, line := range strings.Split(stdout, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "hint: use --json") {
			continue
		}
		if t == "No data" {
			continue
		}
		// Skip lines that look like table-headers / dividers in gcx text mode.
		if strings.HasPrefix(t, "─") || strings.HasPrefix(t, "═") || strings.HasPrefix(t, "+") || looksLikeGCXHeader(t) {
			continue
		}
		n++
	}
	return n
}

func looksLikeGCXHeader(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	for _, f := range fields {
		hasLetter := false
		for _, r := range f {
			switch {
			case r >= 'A' && r <= 'Z':
				hasLetter = true
			case r >= '0' && r <= '9':
			case r == '_' || r == '.' || r == '/':
			default:
				return false
			}
		}
		if !hasLetter {
			return false
		}
	}
	return true
}

// extractMetricValue parses the first numeric data point out of `gcx metrics
// query -o json` output. The schema follows gcx's JSON shape; we only look
// at the fields we need so additions don't break us.
func extractMetricRows(stdout string) ([]assert.Row, int, float64, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, 0, fmt.Errorf("metric value parse: empty result")
	}
	var generic struct {
		Data struct {
			Result []struct {
				Metric map[string]any `json:"metric"`
				Value  [2]any         `json:"value"`
				Values [][2]any       `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &generic); err != nil {
		return nil, 0, 0, fmt.Errorf("metric JSON parse: %w", err)
	}
	if len(generic.Data.Result) == 0 {
		return nil, 0, 0, fmt.Errorf("metric value parse: empty result")
	}
	rows := make([]assert.Row, 0, len(generic.Data.Result))
	for _, item := range generic.Data.Result {
		attrs := stringifyMap(item.Metric)
		rows = append(rows, assert.Row{
			Name:       attrs["__name__"],
			Attributes: attrs,
		})
	}
	r := generic.Data.Result[0]
	raw, ok := r.Value[1].(string)
	if !ok && len(r.Values) > 0 {
		raw, ok = r.Values[len(r.Values)-1][1].(string)
	}
	if !ok {
		return rows, len(generic.Data.Result), 0, fmt.Errorf("metric value parse: result point has no scalar value")
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err != nil {
		return rows, len(generic.Data.Result), 0, fmt.Errorf("metric value parse: %q is not a number", raw)
	}
	return rows, len(generic.Data.Result), f, nil
}

func extractLogRows(stdout string) ([]assert.Row, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var generic struct {
		Data struct {
			Result []struct {
				Stream map[string]any `json:"stream"`
				Values [][]any        `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &generic); err != nil {
		return nil, 0, fmt.Errorf("log JSON parse: %w", err)
	}
	var rows []assert.Row
	for _, stream := range generic.Data.Result {
		attrs := stringifyMap(stream.Stream)
		for _, pair := range stream.Values {
			body := ""
			if len(pair) > 1 {
				body = fmt.Sprint(pair[1])
			}
			rows = append(rows, assert.Row{Name: body, Attributes: attrs})
		}
	}
	return rows, len(rows), nil
}

func extractTraceRows(stdout string) ([]assert.Row, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var root any
	if err := json.Unmarshal([]byte(stdout), &root); err != nil {
		return nil, 0, fmt.Errorf("trace JSON parse: %w", err)
	}
	if rows, ok := extractOTLPTraceRows(root); ok {
		return rows, len(rows), nil
	}
	count := traceResultCount(root)
	rows := collectNamedRows(root)
	if count == 0 {
		count = len(rows)
	}
	return rows, count, nil
}

func extractTraceIDs(stdout string) ([]string, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var payload struct {
		Traces []struct {
			TraceID string `json:"traceID"`
		} `json:"traces"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, 0, fmt.Errorf("trace JSON parse: %w", err)
	}
	ids := make([]string, 0, len(payload.Traces))
	for _, tr := range payload.Traces {
		if tr.TraceID != "" {
			ids = append(ids, tr.TraceID)
		}
	}
	return ids, len(payload.Traces), nil
}

func extractProfileRows(stdout string) ([]assert.Row, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var root any
	if err := json.Unmarshal([]byte(stdout), &root); err != nil {
		return nil, 0, fmt.Errorf("profile JSON parse: %w", err)
	}
	if names := flamebearerNames(root); len(names) > 0 {
		rows := make([]assert.Row, 0, len(names))
		for _, name := range names {
			rows = append(rows, assert.Row{Name: name, Attributes: map[string]string{}})
		}
		return rows, len(rows), nil
	}
	if names := flamegraphNames(root); len(names) > 0 {
		rows := make([]assert.Row, 0, len(names))
		for _, name := range names {
			rows = append(rows, assert.Row{Name: name, Attributes: map[string]string{}})
		}
		return rows, len(rows), nil
	}
	rows := collectNamedRows(root)
	return rows, len(rows), nil
}

func traceResultCount(root any) int {
	m, ok := root.(map[string]any)
	if !ok {
		return 0
	}
	data, ok := m["data"].(map[string]any)
	if !ok {
		return 0
	}
	result, ok := data["result"].([]any)
	if !ok {
		return 0
	}
	return len(result)
}

func collectNamedRows(v any) []assert.Row {
	var rows []assert.Row
	var walk func(any)
	walk = func(cur any) {
		switch t := cur.(type) {
		case map[string]any:
			if row, ok := maybeRow(t); ok {
				rows = append(rows, row)
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return rows
}

func maybeRow(m map[string]any) (assert.Row, bool) {
	row := assert.Row{Attributes: map[string]string{}}
	for _, key := range []string{"name", "spanName", "span_name", "body", "rootTraceName"} {
		if v, ok := m[key]; ok {
			row.Name = fmt.Sprint(v)
			break
		}
	}
	for _, key := range []string{"attributes", "metric", "stream", "resourceAttributes", "resource_attributes"} {
		if child, ok := m[key]; ok {
			for k, v := range stringifyMapAny(child) {
				row.Attributes[k] = v
			}
		}
	}
	if resource, ok := m["resource"].(map[string]any); ok {
		if attrs, ok := resource["attributes"]; ok {
			for k, v := range stringifyMapAny(attrs) {
				row.Attributes[k] = v
			}
		}
	}
	if v, ok := m["rootServiceName"]; ok {
		row.Attributes["service.name"] = fmt.Sprint(v)
	}
	if v, ok := m["traceID"]; ok {
		row.Attributes["trace_id"] = fmt.Sprint(v)
	}
	if row.Name == "" && len(row.Attributes) == 0 {
		return assert.Row{}, false
	}
	return row, true
}

func stringifyMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func stringifyMapAny(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	return stringifyMap(m)
}

func extractOTLPTraceRows(root any) ([]assert.Row, bool) {
	top, ok := root.(map[string]any)
	if !ok {
		return nil, false
	}
	if trace, ok := top["trace"].(map[string]any); ok {
		top = trace
	}
	resourceSpans, ok := top["resourceSpans"].([]any)
	if !ok {
		resourceSpans, ok = top["batches"].([]any)
	}
	if !ok {
		// Some wrappers may nest under "data" first.
		if data, ok := top["data"].(map[string]any); ok {
			if nested, ok := data["resourceSpans"].([]any); ok {
				resourceSpans = nested
			} else if nested, ok := data["batches"].([]any); ok {
				resourceSpans = nested
			}
		}
		if !ok {
			return nil, false
		}
	}
	var rows []assert.Row
	for _, rsAny := range resourceSpans {
		rs, ok := rsAny.(map[string]any)
		if !ok {
			continue
		}
		resourceAttrs := map[string]string{}
		if resource, ok := rs["resource"].(map[string]any); ok {
			resourceAttrs = parseOTelAttributeList(resource["attributes"])
		}
		scopeSpans, _ := rs["scopeSpans"].([]any)
		for _, ssAny := range scopeSpans {
			ss, ok := ssAny.(map[string]any)
			if !ok {
				continue
			}
			scopeName := ""
			if scope, ok := ss["scope"].(map[string]any); ok {
				scopeName = fmt.Sprint(scope["name"])
			}
			spans, _ := ss["spans"].([]any)
			for _, spAny := range spans {
				sp, ok := spAny.(map[string]any)
				if !ok {
					continue
				}
				attrs := map[string]string{}
				for k, v := range resourceAttrs {
					attrs[k] = v
				}
				for k, v := range parseOTelAttributeList(sp["attributes"]) {
					attrs[k] = v
				}
				if scopeName != "" {
					attrs["otel.scope.name"] = scopeName
					attrs["otel.library.name"] = scopeName
				}
				if kind, ok := sp["kind"]; ok {
					attrs["kind"] = fmt.Sprint(kind)
				}
				rows = append(rows, assert.Row{
					Name:       fmt.Sprint(sp["name"]),
					Attributes: attrs,
				})
			}
		}
	}
	if len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

func parseOTelAttributeList(v any) map[string]string {
	list, ok := v.([]any)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(list))
	for _, itemAny := range list {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		key := fmt.Sprint(item["key"])
		if key == "" {
			continue
		}
		value, _ := item["value"].(map[string]any)
		out[key] = parseOTelAnyValue(value)
	}
	return out
}

func parseOTelAnyValue(m map[string]any) string {
	for _, key := range []string{"stringValue", "intValue", "doubleValue", "boolValue"} {
		if v, ok := m[key]; ok {
			return fmt.Sprint(v)
		}
	}
	if arr, ok := m["arrayValue"].(map[string]any); ok {
		if vals, ok := arr["values"].([]any); ok {
			parts := make([]string, 0, len(vals))
			for _, val := range vals {
				if child, ok := val.(map[string]any); ok {
					parts = append(parts, parseOTelAnyValue(child))
				}
			}
			return strings.Join(parts, ",")
		}
	}
	return ""
}

func flamebearerNames(root any) []string {
	top, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	flamebearer, ok := top["flamebearer"].(map[string]any)
	if !ok {
		if data, ok := top["data"].(map[string]any); ok {
			flamebearer, _ = data["flamebearer"].(map[string]any)
		}
	}
	if flamebearer == nil {
		return nil
	}
	raw, ok := flamebearer["names"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func flamegraphNames(root any) []string {
	top, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	flamegraph, ok := top["flamegraph"].(map[string]any)
	if !ok {
		if data, ok := top["data"].(map[string]any); ok {
			flamegraph, _ = data["flamegraph"].(map[string]any)
		}
	}
	if flamegraph == nil {
		return nil
	}
	raw, ok := flamegraph["names"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s := fmt.Sprint(item)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
