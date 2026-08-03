package account

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// MaxAliasCreationHistory is the maximum retained creation batches per account.
	MaxAliasCreationHistory   = 500
	maxAliasHistoryAliases    = MaxAliasAutomationBatchSize
	maxAliasHistoryBatchID    = 80
	maxAliasHistoryLabelRunes = 200
)

const (
	AliasCreationTriggerManual              = "manual"
	AliasCreationTriggerBatch               = "batch"
	AliasCreationTriggerAutomationManual    = "automation_manual"
	AliasCreationTriggerAutomationScheduled = "automation_scheduled"
)

// AliasCreationHistoryAlias identifies one alias made as part of a batch.
type AliasCreationHistoryAlias struct {
	CreatedAt string `json:"created_at"`
	Email     string `json:"email"`
	Label     string `json:"label"`
}

// AliasCreationHistory is the retained, account-scoped trace of a create attempt.
// It contains user-visible alias data but never credentials or upstream raw responses.
type AliasCreationHistory struct {
	Aliases     []AliasCreationHistoryAlias `json:"aliases"`
	BatchID     string                      `json:"batch_id"`
	Complete    bool                        `json:"complete"`
	Created     int                         `json:"created"`
	CreatedAt   string                      `json:"created_at"`
	Error       string                      `json:"error,omitempty"`
	Failed      int                         `json:"failed"`
	LabelPrefix string                      `json:"label_prefix"`
	Requested   int                         `json:"requested"`
	Status      string                      `json:"status"`
	Trigger     string                      `json:"trigger"`
}

func cloneAliasCreationHistory(entries []AliasCreationHistory) []AliasCreationHistory {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]AliasCreationHistory, len(entries))
	for index, entry := range entries {
		cloned[index] = entry
		cloned[index].Aliases = append([]AliasCreationHistoryAlias(nil), entry.Aliases...)
	}
	return cloned
}

func normalizeStoredAliasCreationHistory(entries []AliasCreationHistory) ([]AliasCreationHistory, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entries) > MaxAliasCreationHistory {
		entries = entries[:MaxAliasCreationHistory]
	}
	normalized := make([]AliasCreationHistory, 0, len(entries))
	for index, entry := range entries {
		entry, err := normalizeAliasCreationHistory(entry, time.Time{})
		if err != nil {
			return nil, fmt.Errorf("history[%d]: %w", index, err)
		}
		normalized = append(normalized, entry)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339, normalized[i].CreatedAt)
		right, _ := time.Parse(time.RFC3339, normalized[j].CreatedAt)
		return left.After(right)
	})
	return normalized, nil
}

func normalizeAliasCreationHistory(entry AliasCreationHistory, now time.Time) (AliasCreationHistory, error) {
	entry.BatchID = strings.TrimSpace(entry.BatchID)
	if entry.BatchID == "" {
		entry.BatchID = "batch_" + uuid.NewString()
	}
	if utf8.RuneCountInString(entry.BatchID) > maxAliasHistoryBatchID {
		return AliasCreationHistory{}, fmt.Errorf("batch_id cannot exceed %d characters", maxAliasHistoryBatchID)
	}
	entry.Trigger = strings.TrimSpace(entry.Trigger)
	if !validAliasCreationTrigger(entry.Trigger) {
		return AliasCreationHistory{}, fmt.Errorf("invalid trigger %q", entry.Trigger)
	}
	if !validAliasAutomationStatus(entry.Status) {
		return AliasCreationHistory{}, fmt.Errorf("invalid status %q", entry.Status)
	}
	if entry.Requested < 0 || entry.Requested > MaxAliasAutomationBatchSize || entry.Created < 0 || entry.Created > MaxAliasAutomationBatchSize || entry.Failed < 0 || entry.Failed > MaxAliasAutomationBatchSize {
		return AliasCreationHistory{}, fmt.Errorf("creation counts are invalid")
	}
	if len(entry.Aliases) != entry.Created || len(entry.Aliases) > maxAliasHistoryAliases {
		return AliasCreationHistory{}, fmt.Errorf("created aliases do not match the recorded count")
	}
	entry.LabelPrefix = strings.TrimSpace(entry.LabelPrefix)
	if utf8.RuneCountInString(entry.LabelPrefix) > maxAliasHistoryLabelRunes {
		return AliasCreationHistory{}, fmt.Errorf("label_prefix cannot exceed %d characters", maxAliasHistoryLabelRunes)
	}
	entry.Error = truncate(strings.TrimSpace(entry.Error), 300)
	if strings.TrimSpace(entry.CreatedAt) == "" {
		if now.IsZero() {
			return AliasCreationHistory{}, fmt.Errorf("created_at is required")
		}
		entry.CreatedAt = now.UTC().Format(time.RFC3339)
	}
	createdAt, err := time.Parse(time.RFC3339, entry.CreatedAt)
	if err != nil {
		return AliasCreationHistory{}, fmt.Errorf("created_at is invalid")
	}
	entry.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	entry.Aliases = append([]AliasCreationHistoryAlias(nil), entry.Aliases...)
	for index := range entry.Aliases {
		alias := &entry.Aliases[index]
		alias.Email = strings.TrimSpace(alias.Email)
		alias.Label = strings.TrimSpace(alias.Label)
		if alias.Email == "" || utf8.RuneCountInString(alias.Email) > 320 || utf8.RuneCountInString(alias.Label) > maxAliasHistoryLabelRunes {
			return AliasCreationHistory{}, fmt.Errorf("alias[%d] is invalid", index)
		}
		if strings.TrimSpace(alias.CreatedAt) == "" {
			alias.CreatedAt = entry.CreatedAt
		}
		aliasCreatedAt, err := time.Parse(time.RFC3339, alias.CreatedAt)
		if err != nil {
			return AliasCreationHistory{}, fmt.Errorf("alias[%d] created_at is invalid", index)
		}
		alias.CreatedAt = aliasCreatedAt.UTC().Format(time.RFC3339)
	}
	return entry, nil
}

func validAliasCreationTrigger(trigger string) bool {
	switch trigger {
	case AliasCreationTriggerManual,
		AliasCreationTriggerBatch,
		AliasCreationTriggerAutomationManual,
		AliasCreationTriggerAutomationScheduled:
		return true
	default:
		return false
	}
}

// ListAliasCreationHistory returns newest-first retained creation batches.
func (m *Manager) ListAliasCreationHistory(id string, limit int) ([]AliasCreationHistory, error) {
	if limit <= 0 {
		return []AliasCreationHistory{}, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	acc, ok := m.accounts[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	entries := cloneAliasCreationHistory(acc.AliasCreationHistory)
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// RecordAliasCreation stores a completed create attempt and returns its stable batch ID.
func (m *Manager) RecordAliasCreation(id string, entry AliasCreationHistory, now time.Time) (AliasCreationHistory, error) {
	if now.IsZero() {
		now = time.Now()
	}
	normalized, err := normalizeAliasCreationHistory(entry, now)
	if err != nil {
		return AliasCreationHistory{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return AliasCreationHistory{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	previous := cloneAliasCreationHistory(acc.AliasCreationHistory)
	entries := make([]AliasCreationHistory, 0, min(len(previous)+1, MaxAliasCreationHistory))
	entries = append(entries, normalized)
	entries = append(entries, previous...)
	if len(entries) > MaxAliasCreationHistory {
		entries = entries[:MaxAliasCreationHistory]
	}
	acc.AliasCreationHistory = entries
	if err := m.save(); err != nil {
		acc.AliasCreationHistory = previous
		return AliasCreationHistory{}, err
	}
	return normalized, nil
}
