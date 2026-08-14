package auditlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestRecordUsesOnlyAllowlistedAuditValues(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	store, err := newStore(t.TempDir(), func() time.Time { return now }, false)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	const sensitiveValue = "cookie=qa009-private-value"
	entry, err := store.Record(RecordInput{
		RequestID:     sensitiveValue,
		Level:         LevelError,
		Operation:     "Audit contract probe",
		OperationType: sensitiveValue,
		Status:        502,
		ErrorCode:     sensitiveValue,
		Duration:      20 * time.Millisecond,
		RetryCount:    120,
		Request: RequestSnapshot{
			Source:              sensitiveValue,
			BodyPresent:         true,
			AliasFilterApplied:  true,
			PaginationRequested: true,
		},
		Response: ResponseSnapshot{CreatedCount: -1, FailedCount: -1},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if entry.SchemaVersion != SchemaVersion || !isValidRequestID(entry.RequestID) {
		t.Errorf("entry schema/request ID = %+v", entry)
	}
	if entry.OperationType != OperationTypeUnspecified || entry.ErrorCode != "" || entry.RetryCount != 99 {
		t.Errorf("entry contract normalization = %+v", entry)
	}
	if entry.Request.Source != RequestSourceAPI || !entry.Request.BodyPresent || !entry.Request.AliasFilterApplied || !entry.Request.PaginationRequested {
		t.Errorf("request snapshot = %+v", entry.Request)
	}
	if entry.Response.Success || entry.Response.CreatedCount != 0 || entry.Response.FailedCount != 0 {
		t.Errorf("response snapshot = %+v", entry.Response)
	}

	raw, err := os.ReadFile(store.filePath(now))
	if err != nil {
		t.Fatalf("read persisted entry: %v", err)
	}
	if strings.Contains(string(raw), sensitiveValue) {
		t.Fatal("sensitive audit input was persisted")
	}
}

func TestStoreRecordsConcurrently(t *testing.T) {
	store, err := newStore(t.TempDir(), time.Now, false)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	const writers = 48
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Record(RecordInput{
				Level:         LevelInfo,
				Operation:     "Concurrent audit operation",
				OperationType: "inbox",
				Status:        200,
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent record: %v", err)
		}
	}

	entries, err := store.List(writers)
	if err != nil {
		t.Fatalf("list concurrent entries: %v", err)
	}
	if len(entries) != writers {
		t.Errorf("concurrent entry count = %d, want %d", len(entries), writers)
	}
}

func TestListKeepsLegacyAuditEntriesReadable(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	store, err := newStore(t.TempDir(), func() time.Time { return now }, false)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	legacy := Entry{
		Timestamp:  now,
		Level:      LevelInfo,
		Operation:  "Legacy audit operation",
		Status:     200,
		DurationMS: 10,
	}
	if err := writeEntries(store.filePath(now), []Entry{legacy}); err != nil {
		t.Fatalf("write legacy entry: %v", err)
	}

	entries, err := store.List(1)
	if err != nil {
		t.Fatalf("list legacy entry: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("legacy entry count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.SchemaVersion != 1 || entry.RequestID != "" || entry.OperationType != OperationTypeLegacy {
		t.Errorf("legacy entry contract = %+v", entry)
	}
	if entry.Request.Source != RequestSourceLegacy || !entry.Response.Success {
		t.Errorf("legacy entry snapshots = request:%+v response:%+v", entry.Request, entry.Response)
	}
}
