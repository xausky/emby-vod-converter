package main

import (
	"github.com/akyoto/cache"
	"sync"
	"time"
)

type SafeCache struct {
	cache     *cache.Cache
	lockCache *cache.Cache
}

func NewSafeCache(defaultExpiration time.Duration) *SafeCache {
	return &SafeCache{
		cache:     cache.New(defaultExpiration),
		lockCache: cache.New(defaultExpiration),
	}
}

func (sc *SafeCache) ComputeIfAbsent(key string, computeFunc func() interface{}, duration time.Duration) interface{} {
	// Ensure the lock for the key exists
	lock, _ := sc.lockCache.Get(key)
	if lock == nil {
		lock = &sync.Mutex{}
		sc.lockCache.Set(key, lock, duration)
	}

	// Acquire the lock for the key
	mutex := lock.(*sync.Mutex)
	mutex.Lock()
	defer mutex.Unlock()

	// Check if the key exists in the cache
	val, found := sc.cache.Get(key)
	if !found {
		// If not, compute the value and store it
		val = computeFunc()
		sc.cache.Set(key, val, duration)
	}

	return val
}
