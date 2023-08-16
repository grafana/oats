package responses

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func MatchTraceAttribute(attributes []Attribute, attrType, key, value string) error {
	if attributes == nil {
		return fmt.Errorf("nil attributes")
	}

	found := false
	for _, attr := range attributes {
		if attr.Key == key {
			found = true
			v := attr.Value

			if v == nil {
				return fmt.Errorf("value for key %s missing in definition", attr.Key)
			}

			av, ok := v[attrType+"Value"]

			if !ok {
				return fmt.Errorf("value of type %s for key %s not found", attrType, attr.Key)
			}

			if value != "" && av != value {
				return fmt.Errorf("value for key %s is %s which doesn't match the expect value %s", attr.Key, av, value)
			}
		}
	}

	if !found {
		return fmt.Errorf("couldn't find attribute %s", key)
	}

	return nil
}

type AttributeMatch struct {
	Key   string
	Value string
	Type  string
}

func AttributesMatch(attributes []Attribute, match []AttributeMatch) error {
	for _, m := range match {
		if err := MatchTraceAttribute(attributes, m.Type, m.Key, m.Value); err != nil {
			return err
		}
	}

	return nil
}

func AttributesExist(attributes []Attribute, match []AttributeMatch) error {
	for _, m := range match {
		if err := MatchTraceAttribute(attributes, m.Type, m.Key, ""); err != nil {
			return err
		}
	}

	return nil
}

func TimeIsIncreasing(span Span) error {
	if span.StartTimeUnixNano == "" {
		return fmt.Errorf("span must have start time")
	}

	if span.EndTimeUnixNano == "" {
		return fmt.Errorf("span must have end time")
	}

	start, err := strconv.ParseInt(span.StartTimeUnixNano, 10, 64)
	if err != nil {
		return err
	}

	end, err := strconv.ParseInt(span.EndTimeUnixNano, 10, 64)
	if err != nil {
		return err
	}

	if end < start {
		return fmt.Errorf("span end time %d is less than the start time %d", end, start)
	}

	return nil
}

func ParseTraceDetails(body []byte) (TraceDetails, error) {
	var td TraceDetails
	err := json.Unmarshal(body, &td)

	return td, err
}

func ParseSearchTagsResult(body []byte) (SearchTagsResult, error) {
	var st SearchTagsResult
	err := json.Unmarshal(body, &st)

	return st, err
}
