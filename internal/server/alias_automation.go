package server

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"icloud-hme/internal/account"
)

const aliasAutomationPollInterval = time.Minute

var errAliasAutomationRuleMissing = errors.New("请先配置定时创建数量或库存阈值")

type aliasAutomationTrigger string

const (
	aliasAutomationTriggerManual    aliasAutomationTrigger = "manual"
	aliasAutomationTriggerScheduled aliasAutomationTrigger = "scheduled"
)

type aliasAutomationRunResult struct {
	AccountID    string                  `json:"account_id"`
	ActiveBefore int                     `json:"active_before"`
	Aliases      []createdAliasData      `json:"aliases"`
	Complete     bool                    `json:"complete"`
	Created      int                     `json:"created"`
	Error        string                  `json:"error,omitempty"`
	Failed       int                     `json:"failed"`
	Requested    int                     `json:"requested"`
	Status       string                  `json:"status"`
	Trigger      string                  `json:"trigger"`
	Automation   account.AliasAutomation `json:"automation"`
}

type aliasAutomationService struct {
	mgr        *account.Manager
	operations *aliasOperationService
	now        func() time.Time
	pollEvery  time.Duration

	mu      sync.Mutex
	started bool
	stop    chan struct{}
	done    chan struct{}
}

func newAliasAutomationService(mgr *account.Manager, operations *aliasOperationService) *aliasAutomationService {
	return &aliasAutomationService{
		mgr:        mgr,
		operations: operations,
		now:        time.Now,
		pollEvery:  aliasAutomationPollInterval,
	}
}

// Start reconciles the in-process scheduler with the enabled rules.
// It is safe to call after every configuration update.
func (s *aliasAutomationService) Start() {
	if len(s.mgr.ListEnabledAliasAutomations()) == 0 {
		s.Stop()
		return
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	stop := s.stop
	done := s.done
	pollEvery := s.pollEvery
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(pollEvery)
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-ticker.C:
				s.RunDue()
			case <-stop:
				return
			}
		}
	}()
}

func (s *aliasAutomationService) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	stop := s.stop
	done := s.done
	s.started = false
	s.stop = nil
	s.done = nil
	s.mu.Unlock()

	close(stop)
	<-done
}

// RunDue schedules every due account independently. The account operation lock
// rechecks due state before any upstream call, avoiding duplicate execution.
func (s *aliasAutomationService) RunDue() {
	now := s.now()
	for _, configured := range s.mgr.ListEnabledAliasAutomations() {
		if !aliasAutomationDue(configured.Automation, now) {
			continue
		}
		accountID := configured.AccountID
		go func() {
			if _, err := s.run(accountID, aliasAutomationTriggerScheduled); err != nil {
				log.Printf("别名自动化执行失败 account=%s: %s", accountID, aliasOperationErrorSummary(err))
			}
		}()
	}
}

func (s *aliasAutomationService) RunNow(accountID string) (aliasAutomationRunResult, error) {
	return s.run(accountID, aliasAutomationTriggerManual)
}

func (s *aliasAutomationService) run(accountID string, trigger aliasAutomationTrigger) (aliasAutomationRunResult, error) {
	runAt := s.now()
	result := aliasAutomationRunResult{
		AccountID: accountID,
		Aliases:   make([]createdAliasData, 0),
		Complete:  true,
		Status:    account.AliasAutomationStatusSkipped,
		Trigger:   string(trigger),
	}
	var automation account.AliasAutomation
	shouldRecord := false

	operationErr := s.operations.withClient(accountID, func(client aliasOperationClient) error {
		var err error
		automation, err = s.mgr.GetAliasAutomation(accountID)
		if err != nil {
			return err
		}
		if automation.ScheduledBatchSize == 0 && automation.MinimumActive == 0 {
			return errAliasAutomationRuleMissing
		}
		if trigger == aliasAutomationTriggerScheduled && (!automation.Enabled || !aliasAutomationDue(automation, runAt)) {
			result.Automation = automation
			return nil
		}
		shouldRecord = true

		aliases, err := client.ListAliases()
		if err != nil {
			result.Complete = false
			result.Error = aliasOperationErrorSummary(err)
			result.Failed = 1
			result.Status = account.AliasAutomationStatusError
			return err
		}
		for _, alias := range aliases {
			if alias.Active {
				result.ActiveBefore++
			}
		}

		result.Requested = aliasAutomationCreateCount(automation, result.ActiveBefore)
		if result.Requested == 0 {
			result.Automation = automation
			return nil
		}

		result.Aliases = make([]createdAliasData, 0, result.Requested)
		for index := 0; index < result.Requested; index++ {
			created, err := client.CreateAlias(aliasBatchLabel(automation.LabelPrefix, index, result.Requested), 5)
			if err != nil {
				result.Complete = false
				result.Error = aliasOperationErrorSummary(err)
				result.Failed = 1
				if result.Created > 0 {
					result.Status = account.AliasAutomationStatusPartial
				} else {
					result.Status = account.AliasAutomationStatusError
				}
				return err
			}
			result.Created++
			result.Aliases = append(result.Aliases, createdAliasData{
				AccountID: accountID,
				CreatedAt: created.CreatedAt,
				Email:     created.Email,
				Label:     created.Label,
			})
		}
		result.Status = account.AliasAutomationStatusSuccess
		return nil
	})

	if !shouldRecord {
		return result, operationErr
	}
	if operationErr != nil && result.Status == account.AliasAutomationStatusSkipped {
		result.Complete = false
		result.Error = aliasOperationErrorSummary(operationErr)
		result.Failed = 1
		result.Status = account.AliasAutomationStatusError
	}
	updated, persistErr := s.mgr.RecordAliasAutomationRun(accountID, account.AliasAutomationRun{
		ActiveAliases: result.ActiveBefore + result.Created,
		Created:       result.Created,
		Error:         result.Error,
		Status:        result.Status,
	}, runAt)
	if persistErr != nil {
		return result, fmt.Errorf("保存自动化运行状态失败: %w", persistErr)
	}
	result.Automation = updated
	return result, operationErr
}

func aliasAutomationCreateCount(automation account.AliasAutomation, activeAliases int) int {
	needed := automation.ScheduledBatchSize
	if automation.MinimumActive > 0 && activeAliases < automation.MinimumActive {
		needed = max(needed, automation.TargetActive-activeAliases)
	}
	return min(needed, automation.MaxBatchSize)
}

func aliasAutomationDue(automation account.AliasAutomation, now time.Time) bool {
	if !automation.Enabled {
		return false
	}
	nextRunAt := strings.TrimSpace(automation.NextRunAt)
	if nextRunAt == "" {
		return true
	}
	nextRun, err := time.Parse(time.RFC3339, nextRunAt)
	return err != nil || !nextRun.After(now)
}
