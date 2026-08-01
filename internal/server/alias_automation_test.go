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
	<-factoryStarts
	<-entered
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

	service.now = func() time.Time { return now.Add(31 * time.Minute) }
	service.RunDue()
	time.Sleep(25 * time.Millisecond)
	if got := client.createCallCount(); got != 1 {
		t.Errorf("create calls after a not-due scan = %d, want 1", got)
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
	rule.ScheduledBatchSize = 2
	rule.MaxBatchSize = 4
	body, err := json.Marshal(aliasAutomationReq{
		Enabled:            rule.Enabled,
		IntervalMinutes:    rule.IntervalMinutes,
		ScheduledBatchSize: rule.ScheduledBatchSize,
		MaxBatchSize:       rule.MaxBatchSize,
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
	if !response.Success || !response.Data.Enabled || response.Data.ScheduledBatchSize != 2 || response.Data.NextRunAt == "" {
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
