package responses

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/grafana/oats/model"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type AttributeMatch struct {
	Key   string
	Value string
	Type  pcommon.ValueType
}

func ParseTraceDetails(body []byte) (ptrace.Traces, error) {
	body = fixIds(body, regexp.MustCompile(`"traceId":\s*"(.*?)"`), "traceId", 16)
	body = fixIds(body, regexp.MustCompile(`"spanId":\s*"(.*?)"`), "spanId", 8)
	body = fixIds(body, regexp.MustCompile(`"parentSpanId":\s*"(.*?)"`), "parentSpanId", 8)
	s := string(body)
	s = strings.ReplaceAll(s, `"batches"`, `"resourceSpans"`)
	body = []byte(s)

	unmarshaler := ptrace.JSONUnmarshaler{}
	return unmarshaler.UnmarshalTraces(body)
}

func fixIds(body []byte, re *regexp.Regexp, idName string, capacity int) []byte {
	return re.ReplaceAllFunc(body, func(b []byte) []byte {
		submatch := re.FindStringSubmatch(string(b))
		dst := make([]byte, capacity)
		_, err := base64.StdEncoding.Decode(dst, []byte(submatch[1]))
		if err != nil {
			panic(err)
		}
		r := fmt.Sprintf("\"%s\": \"%s\"", idName, hex.EncodeToString(dst))
		return []byte(r)
	})
}

func ParseTempoSearchResult(body []byte) (TempoSearchResult, error) {
	var st TempoSearchResult
	err := json.Unmarshal(body, &st)

	return st, err
}

func FindSpans(td ptrace.Traces, signal model.ExpectedSignal) ([]ptrace.Span, map[string]string) {
	m := matcherMaybeRegex(signal)
	return FindSpansFunc(td, func(span *ptrace.Span) bool {
		return m(span.Name())
	})
}

func FindSpansFunc(td ptrace.Traces, pred func(*ptrace.Span) bool) ([]ptrace.Span, map[string]string) {
	var result []ptrace.Span
	atts := map[string]string{}
	resourceSpans := td.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		resourceSpan := resourceSpans.At(i)
		scopeSpans := resourceSpan.ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			scopeSpan := scopeSpans.At(j)
			spans := scopeSpan.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if pred(&span) {
					result = append(result, span)
					resourceSpan.Resource().Attributes().Range(func(k string, v pcommon.Value) bool {
						atts[k] = v.AsString()
						return true
					})
					scope := scopeSpan.Scope()
					scope.Attributes().Range(func(k string, v pcommon.Value) bool {
						atts[k] = v.AsString()
						return true
					})
					//this is how the scope name is shown in tempo
					atts["otel.library.name"] = scope.Name()
					atts["otel.library.version"] = scope.Version()
				}
			}
		}
	}
	return result, atts
}

func matcherMaybeRegex(signal model.ExpectedSignal) func(got string) bool {
	var re *regexp.Regexp
	if signal.Regexp != "" {
		re = regexp.MustCompile(signal.Regexp)
	}

	return func(got string) bool {
		if re != nil {
			return re.MatchString(got)
		}
		return signal.Equals == got
	}
}
