package gateshare

import (
	"sync"
	"time"
)

const (
	publicRateWindow = time.Minute
	publicRateMax    = 30
	// An open chat polls every 2s plus a 1.5s seed loop while a turn starts
	// (~70/min); a share page and its drawer, or viewers behind one NAT, share it.
	PreviewRateMax = 180
	ipLimiterGCAt  = 64

	RateBucketPreview = "preview"
	RateBucketDecide  = "decide"
	RateBucketTicket  = "ticket"
)

type ipWindow struct {
	start time.Time
	count int
}

// IPLimiter is a per-IP sliding-window QPS limiter (not login lockout).
// In-process only; expired IP entries are garbage-collected.
// Preview and decide use separate buckets so polling cannot starve confirm.
type IPLimiter struct {
	mu    sync.Mutex
	byKey map[string]*ipWindow
}

// NewIPLimiter builds a public-endpoint limiter.
func NewIPLimiter() *IPLimiter {
	return &IPLimiter{byKey: map[string]*ipWindow{}}
}

func bucketKey(ip, bucket string) string {
	if ip == "" {
		ip = "unknown"
	}
	if bucket == "" {
		bucket = RateBucketPreview
	}
	return ip + "\x00" + bucket
}

// AllowBucket reports whether ip may proceed on the named bucket.
func (l *IPLimiter) AllowBucket(ip, bucket string) bool {
	key := bucketKey(ip, bucket)
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.byKey) >= ipLimiterGCAt {
		l.gcExpiredLocked(now)
	}
	w := l.byKey[key]
	if w == nil || now.Sub(w.start) >= publicRateWindow {
		l.byKey[key] = &ipWindow{start: now, count: 1}
		return true
	}
	if w.count >= bucketMax(bucket) {
		return false
	}
	w.count++
	return true
}

func bucketMax(bucket string) int {
	if bucket == "" || bucket == RateBucketPreview {
		return PreviewRateMax
	}
	return publicRateMax
}

func (l *IPLimiter) gcExpiredLocked(now time.Time) {
	for k, w := range l.byKey {
		if w == nil || now.Sub(w.start) >= publicRateWindow {
			delete(l.byKey, k)
		}
	}
}
