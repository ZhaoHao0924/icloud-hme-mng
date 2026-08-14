package server

import (
	"fmt"
	"sync"
	"time"

	"icloud-hme/internal/mail"
)

const defaultInboxIMAPSessionIdleTTL = 2 * time.Minute

// inboxIMAPClient is the read-only IMAP surface used by the inbox handlers.
// A session is never used concurrently because the IMAP mailbox selection state
// belongs to its connection.
type inboxIMAPClient interface {
	Connect() error
	Disconnect()
	FindByRecipientPage(recipient string, limit int, days int, beforeUID uint32) (mail.MessagePage, error)
	FindByRecipientSummariesPage(
		recipient string,
		limit int,
		days int,
		beforeUID uint32,
	) (mail.MessagePage, error)
	GetFull(uid uint32) (*mail.FullMessage, error)
	GetPreview(uid uint32) (*mail.Message, error)
	ListInboxPage(limit int, days int, beforeUID uint32) (mail.MessagePage, error)
	ListInboxSummariesPage(limit int, days int, beforeUID uint32) (mail.MessagePage, error)
}

type inboxIMAPClientFactory func() (inboxIMAPClient, error)

type inboxIMAPClientFactoryError struct {
	err error
}

func (e *inboxIMAPClientFactoryError) Error() string { return e.err.Error() }

func (e *inboxIMAPClientFactoryError) Unwrap() error { return e.err }

// inboxIMAPSessionPool keeps one short-lived authenticated connection per
// account. This removes the TLS and App Password login round trip from normal
// refreshes while retaining a bounded idle lifetime.
type inboxIMAPSessionPool struct {
	mu      sync.Mutex
	entries map[string]*inboxIMAPSession
	idleTTL time.Duration
	now     func() time.Time
}

type inboxIMAPSession struct {
	mu       sync.Mutex
	client   inboxIMAPClient
	lastUsed time.Time
	retired  bool
	timer    *time.Timer
}

func newInboxIMAPSessionPool() *inboxIMAPSessionPool {
	return &inboxIMAPSessionPool{
		entries: make(map[string]*inboxIMAPSession),
		idleTTL: defaultInboxIMAPSessionIdleTTL,
		now:     time.Now,
	}
}

// Use serializes work for one account and retries once only when a previously
// reused connection fails. New connections are not retried here so ordinary
// IMAP errors preserve their existing behavior.
func (p *inboxIMAPSessionPool) Use(
	accountID string,
	factory inboxIMAPClientFactory,
	operation func(inboxIMAPClient) error,
) error {
	if p == nil {
		return fmt.Errorf("IMAP 会话池不可用")
	}
	if accountID == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}
	if factory == nil {
		return fmt.Errorf("IMAP 客户端工厂不可用")
	}
	if operation == nil {
		return fmt.Errorf("IMAP 操作不可用")
	}

	retryReusedConnection := true
	for {
		entry := p.entryFor(accountID)
		entry.mu.Lock()
		if entry.retired {
			entry.mu.Unlock()
			p.removeEntry(accountID, entry)
			continue
		}

		now := p.currentTime()
		if p.isIdle(entry, now) {
			p.disconnectLocked(entry)
		}

		reused := entry.client != nil
		if entry.client == nil {
			client, err := factory()
			if err != nil {
				p.retireEmptyLocked(entry)
				entry.mu.Unlock()
				p.removeEntry(accountID, entry)
				return &inboxIMAPClientFactoryError{err: err}
			}
			if client == nil {
				p.retireEmptyLocked(entry)
				entry.mu.Unlock()
				p.removeEntry(accountID, entry)
				return &inboxIMAPClientFactoryError{err: fmt.Errorf("IMAP 客户端为空")}
			}
			if err := client.Connect(); err != nil {
				client.Disconnect()
				p.retireEmptyLocked(entry)
				entry.mu.Unlock()
				p.removeEntry(accountID, entry)
				return err
			}
			entry.client = client
		}

		client := entry.client
		err := operation(client)
		if err == nil {
			entry.lastUsed = p.currentTime()
			p.armIdleTimerLocked(accountID, entry)
			entry.mu.Unlock()
			return nil
		}

		p.disconnectLocked(entry)
		entry.mu.Unlock()
		if reused && retryReusedConnection {
			retryReusedConnection = false
			continue
		}
		return err
	}
}

func (p *inboxIMAPSessionPool) InvalidateAccount(accountID string) {
	if p == nil || accountID == "" {
		return
	}
	p.mu.Lock()
	entry := p.entries[accountID]
	delete(p.entries, accountID)
	p.mu.Unlock()
	p.retire(entry)
}

func (p *inboxIMAPSessionPool) Clear() {
	if p == nil {
		return
	}
	p.mu.Lock()
	entries := make([]*inboxIMAPSession, 0, len(p.entries))
	for _, entry := range p.entries {
		entries = append(entries, entry)
	}
	clear(p.entries)
	p.mu.Unlock()
	for _, entry := range entries {
		p.retire(entry)
	}
}

func (p *inboxIMAPSessionPool) entryFor(accountID string) *inboxIMAPSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.entries[accountID]; entry != nil {
		return entry
	}
	entry := &inboxIMAPSession{}
	p.entries[accountID] = entry
	return entry
}

func (p *inboxIMAPSessionPool) removeEntry(accountID string, expected *inboxIMAPSession) {
	p.mu.Lock()
	if p.entries[accountID] == expected {
		delete(p.entries, accountID)
	}
	p.mu.Unlock()
}

func (p *inboxIMAPSessionPool) retire(entry *inboxIMAPSession) {
	if entry == nil {
		return
	}
	entry.mu.Lock()
	if entry.retired {
		entry.mu.Unlock()
		return
	}
	entry.retired = true
	p.stopIdleTimerLocked(entry)
	client := entry.client
	entry.client = nil
	entry.lastUsed = time.Time{}
	entry.mu.Unlock()
	if client != nil {
		client.Disconnect()
	}
}

func (p *inboxIMAPSessionPool) isIdle(entry *inboxIMAPSession, now time.Time) bool {
	if entry.client == nil || entry.lastUsed.IsZero() || p.idleTTL <= 0 {
		return false
	}
	return !now.Before(entry.lastUsed.Add(p.idleTTL))
}

func (p *inboxIMAPSessionPool) disconnectLocked(entry *inboxIMAPSession) {
	p.stopIdleTimerLocked(entry)
	client := entry.client
	entry.client = nil
	entry.lastUsed = time.Time{}
	if client != nil {
		client.Disconnect()
	}
}

func (p *inboxIMAPSessionPool) retireEmptyLocked(entry *inboxIMAPSession) {
	entry.retired = true
	p.stopIdleTimerLocked(entry)
	entry.client = nil
	entry.lastUsed = time.Time{}
}

func (p *inboxIMAPSessionPool) armIdleTimerLocked(accountID string, entry *inboxIMAPSession) {
	if p.idleTTL <= 0 || entry.retired {
		return
	}
	if entry.timer == nil {
		entry.timer = time.AfterFunc(p.idleTTL, func() {
			p.expireIfIdle(accountID, entry)
		})
		return
	}
	entry.timer.Reset(p.idleTTL)
}

func (p *inboxIMAPSessionPool) stopIdleTimerLocked(entry *inboxIMAPSession) {
	if entry.timer != nil {
		entry.timer.Stop()
	}
}

func (p *inboxIMAPSessionPool) expireIfIdle(accountID string, entry *inboxIMAPSession) {
	entry.mu.Lock()
	if entry.retired {
		entry.mu.Unlock()
		return
	}
	if entry.client == nil || p.idleTTL <= 0 {
		entry.retired = true
		entry.mu.Unlock()
		p.removeEntry(accountID, entry)
		return
	}

	now := p.currentTime()
	if !p.isIdle(entry, now) {
		remaining := entry.lastUsed.Add(p.idleTTL).Sub(now)
		if remaining > 0 && entry.timer != nil {
			entry.timer.Reset(remaining)
		}
		entry.mu.Unlock()
		return
	}

	entry.retired = true
	client := entry.client
	entry.client = nil
	entry.lastUsed = time.Time{}
	entry.mu.Unlock()
	p.removeEntry(accountID, entry)
	client.Disconnect()
}

func (p *inboxIMAPSessionPool) currentTime() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}
