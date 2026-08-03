package server

import (
	"testing"
	"time"

	"icloud-hme/internal/mail"
)

func TestInboxPreviewCacheExpiresAndInvalidatesAccounts(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	cache := newInboxPreviewCache()
	cache.now = func() time.Time { return now }
	cache.ttl = time.Minute
	cache.Set("acc_one", mail.Message{ID: "7", Preview: "first"})
	cache.Set("acc_two", mail.Message{ID: "8", Preview: "second"})

	if message, ok := cache.Get("acc_one", "7"); !ok || message.Preview != "first" {
		t.Fatalf("cache hit = (%+v, %v), want first preview", message, ok)
	}
	cache.InvalidateAccount("acc_one")
	if _, ok := cache.Get("acc_one", "7"); ok {
		t.Fatal("invalidated account preview remained cached")
	}
	if _, ok := cache.Get("acc_two", "8"); !ok {
		t.Fatal("invalidating one account removed another account preview")
	}

	now = now.Add(time.Minute)
	if _, ok := cache.Get("acc_two", "8"); ok {
		t.Fatal("expired preview remained cached")
	}
}

func TestInboxPreviewCacheEnforcesEntryLimit(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	cache := newInboxPreviewCache()
	cache.now = func() time.Time { return now }
	cache.maxEntries = 2
	cache.ttl = time.Hour
	cache.Set("acc", mail.Message{ID: "1", Preview: "one"})
	now = now.Add(time.Second)
	cache.Set("acc", mail.Message{ID: "2", Preview: "two"})
	now = now.Add(time.Second)
	cache.Set("acc", mail.Message{ID: "3", Preview: "three"})

	if _, ok := cache.Get("acc", "1"); ok {
		t.Fatal("oldest preview was not evicted")
	}
	for _, id := range []string{"2", "3"} {
		if _, ok := cache.Get("acc", id); !ok {
			t.Fatalf("preview %s unexpectedly evicted", id)
		}
	}
}

func TestInboxPreviewCacheStoresEmptyPreviewAndUpdatesInPlace(t *testing.T) {
	cache := newInboxPreviewCache()
	cache.maxEntries = 1
	cache.Set("acc", mail.Message{ID: "1"})
	cache.Set("acc", mail.Message{ID: "1", Preview: "loaded"})

	message, ok := cache.Get("acc", "1")
	if !ok || message.Preview != "loaded" {
		t.Fatalf("updated cache entry = (%+v, %v), want loaded preview", message, ok)
	}
}
