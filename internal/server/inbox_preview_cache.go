package server

import (
	"sync"
	"time"

	"icloud-hme/internal/mail"
)

const (
	defaultInboxPreviewCacheEntries = 500
	defaultInboxPreviewCacheTTL     = 10 * time.Minute
)

type inboxPreviewCacheKey struct {
	accountID string
	messageID string
}

type inboxPreviewCacheEntry struct {
	expiresAt time.Time
	message   mail.Message
}

type inboxPreviewCache struct {
	mu         sync.Mutex
	entries    map[inboxPreviewCacheKey]inboxPreviewCacheEntry
	maxEntries int
	now        func() time.Time
	ttl        time.Duration
}

func newInboxPreviewCache() *inboxPreviewCache {
	return &inboxPreviewCache{
		entries:    make(map[inboxPreviewCacheKey]inboxPreviewCacheEntry),
		maxEntries: defaultInboxPreviewCacheEntries,
		now:        time.Now,
		ttl:        defaultInboxPreviewCacheTTL,
	}
}

func (c *inboxPreviewCache) Get(accountID, messageID string) (mail.Message, bool) {
	if c == nil {
		return mail.Message{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := inboxPreviewCacheKey{accountID: accountID, messageID: messageID}
	entry, ok := c.entries[key]
	if !ok {
		return mail.Message{}, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return mail.Message{}, false
	}
	return entry.message, true
}

func (c *inboxPreviewCache) Set(accountID string, message mail.Message) {
	if c == nil || accountID == "" || message.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
	key := inboxPreviewCacheKey{accountID: accountID, messageID: message.ID}
	_, replacing := c.entries[key]
	if !replacing && len(c.entries) >= c.maxEntries {
		var oldestKey inboxPreviewCacheKey
		var oldestExpiry time.Time
		for key, entry := range c.entries {
			if oldestExpiry.IsZero() || entry.expiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = entry.expiresAt
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = inboxPreviewCacheEntry{
		expiresAt: now.Add(c.ttl),
		message:   message,
	}
}

func (c *inboxPreviewCache) InvalidateAccount(accountID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.accountID == accountID {
			delete(c.entries, key)
		}
	}
}

func (c *inboxPreviewCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}
