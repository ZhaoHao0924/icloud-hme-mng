package server

import (
	"sync"
	"time"

	"icloud-hme/internal/mail"
)

const (
	defaultInboxPreviewCacheEntries = 500
	defaultInboxPreviewCacheTTL     = 10 * time.Minute
	maxInboxFullMessageCacheBody    = 512 << 10
	maxInboxFullMessageCacheEntries = 50
)

type inboxPreviewCacheKey struct {
	accountID string
	messageID string
}

type inboxPreviewCacheEntry struct {
	expiresAt time.Time
	message   mail.FullMessage
	complete  bool
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
	return entry.message.Message, true
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
	entry := inboxPreviewCacheEntry{
		expiresAt: now.Add(c.ttl),
		message:   mail.FullMessage{Message: message},
	}
	if current, found := c.entries[key]; found && current.complete {
		entry.message = current.message
		entry.message.Message = message
		entry.complete = true
	}
	c.entries[key] = entry
}

func (c *inboxPreviewCache) GetFull(accountID, messageID string) (mail.FullMessage, bool) {
	if c == nil {
		return mail.FullMessage{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := inboxPreviewCacheKey{accountID: accountID, messageID: messageID}
	entry, ok := c.entries[key]
	if !ok || !entry.complete {
		return mail.FullMessage{}, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return mail.FullMessage{}, false
	}
	return entry.message, true
}

func (c *inboxPreviewCache) SetFull(accountID string, message mail.FullMessage) {
	if c == nil || accountID == "" || message.ID == "" || len(message.Body) > maxInboxFullMessageCacheBody {
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
	current, replacing := c.entries[key]
	if !replacing || !current.complete {
		for c.fullEntryCountLocked() >= maxInboxFullMessageCacheEntries {
			if !c.evictOldestFullEntryLocked() {
				break
			}
		}
	}
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
		complete:  true,
		expiresAt: now.Add(c.ttl),
		message:   message,
	}
}

func (c *inboxPreviewCache) fullEntryCountLocked() int {
	count := 0
	for _, entry := range c.entries {
		if entry.complete {
			count++
		}
	}
	return count
}

func (c *inboxPreviewCache) evictOldestFullEntryLocked() bool {
	var oldestKey inboxPreviewCacheKey
	var oldestExpiry time.Time
	for key, entry := range c.entries {
		if !entry.complete {
			continue
		}
		if oldestExpiry.IsZero() || entry.expiresAt.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = entry.expiresAt
		}
	}
	if oldestExpiry.IsZero() {
		return false
	}
	delete(c.entries, oldestKey)
	return true
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
