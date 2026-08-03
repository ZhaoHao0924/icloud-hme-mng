// Package auditlog stores a bounded, privacy-safe operation history.
package auditlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// RetentionDays is the fixed on-disk retention window for operation logs.
	RetentionDays = 7

	cleanupInterval = time.Hour
	fileDateLayout  = "2006-01-02"
	fileExtension   = ".jsonl"
)

var retention = time.Duration(RetentionDays) * 24 * time.Hour

// Level is the severity assigned to a completed operation.
type Level string

const (
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

// Entry is a privacy-safe operation record. It intentionally has no request body,
// query string, account identifier, email address, credential, or upstream response.
type Entry struct {
	Timestamp  time.Time `json:"timestamp"`
	Level      Level     `json:"level"`
	Operation  string    `json:"operation"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
}

// RecordInput is the narrow input accepted by Store.Record.
type RecordInput struct {
	Level     Level
	Operation string
	Status    int
	Duration  time.Duration
}

// Store persists operation logs as one JSON Lines file per UTC day.
type Store struct {
	dir string

	mu  sync.Mutex
	now func() time.Time

	cleanupStop chan struct{}
	cleanupDone chan struct{}
	closeOnce   sync.Once
}

type logFile struct {
	date time.Time
	path string
}

// New creates a persistent log store below dataDir and starts periodic cleanup.
func New(dataDir string) (*Store, error) {
	return newStore(dataDir, time.Now, true)
}

func newStore(dataDir string, now func() time.Time, startCleanup bool) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("operation log data directory is required")
	}
	if now == nil {
		now = time.Now
	}

	store := &Store{
		dir: filepath.Join(dataDir, "operation-logs"),
		now: now,
	}
	if err := os.MkdirAll(store.dir, 0700); err != nil {
		return nil, fmt.Errorf("create operation log directory: %w", err)
	}
	if err := store.Cleanup(); err != nil {
		return nil, err
	}
	if startCleanup {
		store.cleanupStop = make(chan struct{})
		store.cleanupDone = make(chan struct{})
		go store.runCleanup()
	}
	return store, nil
}

// Close stops periodic cleanup. Stored log records remain on disk.
func (s *Store) Close() {
	if s == nil || s.cleanupStop == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.cleanupStop)
		<-s.cleanupDone
	})
}

// Record appends one operation entry after removing expired records.
func (s *Store) Record(input RecordInput) (Entry, error) {
	if s == nil {
		return Entry{}, errors.New("operation log store is unavailable")
	}
	operation := strings.TrimSpace(input.Operation)
	if operation == "" {
		return Entry{}, errors.New("operation is required")
	}

	now := s.now().UTC()
	durationMS := input.Duration.Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	entry := Entry{
		Timestamp:  now,
		Level:      normalizedLevel(input.Level),
		Operation:  operation,
		Status:     input.Status,
		DurationMS: durationMS,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cleanupLocked(now); err != nil {
		return Entry{}, err
	}
	if err := appendEntry(s.filePath(now), entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// List returns the newest records first, capped by limit.
func (s *Store) List(limit int) ([]Entry, error) {
	if s == nil {
		return nil, errors.New("operation log store is unavailable")
	}
	if limit <= 0 {
		return []Entry{}, nil
	}

	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cleanupLocked(now); err != nil {
		return nil, err
	}

	files, err := s.logFilesLocked()
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-retention)
	entries := make([]Entry, 0)
	for _, file := range files {
		loaded, err := readEntries(file.path)
		if err != nil {
			return nil, err
		}
		for _, entry := range loaded {
			if !entry.Timestamp.Before(cutoff) {
				entries = append(entries, entry)
			}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// Cleanup removes records older than the fixed retention window.
func (s *Store) Cleanup() error {
	if s == nil {
		return errors.New("operation log store is unavailable")
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupLocked(now)
}

func (s *Store) runCleanup() {
	defer close(s.cleanupDone)
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = s.Cleanup()
		case <-s.cleanupStop:
			return
		}
	}
}

func (s *Store) cleanupLocked(now time.Time) error {
	files, err := s.logFilesLocked()
	if err != nil {
		return err
	}
	cutoff := now.UTC().Add(-retention)
	cutoffDay := startOfDay(cutoff)
	for _, file := range files {
		if file.date.After(cutoffDay) {
			continue
		}
		entries, err := readEntries(file.path)
		if err != nil {
			return err
		}
		kept := entries[:0]
		for _, entry := range entries {
			if !entry.Timestamp.Before(cutoff) {
				kept = append(kept, entry)
			}
		}
		if len(kept) == len(entries) {
			continue
		}
		if err := writeEntries(file.path, kept); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) filePath(timestamp time.Time) string {
	return filepath.Join(s.dir, timestamp.UTC().Format(fileDateLayout)+fileExtension)
}

func (s *Store) logFilesLocked() ([]logFile, error) {
	dirEntries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read operation log directory: %w", err)
	}
	files := make([]logFile, 0, len(dirEntries))
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, fileExtension) {
			continue
		}
		date, err := time.Parse(fileDateLayout, strings.TrimSuffix(name, fileExtension))
		if err != nil {
			continue
		}
		files = append(files, logFile{
			date: date.UTC(),
			path: filepath.Join(s.dir, name),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].date.Before(files[j].date)
	})
	return files, nil
}

func normalizedLevel(level Level) Level {
	switch level {
	case LevelWarning, LevelError:
		return level
	default:
		return LevelInfo
	}
}

func startOfDay(timestamp time.Time) time.Time {
	timestamp = timestamp.UTC()
	return time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, time.UTC)
}

func appendEntry(path string, entry Entry) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open operation log: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("set operation log permissions: %w", err)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode operation log: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := file.Write(raw); err != nil {
		return fmt.Errorf("write operation log: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync operation log: %w", err)
	}
	return nil
}

func readEntries(path string) ([]Entry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open operation log: %w", err)
	}
	defer file.Close()

	entries := make([]Entry, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 256*1024)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Timestamp.IsZero() || strings.TrimSpace(entry.Operation) == "" {
			continue
		}
		entry.Timestamp = entry.Timestamp.UTC()
		entry.Level = normalizedLevel(entry.Level)
		if entry.DurationMS < 0 {
			entry.DurationMS = 0
		}
		entries = append(entries, entry)
	}
	// A damaged line must not make the retained history unavailable.
	return entries, nil
}

func writeEntries(path string, entries []Entry) (err error) {
	if len(entries) == 0 {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove expired operation log: %w", removeErr)
		}
		return nil
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary operation log: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		if err != nil {
			_ = temp.Close()
			_ = os.Remove(tempPath)
		}
	}()
	if err = temp.Chmod(0600); err != nil {
		return fmt.Errorf("set temporary operation log permissions: %w", err)
	}
	encoder := json.NewEncoder(temp)
	for _, entry := range entries {
		if err = encoder.Encode(entry); err != nil {
			return fmt.Errorf("encode retained operation log: %w", err)
		}
	}
	if err = temp.Sync(); err != nil {
		return fmt.Errorf("sync retained operation log: %w", err)
	}
	if err = temp.Close(); err != nil {
		return fmt.Errorf("close retained operation log: %w", err)
	}
	if err = os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace operation log: %w", err)
	}
	return nil
}
