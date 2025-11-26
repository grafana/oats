package yaml

import (
	"testing"

	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
)

func TestAssertCount(t *testing.T) {
	t.Run("count within min and max range", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		expectedRange := &model.ExpectedRange{Min: 1, Max: 5}
		assertCount(g, expectedRange, 3)
	})

	t.Run("count equals min", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		expectedRange := &model.ExpectedRange{Min: 2, Max: 5}
		assertCount(g, expectedRange, 2)
	})

	t.Run("count equals max", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		expectedRange := &model.ExpectedRange{Min: 1, Max: 5}
		assertCount(g, expectedRange, 5)
	})

	t.Run("count below min should fail", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		expectedRange := &model.ExpectedRange{Min: 5, Max: 10}
		assertCount(g, expectedRange, 3)
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("count above max should fail", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		expectedRange := &model.ExpectedRange{Min: 1, Max: 5}
		assertCount(g, expectedRange, 10)
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("max of 0 means no upper limit", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		expectedRange := &model.ExpectedRange{Min: 1, Max: 0}
		assertCount(g, expectedRange, 1000)
	})
}

func TestAssertName(t *testing.T) {
	t.Run("equals matches substring", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{Equals: "test"}
		assertName(g, signal, "test-name")
	})

	t.Run("equals exact match", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{Equals: "test"}
		assertName(g, signal, "test")
	})

	t.Run("equals fails when not found", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{Equals: "missing"}
		assertName(g, signal, "test-name")
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("regexp matches", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{Regexp: "^test-.*$"}
		assertName(g, signal, "test-name")
	})

	t.Run("regexp fails when not matching", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{Regexp: "^xyz-.*$"}
		assertName(g, signal, "test-name")
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("contains all substrings", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{Contains: []string{"foo", "bar", "baz"}}
		assertName(g, signal, "foo-bar-baz-name")
	})

	t.Run("contains fails when substring missing", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{Contains: []string{"foo", "missing"}}
		assertName(g, signal, "foo-bar-baz")
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("multiple conditions all pass", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			Equals:   "test",
			Regexp:   "^test-.*$",
			Contains: []string{"test", "name"},
		}
		assertName(g, signal, "test-name")
	})

	t.Run("empty signal does nothing", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{}
		assertName(g, signal, "any-name")
	})
}

func TestAssertAttributes(t *testing.T) {
	t.Run("exact attributes match", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			Attributes: map[string]string{
				"service.name": "test-service",
				"environment":  "prod",
			},
		}
		attributes := map[string]string{
			"service.name": "test-service",
			"environment":  "prod",
			"extra":        "value",
		}
		assertAttributes(g, signal, attributes)
	})

	t.Run("attributes fail when value different", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			Attributes: map[string]string{
				"service.name": "test-service",
			},
		}
		attributes := map[string]string{
			"service.name": "wrong-service",
		}
		assertAttributes(g, signal, attributes)
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("attributes fail when key missing", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			Attributes: map[string]string{
				"missing.key": "value",
			},
		}
		attributes := map[string]string{
			"service.name": "test-service",
		}
		assertAttributes(g, signal, attributes)
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("attribute regexp matches", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			AttributeRegexp: map[string]string{
				"trace.id": "^[a-f0-9]{32}$",
				"span.id":  "^[a-f0-9]{16}$",
			},
		}
		attributes := map[string]string{
			"trace.id": "0123456789abcdef0123456789abcdef",
			"span.id":  "0123456789abcdef",
		}
		assertAttributes(g, signal, attributes)
	})

	t.Run("attribute regexp fails when pattern not matching", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			AttributeRegexp: map[string]string{
				"trace.id": "^[a-f0-9]{32}$",
			},
		}
		attributes := map[string]string{
			"trace.id": "invalid",
		}
		assertAttributes(g, signal, attributes)
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("attribute regexp fails when key missing", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			AttributeRegexp: map[string]string{
				"missing.key": ".*",
			},
		}
		attributes := map[string]string{
			"other.key": "value",
		}
		assertAttributes(g, signal, attributes)
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("no extra attributes passes when no extra", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			Attributes: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			NoExtraAttributes: true,
		}
		attributes := map[string]string{
			"key1": "value1",
			"key2": "value2",
		}
		assertAttributes(g, signal, attributes)
	})

	t.Run("no extra attributes fails when extra present", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			Attributes: map[string]string{
				"key1": "value1",
			},
			NoExtraAttributes: true,
		}
		attributes := map[string]string{
			"key1":  "value1",
			"extra": "value",
		}
		assertAttributes(g, signal, attributes)
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("no extra attributes with both attributes and regexp", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			Attributes: map[string]string{
				"service.name": "test-service",
			},
			AttributeRegexp: map[string]string{
				"trace.id": "^[a-f0-9]{32}$",
			},
			NoExtraAttributes: true,
		}
		attributes := map[string]string{
			"service.name": "test-service",
			"trace.id":     "0123456789abcdef0123456789abcdef",
		}
		assertAttributes(g, signal, attributes)
	})

	t.Run("empty attributes signal does nothing", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{}
		attributes := map[string]string{
			"any": "value",
		}
		assertAttributes(g, signal, attributes)
	})
}

func TestAssertSignal(t *testing.T) {
	t.Run("all validations pass", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			Equals: "test-span",
			Count:  &model.ExpectedRange{Min: 1, Max: 5},
			Attributes: map[string]string{
				"service.name": "test-service",
			},
		}
		name := "test-span"
		attributes := map[string]string{
			"service.name": "test-service",
		}
		assertSignal(g, signal, 3, name, attributes)
	})

	t.Run("count validation fails", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			Count: &model.ExpectedRange{Min: 5, Max: 10},
		}
		assertSignal(g, signal, 3, "name", map[string]string{})
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("name validation fails", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			Equals: "expected-name",
		}
		assertSignal(g, signal, 1, "wrong-name", map[string]string{})
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("attributes validation fails", func(t *testing.T) {
		failed := false
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			failed = true
		})
		signal := model.ExpectedSignal{
			Attributes: map[string]string{
				"missing": "value",
			},
		}
		assertSignal(g, signal, 1, "name", map[string]string{})
		require.True(t, failed, "expected assertion to fail")
	})

	t.Run("count is optional", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			Equals: "test-name",
		}
		assertSignal(g, signal, 100, "test-name", map[string]string{})
	})

	t.Run("complex signal with all fields", func(t *testing.T) {
		g := gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		})
		signal := model.ExpectedSignal{
			Equals:   "test",
			Contains: []string{"test", "span"},
			Regexp:   "^test-.*$",
			Count:    &model.ExpectedRange{Min: 1, Max: 10},
			Attributes: map[string]string{
				"service.name": "test-service",
				"environment":  "test",
			},
			AttributeRegexp: map[string]string{
				"trace.id": "^[a-f0-9]+$",
			},
			NoExtraAttributes: true,
		}
		name := "test-span"
		attributes := map[string]string{
			"service.name": "test-service",
			"environment":  "test",
			"trace.id":     "abc123",
		}
		assertSignal(g, signal, 5, name, attributes)
	})
}
