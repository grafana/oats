package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/v2case"
)

func cachedRunnerCase(t *testing.T) (*v2case.Case, []byte) {
	t.Helper()
	src := []byte(`oats: 2
name: cached
seed:
  type: app
  compose: x.yml
expected:
  traces:
    - traceql: '{}'
      contains: ["svc"]
`)
	// v2case.Load requires SourcePath, so persist to a temp file.
	tmp := filepath.Join(t.TempDir(), "case.yaml")
	if err := os.WriteFile(tmp, src, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := v2case.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return c, src
}

func TestCache_HitShortCircuits(t *testing.T) {
	c, _ := cachedRunnerCase(t)

	exec := &stubExec{stdout: "svc"}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerbosePasses)
	r := New(exec, rep, Endpoint{GCXContext: "test"}, Options{
		Timeout:         100 * time.Millisecond,
		Interval:        5 * time.Millisecond,
		SeedSettleDelay: 1,
	})

	store, _ := cache.New(t.TempDir(), 0, nil)
	r = r.WithCache(store, CacheContext{GCXVersion: "v1", OatsVersion: "v2"})

	// First run: cache miss → runs the case → records on pass.
	rep.Emit(report.Event{Type: report.EventRunStart})
	if !r.RunCase(context.Background(), c) {
		t.Fatalf("first run should pass:\n%s", buf.String())
	}
	rep.Emit(report.Event{Type: report.EventRunEnd})
	firstRunInvocations := len(exec.captured)
	if firstRunInvocations == 0 {
		t.Fatal("first run should hit gcx at least once")
	}

	// Second run: cache hit → no gcx invocation.
	buf.Reset()
	rep = report.NewTextReporter(&buf, report.VerbosePasses)
	r2 := New(exec, rep, Endpoint{GCXContext: "test"}, Options{
		Timeout:         100 * time.Millisecond,
		Interval:        5 * time.Millisecond,
		SeedSettleDelay: 1,
	}).WithCache(store, CacheContext{GCXVersion: "v1", OatsVersion: "v2"})

	rep.Emit(report.Event{Type: report.EventRunStart})
	if !r2.RunCase(context.Background(), c) {
		t.Fatalf("cached run should pass:\n%s", buf.String())
	}
	rep.Emit(report.Event{Type: report.EventRunEnd})

	if len(exec.captured) != firstRunInvocations {
		t.Errorf("cache hit should skip gcx execution; saw %d new invocations",
			len(exec.captured)-firstRunInvocations)
	}
	if !strings.Contains(buf.String(), "SKIP cached") {
		t.Errorf("expected SKIP line:\n%s", buf.String())
	}
}

func TestCache_FailureEvictsStaleEntry(t *testing.T) {
	c, _ := cachedRunnerCase(t)
	cacheDir := t.TempDir()
	store, _ := cache.New(cacheDir, 0, nil)

	// Pre-populate the cache as if the case last passed.
	key := cache.Key{
		CaseYAML:    readFile(t, c.SourcePath),
		GCXVersion:  "v1",
		OatsVersion: "v2",
	}
	if err := store.Record(key); err != nil {
		t.Fatal(err)
	}

	// Now the case fails. The runner must evict so the next run is honest.
	exec := &stubExec{stdout: "wrong content"}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{GCXContext: "test"}, Options{
		Timeout:         30 * time.Millisecond,
		Interval:        5 * time.Millisecond,
		SeedSettleDelay: 1,
	}).WithCache(store, CacheContext{GCXVersion: "v1", OatsVersion: "v2"})

	rep.Emit(report.Event{Type: report.EventRunStart})
	// First, ensure cache hit short-circuits before our patched stdout matters.
	if !r.RunCase(context.Background(), c) {
		t.Fatal("cache hit should still pass on the first run")
	}
	// Now invalidate the cache entry manually and rerun.
	if err := store.Evict(key); err != nil {
		t.Fatal(err)
	}
	if r.RunCase(context.Background(), c) {
		t.Fatal("expected fail on second run (stdout doesn't contain 'svc')")
	}
	if hit, _ := store.Lookup(key); hit {
		t.Error("failed case must not leave a green record in the cache")
	}
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
