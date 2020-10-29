/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package http

import (
	"context"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

var randPerm = func(n int) []int {
	return rand.Perm(n)
}

// DialContextWithDNSCache is a helper function which returns `net.DialContext` function.
// It randomly fetches an IP from the DNS cache and dials it by the given dial
// function. It dials one by one and returns first connected `net.Conn`.
// If it fails to dial all IPs from cache it returns first error. If no baseDialFunc
// is given, it sets default dial function.
//
// You can use returned dial function for `http.Transport.DialContext`.
//
// In this function, it uses functions from `rand` package. To make it really random,
// you MUST call `rand.Seed` and change the value from the default in your application
func DialContextWithDNSCache(cache *DNSCache, baseDialCtx DialContext) DialContext {
	if baseDialCtx == nil {
		// This is same as which `http.DefaultTransport` uses.
		baseDialCtx = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext
	}
	return func(ctx context.Context, network, host string) (net.Conn, error) {
		h, p, err := net.SplitHostPort(host)
		if err != nil {
			return nil, err
		}

		// Fetch DNS result from cache.
		//
		// ctxLookup is only used for canceling DNS Lookup.
		ctxLookup, cancelF := context.WithTimeout(ctx, cache.lookupTimeout)
		defer cancelF()
		addrs, err := cache.Fetch(ctxLookup, h)
		if err != nil {
			return nil, err
		}

		var firstErr error
		for _, randomIndex := range randPerm(len(addrs)) {
			conn, err := baseDialCtx(ctx, "tcp", net.JoinHostPort(addrs[randomIndex], p))
			if err == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}

		return nil, firstErr
	}
}

const (
	// cacheSize is initial size of addr and IP list cache map.
	cacheSize = 64
)

// defaultFreq is default frequency a resolver refreshes DNS cache.
var (
	defaultFreq          = 3 * time.Second
	defaultLookupTimeout = 10 * time.Second
)

// DNSCache is DNS cache resolver which cache DNS resolve results in memory.
type DNSCache struct {
	sync.RWMutex

	lookupHostFn  func(ctx context.Context, host string) ([]string, error)
	lookupTimeout time.Duration

	cache  map[string][]string
	closer func()
}

// NewDNSCache initializes DNS cache resolver and starts auto refreshing
// in a new goroutine. To stop auto refreshing, call `Stop()` function.
// Once `Stop()` is called auto refreshing cannot be resumed.
func NewDNSCache(freq time.Duration, lookupTimeout time.Duration) *DNSCache {
	if freq <= 0 {
		freq = defaultFreq
	}

	if lookupTimeout <= 0 {
		lookupTimeout = defaultLookupTimeout
	}

	ticker := time.NewTicker(freq)
	ch := make(chan struct{})
	closer := func() {
		ticker.Stop()
		close(ch)
	}

	r := &DNSCache{
		lookupHostFn:  net.DefaultResolver.LookupHost,
		lookupTimeout: lookupTimeout,
		cache:         make(map[string][]string, cacheSize),
		closer:        closer,
	}

	go func() {
		for {
			select {
			case <-ticker.C:
				r.Refresh()
			case <-ch:
				return
			}
		}
	}()

	return r
}

// LookupHost lookups address list from DNS server, persist the results
// in-memory cache. `Fetch` is used to obtain the values for a given host.
func (r *DNSCache) LookupHost(ctx context.Context, host string) ([]string, error) {
	addrs, err := r.lookupHostFn(ctx, host)
	if err != nil {
		return nil, err
	}

	r.Lock()
	r.cache[host] = addrs
	r.Unlock()

	return addrs, nil
}

// Fetch fetches IP list from the cache. If IP list of the given addr is not in the cache,
// then it lookups from DNS server by `Lookup` function.
func (r *DNSCache) Fetch(ctx context.Context, host string) ([]string, error) {
	r.RLock()
	addrs, ok := r.cache[host]
	r.RUnlock()
	if ok {
		return addrs, nil
	}
	return r.LookupHost(ctx, host)
}

// Refresh refreshes IP list cache, automatically.
func (r *DNSCache) Refresh() {
	r.RLock()
	hosts := make([]string, 0, len(r.cache))
	for host := range r.cache {
		hosts = append(hosts, host)
	}
	r.RUnlock()

	for _, host := range hosts {
		ctx, cancelF := context.WithTimeout(context.Background(), r.lookupTimeout)
		if _, err := r.LookupHost(ctx, host); err != nil {
			log.Println("failed to refresh DNS cache, resolver is unavailable", err)
		}
		cancelF()
	}
}

// Stop stops auto refreshing.
func (r *DNSCache) Stop() {
	r.Lock()
	defer r.Unlock()
	if r.closer != nil {
		r.closer()
		r.closer = nil
	}
}
