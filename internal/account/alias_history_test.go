package account

import (
	"strings"
	"testing"
	"time"
)

func TestAliasCreationHistoryPersistsAndReturnsDeepCopies(t *testing.T) {
	mgr := newManagerWithCookies(t)
	now := time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)
	recorded, err := mgr.RecordAliasCreation("acc_cookie", AliasCreationHistory{
		Aliases: []AliasCreationHistoryAlias{
			{CreatedAt: now.Format(time.RFC3339), Email: "one@icloud.com", Label: "batch 1"},
			{CreatedAt: now.Format(time.RFC3339), Email: "two@icloud.com", Label: "batch 2"},
		},
		Complete:    false,
		Created:     2,
		Error:       "create failed",
		Failed:      1,
		LabelPrefix: "batch",
		Requested:   3,
		Status:      AliasAutomationStatusPartial,
		Trigger:     AliasCreationTriggerBatch,
	}, now)
	if err != nil {
		t.Fatalf("RecordAliasCreation() error = %v", err)
	}
	if !strings.HasPrefix(recorded.BatchID, "batch_") || recorded.CreatedAt != now.Format(time.RFC3339) {
		t.Errorf("recorded history = %+v", recorded)
	}

	history, err := mgr.ListAliasCreationHistory("acc_cookie", 10)
	if err != nil {
		t.Fatalf("ListAliasCreationHistory() error = %v", err)
	}
	if len(history) != 1 || history[0].BatchID != recorded.BatchID || len(history[0].Aliases) != 2 {
		t.Fatalf("history = %+v", history)
	}
	history[0].Aliases[0].Email = "mutated@example.test"
	historyAgain, err := mgr.ListAliasCreationHistory("acc_cookie", 10)
	if err != nil {
		t.Fatalf("second ListAliasCreationHistory() error = %v", err)
	}
	if historyAgain[0].Aliases[0].Email != "one@icloud.com" {
		t.Errorf("history clone leaked mutation: %+v", historyAgain[0])
	}

	reloaded, err := NewManager(mgr.DataDir())
	if err != nil {
		t.Fatalf("NewManager() after persistence error = %v", err)
	}
	persisted, err := reloaded.ListAliasCreationHistory("acc_cookie", 10)
	if err != nil {
		t.Fatalf("reloaded ListAliasCreationHistory() error = %v", err)
	}
	if len(persisted) != 1 || persisted[0].BatchID != recorded.BatchID || persisted[0].Error != "create failed" {
		t.Errorf("persisted history = %+v", persisted)
	}
}

func TestAliasCreationHistoryRejectsInvalidRecord(t *testing.T) {
	mgr := newManagerWithCookies(t)
	_, err := mgr.RecordAliasCreation("acc_cookie", AliasCreationHistory{
		Status:  AliasAutomationStatusSuccess,
		Trigger: "unknown",
	}, time.Now())
	if err == nil {
		t.Fatal("RecordAliasCreation() error = nil, want validation error")
	}
}
