package updater

import (
	"sync"
	"time"
)

// appStatusCache owns the independently refreshed status response. Keeping it
// separate from the inventory cache prevents slow manager/task checks from
// blocking package inventory reads or Store scan coordination.
type appStatusCache struct {
	mu        sync.RWMutex
	response  StatusResponse
	loading   bool
	queued    bool
	fetchedAt time.Time
	err       string
}

type statusRefreshRequest struct {
	started bool
	queued  bool
}

func (cache *appStatusCache) beginBackgroundRefresh(forceRefresh bool, now time.Time) statusRefreshRequest {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cacheExpired := cache.fetchedAt.IsZero() || now.Sub(cache.fetchedAt) > statusCacheTTL
	if cache.loading {
		if forceRefresh {
			cache.queued = true
			return statusRefreshRequest{queued: true}
		}
		return statusRefreshRequest{}
	}
	if !forceRefresh && !cacheExpired {
		return statusRefreshRequest{}
	}
	cache.loading = true
	cache.err = ""
	return statusRefreshRequest{started: true}
}

func (cache *appStatusCache) beginSynchronousRefresh() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.loading = true
	cache.queued = false
	cache.err = ""
}

func (cache *appStatusCache) finishRefresh(response StatusResponse, now time.Time) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.response = response
	cache.fetchedAt = now
	cache.err = ""
	if cache.queued {
		cache.queued = false
		cache.loading = true
		return true
	}
	cache.loading = false
	return false
}

func (cache *appStatusCache) cancelRefresh(errorText string) StatusResponse {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.loading = false
	cache.queued = false
	cache.err = errorText
	return cache.response
}

func (cache *appStatusCache) failToStart(errorText string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.loading = false
	cache.err = errorText
}

func (cache *appStatusCache) snapshot() (StatusResponse, bool, time.Time, string) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	return cache.response, cache.loading, cache.fetchedAt, cache.err
}

func (cache *appStatusCache) refreshState() (loading bool, queued bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	return cache.loading, cache.queued
}
