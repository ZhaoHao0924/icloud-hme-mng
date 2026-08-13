package account

import (
	"errors"
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
	MaxAliasAutomationTargetCreated       = 1000
	DefaultAliasAutomationMaxTotalAliases = 1000
	MaxAliasAutomationTotalAliases        = 1000
	DefaultAliasAutomationFailureLimit    = 3
	MaxAliasAutomationFailureLimit        = 10
	MaxAliasAutomationDailyCreationLimit  = 1000
	MaxAliasAutomationLabelPrefixRunes    = 196

	AliasAutomationStatusSuccess = "success"
	AliasAutomationStatusPartial = "partial"
	AliasAutomationStatusSkipped = "skipped"
	AliasAutomationStatusError   = "error"

	AliasAutomationPauseReasonTargetReached = "target_reached"
	AliasAutomationPauseReasonAliasLimit    = "alias_limit"
	AliasAutomationPauseReasonFailureLimit  = "failure_limit"
	AliasAutomationPauseReasonManual        = "manual"
)

const maxAliasAutomationRetryDelay = 7 * 24 * time.Hour

const aliasAutomationClockLayout = "15:04"

var (
	ErrAliasAutomationNotConfigured = errors.New("账号未配置别名自动化规则")
	ErrAliasAutomationTargetReached = errors.New("累计创建目标已完成，请调整目标后再恢复规则")
)

// AliasAutomation 是单个账户的别名创建规则及最近一次运行状态。
// 它会随账户一起保存在 accounts.json 中，不包含凭据或上游原始响应。
type AliasAutomation struct {
	Enabled              bool   `json:"enabled"`
	IntervalMinutes      int    `json:"interval_minutes"`
	AllowedWeekdays      []int  `json:"allowed_weekdays"`
	ExecutionWindowStart string `json:"execution_window_start"`
	ExecutionWindowEnd   string `json:"execution_window_end"`
	ScheduledBatchSize   int    `json:"scheduled_batch_size"`
	MinimumActive        int    `json:"minimum_active"`
	TargetActive         int    `json:"target_active"`
	MaxBatchSize         int    `json:"max_batch_size"`
	MaxTotalAliases      int    `json:"max_total_aliases"`
	MaxFailureCount      int    `json:"max_failure_count"`
	DailyCreationLimit   int    `json:"daily_creation_limit"`
	TargetCreated        int    `json:"target_created"`
	LabelPrefix          string `json:"label_prefix"`
	LastRunAt            string `json:"last_run_at,omitempty"`
	NextRunAt            string `json:"next_run_at,omitempty"`
	LastStatus           string `json:"last_status,omitempty"`
	LastActive           int    `json:"last_active"`
	LastCreated          int    `json:"last_created"`
	CreatedTotal         int    `json:"created_total"`
	ConsecutiveFailure   int    `json:"consecutive_failure"`
	DailyCreated         int    `json:"daily_created"`
	DailyCreatedDate     string `json:"daily_created_date,omitempty"`
	PauseReason          string `json:"pause_reason,omitempty"`
	LastError            string `json:"last_error,omitempty"`
}

// AliasAutomationRun 是一次自动化规则执行后需要写回的运行摘要。
type AliasAutomationRun struct {
	ActiveAliases int
	Created       int
	Error         string
	NextRunAt     string
	PauseReason   string
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
		AllowedWeekdays: []int{},
		MaxBatchSize:    DefaultAliasAutomationBatchSize,
		MaxTotalAliases: DefaultAliasAutomationMaxTotalAliases,
		MaxFailureCount: DefaultAliasAutomationFailureLimit,
	}
}

func cloneAliasAutomation(automation *AliasAutomation) *AliasAutomation {
	if automation == nil {
		return nil
	}
	cloned := *automation
	cloned.AllowedWeekdays = append([]int(nil), automation.AllowedWeekdays...)
	if cloned.AllowedWeekdays == nil {
		cloned.AllowedWeekdays = []int{}
	}
	return &cloned
}

// ValidateAliasAutomation 检查可由 API 配置的自动化字段。
func ValidateAliasAutomation(automation AliasAutomation) error {
	if err := validateAliasAutomationSchedule(automation); err != nil {
		return err
	}
	if automation.IntervalMinutes < MinAliasAutomationIntervalMinutes || automation.IntervalMinutes > MaxAliasAutomationIntervalMinutes {
		return fmt.Errorf("interval_minutes 必须是 %d 到 %d 之间的整数", MinAliasAutomationIntervalMinutes, MaxAliasAutomationIntervalMinutes)
	}
	if automation.MaxBatchSize < MinAliasAutomationBatchSize || automation.MaxBatchSize > MaxAliasAutomationBatchSize {
		return fmt.Errorf("max_batch_size 必须是 %d 到 %d 之间的整数", MinAliasAutomationBatchSize, MaxAliasAutomationBatchSize)
	}
	if automation.MaxTotalAliases < 1 || automation.MaxTotalAliases > MaxAliasAutomationTotalAliases {
		return fmt.Errorf("max_total_aliases 必须是 1 到 %d 之间的整数", MaxAliasAutomationTotalAliases)
	}
	if automation.MaxFailureCount < 1 || automation.MaxFailureCount > MaxAliasAutomationFailureLimit {
		return fmt.Errorf("max_failure_count 必须是 1 到 %d 之间的整数", MaxAliasAutomationFailureLimit)
	}
	if automation.DailyCreationLimit < 0 || automation.DailyCreationLimit > MaxAliasAutomationDailyCreationLimit {
		return fmt.Errorf("daily_creation_limit 必须是 0 到 %d 之间的整数", MaxAliasAutomationDailyCreationLimit)
	}
	if automation.DailyCreated < 0 || automation.DailyCreated > automation.DailyCreationLimit {
		return fmt.Errorf("daily_created 必须是 0 到 daily_creation_limit 之间的整数")
	}
	if automation.DailyCreationLimit == 0 && (automation.DailyCreated != 0 || automation.DailyCreatedDate != "") {
		return fmt.Errorf("daily_creation_limit 为 0 时不应保存每日创建进度")
	}
	if automation.DailyCreatedDate != "" {
		if _, err := time.Parse("2006-01-02", automation.DailyCreatedDate); err != nil {
			return fmt.Errorf("daily_created_date 必须是 YYYY-MM-DD")
		}
	}
	if automation.DailyCreated > 0 && automation.DailyCreatedDate == "" {
		return fmt.Errorf("daily_created 大于 0 时必须保存 daily_created_date")
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
	if automation.TargetCreated < 0 || automation.TargetCreated > MaxAliasAutomationTargetCreated {
		return fmt.Errorf("target_created 必须是 0 到 %d 之间的整数", MaxAliasAutomationTargetCreated)
	}
	if automation.CreatedTotal < 0 || automation.CreatedTotal > automation.TargetCreated {
		return fmt.Errorf("created_total 必须是 0 到 target_created 之间的整数")
	}
	if automation.Enabled && automation.ScheduledBatchSize == 0 && automation.MinimumActive == 0 && automation.TargetCreated == 0 {
		return fmt.Errorf("启用自动化时必须设置定时创建数量、库存阈值或累计创建目标")
	}
	if utf8.RuneCountInString(strings.TrimSpace(automation.LabelPrefix)) > MaxAliasAutomationLabelPrefixRunes {
		return fmt.Errorf("label_prefix 不能超过 %d 个字符", MaxAliasAutomationLabelPrefixRunes)
	}
	return nil
}

func validateAliasAutomationSchedule(automation AliasAutomation) error {
	seenWeekdays := make(map[int]struct{}, len(automation.AllowedWeekdays))
	for _, weekday := range automation.AllowedWeekdays {
		if weekday < int(time.Sunday) || weekday > int(time.Saturday) {
			return fmt.Errorf("allowed_weekdays 只能包含 0 到 6 的星期值")
		}
		if _, exists := seenWeekdays[weekday]; exists {
			return fmt.Errorf("allowed_weekdays 不能包含重复的星期值")
		}
		seenWeekdays[weekday] = struct{}{}
	}

	start := strings.TrimSpace(automation.ExecutionWindowStart)
	end := strings.TrimSpace(automation.ExecutionWindowEnd)
	if start == "" && end == "" {
		return nil
	}
	if start == "" || end == "" {
		return fmt.Errorf("execution_window_start 和 execution_window_end 必须同时设置")
	}
	startMinutes, err := aliasAutomationClockMinutes(start)
	if err != nil {
		return fmt.Errorf("execution_window_start 必须是 HH:MM 格式")
	}
	endMinutes, err := aliasAutomationClockMinutes(end)
	if err != nil {
		return fmt.Errorf("execution_window_end 必须是 HH:MM 格式")
	}
	if startMinutes >= endMinutes {
		return fmt.Errorf("execution_window_end 必须晚于 execution_window_start")
	}
	return nil
}

func normalizeAliasAutomationSchedule(automation *AliasAutomation) {
	if automation == nil {
		return
	}
	automation.AllowedWeekdays = append([]int(nil), automation.AllowedWeekdays...)
	if automation.AllowedWeekdays == nil {
		automation.AllowedWeekdays = []int{}
	}
	sort.Ints(automation.AllowedWeekdays)
	automation.ExecutionWindowStart = strings.TrimSpace(automation.ExecutionWindowStart)
	automation.ExecutionWindowEnd = strings.TrimSpace(automation.ExecutionWindowEnd)
}

func aliasAutomationClockMinutes(value string) (int, error) {
	parsed, err := time.Parse(aliasAutomationClockLayout, value)
	if err != nil || parsed.Format(aliasAutomationClockLayout) != value {
		return 0, fmt.Errorf("invalid clock value")
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func normalizeStoredAliasAutomation(automation AliasAutomation) (AliasAutomation, error) {
	if automation.IntervalMinutes == 0 {
		automation.IntervalMinutes = DefaultAliasAutomationIntervalMinutes
	}
	if automation.MaxBatchSize == 0 {
		automation.MaxBatchSize = DefaultAliasAutomationBatchSize
	}
	if automation.MaxTotalAliases == 0 {
		automation.MaxTotalAliases = DefaultAliasAutomationMaxTotalAliases
	}
	if automation.MaxFailureCount == 0 {
		automation.MaxFailureCount = DefaultAliasAutomationFailureLimit
	}
	automation.LabelPrefix = strings.TrimSpace(automation.LabelPrefix)
	normalizeAliasAutomationSchedule(&automation)
	if err := ValidateAliasAutomation(automation); err != nil {
		return AliasAutomation{}, err
	}
	if automation.LastActive < 0 || automation.LastCreated < 0 || automation.ConsecutiveFailure < 0 {
		return AliasAutomation{}, fmt.Errorf("运行状态计数不能为负数")
	}
	if automation.ConsecutiveFailure > automation.MaxFailureCount {
		return AliasAutomation{}, fmt.Errorf("连续失败计数不能大于 max_failure_count")
	}
	if automation.LastStatus != "" && !validAliasAutomationStatus(automation.LastStatus) {
		return AliasAutomation{}, fmt.Errorf("last_status %q 无效", automation.LastStatus)
	}
	if automation.PauseReason != "" && !validAliasAutomationPauseReason(automation.PauseReason) {
		return AliasAutomation{}, fmt.Errorf("pause_reason %q 无效", automation.PauseReason)
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

func validAliasAutomationPauseReason(reason string) bool {
	switch reason {
	case AliasAutomationPauseReasonTargetReached,
		AliasAutomationPauseReasonAliasLimit,
		AliasAutomationPauseReasonFailureLimit,
		AliasAutomationPauseReasonManual:
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
	normalizeAliasAutomationSchedule(&automation)
	// 创建进度由服务端在每次实际运行后写入，配置请求不能篡改它。
	automation.CreatedTotal = 0
	automation.ConsecutiveFailure = 0
	automation.DailyCreated = 0
	automation.DailyCreatedDate = ""
	automation.PauseReason = ""
	if automation.MaxTotalAliases == 0 {
		automation.MaxTotalAliases = DefaultAliasAutomationMaxTotalAliases
	}
	if automation.MaxFailureCount == 0 {
		automation.MaxFailureCount = DefaultAliasAutomationFailureLimit
	}
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
	targetChanged := previous == nil || automation.TargetCreated != previous.TargetCreated
	if previous != nil {
		automation.LastRunAt = previous.LastRunAt
		automation.LastStatus = previous.LastStatus
		automation.LastActive = previous.LastActive
		automation.LastCreated = previous.LastCreated
		automation.LastError = previous.LastError
		if !targetChanged {
			automation.CreatedTotal = previous.CreatedTotal
		}
		if !targetChanged && previous.Enabled && automation.Enabled {
			automation.ConsecutiveFailure = previous.ConsecutiveFailure
		}
		if !targetChanged && !previous.Enabled && !automation.Enabled {
			automation.ConsecutiveFailure = previous.ConsecutiveFailure
			automation.PauseReason = previous.PauseReason
		}
		if automation.DailyCreationLimit > 0 {
			automation.DailyCreated = previous.DailyCreated
			automation.DailyCreatedDate = previous.DailyCreatedDate
			resetAliasAutomationDailyCounter(&automation, now)
		}
	}
	if automation.TargetCreated > 0 && automation.CreatedTotal >= automation.TargetCreated {
		automation.Enabled = false
		automation.PauseReason = AliasAutomationPauseReasonTargetReached
	}
	if automation.Enabled {
		automation.NextRunAt = nextAliasAutomationRunAt(automation, now)
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
	if run.PauseReason != "" && !validAliasAutomationPauseReason(run.PauseReason) {
		return AliasAutomation{}, fmt.Errorf("暂停原因 %q 无效", run.PauseReason)
	}
	if run.NextRunAt != "" {
		nextRun, err := time.Parse(time.RFC3339, run.NextRunAt)
		if err != nil {
			return AliasAutomation{}, fmt.Errorf("下次运行时间无效")
		}
		run.NextRunAt = nextRun.Format(time.RFC3339)
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
	resetAliasAutomationDailyCounter(&automation, now)
	automation.LastRunAt = now.Format(time.RFC3339)
	automation.LastStatus = run.Status
	automation.LastActive = run.ActiveAliases
	automation.LastCreated = run.Created
	automation.LastError = truncate(strings.TrimSpace(run.Error), 300)
	if automation.LastError == "" && run.PauseReason != "" {
		automation.LastError = aliasAutomationPauseReasonMessage(run.PauseReason)
	}
	switch run.Status {
	case AliasAutomationStatusSuccess:
		automation.ConsecutiveFailure = 0
	case AliasAutomationStatusPartial, AliasAutomationStatusError:
		automation.ConsecutiveFailure++
	}
	if automation.TargetCreated > 0 {
		automation.CreatedTotal += run.Created
		if automation.CreatedTotal >= automation.TargetCreated {
			automation.CreatedTotal = automation.TargetCreated
			automation.Enabled = false
			automation.PauseReason = AliasAutomationPauseReasonTargetReached
		}
	}
	if automation.DailyCreationLimit > 0 {
		automation.DailyCreated += run.Created
		if automation.DailyCreated > automation.DailyCreationLimit {
			automation.DailyCreated = automation.DailyCreationLimit
		}
	}
	if automation.TargetCreated > 0 && automation.CreatedTotal >= automation.TargetCreated {
		automation.Enabled = false
		automation.PauseReason = AliasAutomationPauseReasonTargetReached
	} else if run.PauseReason != "" {
		automation.Enabled = false
		automation.PauseReason = run.PauseReason
	} else if (run.Status == AliasAutomationStatusPartial || run.Status == AliasAutomationStatusError) && automation.ConsecutiveFailure >= automation.MaxFailureCount {
		automation.Enabled = false
		automation.PauseReason = AliasAutomationPauseReasonFailureLimit
	}
	if automation.Enabled {
		automation.PauseReason = ""
		if run.NextRunAt != "" {
			automation.NextRunAt = run.NextRunAt
		} else {
			automation.NextRunAt = nextAliasAutomationRunAt(automation, now)
		}
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

func nextAliasAutomationRunAt(automation AliasAutomation, now time.Time) string {
	delay := time.Duration(automation.IntervalMinutes) * time.Minute
	for failures := automation.ConsecutiveFailure; failures > 0 && delay < maxAliasAutomationRetryDelay; failures-- {
		delay *= 2
	}
	if delay > maxAliasAutomationRetryDelay {
		delay = maxAliasAutomationRetryDelay
	}
	return NextAliasAutomationAllowedAt(automation, now.Add(delay)).Format(time.RFC3339)
}

// IsAliasAutomationScheduleAllowed reports whether a scheduled run may start
// at now. An empty weekday list and empty time window allow every time.
func IsAliasAutomationScheduleAllowed(automation AliasAutomation, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	if !aliasAutomationWeekdayAllowed(automation.AllowedWeekdays, now.Weekday()) {
		return false
	}
	start, end, hasWindow := aliasAutomationWindowForDate(automation, now)
	return !hasWindow || (!now.Before(start) && now.Before(end))
}

// NextAliasAutomationAllowedAt returns the first permitted scheduled time at
// or after candidate. The execution window end is exclusive.
func NextAliasAutomationAllowedAt(automation AliasAutomation, candidate time.Time) time.Time {
	if candidate.IsZero() {
		candidate = time.Now()
	}
	for offset := 0; offset <= 7; offset++ {
		day := time.Date(candidate.Year(), candidate.Month(), candidate.Day()+offset, 0, 0, 0, 0, candidate.Location())
		if !aliasAutomationWeekdayAllowed(automation.AllowedWeekdays, day.Weekday()) {
			continue
		}
		start, end, hasWindow := aliasAutomationWindowForDate(automation, day)
		if !hasWindow {
			if offset == 0 {
				return candidate
			}
			return day
		}
		if offset > 0 || candidate.Before(start) {
			return start
		}
		if candidate.Before(end) {
			return candidate
		}
	}
	// Validation guarantees at least one permitted weekday. This fallback keeps
	// the helper total for callers that pass an unvalidated rule.
	return candidate.AddDate(0, 0, 7)
}

func aliasAutomationWeekdayAllowed(allowedWeekdays []int, weekday time.Weekday) bool {
	if len(allowedWeekdays) == 0 {
		return true
	}
	for _, allowed := range allowedWeekdays {
		if allowed == int(weekday) {
			return true
		}
	}
	return false
}

func aliasAutomationWindowForDate(automation AliasAutomation, day time.Time) (time.Time, time.Time, bool) {
	startValue := strings.TrimSpace(automation.ExecutionWindowStart)
	endValue := strings.TrimSpace(automation.ExecutionWindowEnd)
	if startValue == "" || endValue == "" {
		return time.Time{}, time.Time{}, false
	}
	startMinutes, startErr := aliasAutomationClockMinutes(startValue)
	endMinutes, endErr := aliasAutomationClockMinutes(endValue)
	if startErr != nil || endErr != nil || startMinutes >= endMinutes {
		return time.Time{}, time.Time{}, false
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), startMinutes/60, startMinutes%60, 0, 0, day.Location())
	end := time.Date(day.Year(), day.Month(), day.Day(), endMinutes/60, endMinutes%60, 0, 0, day.Location())
	return start, end, true
}

// RemainingAliasAutomationDailyCapacity returns how many aliases the rule may
// still create today. A zero daily limit means the capacity is unrestricted.
func RemainingAliasAutomationDailyCapacity(automation AliasAutomation, now time.Time) int {
	if automation.DailyCreationLimit == 0 {
		return MaxAliasAutomationDailyCreationLimit
	}
	created := automation.DailyCreated
	if automation.DailyCreatedDate != aliasAutomationDay(now) {
		created = 0
	}
	return max(0, automation.DailyCreationLimit-created)
}

// NextAliasAutomationDayStart returns the next local calendar-day boundary.
func NextAliasAutomationDayStart(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

func resetAliasAutomationDailyCounter(automation *AliasAutomation, now time.Time) {
	if automation == nil {
		return
	}
	if automation.DailyCreationLimit == 0 {
		automation.DailyCreated = 0
		automation.DailyCreatedDate = ""
		return
	}
	day := aliasAutomationDay(now)
	if automation.DailyCreatedDate != day {
		automation.DailyCreated = 0
		automation.DailyCreatedDate = day
	}
}

func aliasAutomationDay(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return now.Format("2006-01-02")
}

func aliasAutomationPauseReasonMessage(reason string) string {
	switch reason {
	case AliasAutomationPauseReasonTargetReached:
		return "累计创建目标已完成"
	case AliasAutomationPauseReasonAliasLimit:
		return "已达到总别名安全上限"
	case AliasAutomationPauseReasonFailureLimit:
		return "连续创建失败，规则已自动暂停"
	case AliasAutomationPauseReasonManual:
		return "规则已手动暂停"
	default:
		return ""
	}
}

func clearAliasAutomationSessionError(acc *Account) {
	if acc == nil || acc.AliasAutomation == nil || !isAliasAutomationSessionError(acc.AliasAutomation.LastError) {
		return
	}
	acc.AliasAutomation = cloneAliasAutomation(acc.AliasAutomation)
	acc.AliasAutomation.LastError = ""
}

func isAliasAutomationSessionError(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(normalized, "icloud 会话失效") ||
		strings.Contains(normalized, "cookie 会话已过期") ||
		strings.Contains(normalized, "cookie 已过期") ||
		strings.Contains(normalized, "cookie已过期") ||
		strings.Contains(normalized, "session expired") ||
		strings.Contains(normalized, "http 401") ||
		strings.Contains(normalized, "http 403") ||
		strings.Contains(normalized, "unauthorized") ||
		strings.Contains(normalized, "forbidden")
}

// PauseAliasAutomation stops an existing rule without changing its progress.
func (m *Manager) PauseAliasAutomation(id string, now time.Time) (AliasAutomation, error) {
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
		return AliasAutomation{}, ErrAliasAutomationNotConfigured
	}
	previous := *cloneAliasAutomation(acc.AliasAutomation)
	automation := previous
	resetAliasAutomationDailyCounter(&automation, now)
	automation.Enabled = false
	automation.NextRunAt = ""
	if automation.TargetCreated > 0 && automation.CreatedTotal >= automation.TargetCreated {
		automation.PauseReason = AliasAutomationPauseReasonTargetReached
	} else {
		automation.PauseReason = AliasAutomationPauseReasonManual
	}
	acc.AliasAutomation = &automation
	if err := m.save(); err != nil {
		acc.AliasAutomation = &previous
		return AliasAutomation{}, err
	}
	return automation, nil
}

// ResumeAliasAutomation clears a pause and schedules the next eligible run.
func (m *Manager) ResumeAliasAutomation(id string, now time.Time) (AliasAutomation, error) {
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return AliasAutomation{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	if acc.AliasAutomation == nil || !aliasAutomationHasConfiguration(*acc.AliasAutomation) {
		return AliasAutomation{}, ErrAliasAutomationNotConfigured
	}
	previous := *cloneAliasAutomation(acc.AliasAutomation)
	automation := previous
	if automation.TargetCreated > 0 && automation.CreatedTotal >= automation.TargetCreated {
		return AliasAutomation{}, ErrAliasAutomationTargetReached
	}
	resetAliasAutomationDailyCounter(&automation, now)
	automation.ConsecutiveFailure = 0
	automation.Enabled = true
	automation.LastError = ""
	automation.NextRunAt = nextAliasAutomationRunAt(automation, now)
	automation.PauseReason = ""
	acc.AliasAutomation = &automation
	if err := m.save(); err != nil {
		acc.AliasAutomation = &previous
		return AliasAutomation{}, err
	}
	return automation, nil
}

// DeferAliasAutomationToNextAllowedTime moves an overdue scheduled rule to its
// next permitted window without recording a run or changing its progress.
func (m *Manager) DeferAliasAutomationToNextAllowedTime(id string, now time.Time) (AliasAutomation, error) {
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
		return AliasAutomation{}, ErrAliasAutomationNotConfigured
	}

	previous := *cloneAliasAutomation(acc.AliasAutomation)
	automation := previous
	if !automation.Enabled || IsAliasAutomationScheduleAllowed(automation, now) {
		return automation, nil
	}
	if nextRunAt := strings.TrimSpace(automation.NextRunAt); nextRunAt != "" {
		if nextRun, err := time.Parse(time.RFC3339, nextRunAt); err == nil && nextRun.After(now) {
			return automation, nil
		}
	}
	automation.NextRunAt = NextAliasAutomationAllowedAt(automation, now).Format(time.RFC3339)
	acc.AliasAutomation = &automation
	if err := m.save(); err != nil {
		acc.AliasAutomation = &previous
		return AliasAutomation{}, err
	}
	return automation, nil
}

func aliasAutomationHasConfiguration(automation AliasAutomation) bool {
	return automation.ScheduledBatchSize > 0 || automation.MinimumActive > 0 || automation.TargetCreated > 0
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
