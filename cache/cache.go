// Package cache holds OATS's skip-when-unchanged store.
//
// Idea: if (this case yaml, this fixture config, this gcx version) all hash
// to the same value as a previous green run within the TTL, skip the case.
// On a hit, the runner emits a case.skip event and moves on. On a miss, the
// case runs as usual; on pass, we record the hash. On fail, we evict the
// entry.
//
// Storage is one file per hash under cacheDir, holding a single RFC3339
// timestamp. Crude on purpose — there is no daemon to coordinate eviction,
// no concurrent-write protocol beyond "last writer wins." A future enhancement
// could move to a single index file if directory listings become slow.
//
// There is no hard cap on entry count: files accumulate one per
// (case, fixture, gcx version) key, and expired entries are evicted lazily on
// access, so TTL bounds growth in practice. A size/count cap is a possible
// future enhancement if a long-lived cache dir ever grows unwieldy.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultTTL is applied when CacheConfig.TTLDays is zero. One week strikes a
// balance: long enough that local dev re-runs are essentially free, short
// enough that a stale entry from before a gcx upgrade doesn't survive
// indefinitely.
const DefaultTTL = 7 * 24 * time.Hour

// Store is the on-disk cache.
type Store struct {
	dir string
	ttl time.Duration
	now func() time.Time
}

// New constructs a Store rooted at dir. dir is created if absent. ttl=0
// means "use DefaultTTL." now is a clock injection point for tests; nil
// uses time.Now.
func New(dir string, ttl time.Duration, now func() time.Time) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("cache: dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cache: mkdir %s: %w", dir, err)
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Store{dir: dir, ttl: ttl, now: now}, nil
}

// Key represents the inputs that, taken together, decide whether a case is
// safe to skip. All fields participate in the hash; an unset field hashes
// the same as an empty string, so two callers must agree on whether to set
// optional fields.
type Key struct {
	CaseYAML     []byte
	FixtureBytes []byte
	GCXVersion   string
	OatsVersion  string
	// Extra is an open-ended hook for callers that want to invalidate on
	// something beyond the canonical fields (e.g. image digest once compose
	// fixtures track that). Keys are sorted before hashing for stability.
	Extra map[string]string
}

// Hash returns the canonical hex hash of Key. Stable across processes and OS.
func (k Key) Hash() string {
	h := sha256.New()
	_, _ = h.Write([]byte("oats-cache-v1\n"))
	_, _ = h.Write([]byte("case:\n"))
	_, _ = h.Write(k.CaseYAML)
	_, _ = h.Write([]byte("\nfixture:\n"))
	_, _ = h.Write(k.FixtureBytes)
	_, _ = h.Write([]byte("\ngcx:" + k.GCXVersion + "\noats:" + k.OatsVersion))
	if len(k.Extra) > 0 {
		_, _ = h.Write([]byte("\nextra:\n"))
		keys := make([]string, 0, len(k.Extra))
		for k := range k.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, name := range keys {
			_, _ = h.Write([]byte(name + "=" + k.Extra[name] + "\n"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Lookup reports whether Key is recorded as green within the TTL.
// Returns (hit, recordedAt). On miss, recordedAt is the zero value.
func (s *Store) Lookup(k Key) (bool, time.Time) {
	hash := k.Hash()
	path := filepath.Join(s.dir, hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		// Corrupt entry — best to evict so we don't keep returning false
		// positives. Errors are silent: the cache is advisory.
		_ = os.Remove(path)
		return false, time.Time{}
	}
	if s.now().Sub(ts) > s.ttl {
		// Expired. Eager eviction keeps directory size bounded.
		_ = os.Remove(path)
		return false, time.Time{}
	}
	return true, ts
}

// Record marks Key as green at "now."
func (s *Store) Record(k Key) error {
	hash := k.Hash()
	path := filepath.Join(s.dir, hash)
	return os.WriteFile(path, []byte(s.now().Format(time.RFC3339Nano)), 0o644)
}

// Evict removes any entry for Key, regardless of age.
func (s *Store) Evict(k Key) error {
	hash := k.Hash()
	err := os.Remove(filepath.Join(s.dir, hash))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Clear removes every entry in the cache directory. Convenience for the
// "oats cache clear" subcommand that will land alongside the migration
// tool.
func (s *Store) Clear() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
