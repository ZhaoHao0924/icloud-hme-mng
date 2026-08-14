package server

import (
	"errors"
	"sync"
	"testing"
	"time"

	"icloud-hme/internal/mail"
)

type fakeInboxIMAPClient struct {
	connectErr                 error
	connectCalls               int
	disconnectCalls            int
	listSummaries              []mail.Message
	listSummariesCalls         int
	listSummariesNextBeforeUID uint32
	lastListSummariesBeforeUID uint32
	fullMessage                 *mail.FullMessage
	getFullCalls                int
	lastFullUID                 uint32
}

func (c *fakeInboxIMAPClient) Connect() error {
	c.connectCalls++
	return c.connectErr
}

func (c *fakeInboxIMAPClient) Disconnect() { c.disconnectCalls++ }

func (c *fakeInboxIMAPClient) FindByRecipient(string, int, int) ([]mail.Message, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeInboxIMAPClient) FindByRecipientSummaries(string, int, int) ([]mail.Message, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeInboxIMAPClient) FindByRecipientPage(
	string,
	int,
	int,
	uint32,
) (mail.MessagePage, error) {
	return mail.MessagePage{}, errors.New("not implemented")
}

func (c *fakeInboxIMAPClient) FindByRecipientSummariesPage(
	string,
	int,
	int,
	uint32,
) (mail.MessagePage, error) {
	return mail.MessagePage{}, errors.New("not implemented")
}

func (c *fakeInboxIMAPClient) GetPreview(uint32) (*mail.Message, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeInboxIMAPClient) GetFull(uid uint32) (*mail.FullMessage, error) {
	c.getFullCalls++
	c.lastFullUID = uid
	if c.fullMessage == nil {
		return nil, errors.New("not implemented")
	}
	message := *c.fullMessage
	return &message, nil
}

func (c *fakeInboxIMAPClient) ListInbox(int, int) ([]mail.Message, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeInboxIMAPClient) ListInboxSummaries(int, int) ([]mail.Message, error) {
	c.listSummariesCalls++
	return append([]mail.Message(nil), c.listSummaries...), nil
}

func (c *fakeInboxIMAPClient) ListInboxPage(int, int, uint32) (mail.MessagePage, error) {
	return mail.MessagePage{}, errors.New("not implemented")
}

func (c *fakeInboxIMAPClient) ListInboxSummariesPage(_ int, _ int, beforeUID uint32) (mail.MessagePage, error) {
	c.listSummariesCalls++
	c.lastListSummariesBeforeUID = beforeUID
	return mail.MessagePage{
		Messages:      append([]mail.Message(nil), c.listSummaries...),
		NextBeforeUID: c.listSummariesNextBeforeUID,
	}, nil
}

func TestInboxIMAPSessionPoolReusesOneClientPerAccount(t *testing.T) {
	pool := newInboxIMAPSessionPool()
	pool.idleTTL = time.Hour
	var created []*fakeInboxIMAPClient
	factory := func() (inboxIMAPClient, error) {
		client := &fakeInboxIMAPClient{}
		created = append(created, client)
		return client, nil
	}
	var used []inboxIMAPClient
	for range 2 {
		if err := pool.Use("acc_one", factory, func(client inboxIMAPClient) error {
			used = append(used, client)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	if len(created) != 1 || created[0].connectCalls != 1 {
		t.Fatalf("created = %d, connect calls = %d, want one", len(created), created[0].connectCalls)
	}
	if used[0] != used[1] {
		t.Fatal("same account did not reuse its IMAP client")
	}

	pool.Clear()
	if created[0].disconnectCalls != 1 {
		t.Fatalf("disconnect calls = %d, want 1 after clear", created[0].disconnectCalls)
	}
}

func TestInboxIMAPSessionPoolSeparatesAccountsAndInvalidatesOne(t *testing.T) {
	pool := newInboxIMAPSessionPool()
	pool.idleTTL = time.Hour
	created := make(map[string][]*fakeInboxIMAPClient)
	factoryFor := func(accountID string) inboxIMAPClientFactory {
		return func() (inboxIMAPClient, error) {
			client := &fakeInboxIMAPClient{}
			created[accountID] = append(created[accountID], client)
			return client, nil
		}
	}
	use := func(accountID string) {
		t.Helper()
		if err := pool.Use(accountID, factoryFor(accountID), func(inboxIMAPClient) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}

	use("acc_one")
	use("acc_two")
	pool.InvalidateAccount("acc_one")
	use("acc_one")
	use("acc_two")

	if len(created["acc_one"]) != 2 {
		t.Fatalf("acc_one clients = %d, want 2 after invalidation", len(created["acc_one"]))
	}
	if created["acc_one"][0].disconnectCalls != 1 {
		t.Fatalf("invalidated client disconnects = %d, want 1", created["acc_one"][0].disconnectCalls)
	}
	if len(created["acc_two"]) != 1 || created["acc_two"][0].disconnectCalls != 0 {
		t.Fatalf("unrelated account clients = %+v", created["acc_two"])
	}
	pool.Clear()
}

func TestInboxIMAPSessionPoolReconnectsOnlyAfterReusedConnectionFails(t *testing.T) {
	pool := newInboxIMAPSessionPool()
	pool.idleTTL = time.Hour
	first := &fakeInboxIMAPClient{}
	second := &fakeInboxIMAPClient{}
	clients := []inboxIMAPClient{first, second}
	factoryCalls := 0
	factory := func() (inboxIMAPClient, error) {
		client := clients[factoryCalls]
		factoryCalls++
		return client, nil
	}
	if err := pool.Use("acc_one", factory, func(inboxIMAPClient) error { return nil }); err != nil {
		t.Fatal(err)
	}

	transientErr := errors.New("IMAP connection closed")
	operationCalls := 0
	err := pool.Use("acc_one", factory, func(client inboxIMAPClient) error {
		operationCalls++
		if client == first {
			return transientErr
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if factoryCalls != 2 || operationCalls != 2 {
		t.Fatalf("factory calls = %d, operation calls = %d, want 2 each", factoryCalls, operationCalls)
	}
	if first.disconnectCalls != 1 || second.connectCalls != 1 {
		t.Fatalf("first disconnects = %d, second connects = %d", first.disconnectCalls, second.connectCalls)
	}
	pool.Clear()
}

func TestInboxIMAPSessionPoolDoesNotRetryNewConnectionOperationFailure(t *testing.T) {
	pool := newInboxIMAPSessionPool()
	pool.idleTTL = time.Hour
	client := &fakeInboxIMAPClient{}
	factoryCalls := 0
	operationErr := errors.New("mailbox unavailable")
	err := pool.Use("acc_one", func() (inboxIMAPClient, error) {
		factoryCalls++
		return client, nil
	}, func(inboxIMAPClient) error {
		return operationErr
	})
	if !errors.Is(err, operationErr) {
		t.Fatalf("Use() error = %v, want %v", err, operationErr)
	}
	if factoryCalls != 1 || client.disconnectCalls != 1 {
		t.Fatalf("factory calls = %d, disconnect calls = %d, want 1", factoryCalls, client.disconnectCalls)
	}
	pool.Clear()
}

func TestInboxIMAPSessionPoolReconnectsAfterIdleTTL(t *testing.T) {
	now := time.Date(2026, time.August, 2, 13, 0, 0, 0, time.UTC)
	pool := newInboxIMAPSessionPool()
	pool.now = func() time.Time { return now }
	pool.idleTTL = time.Minute
	first := &fakeInboxIMAPClient{}
	second := &fakeInboxIMAPClient{}
	clients := []inboxIMAPClient{first, second}
	factoryCalls := 0
	factory := func() (inboxIMAPClient, error) {
		client := clients[factoryCalls]
		factoryCalls++
		return client, nil
	}
	if err := pool.Use("acc_one", factory, func(inboxIMAPClient) error { return nil }); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := pool.Use("acc_one", factory, func(inboxIMAPClient) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if factoryCalls != 2 || first.disconnectCalls != 1 {
		t.Fatalf("factory calls = %d, first disconnects = %d, want 2 and 1", factoryCalls, first.disconnectCalls)
	}
	pool.Clear()
}

func TestInboxIMAPSessionPoolExpiresIdleClientAutomatically(t *testing.T) {
	pool := newInboxIMAPSessionPool()
	pool.idleTTL = 20 * time.Millisecond
	client := &timerInboxIMAPClient{disconnected: make(chan struct{})}
	if err := pool.Use("acc_one", func() (inboxIMAPClient, error) {
		return client, nil
	}, func(inboxIMAPClient) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-client.disconnected:
	case <-time.After(time.Second):
		t.Fatal("idle IMAP client was not disconnected")
	}
	pool.mu.Lock()
	_, retained := pool.entries["acc_one"]
	pool.mu.Unlock()
	if retained {
		t.Fatal("expired IMAP session remained in the pool")
	}
}

func TestInboxIMAPSessionPoolSerializesOneAccount(t *testing.T) {
	pool := newInboxIMAPSessionPool()
	pool.idleTTL = time.Hour
	client := &fakeInboxIMAPClient{}
	started := make(chan struct{})
	release := make(chan struct{})
	secondEntered := make(chan struct{})
	var once sync.Once

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- pool.Use("acc_one", func() (inboxIMAPClient, error) { return client, nil }, func(inboxIMAPClient) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	secondDone := make(chan error, 1)
	factoryCalled := make(chan struct{}, 1)
	go func() {
		secondDone <- pool.Use("acc_one", func() (inboxIMAPClient, error) {
			factoryCalled <- struct{}{}
			return nil, nil
		}, func(inboxIMAPClient) error {
			once.Do(func() { close(secondEntered) })
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("second operation ran while first operation still held the IMAP session")
	case <-factoryCalled:
		t.Fatal("second request unexpectedly created another IMAP client")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second operation did not run after the first released the IMAP session")
	}
	pool.Clear()
}

func TestInboxIMAPSessionPoolRemovesFailedFactories(t *testing.T) {
	pool := newInboxIMAPSessionPool()
	factoryErr := errors.New("account not configured")
	err := pool.Use("missing", func() (inboxIMAPClient, error) {
		return nil, factoryErr
	}, func(inboxIMAPClient) error { return nil })
	if !errors.Is(err, factoryErr) {
		t.Fatalf("Use() error = %v, want %v", err, factoryErr)
	}
	pool.mu.Lock()
	_, retained := pool.entries["missing"]
	pool.mu.Unlock()
	if retained {
		t.Fatal("failed factory retained an empty account entry")
	}
}

type timerInboxIMAPClient struct {
	fakeInboxIMAPClient
	disconnected chan struct{}
	once         sync.Once
}

func (c *timerInboxIMAPClient) Disconnect() {
	c.once.Do(func() { close(c.disconnected) })
}
