package yaml

import (
	"testing"

	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
)

func TestAssertCount(t *testing.T) {
	tests := []struct {
		name          string
		expectedRange *model.ExpectedRange
		count         int
		shouldFail    bool
	}{
		{
			name:          "count within min and max range",
			expectedRange: &model.ExpectedRange{Min: 1, Max: 5},
			count:         3,
			shouldFail:    false,
		},
		{
			name:          "count equals min",
			expectedRange: &model.ExpectedRange{Min: 2, Max: 5},
			count:         2,
			shouldFail:    false,
		},
		{
			name:          "count equals max",
			expectedRange: &model.ExpectedRange{Min: 1, Max: 5},
			count:         5,
			shouldFail:    false,
		},
		{
			name:          "count below min should fail",
			expectedRange: &model.ExpectedRange{Min: 5, Max: 10},
			count:         3,
			shouldFail:    true,
		},
		{
			name:          "count above max should fail",
			expectedRange: &model.ExpectedRange{Min: 1, Max: 5},
			count:         10,
			shouldFail:    true,
		},
		{
			name:          "max of 0 means no upper limit",
			expectedRange: &model.ExpectedRange{Min: 1, Max: 0},
			count:         1000,
			shouldFail:    false,
		},
		{
			name:          "expect absent passes with count 0",
			expectedRange: &model.ExpectedRange{Min: 0, Max: 0},
			count:         0,
			shouldFail:    false,
		},
		{
			name:          "expect absent fails with count > 0",
			expectedRange: &model.ExpectedRange{Min: 0, Max: 0},
			count:         1,
			shouldFail:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := false
			g := gomega.NewGomega(func(message string, callerSkip ...int) {
				if !tt.shouldFail {
					t.Error(message)
				}
				failed = true
			})
			assertCount(g, tt.expectedRange, tt.count)
			if tt.shouldFail {
				require.True(t, failed, "expected assertion to fail")
			}
		})
	}
}

func TestAssertName(t *testing.T) {
	tests := []struct {
		name       string
		signal     model.ExpectedSignal
		inputName  string
		shouldFail bool
	}{
		{
			name:       "equals does not match substring",
			signal:     model.ExpectedSignal{NameEquals: "test"},
			inputName:  "test-name",
			shouldFail: true,
		},
		{
			name:       "equals exact match",
			signal:     model.ExpectedSignal{NameEquals: "test"},
			inputName:  "test",
			shouldFail: false,
		},
		{
			name:       "equals fails when not found",
			signal:     model.ExpectedSignal{NameEquals: "missing"},
			inputName:  "test-name",
			shouldFail: true,
		},
		{
			name:       "regexp matches",
			signal:     model.ExpectedSignal{NameRegexp: "^test-.*$"},
			inputName:  "test-name",
			shouldFail: false,
		},
		{
			name:       "regexp fails when not matching",
			signal:     model.ExpectedSignal{NameRegexp: "^xyz-.*$"},
			inputName:  "test-name",
			shouldFail: true,
		},
		{
			name: "multiple conditions all pass",
			signal: model.ExpectedSignal{
				NameEquals: "test-name",
				NameRegexp: "^test-.*$",
			},
			inputName:  "test-name",
			shouldFail: false,
		},
		{
			name:       "empty signal does nothing",
			signal:     model.ExpectedSignal{},
			inputName:  "any-name",
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := false
			g := gomega.NewGomega(func(message string, callerSkip ...int) {
				if !tt.shouldFail {
					t.Error(message)
				}
				failed = true
			})
			assertSignalName(g, tt.signal, tt.inputName)
			if tt.shouldFail {
				require.True(t, failed, "expected assertion to fail")
			}
		})
	}
}

func TestAssertAttributes(t *testing.T) {
	tests := []struct {
		name       string
		signal     model.ExpectedSignal
		attributes map[string]string
		shouldFail bool
	}{
		{
			name: "exact attributes match",
			signal: model.ExpectedSignal{
				Attributes: map[string]string{
					"service.name": "test-service",
					"environment":  "prod",
				},
			},
			attributes: map[string]string{
				"service.name": "test-service",
				"environment":  "prod",
				"extra":        "value",
			},
			shouldFail: false,
		},
		{
			name: "attributes fail when value different",
			signal: model.ExpectedSignal{
				Attributes: map[string]string{
					"service.name": "test-service",
				},
			},
			attributes: map[string]string{
				"service.name": "wrong-service",
			},
			shouldFail: true,
		},
		{
			name: "attributes fail when key missing",
			signal: model.ExpectedSignal{
				Attributes: map[string]string{
					"missing.key": "value",
				},
			},
			attributes: map[string]string{
				"service.name": "test-service",
			},
			shouldFail: true,
		},
		{
			name: "attribute regexp matches",
			signal: model.ExpectedSignal{
				AttributeRegexp: map[string]string{
					"trace.id": "^[a-f0-9]{32}$",
					"span.id":  "^[a-f0-9]{16}$",
				},
			},
			attributes: map[string]string{
				"trace.id": "0123456789abcdef0123456789abcdef",
				"span.id":  "0123456789abcdef",
			},
			shouldFail: false,
		},
		{
			name: "attribute regexp fails when pattern not matching",
			signal: model.ExpectedSignal{
				AttributeRegexp: map[string]string{
					"trace.id": "^[a-f0-9]{32}$",
				},
			},
			attributes: map[string]string{
				"trace.id": "invalid",
			},
			shouldFail: true,
		},
		{
			name: "attribute regexp fails when key missing",
			signal: model.ExpectedSignal{
				AttributeRegexp: map[string]string{
					"missing.key": ".*",
				},
			},
			attributes: map[string]string{
				"other.key": "value",
			},
			shouldFail: true,
		},
		{
			name: "no extra attributes passes when no extra",
			signal: model.ExpectedSignal{
				Attributes: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
				NoExtraAttributes: true,
			},
			attributes: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			shouldFail: false,
		},
		{
			name: "no extra attributes fails when extra present",
			signal: model.ExpectedSignal{
				Attributes: map[string]string{
					"key1": "value1",
				},
				NoExtraAttributes: true,
			},
			attributes: map[string]string{
				"key1":  "value1",
				"extra": "value",
			},
			shouldFail: true,
		},
		{
			name: "no extra attributes with both attributes and regexp",
			signal: model.ExpectedSignal{
				Attributes: map[string]string{
					"service.name": "test-service",
				},
				AttributeRegexp: map[string]string{
					"trace.id": "^[a-f0-9]{32}$",
				},
				NoExtraAttributes: true,
			},
			attributes: map[string]string{
				"service.name": "test-service",
				"trace.id":     "0123456789abcdef0123456789abcdef",
			},
			shouldFail: false,
		},
		{
			name: "no extra attributes without attributes fails when extra present",
			signal: model.ExpectedSignal{
				NoExtraAttributes: true,
			},
			attributes: map[string]string{
				"service.name": "test-service",
				"trace.id":     "0123456789abcdef0123456789abcdef",
			},
			shouldFail: true,
		},
		{
			name:   "empty attributes signal does nothing",
			signal: model.ExpectedSignal{},
			attributes: map[string]string{
				"any": "value",
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := false
			g := gomega.NewGomega(func(message string, callerSkip ...int) {
				if !tt.shouldFail {
					t.Error(message)
				}
				failed = true
			})
			assertAttributes(g, tt.signal, tt.attributes)
			if tt.shouldFail {
				require.True(t, failed, "expected assertion to fail")
			}
		})
	}
}

func TestAssertSignal(t *testing.T) {
	tests := []struct {
		name       string
		signal     model.ExpectedSignal
		count      int
		signalName string
		attributes map[string]string
		shouldFail bool
	}{
		{
			name: "all validations pass",
			signal: model.ExpectedSignal{
				NameEquals: "test-span",
				Count:      &model.ExpectedRange{Min: 1, Max: 5},
				Attributes: map[string]string{
					"service.name": "test-service",
				},
			},
			count:      3,
			signalName: "test-span",
			attributes: map[string]string{
				"service.name": "test-service",
			},
			shouldFail: false,
		},
		{
			name: "count validation fails",
			signal: model.ExpectedSignal{
				Count: &model.ExpectedRange{Min: 5, Max: 10},
			},
			count:      3,
			signalName: "name",
			attributes: map[string]string{},
			shouldFail: true,
		},
		{
			name: "name validation fails",
			signal: model.ExpectedSignal{
				NameEquals: "expected-name",
			},
			count:      1,
			signalName: "wrong-name",
			attributes: map[string]string{},
			shouldFail: true,
		},
		{
			name: "attributes validation fails",
			signal: model.ExpectedSignal{
				Attributes: map[string]string{
					"missing": "value",
				},
			},
			count:      1,
			signalName: "name",
			attributes: map[string]string{},
			shouldFail: true,
		},
		{
			name: "count is optional",
			signal: model.ExpectedSignal{
				NameEquals: "test-name",
			},
			count:      100,
			signalName: "test-name",
			attributes: map[string]string{},
			shouldFail: false,
		},
		{
			name: "complex signal with all fields",
			signal: model.ExpectedSignal{
				NameEquals: "test-span",
				NameRegexp: "^test-.*$",
				Count:      &model.ExpectedRange{Min: 1, Max: 10},
				Attributes: map[string]string{
					"service.name": "test-service",
					"environment":  "test",
				},
				AttributeRegexp: map[string]string{
					"trace.id": "^[a-f0-9]+$",
				},
				NoExtraAttributes: true,
			},
			count:      5,
			signalName: "test-span",
			attributes: map[string]string{
				"service.name": "test-service",
				"environment":  "test",
				"trace.id":     "abc123",
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := false
			g := gomega.NewGomega(func(message string, callerSkip ...int) {
				if !tt.shouldFail {
					t.Error(message)
				}
				failed = true
			})
			assertSignal(g, tt.signal, tt.count, tt.signalName, tt.attributes)
			if tt.shouldFail {
				require.True(t, failed, "expected assertion to fail")
			}
		})
	}
}
