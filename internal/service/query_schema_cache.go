// Package service provides a bounded in-memory cache for schema metadata
// queries. It reuses the shared Clock interface for testable TTLs and
// golang.org/x/sync/singleflight for request coalescing.
// input: sync, time, golang.org/x/sync/singleflight, internal/model
// output: QuerySchemaCache, schemaCacheKey, NewQuerySchemaCache
// pos: Bounded in-memory schema metadata cache with TTL, eviction, and singleflight
// note: if this file changes, update header and README.md
package service

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/fan/controlhub/internal/model"
)

// TTLs for schema metadata cache entries.
const (
	schemaCachePositiveTTL = 5 * time.Minute
	schemaCacheNegativeTTL = 30 * time.Second
)

// schemaCacheKey uniquely identifies a schema metadata request. It includes
// target_id, credential_ref, scope, database, kind, query, page, pageSize, and
// includeSystem. It intentionally excludes DSN, password, and database username.
type schemaCacheKey struct {
	Scope          string // "databases", "objects", "object_details"
	TargetID       uint64
	CredentialRef  string // non-secret reference only
	Database       string
	Kind           string
	Query          string
	Page           int
	PageSize       int
	IncludeSystem  bool
}

// cacheKey builds a schemaCacheKey from request parameters. The credentialRef
// is the non-secret reference (e.g. "ORDER_MYSQL_RO"), never a DSN or password.
func cacheKey(scope string, targetID uint64, credentialRef, database, kind, query string, page, pageSize int, includeSystem bool) schemaCacheKey {
	return schemaCacheKey{
		Scope:         scope,
		TargetID:      targetID,
		CredentialRef: credentialRef,
		Database:      database,
		Kind:          kind,
		Query:         query,
		Page:          page,
		PageSize:      pageSize,
		IncludeSystem: includeSystem,
	}
}

// schemaCacheEntry holds a cached value with its insertion time and whether it
// represents an empty result (for negative TTL).
type schemaCacheEntry struct {
	value     any
	insertedAt time.Time
	isEmpty   bool
}

// QuerySchemaCache is a bounded, concurrency-safe, in-memory cache for schema
// metadata. It uses injected Clock for testable TTLs and singleflight for
// coalescing identical in-flight requests.
type QuerySchemaCache struct {
	mu       sync.RWMutex
	entries  map[schemaCacheKey]*schemaCacheEntry
	order    []schemaCacheKey
	capacity int
	clock    Clock
	group    singleflight.Group
}

// NewQuerySchemaCache constructs a cache with the given capacity and clock.
func NewQuerySchemaCache(capacity int, clock Clock) *QuerySchemaCache {
	return &QuerySchemaCache{
		entries:  make(map[schemaCacheKey]*schemaCacheEntry),
		order:    make([]schemaCacheKey, 0, capacity),
		capacity: capacity,
		clock:    clock,
	}
}

// Get returns the cached value for key if it exists and has not expired. It
// returns false if the entry is missing or expired. Expired entries are lazily
// evicted.
func (c *QuerySchemaCache) Get(key schemaCacheKey) (any, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}

	now := c.clock.Now()
	ttl := schemaCachePositiveTTL
	if entry.isEmpty {
		ttl = schemaCacheNegativeTTL
	}
	if now.Sub(entry.insertedAt) >= ttl {
		// Expired — remove lazily.
		c.mu.Lock()
		c.removeKey(key)
		c.mu.Unlock()
		return nil, false
	}
	return entry.value, true
}

// Set stores a value in the cache. If the cache is at capacity, the oldest
// entry is evicted first. Empty slices and nil are treated as empty results
// with a shorter negative TTL.
func (c *QuerySchemaCache) Set(key schemaCacheKey, value any) {
	isEmpty := c.isEmptyResult(value)

	c.mu.Lock()
	defer c.mu.Unlock()

	// If the key already exists, update in place.
	if _, ok := c.entries[key]; ok {
		c.entries[key] = &schemaCacheEntry{
			value:      value,
			insertedAt: c.clock.Now(),
			isEmpty:    isEmpty,
		}
		return
	}

	// Evict oldest if at capacity.
	for len(c.entries) >= c.capacity {
		if len(c.order) == 0 {
			break
		}
		oldest := c.order[0]
		delete(c.entries, oldest)
		c.order = c.order[1:]
	}

	c.entries[key] = &schemaCacheEntry{
		value:      value,
		insertedAt: c.clock.Now(),
		isEmpty:    isEmpty,
	}
	c.order = append(c.order, key)
}

// Do coalesces concurrent calls for the same key through singleflight. The fn
// is called only once per in-flight key; other callers receive the same result.
func (c *QuerySchemaCache) Do(key schemaCacheKey, fn func() (any, error)) (any, error, bool) {
	k := fmt.Sprintf("%+v", key)
	val, err, shared := c.group.Do(k, func() (any, error) {
		return fn()
	})
	return val, err, shared
}

// removeKey removes a key from the entries map and order slice. Caller must
// hold c.mu.
func (c *QuerySchemaCache) removeKey(key schemaCacheKey) {
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// isEmptyResult reports whether value represents an empty result (nil slice,
// empty slice, or nil).
func (c *QuerySchemaCache) isEmptyResult(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case model.DatabaseListResponse:
		return len(v.Items) == 0
	case model.ObjectListResponse:
		return len(v.Items) == 0
	case *model.ObjectDetailResponse:
		return v == nil
	default:
		return false
	}
}

// unused import guard
var _ = singleflight.Group{}
