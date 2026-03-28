package cache

import (
	"sync"
	"time"

	"web-scrapper/internal/models"
)

// entry holds a cached result with an expiry time.
type entry struct {
	result    *models.AnalysisResult
	expiresAt time.Time
}

// Cache provides a thread-safe TTL cache for analysis results.
type Cache struct {
	mu        sync.RWMutex
	items     map[string]entry
	ttl       time.Duration
	maxSize   int
	stopCh    chan struct{}
	doneCh    chan struct{}
	closeOnce sync.Once
}

// New creates a new Cache with the given TTL and max size.
// It starts a background goroutine to periodically evict expired entries.
func New(ttl time.Duration, maxSize int) *Cache {
	c := &Cache{
		items:   make(map[string]entry),
		ttl:     ttl,
		maxSize: maxSize,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go c.evictLoop()
	return c
}

// Get retrieves a cached result for the given URL.
// Returns the result and true if found and not expired, nil and false otherwise.
// Expired entries are deleted eagerly to free capacity.
func (c *Cache) Get(url string) (*models.AnalysisResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[url]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.items, url)
		return nil, false
	}
	return e.result, true
}

// Set stores a result in the cache. If the cache is full, expired entries are
// evicted first; if still at capacity, the oldest entry is evicted.
func (c *Cache) Set(url string, result *models.AnalysisResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.items) >= c.maxSize {
		c.evictExpired()
	}
	if len(c.items) >= c.maxSize {
		c.evictOldest()
	}

	c.items[url] = entry{
		result:    result,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// evictOldest removes the entry with the earliest expiry time.
func (c *Cache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, e := range c.items {
		if oldestKey == "" || e.expiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = e.expiresAt
		}
	}

	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}

// evictExpired removes all expired entries. Caller must hold c.mu.
func (c *Cache) evictExpired() {
	now := time.Now()
	for key, e := range c.items {
		if now.After(e.expiresAt) {
			delete(c.items, key)
		}
	}
}

// evictLoop periodically removes expired entries.
func (c *Cache) evictLoop() {
	defer close(c.doneCh)
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			c.evictExpired()
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}

// Close stops the background eviction goroutine.
func (c *Cache) Close() {
	c.closeOnce.Do(func() {
		close(c.stopCh)
		<-c.doneCh
	})
}
