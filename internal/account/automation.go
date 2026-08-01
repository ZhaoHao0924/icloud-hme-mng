package account

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MinAliasAutomationIntervalMinutes     = 5
	MaxAliasAutomationIntervalMinutes     = 7 * 24 * 60
	DefaultAliasAutomationIntervalMinutes = 60
	MinAliasAutomationBatchSize           = 1
	MaxAliasAutomationBatchSize           = 20
	DefaultAliasAutomationBatchSize       = 5
	MaxAliasAutomationTargetActive        = 100
	MaxAliasAutomationLabelPrefixRunes    = 196

	AliasAutomationStatusSuccess = "success"
	AliasAutomationStatusPartial = "partial"
	AliasAutomationStatusSkipped = "skipped"
	AliasAutomationStatusError   = "error"
)

// AliasAutomation 是单个账户的别名创建规则及最近一次运行状态。
// 它会随账户一起保存在 accounts.json 中，不包含凭据或上游原始响应。
type AliasAutomation struct {
	Enabled            bool   `json:"enabled"`
	IntervalMinutes    int    `json:"interval_minutes"`
	ScheduledBatchSize int    `json:"scheduled_batch_size"`
	MinimumActive      int    `json:"minimum_active"`
	TargetActive       int    `json:"target_active"`
	MaxBatchSize       int    `json:"max_batch_size"`
	LabelPrefix        string `json:"label_prefix"`
	LastRunAt          string `json:"last_run_at,omitempty"`
	NextRunAt          string `json:"next_run_at,omitempty"`
	LastStatus         string `json:"last_status,omitempty"`
	LastActive         int    `json:"last_active"`
	LastCreated        int    `json:"last_created"`
	LastError          string `json:"last_error,omitempty"`
}

// AliasAutomationRun 是一次自动化规则执行后需要写回的运行摘要。
type AliasAutomationRun struct {
	ActiveAliases int
	Created       int
	Error         string
	Status        string
}

// AliasAutomationAccount 是调度器扫描启用规则时使用的轻量快照。
type AliasAutomationAccount struct {
	AccountID  string
	Automation AliasAutomation
}

func DefaultAliasAutomation() AliasAutomation {
	return AliasAutomation{
		IntervalMinutes: DefaultAliasAutomationIntervalMinutes,
		MaxBatchSize:    DefaultAliasAutomationBatchSize,
	}
}

func cloneAliasAutomation(automation *AliasAutomation) *AliasAutomation {
	if automation == nil {
		return nil
	}
	cloned := *automation
	return &cloned
}

// ValidateAliasAutomation 检查可由 API 配置的自动化字段。
func ValidateAliasAutomation(automation AliasAutomation) error {
	if automation.IntervalMinutes < MinAliasAutomationIntervalMinutes || automation.IntervalMinutes > MaxAliasAutomationIntervalMinutes {
		return fmt.Errorf("interval_minutes 必须是 %d 到 %d 之间的整数", MinAliasAutomationIntervalMinutes, MaxAliasAutomationIntervalMinutes)
	}
	if automation.MaxBatchSize < MinAliasAutomationBatchSize || automation.MaxBatchSize > MaxAliasAutomationBatchSize {
		return fmt.Errorf("max_batch_size 必须是 %d 到 %d 之间的整数", MinAliasAutomationBatchSize, MaxAliasAutomationBatchSize)
	}
	if automation.ScheduledBatchSize < 0 || automation.ScheduledBatchSize > automation.MaxBatchSize {
		return fmt.Errorf("scheduled_batch_size 必须是 0 到 max_batch_size 之间的整数")
	}
	if automation.MinimumActive < 0 || automation.MinimumActive > MaxAliasAutomationTargetActive {
		return fmt.Errorf("minimum_active 必须是 0 到 %d 之间的整数", MaxAliasAutomationTargetActive)
	}
	if automation.TargetActive < 0 || automation.TargetActive > MaxAliasAutomationTargetActive {
		return fmt.Errorf("target_active 必须是 0 到 %d 之间的整数", MaxAliasAutomationTargetActive)
	}
	if automation.MinimumActive == 0 && automation.TargetActive != 0 {
		return fmt.Errorf("minimum_active 为 0 时 target_active 必须为 0")
	}
	if automation.MinimumActive > 0 && automation.TargetActive < automation.MinimumActive {
		return fmt.Errorf("target_active 不能小于 minimum_active")
	}
	if automation.Enabled && automation.ScheduledBatchSize == 0 && automation.MinimumActive == 0 {
		return fmt.Errorf("启用自动化时必须设置定时创建数量或库存阈值")
	}
	if utf8.RuneCountInString(strings.TrimSpace(automation.LabelPrefix)) > MaxAliasAutomationLabelPrefixRunes {
		return fmt.Errorf("label_prefix 不能超过 %d 个字符", MaxAliasAutomationLabelPrefixRunes)
	}
	return nil
}

func normalizeStoredAliasAutomation(automation AliasAutomation) (AliasAutomation, error) {
	if automation.IntervalMinutes == 0 {
		automation.IntervalMinutes = DefaultAliasAutomationIntervalMinutes
	}
	if automation.MaxBatchSize == 0 {
		automation.MaxBatchSize = DefaultAliasAutomationBatchSize
	}
	automation.LabelPrefix = strings.TrimSpace(automation.LabelPrefix)
	if err := ValidateAliasAutomation(automation); err != nil {
		return AliasAutomation{}, err
	}
	if automation.LastActive < 0 || automation.LastCreated < 0 {
		return AliasAutomation{}, fmt.Errorf("运行状态计数不能为负数")
	}
	if automation.LastStatus != "" && !validAliasAutomationStatus(automation.LastStatus) {
		return AliasAutomation{}, fmt.Errorf("last_status %q 无效", automation.LastStatus)
	}
	automation.LastError = truncate(strings.TrimSpace(automation.LastError), 300)
	return automation, nil
}

func validAliasAutomationStatus(status string) bool {
	switch status {
	case AliasAutomationStatusSuccess, AliasAutomationStatusPartial, AliasAutomationStatusSkipped, AliasAutomationStatusError:
		return true
	default:
		return false
	}
}

// GetAliasAutomation 返回账户已配置的规则；未配置时返回安全默认值。
func (m *Manager) GetAliasAutomation(id string) (AliasAutomation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	acc, ok := m.accounts[id]
	if !ok {
		return AliasAutomation{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	if acc.AliasAutomation == nil {
		return DefaultAliasAutomation(), nil
	}
	return *cloneAliasAutomation(acc.AliasAutomation), nil
}

// SetAliasAutomation 保存账户规则，并从当前时刻重新计算下次定时执行时间。
func (m *Manager) SetAliasAutomation(id string, automation AliasAutomation, now time.Time) (AliasAutomation, error) {
	automation.LabelPrefix = strings.TrimSpace(automation.LabelPrefix)
	if err := ValidateAliasAutomation(automation); err != nil {
		return AliasAutomation{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return AliasAutomation{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}

	previous := cloneAliasAutomation(acc.AliasAutomation)
	if previous != nil {
		automation.LastRunAt = previous.LastRunAt
		automation.LastStatus = previous.LastStatus
		automation.LastActive = previous.LastActive
		automation.LastCreated = previous.LastCreated
		automation.LastError = previous.LastError
	}
	if automation.Enabled {
		automation.NextRunAt = now.Add(time.Duration(automation.IntervalMinutes) * time.Minute).Format(time.RFC3339)
	} else {
		automation.NextRunAt = ""
	}
	acc.AliasAutomation = &automation
	if err := m.save(); err != nil {
		acc.AliasAutomation = previous
		return AliasAutomation{}, err
	}
	return automation, nil
}

// RecordAliasAutomationRun 更新最近运行摘要，并为仍启用的规则安排下一次执行。
func (m *Manager) RecordAliasAutomationRun(id string, run AliasAutomationRun, now time.Time) (AliasAutomation, error) {
	if !validAliasAutomationStatus(run.Status) {
		return AliasAutomation{}, fmt.Errorf("运行状态 %q 无效", run.Status)
	}
	if run.ActiveAliases < 0 || run.Created < 0 {
		return AliasAutomation{}, fmt.Errorf("运行状态计数不能为负数")
	}
	if now.IsZero() {
		now = time.Now()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return AliasAutomation{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	if acc.AliasAutomation == nil {
		return AliasAutomation{}, fmt.Errorf("账号未配置别名自动化")
	}

	previous := cloneAliasAutomation(acc.AliasAutomation)
	automation := *acc.AliasAutomation
	automation.LastRunAt = now.Format(time.RFC3339)
	automation.LastStatus = run.Status
	automation.LastActive = run.ActiveAliases
	automation.LastCreated = run.Created
	automation.LastError = truncate(strings.TrimSpace(run.Error), 300)
	if automation.Enabled {
		automation.NextRunAt = now.Add(time.Duration(automation.IntervalMinutes) * time.Minute).Format(time.RFC3339)
	} else {
		automation.NextRunAt = ""
	}
	acc.AliasAutomation = &automation
	if err := m.save(); err != nil {
		acc.AliasAutomation = previous
		return AliasAutomation{}, err
	}
	return automation, nil
}

// ListEnabledAliasAutomations 返回所有启用自动化的账户快照，按账户 ID 排序。
func (m *Manager) ListEnabledAliasAutomations() []AliasAutomationAccount {
	m.mu.RLock()
	defer m.mu.RUnlock()

	accounts := make([]AliasAutomationAccount, 0)
	for id, acc := range m.accounts {
		if acc.AliasAutomation == nil || !acc.AliasAutomation.Enabled {
			continue
		}
		accounts = append(accounts, AliasAutomationAccount{
			AccountID:  id,
			Automation: *cloneAliasAutomation(acc.AliasAutomation),
		})
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].AccountID < accounts[j].AccountID
	})
	return accounts
}
