package dingtalk

import (
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/channels"
)

type webhookEntry struct {
	URL       string
	ExpiredAt time.Time
	StaffID   string
}

type webhookCache struct {
	mu    sync.Mutex
	byKey map[string]webhookEntry
}

func newWebhookCache() *webhookCache {
	return &webhookCache{byKey: map[string]webhookEntry{}}
}

func cacheKey(scene channels.Scene, conversationID string) string {
	return string(scene) + "|" + strings.TrimSpace(conversationID)
}

func (c *webhookCache) put(scene channels.Scene, conversationID, webhook string, expiredMs int64, staffID string) {
	webhook = strings.TrimSpace(webhook)
	conversationID = strings.TrimSpace(conversationID)
	if webhook == "" || conversationID == "" {
		return
	}
	entry := webhookEntry{
		URL:     webhook,
		StaffID: strings.TrimSpace(staffID),
	}
	if expiredMs > 0 {
		entry.ExpiredAt = time.UnixMilli(expiredMs)
	} else {
		// DingTalk typically gives ~1h; keep a conservative default when absent.
		entry.ExpiredAt = time.Now().Add(50 * time.Minute)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(time.Now())
	c.byKey[cacheKey(scene, conversationID)] = entry
}

func (c *webhookCache) get(scene channels.Scene, conversationID string) (webhookEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.sweepLocked(now)
	entry, ok := c.byKey[cacheKey(scene, conversationID)]
	if !ok || strings.TrimSpace(entry.URL) == "" {
		return webhookEntry{}, false
	}
	if !entry.ExpiredAt.IsZero() && now.After(entry.ExpiredAt) {
		entry.URL = ""
		entry.ExpiredAt = time.Time{}
		c.byKey[cacheKey(scene, conversationID)] = entry
		return webhookEntry{}, false
	}
	return entry, true
}

func (c *webhookCache) staffID(scene channels.Scene, conversationID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(time.Now())
	if entry, ok := c.byKey[cacheKey(scene, conversationID)]; ok {
		return entry.StaffID
	}
	return ""
}

func (c *webhookCache) rememberStaff(scene channels.Scene, conversationID, staffID string) {
	staffID = strings.TrimSpace(staffID)
	conversationID = strings.TrimSpace(conversationID)
	if staffID == "" || conversationID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cacheKey(scene, conversationID)
	entry := c.byKey[key]
	entry.StaffID = staffID
	c.byKey[key] = entry
}

func (c *webhookCache) sweepLocked(now time.Time) {
	for k, e := range c.byKey {
		if !e.ExpiredAt.IsZero() && now.After(e.ExpiredAt) && e.URL != "" {
			// Keep staffID for OpenAPI after webhook TTL by clearing URL only.
			e.URL = ""
			e.ExpiredAt = time.Time{}
			c.byKey[k] = e
		}
	}
	if len(c.byKey) > 4096 {
		for k := range c.byKey {
			delete(c.byKey, k)
			if len(c.byKey) <= 2048 {
				break
			}
		}
	}
}
