package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHash_StableAcrossCalls(t *testing.T) {
	k := Key{CaseYAML: []byte("name: x\n"), GCXVersion: "1.2.3"}
	if a, b := k.Hash(), k.Hash(); a != b {
		t.Errorf("hash unstable: %q vs %q", a, b)
	}
}

func TestHash_DiffersOnAnyField(t *testing.T) {
	base := Key{CaseYAML: []byte("a"), FixtureBytes: []byte("b"), GCXVersion: "v1", OatsVersion: "v2"}
	bytesBase := base.Hash()

	variations := []Key{
		{CaseYAML: []byte("a-modified"), FixtureBytes: []byte("b"), GCXVersion: "v1", OatsVersion: "v2"},
		{CaseYAML: []byte("a"), FixtureBytes: []byte("b-modified"), GCXVersion: "v1", OatsVersion: "v2"},
		{CaseYAML: []byte("a"), FixtureBytes: []byte("b"), GCXVersion: "v1-modified", OatsVersion: "v2"},
		{CaseYAML: []byte("a"), FixtureBytes: []byte("b"), GCXVersion: "v1", OatsVersion: "v2-modified"},
		{CaseYAML: []byte("a"), FixtureBytes: []byte("b"), GCXVersion: "v1", OatsVersion: "v2", Extra: map[string]string{"k": "v"}},
	}
	for i, v := range variations {
		if v.Hash() == bytesBase {
			t.Errorf("variation %d: hash collides with base", i)
		}
	}
}

func TestHash_ExtraOrderIndependent(t *testing.T) {
	a := Key{Extra: map[string]string{"x": "1", "y": "2"}}.Hash()
	b := Key{Extra: map[string]string{"y": "2", "x": "1"}}.Hash()
	if a != b {
		t.Errorf("Extra map order changed hash: %s vs %s", a, b)
	}
}

func TestLookupMiss(t *testing.T) {
	s, _ := New(t.TempDir(), 0, nil)
	hit, _ := s.Lookup(Key{CaseYAML: []byte("x")})
	if hit {
		t.Error("expected miss on empty cache")
	}
}

func TestRecordThenLookup(t *testing.T) {
	s, _ := New(t.TempDir(), 0, nil)
	k := Key{CaseYAML: []byte("x")}
	if err := s.Record(k); err != nil {
		t.Fatal(err)
	}
	hit, ts := s.Lookup(k)
	if !hit {
		t.Fatal("expected hit after Record")
	}
	if ts.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestTTLExpiry(t *testing.T) {
	clock := time.Now()
	s, _ := New(t.TempDir(), time.Hour, func() time.Time { return clock })

	k := Key{CaseYAML: []byte("x")}
	if err := s.Record(k); err != nil {
		t.Fatal(err)
	}

	// Within TTL.
	if hit, _ := s.Lookup(k); !hit {
		t.Error("fresh entry should hit")
	}

	// Push clock past TTL.
	clock = clock.Add(2 * time.Hour)
	if hit, _ := s.Lookup(k); hit {
		t.Error("expired entry should miss")
	}
}

func TestExpiredEntryIsEvicted(t *testing.T) {
	clock := time.Now()
	dir := t.TempDir()
	s, _ := New(dir, time.Hour, func() time.Time { return clock })
	k := Key{CaseYAML: []byte("x")}

	if err := s.Record(k); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Hour)
	_, _ = s.Lookup(k)

	// File should be gone.
	if _, err := os.Stat(filepath.Join(dir, k.Hash())); !os.IsNotExist(err) {
		t.Errorf("expected eviction; stat: %v", err)
	}
}

func TestEvict(t *testing.T) {
	s, _ := New(t.TempDir(), 0, nil)
	k := Key{CaseYAML: []byte("x")}
	_ = s.Record(k)
	if err := s.Evict(k); err != nil {
		t.Fatal(err)
	}
	if hit, _ := s.Lookup(k); hit {
		t.Error("expected miss after Evict")
	}
	// Idempotent.
	if err := s.Evict(k); err != nil {
		t.Errorf("Evict on missing key should be no-op, got %v", err)
	}
}

func TestClear(t *testing.T) {
	s, _ := New(t.TempDir(), 0, nil)
	_ = s.Record(Key{CaseYAML: []byte("a")})
	_ = s.Record(Key{CaseYAML: []byte("b")})
	_ = s.Record(Key{CaseYAML: []byte("c")})

	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"a", "b", "c"} {
		if hit, _ := s.Lookup(Key{CaseYAML: []byte(body)}); hit {
			t.Errorf("expected all-miss after Clear, %q still hit", body)
		}
	}
}

func TestCorruptEntryIsEvicted(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, 0, nil)
	k := Key{CaseYAML: []byte("x")}
	// Write a bogus timestamp.
	if err := os.WriteFile(filepath.Join(dir, k.Hash()), []byte("not a timestamp"), 0o644); err != nil {
		t.Fatal(err)
	}
	hit, _ := s.Lookup(k)
	if hit {
		t.Error("corrupt entry should not hit")
	}
	if _, err := os.Stat(filepath.Join(dir, k.Hash())); !os.IsNotExist(err) {
		t.Errorf("corrupt entry should be evicted; stat: %v", err)
	}
}
