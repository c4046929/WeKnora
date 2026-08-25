package embedding

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultEmbeddingCacheTTL        = 24 * time.Hour
	defaultEmbeddingCacheMaxEntries = 10_000

	embeddingCacheEnabledKey    = "embedding_cache_enabled"
	embeddingCacheTTLSecondsKey = "embedding_cache_ttl_seconds"
	embeddingCacheMaxEntriesKey = "embedding_cache_max_entries"
)

type embeddingCacheOptions struct {
	enabled    bool
	ttl        time.Duration
	maxEntries int
}

type embeddingCacheEntry struct {
	key       string
	vector    []float32
	expiresAt time.Time
}

// embeddingCacheStore is shared across model instances because modelService
// rebuilds an Embedder for each lookup. The bounded LRU prevents repeated model
// creation from creating an unbounded number of caches.
type embeddingCacheStore struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	lru     *list.List
}

var sharedEmbeddingCache = newEmbeddingCacheStore()

func newEmbeddingCacheStore() *embeddingCacheStore {
	return &embeddingCacheStore{
		entries: make(map[string]*list.Element),
		lru:     list.New(),
	}
}

func (s *embeddingCacheStore) get(key string, now time.Time) ([]float32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	element, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	entry := element.Value.(*embeddingCacheEntry)
	if !now.Before(entry.expiresAt) {
		s.remove(element)
		return nil, false
	}
	s.lru.MoveToFront(element)
	return cloneVector(entry.vector), true
}

func (s *embeddingCacheStore) put(key string, vector []float32, expiresAt time.Time, maxEntries int) {
	if maxEntries <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if element, ok := s.entries[key]; ok {
		entry := element.Value.(*embeddingCacheEntry)
		entry.vector = cloneVector(vector)
		entry.expiresAt = expiresAt
		s.lru.MoveToFront(element)
	} else {
		entry := &embeddingCacheEntry{key: key, vector: cloneVector(vector), expiresAt: expiresAt}
		element := s.lru.PushFront(entry)
		s.entries[key] = element
	}
	for s.lru.Len() > maxEntries {
		s.remove(s.lru.Back())
	}
}

func (s *embeddingCacheStore) remove(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(*embeddingCacheEntry)
	delete(s.entries, entry.key)
	s.lru.Remove(element)
}

type cachedEmbedder struct {
	inner     Embedder
	namespace string
	options   embeddingCacheOptions
	store     *embeddingCacheStore
	now       func() time.Time
}

func (c *cachedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	key := c.key(text)
	if vector, ok := c.store.get(key, c.now()); ok {
		return vector, nil
	}
	vector, err := c.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	c.store.put(key, vector, c.now().Add(c.options.ttl), c.options.maxEntries)
	return cloneVector(vector), nil
}

func (c *cachedEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	return c.batchEmbed(ctx, texts, func(missing []string) ([][]float32, error) {
		return c.inner.BatchEmbed(ctx, missing)
	})
}

func (c *cachedEmbedder) BatchEmbedWithPool(
	ctx context.Context, _ Embedder, texts []string,
) ([][]float32, error) {
	return c.batchEmbed(ctx, texts, func(missing []string) ([][]float32, error) {
		return c.inner.BatchEmbedWithPool(ctx, c.inner, missing)
	})
}

func (c *cachedEmbedder) batchEmbed(
	ctx context.Context,
	texts []string,
	fetch func([]string) ([][]float32, error),
) ([][]float32, error) {
	results := make([][]float32, len(texts))
	type miss struct {
		key     string
		text    string
		indices []int
	}
	misses := make([]miss, 0, len(texts))
	missByKey := make(map[string]int, len(texts))
	now := c.now()

	for index, text := range texts {
		key := c.key(text)
		if vector, ok := c.store.get(key, now); ok {
			results[index] = vector
			continue
		}
		if missIndex, ok := missByKey[key]; ok {
			misses[missIndex].indices = append(misses[missIndex].indices, index)
			continue
		}
		missByKey[key] = len(misses)
		misses = append(misses, miss{key: key, text: text, indices: []int{index}})
	}
	if len(misses) == 0 {
		return results, nil
	}

	missingTexts := make([]string, len(misses))
	for index := range misses {
		missingTexts[index] = misses[index].text
	}
	vectors, err := fetch(missingTexts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(misses) {
		return nil, fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(vectors), len(misses))
	}

	expiresAt := c.now().Add(c.options.ttl)
	for missIndex, item := range misses {
		vector := vectors[missIndex]
		c.store.put(item.key, vector, expiresAt, c.options.maxEntries)
		for _, resultIndex := range item.indices {
			results[resultIndex] = cloneVector(vector)
		}
	}
	return results, nil
}

func (c *cachedEmbedder) key(text string) string {
	digest := sha256.Sum256([]byte(c.namespace + "\x00" + text))
	return hex.EncodeToString(digest[:])
}

func (c *cachedEmbedder) GetModelName() string { return c.inner.GetModelName() }
func (c *cachedEmbedder) GetDimensions() int   { return c.inner.GetDimensions() }
func (c *cachedEmbedder) GetModelID() string   { return c.inner.GetModelID() }

func wrapEmbeddingCache(embedder Embedder, config Config) Embedder {
	options := embeddingCacheOptionsFromConfig(config)
	if embedder == nil || !options.enabled {
		return embedder
	}
	return newCachedEmbedder(embedder, embeddingCacheNamespace(config), options, sharedEmbeddingCache)
}

func newCachedEmbedder(
	embedder Embedder,
	namespace string,
	options embeddingCacheOptions,
	store *embeddingCacheStore,
) Embedder {
	return &cachedEmbedder{
		inner:     embedder,
		namespace: namespace,
		options:   options,
		store:     store,
		now:       time.Now,
	}
}

func embeddingCacheOptionsFromConfig(config Config) embeddingCacheOptions {
	options := embeddingCacheOptions{
		enabled:    true,
		ttl:        defaultEmbeddingCacheTTL,
		maxEntries: defaultEmbeddingCacheMaxEntries,
	}
	if value := strings.TrimSpace(config.ExtraConfig[embeddingCacheEnabledKey]); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			options.enabled = parsed
		}
	}
	if value := strings.TrimSpace(config.ExtraConfig[embeddingCacheTTLSecondsKey]); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
			options.ttl = time.Duration(seconds) * time.Second
		}
	}
	if value := strings.TrimSpace(config.ExtraConfig[embeddingCacheMaxEntriesKey]); value != "" {
		if entries, err := strconv.Atoi(value); err == nil && entries > 0 {
			options.maxEntries = entries
		}
	}
	return options
}

func embeddingCacheNamespace(config Config) string {
	identity := strings.Join([]string{
		config.ModelID,
		config.ModelName,
		string(config.Source),
		config.Provider,
		config.BaseURL,
		strconv.Itoa(config.Dimensions),
		strconv.Itoa(config.TruncatePromptTokens),
		strconv.FormatBool(config.SupportsDimensionOverride),
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func cloneVector(vector []float32) []float32 {
	if vector == nil {
		return nil
	}
	return append([]float32(nil), vector...)
}
