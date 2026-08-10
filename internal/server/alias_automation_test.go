package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
)

type fakeAliasOperationClient struct {
	mu sync.Mutex

	aliases      []hme.Alias
	createCalls  int
	createErrAt  int
	createLabels []string
	sessionCalls int
	entered      chan struct{}
	release      <-chan struct{}
}

func (c *fakeAliasOperationClient) CreateAlias(label string, _ int) (*hme.CreateResult, error) {
	c.mu.Lock()
	c.createCalls++
	call := c.createCalls
	c.createLabels = append(c.createLabels, label)
	entered := c.entered
	release := c.release
	c.mu.Unlock()

	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		<-release
	}
	if c.createErrAt == call {
		return nil, errors.New("upstream create failed")
	}
	return &hme.CreateResult{
		CreatedAt: fmt.Sprintf("2026-08-02T09:00:%02dZ", call),
		Email:     fmt.Sprintf("created-%d@icloud.com", call),
		Label:     label,
	}, nil
}

func (c *fakeAliasOperationClient) DeactivateHME(string) (bool, error) { return true, nil }

func (c *fakeAliasOperationClient) Delete(string) error { return nil }

func (c *fakeAliasOperationClient) ListAliases() ([]hme.Alias, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]hme.Alias(nil), c.aliases...), nil
}

func (c *fakeAliasOperationClient) ReactivateHME(string) (bool, error) { return true, nil }

func (c *fakeAliasOperationClient) SessionCookies() map[string]string {
	c.mu.Lock()
	c.sessionCalls++
	c.mu.Unlock()
	return map[string]string{"session": "updated"}
}

func (c *fakeAliasOperationClient) labels() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.createLabels...)
}

func (c *fakeAliasOperationClient) createCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.createCalls
}

func (c *fakeAliasOperationClient) sessionCookieCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionCalls
}

func newAutomationTestManager(t *testing.T) *account.Manager {
	t.Helper()
	dataDir := t.TempDir()
	config := `{
  "accounts": [
    {
      "id": "acc_automation",
      "name": "自动化测试账号",
      "host": "icloud.com",
      "cookies": {"session": "initial"},
      "status": "active"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dataDir, "accounts.json"), []byte(config), 0600); err != nil {
		t.Fatalf("write accounts config: %v", err)
	}
	mgr, err := account.NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return mgr
}

func newFakeAliasOperations(mgr *account.Manager, client aliasOperationClient) *aliasOperationService {
	operations := newAliasOperationService(mgr)
	operations.newClient = func(string) (aliasOperationClient, error) {
		return client, nil
	}
	return operations
}

func TestAliasAutomationCreateCount(t *testing.T) {
	tests := []struct {
		name   string
		rule   account.AliasAutomation
		active int
		want   int
	}{
		{
			name:   "scheduled batch when inventory is healthy",
			rule:   account.AliasAutomation{ScheduledBatchSize: 2, MaxBatchSize: 5},
			active: 12,
			want:   2,
		},
		{
			name:   "threshold replenishment targets active inventory",
			rule:   account.AliasAutomation{MinimumActive: 5, TargetActive: 8, MaxBatchSize: 8},
			active: 3,
			want:   5,
		},
		{
			name:   "per run cap wins over target deficit",
			rule:   account.AliasAutomation{MinimumActive: 8, TargetActive: 20, MaxBatchSize: 4},
			active: 1,
			want:   4,
		},
		{
			name:   "higher scheduled quantity wins when both rules apply",
			rule:   account.AliasAutomation{ScheduledBatchSize: 4, MinimumActive: 5, TargetActive: 6, MaxBatchSize: 5},
			active: 3,
			want:   4,
		},
		{
			name:   "cumulative target creates up to the per-run cap",
			rule:   account.AliasAutomation{TargetCreated: 750, MaxBatchSize: 5},
			active: 0,
			want:   5,
		},
		{
			name:   "cumulative target shrinks the final run to the remaining count",
			rule:   account.AliasAutomation{TargetCreated: 750, CreatedTotal: 748, MaxBatchSize: 5},
			active: 0,
			want:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aliasAutomationCreateCount(tt.rule, tt.active); got != tt.want {
				t.Errorf("aliasAutomationCreateCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAliasAutomationRunReplenishesInventoryAndPersistsStatus(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 30
	rule.MinimumActive = 5
	rule.TargetActive = 8
	rule.MaxBatchSize = 4
	rule.LabelPrefix = "补货"
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}

	client := &fakeAliasOperationClient{aliases: []hme.Alias{
		{Active: true}, {Active: true}, {Active: true}, {Active: false},
	}}
	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, client))
	service.now = func() time.Time { return now.Add(5 * time.Minute) }

	result, err := service.RunNow("acc_automation")
	if err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}
	if result.Status != account.AliasAutomationStatusSuccess || !result.Complete {
		t.Errorf("unexpected run status: %+v", result)
	}
	if result.ActiveBefore != 3 || result.Requested != 4 || result.Created != 4 || result.Failed != 0 {
		t.Errorf("unexpected run counts: %+v", result)
	}
	if got, want := client.labels(), []string{"补货 1", "补货 2", "补货 3", "补货 4"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("created labels = %v, want %v", got, want)
	}
	if result.Automation.LastActive != 7 || result.Automation.LastCreated != 4 {
		t.Errorf("persisted run summary = %+v", result.Automation)
	}
	if result.Automation.NextRunAt != now.Add(35*time.Minute).Format(time.RFC3339) {
		t.Errorf("NextRunAt = %q, want %q", result.Automation.NextRunAt, now.Add(35*time.Minute).Format(time.RFC3339))
	}
}

func TestAliasAutomationRunStopsAtCumulativeTarget(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 60
	rule.MaxBatchSize = 5
	rule.TargetCreated = 5
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	if _, err := mgr.RecordAliasAutomationRun("acc_automation", account.AliasAutomationRun{
		ActiveAliases: 3,
		Created:       3,
		Status:        account.AliasAutomationStatusSuccess,
	}, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("RecordAliasAutomationRun() partial error = %v", err)
	}

	client := &fakeAliasOperationClient{}
	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, client))
	service.now = func() time.Time { return now.Add(10 * time.Minute) }

	result, err := service.RunNow("acc_automation")
	if err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}
	if result.Requested != 2 || result.Created != 2 || result.Status != account.AliasAutomationStatusSuccess {
		t.Errorf("run result = %+v, want final two aliases", result)
	}
	if result.Automation.CreatedTotal != 5 || result.Automation.Enabled || result.Automation.NextRunAt != "" {
		t.Errorf("completed automation = %+v, want disabled target", result.Automation)
	}
	if got := client.createCallCount(); got != 2 {
		t.Errorf("create calls = %d, want 2", got)
	}
}

func TestAliasAutomationRunStopsAtTotalAliasSafetyLimit(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 60
	rule.ScheduledBatchSize = 5
	rule.MaxBatchSize = 5
	rule.MaxTotalAliases = 3
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}

	client := &fakeAliasOperationClient{aliases: []hme.Alias{{Active: true}, {Active: true}}}
	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, client))
	service.now = func() time.Time { return now.Add(5 * time.Minute) }

	result, err := service.RunNow("acc_automation")
	if err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}
	if result.Requested != 1 || result.Created != 1 || result.Status != account.AliasAutomationStatusSuccess {
		t.Errorf("run result = %+v, want one safe final alias", result)
	}
	if result.Automation.Enabled || result.Automation.PauseReason != account.AliasAutomationPauseReasonAliasLimit || result.Automation.NextRunAt != "" {
		t.Errorf("safety-limited automation = %+v, want alias-limit pause", result.Automation)
	}
	if got := client.createCallCount(); got != 1 {
		t.Errorf("create calls = %d, want 1", got)
	}
	history, err := mgr.ListAliasCreationHistory("acc_automation", 10)
	if err != nil {
		t.Fatalf("ListAliasCreationHistory() error = %v", err)
	}
	if len(history) != 1 || history[0].Trigger != account.AliasCreationTriggerAutomationManual || history[0].Created != 1 || len(history[0].Aliases) != 1 || result.BatchID != history[0].BatchID {
		t.Errorf("creation history = %+v, result batch ID = %q", history, result.BatchID)
	}
}

func TestAliasAutomationRunDefersAfterDailyCreationLimit(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.ScheduledBatchSize = 5
	rule.MaxBatchSize = 5
	rule.DailyCreationLimit = 3
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}

	client := &fakeAliasOperationClient{}
	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, client))
	runAt := now.Add(5 * time.Minute)
	service.now = func() time.Time { return runAt }
	first, err := service.RunNow("acc_automation")
	if err != nil {
		t.Fatalf("first RunNow() error = %v", err)
	}
	wantNextRun := account.NextAliasAutomationDayStart(runAt).Format(time.RFC3339)
	if first.Created != 3 || first.Requested != 3 || first.Status != account.AliasAutomationStatusSuccess || first.Automation.DailyCreated != 3 || first.Automation.NextRunAt != wantNextRun {
		t.Errorf("first run = %+v, want daily-limited creation", first)
	}

	service.now = func() time.Time { return now.Add(10 * time.Minute) }
	second, err := service.RunNow("acc_automation")
	if err != nil {
		t.Fatalf("second RunNow() error = %v", err)
	}
	if second.Created != 0 || second.Status != account.AliasAutomationStatusSkipped || second.Automation.NextRunAt != wantNextRun || !strings.Contains(second.Automation.LastError, "今日自动创建上限") {
		t.Errorf("second run = %+v, want deferred daily-limit skip", second)
	}
	if got := client.createCallCount(); got != 3 {
		t.Errorf("create calls = %d, want 3", got)
	}
}

func TestAliasAutomationManualRunRequiresResumeAfterAutoPause(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.ScheduledBatchSize = 1
	rule.MaxFailureCount = 1
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	paused, err := mgr.RecordAliasAutomationRun("acc_automation", account.AliasAutomationRun{
		ActiveAliases: 0,
		Error:         "upstream create failed",
		Status:        account.AliasAutomationStatusError,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecordAliasAutomationRun() error = %v", err)
	}
	if paused.Enabled || paused.PauseReason != account.AliasAutomationPauseReasonFailureLimit {
		t.Fatalf("paused automation = %+v, want failure-limit pause", paused)
	}

	client := &fakeAliasOperationClient{}
	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, client))
	result, err := service.RunNow("acc_automation")
	if !errors.Is(err, errAliasAutomationPaused) {
		t.Fatalf("RunNow() error = %v, want auto-pause error", err)
	}
	if result.Automation.PauseReason != account.AliasAutomationPauseReasonFailureLimit {
		t.Errorf("run result automation = %+v, want preserved pause", result.Automation)
	}
	if got := client.createCallCount(); got != 0 {
		t.Errorf("create calls = %d, want 0 while paused", got)
	}
}

func TestAliasAutomationPauseAndResumeAPI(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.ScheduledBatchSize = 1
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	srv := New(mgr, false, "")
	t.Cleanup(srv.Close)

	pauseRequest := httptest.NewRequest(http.MethodPost, "/api/accounts/acc_automation/alias-automation/pause", nil)
	pauseResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(pauseResponse, pauseRequest)
	if pauseResponse.Code != http.StatusOK {
		t.Fatalf("pause status = %d, body = %s", pauseResponse.Code, pauseResponse.Body.String())
	}
	var paused struct {
		Data    account.AliasAutomation `json:"data"`
		Success bool                    `json:"success"`
	}
	if err := json.Unmarshal(pauseResponse.Body.Bytes(), &paused); err != nil {
		t.Fatalf("decode pause response: %v", err)
	}
	if !paused.Success || paused.Data.Enabled || paused.Data.PauseReason != account.AliasAutomationPauseReasonManual || paused.Data.NextRunAt != "" {
		t.Errorf("pause response = %+v", paused)
	}

	resumeRequest := httptest.NewRequest(http.MethodPost, "/api/accounts/acc_automation/alias-automation/resume", nil)
	resumeResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resumeResponse, resumeRequest)
	if resumeResponse.Code != http.StatusOK {
		t.Fatalf("resume status = %d, body = %s", resumeResponse.Code, resumeResponse.Body.String())
	}
	var resumed struct {
		Data    account.AliasAutomation `json:"data"`
		Success bool                    `json:"success"`
	}
	if err := json.Unmarshal(resumeResponse.Body.Bytes(), &resumed); err != nil {
		t.Fatalf("decode resume response: %v", err)
	}
	if !resumed.Success || !resumed.Data.Enabled || resumed.Data.PauseReason != "" || resumed.Data.NextRunAt == "" {
		t.Errorf("resume response = %+v", resumed)
	}
}

func TestAliasBatchStopsOnFirstFailure(t *testing.T) {
	mgr := newAutomationTestManager(t)
	client := &fakeAliasOperationClient{createErrAt: 3}
	operations := newFakeAliasOperations(mgr, client)

	result, err := operations.createBatch("acc_automation", 5, "批量")
	if err == nil {
		t.Fatal("createBatch() error = nil, want upstream failure")
	}
	if result.Complete || result.Created != 2 || result.Failed != 1 || result.Requested != 5 {
		t.Errorf("unexpected batch result: %+v", result)
	}
	if got, want := client.labels(), []string{"批量 1", "批量 2", "批量 3"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("create attempts = %v, want %v", got, want)
	}
}

func TestAliasOperationsSerializeSameAccount(t *testing.T) {
	mgr := newAutomationTestManager(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	client := &fakeAliasOperationClient{entered: entered, release: release}
	operations := newFakeAliasOperations(mgr, client)
	factoryStarts := make(chan struct{}, 2)
	operations.newClient = func(string) (aliasOperationClient, error) {
		factoryStarts <- struct{}{}
		return client, nil
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := operations.createBatch("acc_automation", 1, "第一批")
		firstDone <- err
	}()
	<-factoryStarts
	<-entered

	secondDone := make(chan error, 1)
	go func() {
		_, err := operations.createBatch("acc_automation", 1, "第二批")
		secondDone <- err
	}()
	select {
	case <-factoryStarts:
		t.Fatal("second operation reached the client factory before the first operation completed")
	case <-time.After(40 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first batch error = %v", err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second operation did not reach the reused client after the first operation completed")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second batch error = %v", err)
	}
}

func TestAliasAutomationRunDueExecutesOnlyWhenDue(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 30
	rule.ScheduledBatchSize = 1
	rule.MaxBatchSize = 1
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}

	client := &fakeAliasOperationClient{}
	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, client))
	service.now = func() time.Time { return now.Add(30 * time.Minute) }
	audits := make(chan aliasAutomationScheduledRun, 1)
	service.onScheduledRun = func(run aliasAutomationScheduledRun) {
		audits <- run
	}
	service.RunDue()

	deadline := time.Now().Add(time.Second)
	for {
		automation, err := mgr.GetAliasAutomation("acc_automation")
		if err != nil {
			t.Fatalf("GetAliasAutomation() error = %v", err)
		}
		if automation.LastRunAt != "" {
			if automation.LastStatus != account.AliasAutomationStatusSuccess || automation.LastCreated != 1 {
				t.Errorf("unexpected persisted run: %+v", automation)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RunDue() did not execute the due rule")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case run := <-audits:
		if run.Status != account.AliasAutomationStatusSuccess || run.Created != 1 || run.Failed != 0 || run.PauseReason != "" {
			t.Errorf("scheduled audit = %+v", run)
		}
	case <-time.After(time.Second):
		t.Fatal("RunDue() did not emit a persisted scheduled-run audit")
	}

	service.now = func() time.Time { return now.Add(31 * time.Minute) }
	service.RunDue()
	time.Sleep(25 * time.Millisecond)
	if got := client.createCallCount(); got != 1 {
		t.Errorf("create calls after a not-due scan = %d, want 1", got)
	}
}

func TestAliasAutomationRunDueDefersOutsideSchedule(t *testing.T) {
	mgr := newAutomationTestManager(t)
	configuredAt := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 60
	rule.ScheduledBatchSize = 1
	rule.MaxBatchSize = 1
	rule.AllowedWeekdays = []int{1, 2, 3, 4, 5}
	rule.ExecutionWindowStart = "09:00"
	rule.ExecutionWindowEnd = "17:00"
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, configuredAt); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}

	client := &fakeAliasOperationClient{}
	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, client))
	dueAfterWindow := time.Date(2026, time.August, 3, 17, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return dueAfterWindow }
	service.RunDue()

	automation, err := mgr.GetAliasAutomation("acc_automation")
	if err != nil {
		t.Fatalf("GetAliasAutomation() error = %v", err)
	}
	wantNextRun := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if automation.NextRunAt != wantNextRun || automation.LastRunAt != "" {
		t.Errorf("deferred automation = %+v, want next run %q without a recorded run", automation, wantNextRun)
	}
	if got := client.createCallCount(); got != 0 {
		t.Errorf("create calls = %d, want 0", got)
	}

	service.now = func() time.Time { return dueAfterWindow.Add(time.Minute) }
	service.RunDue()
	if got := client.createCallCount(); got != 0 {
		t.Errorf("create calls after deferred scan = %d, want 0", got)
	}
}

func TestAliasAutomationPreviewAPIDoesNotCreateOrPersist(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC)
	client := &fakeAliasOperationClient{aliases: []hme.Alias{
		{Active: true}, {Active: true}, {Active: false},
	}}
	srv := New(mgr, false, "")
	t.Cleanup(srv.Close)
	srv.aliasOperations = newFakeAliasOperations(mgr, client)
	srv.aliasAutomation = newAliasAutomationService(mgr, srv.aliasOperations)
	srv.aliasAutomation.now = func() time.Time { return now }

	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.ScheduledBatchSize = 5
	rule.MaxBatchSize = 5
	rule.MaxTotalAliases = 4
	rule.DailyCreationLimit = 3
	rule.TargetCreated = 10
	rule.AllowedWeekdays = []int{1, 2, 3, 4, 5}
	rule.ExecutionWindowStart = "09:00"
	rule.ExecutionWindowEnd = "17:00"
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	before, err := mgr.GetAliasAutomation("acc_automation")
	if err != nil {
		t.Fatalf("GetAliasAutomation() before preview error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/accounts/acc_automation/alias-automation/preview", nil)
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Data    aliasAutomationPreviewResult `json:"data"`
		Success bool                         `json:"success"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if !body.Success || body.Data.TotalAliases != 3 || body.Data.ActiveAliases != 2 || body.Data.Requested != 1 || body.Data.DailyRemaining != 3 || body.Data.RemainingTotalCapacity != 1 || body.Data.TargetRemaining != 10 || body.Data.ScheduleAllowed || body.Data.ScheduleReason == "" {
		t.Errorf("preview response = %+v", body)
	}
	wantEligibleAt := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if body.Data.NextEligibleAt != wantEligibleAt {
		t.Errorf("NextEligibleAt = %q, want %q", body.Data.NextEligibleAt, wantEligibleAt)
	}
	if got := client.createCallCount(); got != 0 {
		t.Errorf("create calls = %d, want 0", got)
	}
	if got := client.sessionCookieCallCount(); got != 0 {
		t.Errorf("SessionCookies() calls = %d, want 0", got)
	}
	after, err := mgr.GetAliasAutomation("acc_automation")
	if err != nil {
		t.Fatalf("GetAliasAutomation() after preview error = %v", err)
	}
	if after.LastRunAt != before.LastRunAt || after.CreatedTotal != before.CreatedTotal || after.NextRunAt != before.NextRunAt {
		t.Errorf("preview changed automation state: before=%+v after=%+v", before, after)
	}
	history, err := mgr.ListAliasCreationHistory("acc_automation", 10)
	if err != nil {
		t.Fatalf("ListAliasCreationHistory() error = %v", err)
	}
	if len(history) != 0 {
		t.Errorf("preview recorded history = %+v", history)
	}
}

func TestAliasAutomationStartStopsAfterLastRuleIsDisabled(t *testing.T) {
	mgr := newAutomationTestManager(t)
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.ScheduledBatchSize = 1
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}

	service := newAliasAutomationService(mgr, newFakeAliasOperations(mgr, &fakeAliasOperationClient{}))
	service.pollEvery = time.Hour
	service.Start()
	t.Cleanup(service.Stop)

	service.mu.Lock()
	started := service.started
	service.mu.Unlock()
	if !started {
		t.Fatal("Start() did not start the scheduler for an enabled rule")
	}

	rule.Enabled = false
	if _, err := mgr.SetAliasAutomation("acc_automation", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() disable error = %v", err)
	}
	service.Start()

	service.mu.Lock()
	started = service.started
	service.mu.Unlock()
	if started {
		t.Fatal("Start() did not stop the scheduler after all rules were disabled")
	}
}

func TestAliasCreationHistoryAPIAndCSVExport(t *testing.T) {
	mgr := newAutomationTestManager(t)
	srv := New(mgr, false, "")
	t.Cleanup(srv.Close)
	client := &fakeAliasOperationClient{}
	srv.aliasOperations = newFakeAliasOperations(mgr, client)
	srv.aliasAutomation = newAliasAutomationService(mgr, srv.aliasOperations)

	emptyHistoryRequest := httptest.NewRequest(http.MethodGet, "/api/accounts/acc_automation/alias-creation-history?limit=10", nil)
	emptyHistoryResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(emptyHistoryResponse, emptyHistoryRequest)
	if emptyHistoryResponse.Code != http.StatusOK {
		t.Fatalf("empty history status = %d, body = %s", emptyHistoryResponse.Code, emptyHistoryResponse.Body.String())
	}
	var emptyHistoryBody struct {
		Data    aliasCreationHistoryData `json:"data"`
		Success bool                     `json:"success"`
	}
	if err := json.Unmarshal(emptyHistoryResponse.Body.Bytes(), &emptyHistoryBody); err != nil {
		t.Fatalf("decode empty history response: %v", err)
	}
	if !emptyHistoryBody.Success || emptyHistoryBody.Data.Entries == nil {
		t.Fatalf("empty history response = %+v, want a non-nil entries array", emptyHistoryBody)
	}

	batchRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/accounts/acc_automation/aliases/batch",
		bytes.NewBufferString(`{"count":2,"label_prefix":"trace"}`),
	)
	batchRequest.Header.Set("Content-Type", "application/json")
	batchResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(batchResponse, batchRequest)
	if batchResponse.Code != http.StatusOK {
		t.Fatalf("batch status = %d, body = %s", batchResponse.Code, batchResponse.Body.String())
	}
	var batchBody struct {
		Data    aliasBatchResult `json:"data"`
		Success bool             `json:"success"`
	}
	if err := json.Unmarshal(batchResponse.Body.Bytes(), &batchBody); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if !batchBody.Success || batchBody.Data.BatchID == "" || len(batchBody.Data.Aliases) != 2 || batchBody.Data.Aliases[0].BatchID != batchBody.Data.BatchID {
		t.Fatalf("batch response = %+v", batchBody)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/api/accounts/acc_automation/alias-creation-history?limit=10", nil)
	historyResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(historyResponse, historyRequest)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history status = %d, body = %s", historyResponse.Code, historyResponse.Body.String())
	}
	var historyBody struct {
		Data    aliasCreationHistoryData `json:"data"`
		Success bool                     `json:"success"`
	}
	if err := json.Unmarshal(historyResponse.Body.Bytes(), &historyBody); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if !historyBody.Success || historyBody.Data.Count != 1 || len(historyBody.Data.Entries) != 1 || historyBody.Data.Entries[0].BatchID != batchBody.Data.BatchID || historyBody.Data.Entries[0].Trigger != account.AliasCreationTriggerBatch {
		t.Errorf("history response = %+v", historyBody)
	}

	csvRequest := httptest.NewRequest(http.MethodGet, "/api/accounts/acc_automation/alias-creation-history.csv", nil)
	csvResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(csvResponse, csvRequest)
	if csvResponse.Code != http.StatusOK {
		t.Fatalf("CSV status = %d, body = %s", csvResponse.Code, csvResponse.Body.String())
	}
	if got := csvResponse.Header().Get("Content-Disposition"); got != `attachment; filename="alias-creation-history.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.Contains(csvResponse.Body.String(), batchBody.Data.BatchID) || !strings.Contains(csvResponse.Body.String(), "created-1@icloud.com") {
		t.Errorf("CSV body is missing batch trace: %s", csvResponse.Body.String())
	}
}

func TestAliasAutomationConfigurationAndBatchValidationAPI(t *testing.T) {
	mgr := newAutomationTestManager(t)
	srv := New(mgr, false, "")
	t.Cleanup(srv.Close)
	client := &fakeAliasOperationClient{aliases: []hme.Alias{{Active: true}}}
	srv.aliasOperations = newFakeAliasOperations(mgr, client)
	srv.aliasAutomation = newAliasAutomationService(mgr, srv.aliasOperations)

	rule := account.DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 30
	rule.AllowedWeekdays = []int{1, 3, 5}
	rule.ExecutionWindowStart = "09:00"
	rule.ExecutionWindowEnd = "17:00"
	rule.ScheduledBatchSize = 2
	rule.MaxBatchSize = 4
	rule.TargetCreated = 5
	rule.MaxTotalAliases = 900
	rule.MaxFailureCount = 4
	body, err := json.Marshal(aliasAutomationReq{
		Enabled:              rule.Enabled,
		IntervalMinutes:      rule.IntervalMinutes,
		AllowedWeekdays:      rule.AllowedWeekdays,
		ExecutionWindowStart: rule.ExecutionWindowStart,
		ExecutionWindowEnd:   rule.ExecutionWindowEnd,
		ScheduledBatchSize:   rule.ScheduledBatchSize,
		MaxBatchSize:         rule.MaxBatchSize,
		MaxTotalAliases:      rule.MaxTotalAliases,
		MaxFailureCount:      rule.MaxFailureCount,
		TargetCreated:        rule.TargetCreated,
	})
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}
	put := httptest.NewRequest(http.MethodPut, "/api/accounts/acc_automation/alias-automation", bytes.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	putRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(putRes, put)
	if putRes.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body = %s", putRes.Code, putRes.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/accounts/acc_automation/alias-automation", nil)
	getRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body = %s", getRes.Code, getRes.Body.String())
	}
	var response struct {
		Success bool                    `json:"success"`
		Data    account.AliasAutomation `json:"data"`
	}
	if err := json.Unmarshal(getRes.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if !response.Success || !response.Data.Enabled || response.Data.ScheduledBatchSize != 2 || response.Data.TargetCreated != 5 || response.Data.MaxTotalAliases != 900 || response.Data.MaxFailureCount != 4 || response.Data.NextRunAt == "" || fmt.Sprint(response.Data.AllowedWeekdays) != "[1 3 5]" || response.Data.ExecutionWindowStart != "09:00" || response.Data.ExecutionWindowEnd != "17:00" {
		t.Errorf("unexpected automation response: %+v", response)
	}

	batch := httptest.NewRequest(http.MethodPost, "/api/accounts/acc_automation/aliases/batch", bytes.NewBufferString(`{"count":2,"label_prefix":"batch"}`))
	batch.Header.Set("Content-Type", "application/json")
	batchRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(batchRes, batch)
	if batchRes.Code != http.StatusOK {
		t.Fatalf("batch status = %d, want 200; body = %s", batchRes.Code, batchRes.Body.String())
	}
	var batchResponse struct {
		Success bool             `json:"success"`
		Data    aliasBatchResult `json:"data"`
	}
	if err := json.Unmarshal(batchRes.Body.Bytes(), &batchResponse); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if !batchResponse.Success || !batchResponse.Data.Complete || batchResponse.Data.Created != 2 {
		t.Errorf("unexpected batch response: %+v", batchResponse)
	}

	run := httptest.NewRequest(http.MethodPost, "/api/accounts/acc_automation/alias-automation/run", nil)
	runRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(runRes, run)
	if runRes.Code != http.StatusOK {
		t.Fatalf("run status = %d, want 200; body = %s", runRes.Code, runRes.Body.String())
	}
	var runResponse struct {
		Success bool                     `json:"success"`
		Data    aliasAutomationRunResult `json:"data"`
	}
	if err := json.Unmarshal(runRes.Body.Bytes(), &runResponse); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	if !runResponse.Success || !runResponse.Data.Complete || runResponse.Data.Created != 2 || runResponse.Data.Trigger != "manual" {
		t.Errorf("unexpected run response: %+v", runResponse)
	}

	invalidBatch := httptest.NewRequest(http.MethodPost, "/api/accounts/acc_automation/aliases/batch", bytes.NewBufferString(`{"count":0}`))
	invalidBatch.Header.Set("Content-Type", "application/json")
	invalidBatchRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(invalidBatchRes, invalidBatch)
	if invalidBatchRes.Code != http.StatusBadRequest {
		t.Fatalf("invalid batch status = %d, want 400; body = %s", invalidBatchRes.Code, invalidBatchRes.Body.String())
	}
}
