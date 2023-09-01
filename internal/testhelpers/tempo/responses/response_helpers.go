package responses

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"regexp"
	"strings"
)

func MatchTraceAttribute(attributes pcommon.Map, attrType pcommon.ValueType, key, value string) error {
	att, found := attributes.Get(key)
	if !found {
		return fmt.Errorf("couldn't find attribute %s", key)
	}
	valueType := att.Type()
	if valueType != attrType {
		return fmt.Errorf("value type for key %s is %s which doesn't match the expect type %s", key, valueType, attrType)
	}

	str := att.AsString()
	if value != "" && value != str {
		return fmt.Errorf("value for key %s is %s which doesn't match the expect value %s", key, str, value)
	}
	return nil
}

type AttributeMatch struct {
	Key   string
	Value string
	Type  pcommon.ValueType
}

func AttributesMatch(attributes pcommon.Map, match []AttributeMatch) error {
	for _, m := range match {
		if err := MatchTraceAttribute(attributes, m.Type, m.Key, m.Value); err != nil {
			return err
		}
	}

	return nil
}

func AttributesExist(attributes pcommon.Map, match []AttributeMatch) error {
	for _, m := range match {
		if err := MatchTraceAttribute(attributes, m.Type, m.Key, ""); err != nil {
			return err
		}
	}

	return nil
}

func TimeIsIncreasing(span ptrace.Span) error {
	start := span.StartTimestamp()
	if start == 0 {
		return fmt.Errorf("span must have start time")
	}

	end := span.EndTimestamp()
	if end == 0 {
		return fmt.Errorf("span must have end time")
	}

	if end < start {
		return fmt.Errorf("span end time %d is less than the start time %d", end, start)
	}

	return nil
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

func ParseTempoResult(body []byte) (TempoResult, error) {
	var st TempoResult
	err := json.Unmarshal(body, &st)

	return st, err
}

func FindSpans(td ptrace.Traces, name string) []ptrace.Span {
	return FindSpansFunc(td, func(span *ptrace.Span) bool {
		return span.Name() == name
	})
}

func ChildrenOf(td ptrace.Traces, spanId string) []ptrace.Span {
	return FindSpansFunc(td, func(span *ptrace.Span) bool {
		return span.ParentSpanID().String() == spanId
	})
}

func FindSpansFunc(td ptrace.Traces, pred func(*ptrace.Span) bool) []ptrace.Span {
	var result []ptrace.Span
	resourceSpans := td.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		scopeSpans := resourceSpans.At(i).ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			spans := scopeSpans.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if pred(&span) {
					result = append(result, span)
				}
			}
		}
	}
	return result
}
