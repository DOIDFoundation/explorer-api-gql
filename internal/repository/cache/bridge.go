// Cache package implements bridge to fast in-memory object cache.
package cache

import (
	"fantom-api-graphql/internal/config"
	"fantom-api-graphql/internal/logger"
	"github.com/allegro/bigcache"
	"time"
)

// Bridge represents BigCache abstraction layer.
type Bridge struct {
	cache *bigcache.BigCache
	log   logger.Logger
}

// New creates a new BigCache bridge.
func New(cfg *config.Config, log logger.Logger) (*Bridge, error) {
	// create the cache
	c, err := bigcache.NewBigCache(cacheConfig(cfg, log))
	if err != nil {
		log.Critical(err)
		return nil, err
	}

	// log the event
	log.Notice("memory cache initialized")

	// make a new Bridge
	return &Bridge{
		cache: c,
		log:   log,
	}, nil
}

// cacheConfig constructs a configuration structure for BigCache initialization.
func cacheConfig(cfg *config.Config, log logger.Logger) bigcache.Config {
	return bigcache.Config{
		// number of shards (must be a power of 2)
		Shards: 1024,

		// time after which entry can be evicted
		LifeWindow: cfg.CacheEvictionTime,

		// Interval between removing expired entries (clean up).
		// If set to <= 0 then no action is performed.
		// Setting to < 1 second is counterproductive — bigcache has a one second resolution.
		CleanWindow: 5 * time.Minute,

		// rps * lifeWindow, used only in initial memory allocation
		MaxEntriesInWindow: 1000 * 10 * 60,

		// max entry size in bytes, used only in initial memory allocation
		MaxEntrySize: 500,

		// cache will not allocate more memory than this limit, value in MB
		// if value is reached then the oldest entries can be overridden for the new ones
		// 0 value means no size limit
		HardMaxCacheSize: 512,

		// callback fired when the oldest entry is removed because of its expiration time or no space left
		// for the new entry, or because delete was called. A bitmask representing the reason will be returned.
		// Default value is nil which means no callback and it prevents from unwrapping the oldest entry.
		OnRemove: nil,

		// OnRemoveWithReason is a callback fired when the oldest entry is removed because of its expiration time or no space left
		// for the new entry, or because delete was called. A constant representing the reason will be passed through.
		// Default value is nil which means no callback and it prevents from unwrapping the oldest entry.
		// Ignored if OnRemove is specified.
		OnRemoveWithReason: nil,

		// prints information about additional memory allocation
		Verbose: true,

		// Logger is a logging interface and used in combination with `Verbose`
		Logger: log,
	}
}