package assert

import (
	"testing"
)

func TestContains(t *testing.T) {
	cases := []struct {
		name       string
		stdout     string
		substrings []string
		wantFails  int
	}{
		{"all present", "hello world foo bar", []string{"hello", "foo"}, 0},
		{"one missing", "hello world", []string{"hello", "missing"}, 1},
		{"all missing", "x", []string{"a", "b", "c"}, 3},
		{"empty list", "anything", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Contains(tc.stdout, tc.substrings)
			if len(got) != tc.wantFails {
				t.Errorf("got %d failures, want %d: %v", len(got), tc.wantFails, got)
			}
		})
	}
}

func TestNotContains(t *testing.T) {
	if got := NotContains("hello world", []string{"goodbye"}); len(got) != 0 {
		t.Errorf("expected zero failures, got %v", got)
	}
	if got := NotContains("hello world", []string{"hello"}); len(got) != 1 {
		t.Errorf("expected one failure, got %d", len(got))
	}
}

func TestRegex(t *testing.T) {
	if got := Regex("err: 500", []string{`err: \d{3}`}); len(got) != 0 {
		t.Errorf("expected match, got %v", got)
	}
	if got := Regex("err: 500", []string{`^err: 4\d{2}$`}); len(got) != 1 {
		t.Errorf("expected one failure (no match), got %d", len(got))
	}
	if got := Regex("anything", []string{`[invalid`}); len(got) != 1 {
		t.Errorf("expected one failure (invalid pattern), got %d", len(got))
	}
}

func TestValue(t *testing.T) {
	cases := []struct {
		name      string
		actual    float64
		expr      string
		wantFails int
	}{
		{">= holds", 5, ">= 1", 0},
		{">= fails", 0, ">= 1", 1},
		{"> holds", 1.5, "> 1", 0},
		{"> fails on equal", 1.0, "> 1", 1},
		{"<= holds", 0, "<= 0", 0},
		{"== holds", 42, "== 42", 0},
		{"== fails", 41, "== 42", 1},
		{"!= holds", 1, "!= 0", 0},
		{"bad operator", 0, "?? 1", 1},
		{"bad rhs", 0, ">= banana", 1},
		{"extra whitespace", 5, "  >=  1  ", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Value(tc.actual, tc.expr)
			if len(got) != tc.wantFails {
				t.Errorf("got %d failures, want %d: %v", len(got), tc.wantFails, got)
			}
		})
	}
}

func TestCount(t *testing.T) {
	got := Count(3, ">= 1")
	if len(got) != 0 {
		t.Errorf("expected pass, got %v", got)
	}
	got = Count(0, ">= 1")
	if len(got) != 1 || got[0].Rule != "count" {
		t.Errorf("expected one count-tagged failure, got %v", got)
	}
}

func TestAbsent(t *testing.T) {
	if got := Absent(0); len(got) != 0 {
		t.Errorf("expected pass, got %v", got)
	}
	if got := Absent(2); len(got) != 1 || got[0].Rule != "absent" {
		t.Errorf("expected one absent-tagged failure, got %v", got)
	}
}
