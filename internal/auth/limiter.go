package auth

import (
	"sync"
	"time"
)

const (
	ScopeLogin       = "login"
	ScopeSetup       = "setup"
	ScopeProbe       = "probe"
	ScopeIntegration = "integration"
	ScopeReauth      = "reauth"
)

type bucketKey struct {
	scope string
	ip    string
}

type limitRule struct {
	maximum int
	window  time.Duration
}

type limitBucket struct {
	count  int
	start  time.Time
	window time.Duration
}

type Limiter struct {
	mu      sync.Mutex
	buckets map[bucketKey]limitBucket
}

func NewLimiter() *Limiter {
	return &Limiter{buckets: map[bucketKey]limitBucket{}}
}

func (l *Limiter) Allow(scope, remoteIP string, now time.Time) bool {
	rule, ok := limiterRule(scope)
	if !ok || remoteIP == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, bucket := range l.buckets {
		if !now.Before(bucket.start.Add(bucket.window)) {
			delete(l.buckets, key)
		}
	}
	key := bucketKey{scope: scope, ip: remoteIP}
	bucket := l.buckets[key]
	if bucket.start.IsZero() {
		bucket = limitBucket{start: now, window: rule.window}
	}
	if bucket.count >= rule.maximum {
		l.buckets[key] = bucket
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func (l *Limiter) Reset(scope, remoteIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, bucketKey{scope: scope, ip: remoteIP})
}

func limiterRule(scope string) (limitRule, bool) {
	switch scope {
	case ScopeLogin, ScopeReauth:
		return limitRule{maximum: 5, window: 10 * time.Minute}, true
	case ScopeSetup:
		return limitRule{maximum: 10, window: 10 * time.Minute}, true
	case ScopeProbe:
		return limitRule{maximum: 10, window: time.Minute}, true
	case ScopeIntegration:
		return limitRule{maximum: 30, window: time.Minute}, true
	default:
		return limitRule{}, false
	}
}
