package auditlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsEntriesNewestFirst(t *testing.T) {
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	store, err := newStore(dataDir, func() time.Time { return now }, false)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if _, err := store.Record(RecordInput{
		Duration:  120 * time.Millisecond,
		Level:     LevelInfo,
		Operation: "读取收件箱",
		Status:    200,
	}); err != nil {
		t.Fatalf("record first entry: %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := store.Record(RecordInput{
		Duration:  3 * time.Second,
		Level:     LevelWarning,
		Operation: "更新 Cookie",
		Status:    401,
	}); err != nil {
		t.Fatalf("record second entry: %v", err)
	}

	entries, err := store.List(10)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	if entries[0].Operation != "更新 Cookie" || entries[0].DurationMS != 3000 {
		t.Errorf("newest entry = %+v", entries[0])
	}

	reopened, err := newStore(dataDir, func() time.Time { return now }, false)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	persisted, err := reopened.List(1)
	if err != nil {
		t.Fatalf("list persisted entries: %v", err)
	}
	if len(persisted) != 1 || persisted[0].Operation != "更新 Cookie" {
		t.Errorf("persisted entries = %+v", persisted)
	}
}

func TestCleanupKeepsOnlyTheRecentWeek(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	store, err := newStore(t.TempDir(), func() time.Time { return now }, false)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	cutoff := now.Add(-retention)
	expired := Entry{
		Timestamp: cutoff.Add(-24 * time.Hour),
		Level:     LevelInfo,
		Operation: "过期整日记录",
		Status:    200,
	}
	boundaryExpired := Entry{
		Timestamp: cutoff.Add(-time.Nanosecond),
		Level:     LevelWarning,
		Operation: "过期边界记录",
		Status:    400,
	}
	kept := Entry{
		Timestamp: cutoff,
		Level:     LevelError,
		Operation: "保留记录",
		Status:    502,
	}
	if err := writeEntries(store.filePath(expired.Timestamp), []Entry{expired}); err != nil {
		t.Fatalf("write expired entry: %v", err)
	}
	if err := writeEntries(store.filePath(kept.Timestamp), []Entry{boundaryExpired, kept}); err != nil {
		t.Fatalf("write boundary entries: %v", err)
	}

	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	entries, err := store.List(10)
	if err != nil {
		t.Fatalf("list after cleanup: %v", err)
	}
	if len(entries) != 1 || entries[0].Operation != "保留记录" {
		t.Errorf("entries after cleanup = %+v", entries)
	}
	if _, err := os.Stat(filepath.Join(store.dir, expired.Timestamp.Format(fileDateLayout)+fileExtension)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expired daily file error = %v, want not exist", err)
	}
}

func TestRecordNormalizesUnsafeValues(t *testing.T) {
	store, err := newStore(t.TempDir(), time.Now, false)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	entry, err := store.Record(RecordInput{
		Duration:  -time.Second,
		Level:     "unknown",
		Operation: "  重载配置  ",
		Status:    200,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if entry.Level != LevelInfo || entry.DurationMS != 0 || entry.Operation != "重载配置" {
		t.Errorf("normalized entry = %+v", entry)
	}
	if _, err := store.Record(RecordInput{Operation: "  "}); err == nil {
		t.Fatal("record empty operation succeeded")
	}
}
