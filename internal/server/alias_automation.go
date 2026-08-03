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

var (
	errAliasAutomationRuleMissing = errors.New("请先配置定时创建数量、库存阈值或累计创建目标")
	errAliasAutomationPaused      = errors.New("自动化规则已暂停，请重新启用并保存后再执行")
)

type aliasAutomationTrigger string

const (
	aliasAutomationTriggerManual    aliasAutomationTrigger = "manual"
	aliasAutomationTriggerScheduled aliasAutomationTrigger = "scheduled"
)

type aliasAutomationRunResult struct {
	AccountID    string                  `json:"account_id"`
	ActiveBefore int                     `json:"active_before"`
	Aliases      []createdAliasData      `json:"aliases"`
	BatchID      string                  `json:"batch_id,omitempty"`
	Complete     bool                    `json:"complete"`
	Created      int                     `json:"created"`
	Error        string                  `json:"error,omitempty"`
	Failed       int                     `json:"failed"`
	PauseReason  string                  `json:"pause_reason,omitempty"`
	Requested    int                     `json:"requested"`
	Status       string                  `json:"status"`
	Trigger      string                  `json:"trigger"`
	Automation   account.AliasAutomation `json:"automation"`
}

// aliasAutomationPreviewResult describes a potential manual execution without
// creating aliases, persisting progress, or recording creation history.
type aliasAutomationPreviewResult struct {
	AccountID              string                  `json:"account_id"`
	ActiveAliases          int                     `json:"active_aliases"`
	Automation             account.AliasAutomation `json:"automation"`
	DailyRemaining         int                     `json:"daily_remaining"`
	MaxTotalAliases        int                     `json:"max_total_aliases"`
	NextEligibleAt         string                  `json:"next_eligible_at,omitempty"`
	RemainingTotalCapacity int                     `json:"remaining_total_capacity"`
	Requested              int                     `json:"requested"`
	ScheduleAllowed        bool                    `json:"schedule_allowed"`
	ScheduleReason         string                  `json:"schedule_reason,omitempty"`
	TargetRemaining        int                     `json:"target_remaining"`
	TotalAliases           int                     `json:"total_aliases"`
}

// aliasAutomationScheduledRun is the privacy-safe summary emitted after a
// scheduled run has been persisted. It intentionally omits account and alias data.
type aliasAutomationScheduledRun struct {
	Complete    bool
	Created     int
	Duration    time.Duration
	Failed      int
	PauseReason string
	Status      string
}

// aliasAutomationRunNotification is a privacy-safe summary for external
// notifications. It intentionally omits created alias addresses.
type aliasAutomationRunNotification struct {
	AccountID   string
	Complete    bool
	Created     int
	Error       string
	Failed      int
	PauseReason string
	Requested   int
	Status      string
	Trigger     string
}

type aliasAutomationService struct {
	mgr        *account.Manager
	operations *aliasOperationService
	now        func() time.Time
	pollEvery  time.Duration

	onScheduledRun func(aliasAutomationScheduledRun)
	onRun          func(aliasAutomationRunNotification)

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
		if !account.IsAliasAutomationScheduleAllowed(configured.Automation, now) {
			if _, err := s.mgr.DeferAliasAutomationToNextAllowedTime(configured.AccountID, now); err != nil {
				log.Printf("别名自动化延后执行失败 account=%s: %s", configured.AccountID, err)
			}
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

func (s *aliasAutomationService) Preview(accountID string) (aliasAutomationPreviewResult, error) {
	runAt := s.now()
	result := aliasAutomationPreviewResult{AccountID: accountID}
	automation, err := s.mgr.GetAliasAutomation(accountID)
	if err != nil {
		return result, err
	}
	if !aliasAutomationConfigured(automation) {
		return result, errAliasAutomationRuleMissing
	}
	result.Automation = automation
	setAliasAutomationPreviewSchedule(&result, automation, runAt)

	err = s.operations.withReadOnlyClient(accountID, func(client aliasOperationClient) error {
		current, err := s.mgr.GetAliasAutomation(accountID)
		if err != nil {
			return err
		}
		if !aliasAutomationConfigured(current) {
			return errAliasAutomationRuleMissing
		}
		automation = current
		result.Automation = automation
		setAliasAutomationPreviewSchedule(&result, automation, runAt)

		aliases, err := client.ListAliases()
		if err != nil {
			return err
		}
		result.TotalAliases = len(aliases)
		for _, alias := range aliases {
			if alias.Active {
				result.ActiveAliases++
			}
		}
		result.MaxTotalAliases = automation.MaxTotalAliases
		result.RemainingTotalCapacity = max(0, automation.MaxTotalAliases-result.TotalAliases)
		if automation.TargetCreated > 0 {
			result.TargetRemaining = max(0, automation.TargetCreated-automation.CreatedTotal)
		}
		if automation.DailyCreationLimit > 0 {
			result.DailyRemaining = account.RemainingAliasAutomationDailyCapacity(automation, runAt)
		}

		result.Requested = aliasAutomationCreateCount(automation, result.ActiveAliases)
		result.Requested = min(result.Requested, result.RemainingTotalCapacity)
		if automation.DailyCreationLimit > 0 {
			result.Requested = min(result.Requested, result.DailyRemaining)
		}
		return nil
	})
	return result, err
}

func setAliasAutomationPreviewSchedule(result *aliasAutomationPreviewResult, automation account.AliasAutomation, now time.Time) {
	result.ScheduleAllowed = account.IsAliasAutomationScheduleAllowed(automation, now)
	result.ScheduleReason = ""
	result.NextEligibleAt = ""
	if result.ScheduleAllowed {
		return
	}
	if !aliasAutomationWeekdayAllowed(automation, now) {
		result.ScheduleReason = "当前不在允许的执行日"
	} else {
		result.ScheduleReason = "当前不在执行时间窗内"
	}
	result.NextEligibleAt = account.NextAliasAutomationAllowedAt(automation, now).Format(time.RFC3339)
}

func aliasAutomationWeekdayAllowed(automation account.AliasAutomation, now time.Time) bool {
	if len(automation.AllowedWeekdays) == 0 {
		return true
	}
	for _, weekday := range automation.AllowedWeekdays {
		if weekday == int(now.Weekday()) {
			return true
		}
	}
	return false
}

func (s *aliasAutomationService) run(accountID string, trigger aliasAutomationTrigger) (aliasAutomationRunResult, error) {
	startedAt := time.Now()
	runAt := s.now()
	result := aliasAutomationRunResult{
		AccountID: accountID,
		Aliases:   make([]createdAliasData, 0),
		Complete:  true,
		Status:    account.AliasAutomationStatusSkipped,
		Trigger:   string(trigger),
	}
	automation, err := s.mgr.GetAliasAutomation(accountID)
	if err != nil {
		return result, err
	}
	if !aliasAutomationConfigured(automation) {
		return result, errAliasAutomationRuleMissing
	}
	if trigger == aliasAutomationTriggerManual && !automation.Enabled && automation.PauseReason != "" {
		result.ActiveBefore = automation.LastActive
		result.Automation = automation
		return result, errAliasAutomationPaused
	}
	if trigger == aliasAutomationTriggerScheduled {
		if !automation.Enabled || !aliasAutomationDue(automation, runAt) {
			result.Automation = automation
			return result, nil
		}
		if !account.IsAliasAutomationScheduleAllowed(automation, runAt) {
			deferred, deferErr := s.mgr.DeferAliasAutomationToNextAllowedTime(accountID, runAt)
			if deferErr == nil {
				result.Automation = deferred
			}
			return result, deferErr
		}
	}

	shouldRecord := true
	var operationErr error
	var nextRunAt string
	if aliasAutomationTargetReached(automation) {
		result.ActiveBefore = automation.LastActive
	} else {
		operationErr = s.operations.withClient(accountID, func(client aliasOperationClient) error {
			var err error
			automation, err = s.mgr.GetAliasAutomation(accountID)
			if err != nil {
				return err
			}
			if !aliasAutomationConfigured(automation) {
				shouldRecord = false
				return errAliasAutomationRuleMissing
			}
			if trigger == aliasAutomationTriggerScheduled && (!automation.Enabled || !aliasAutomationDue(automation, runAt) || !account.IsAliasAutomationScheduleAllowed(automation, runAt)) {
				shouldRecord = false
				result.Automation = automation
				return nil
			}
			if aliasAutomationTargetReached(automation) {
				result.ActiveBefore = automation.LastActive
				return nil
			}

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

			pauseAtAliasLimit := false
			remainingAliases := automation.MaxTotalAliases - len(aliases)
			if remainingAliases <= 0 {
				result.Error = fmt.Sprintf("已达到总别名安全上限 %d", automation.MaxTotalAliases)
				result.PauseReason = account.AliasAutomationPauseReasonAliasLimit
				return nil
			}
			if result.Requested > remainingAliases {
				result.Requested = remainingAliases
				pauseAtAliasLimit = true
			}
			if automation.DailyCreationLimit > 0 {
				dailyRemaining := account.RemainingAliasAutomationDailyCapacity(automation, runAt)
				if dailyRemaining <= 0 {
					result.Error = fmt.Sprintf("已达到今日自动创建上限 %d，将在次日继续", automation.DailyCreationLimit)
					result.Status = account.AliasAutomationStatusSkipped
					nextRunAt = account.NextAliasAutomationAllowedAt(automation, account.NextAliasAutomationDayStart(runAt)).Format(time.RFC3339)
					return nil
				}
				if result.Requested >= dailyRemaining {
					result.Requested = min(result.Requested, dailyRemaining)
					nextRunAt = account.NextAliasAutomationAllowedAt(automation, account.NextAliasAutomationDayStart(runAt)).Format(time.RFC3339)
				}
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
			if pauseAtAliasLimit {
				result.Error = fmt.Sprintf("已达到总别名安全上限 %d", automation.MaxTotalAliases)
				result.PauseReason = account.AliasAutomationPauseReasonAliasLimit
			}
			if nextRunAt != "" && !pauseAtAliasLimit {
				result.Error = fmt.Sprintf("已达到今日自动创建上限 %d，将在次日继续", automation.DailyCreationLimit)
			}
			result.Status = account.AliasAutomationStatusSuccess
			return nil
		})
	}

	if !shouldRecord {
		return result, operationErr
	}
	if operationErr != nil && result.Status == account.AliasAutomationStatusSkipped {
		result.Complete = false
		result.Error = aliasOperationErrorSummary(operationErr)
		result.Failed = 1
		result.Status = account.AliasAutomationStatusError
	}
	if result.Status != account.AliasAutomationStatusSuccess && result.Status != account.AliasAutomationStatusSkipped {
		nextRunAt = ""
	}
	updated, persistErr := s.mgr.RecordAliasAutomationRun(accountID, account.AliasAutomationRun{
		ActiveAliases: result.ActiveBefore + result.Created,
		Created:       result.Created,
		Error:         result.Error,
		NextRunAt:     nextRunAt,
		PauseReason:   result.PauseReason,
		Status:        result.Status,
	}, runAt)
	if persistErr != nil {
		return result, fmt.Errorf("保存自动化运行状态失败: %w", persistErr)
	}
	result.Automation = updated
	triggerName := account.AliasCreationTriggerAutomationManual
	if trigger == aliasAutomationTriggerScheduled {
		triggerName = account.AliasCreationTriggerAutomationScheduled
	}
	history, historyErr := s.operations.recordAliasCreation(accountID, account.AliasCreationHistory{
		Aliases:     aliasCreationHistoryAliases(result.Aliases),
		Complete:    result.Complete,
		Created:     result.Created,
		Error:       result.Error,
		Failed:      result.Failed,
		LabelPrefix: automation.LabelPrefix,
		Requested:   result.Requested,
		Status:      result.Status,
		Trigger:     triggerName,
	}, runAt)
	if historyErr != nil {
		return result, fmt.Errorf("保存别名创建历史失败: %w", historyErr)
	}
	result.BatchID = history.BatchID
	applyAliasCreationBatchID(result.Aliases, history.BatchID)
	if trigger == aliasAutomationTriggerScheduled && s.onScheduledRun != nil {
		s.onScheduledRun(aliasAutomationScheduledRun{
			Complete:    result.Complete,
			Created:     result.Created,
			Duration:    time.Since(startedAt),
			Failed:      result.Failed,
			PauseReason: updated.PauseReason,
			Status:      result.Status,
		})
	}
	if s.onRun != nil {
		s.onRun(aliasAutomationRunNotification{
			AccountID:   accountID,
			Complete:    result.Complete,
			Created:     result.Created,
			Error:       result.Error,
			Failed:      result.Failed,
			PauseReason: updated.PauseReason,
			Requested:   result.Requested,
			Status:      result.Status,
			Trigger:     string(trigger),
		})
	}
	if !updated.Enabled {
		s.Start()
	}
	return result, operationErr
}

func aliasAutomationCreateCount(automation account.AliasAutomation, activeAliases int) int {
	needed := automation.ScheduledBatchSize
	if automation.MinimumActive > 0 && activeAliases < automation.MinimumActive {
		needed = max(needed, automation.TargetActive-activeAliases)
	}
	if needed == 0 && automation.TargetCreated > automation.CreatedTotal {
		needed = automation.MaxBatchSize
	}
	needed = min(needed, automation.MaxBatchSize)
	if automation.TargetCreated > 0 {
		remaining := automation.TargetCreated - automation.CreatedTotal
		if remaining <= 0 {
			return 0
		}
		needed = min(needed, remaining)
	}
	return needed
}

func aliasAutomationConfigured(automation account.AliasAutomation) bool {
	return automation.ScheduledBatchSize > 0 || automation.MinimumActive > 0 || automation.TargetCreated > 0
}

func aliasAutomationTargetReached(automation account.AliasAutomation) bool {
	return automation.TargetCreated > 0 && automation.CreatedTotal >= automation.TargetCreated
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
