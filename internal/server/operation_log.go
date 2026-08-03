package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/auditlog"
)

const (
	defaultOperationLogLimit = 200
	maxOperationLogLimit     = 500
)

type operationLogData struct {
	Count         int              `json:"count"`
	Entries       []auditlog.Entry `json:"entries"`
	RetentionDays int              `json:"retention_days"`
}

func (s *Server) listOperationLogs(c *gin.Context) {
	if s.operationLogs == nil {
		fail(c, http.StatusServiceUnavailable, "日志存储暂不可用")
		return
	}
	limit, valid := boundedIntQuery(
		c,
		"limit",
		defaultOperationLogLimit,
		1,
		maxOperationLogLimit,
	)
	if !valid {
		return
	}
	entries, err := s.operationLogs.List(limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取日志失败")
		return
	}
	ok(c, operationLogData{
		Count:         len(entries),
		Entries:       entries,
		RetentionDays: auditlog.RetentionDays,
	})
}

func (s *Server) operationLogMiddleware(c *gin.Context) {
	startedAt := time.Now()
	c.Next()

	operation, shouldRecord := operationLogOperation(c.Request.Method, c.FullPath())
	if !shouldRecord || s.operationLogs == nil {
		return
	}
	_, _ = s.operationLogs.Record(auditlog.RecordInput{
		Duration:  time.Since(startedAt),
		Level:     operationLogLevel(c.Writer.Status()),
		Operation: operation,
		Status:    c.Writer.Status(),
	})
}

func (s *Server) recordScheduledAliasAutomationRun(run aliasAutomationScheduledRun) {
	if s.operationLogs == nil {
		return
	}
	status, level := scheduledAliasAutomationLogStatus(run)
	_, _ = s.operationLogs.Record(auditlog.RecordInput{
		Duration:  run.Duration,
		Level:     level,
		Operation: "定时执行别名自动化",
		Status:    status,
	})
}

func scheduledAliasAutomationLogStatus(run aliasAutomationScheduledRun) (int, auditlog.Level) {
	switch run.Status {
	case "error":
		return http.StatusBadGateway, auditlog.LevelError
	case "partial":
		return http.StatusPartialContent, auditlog.LevelWarning
	}
	switch run.PauseReason {
	case "alias_limit", "failure_limit":
		return http.StatusConflict, auditlog.LevelWarning
	default:
		return http.StatusOK, auditlog.LevelInfo
	}
}

func operationLogLevel(status int) auditlog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return auditlog.LevelError
	case status >= http.StatusBadRequest:
		return auditlog.LevelWarning
	default:
		return auditlog.LevelInfo
	}
}

func operationLogOperation(method, route string) (string, bool) {
	switch {
	case method == http.MethodPost && route == "/api/auth/setup":
		return "初始化平台管理员", true
	case method == http.MethodPost && route == "/api/auth/login":
		return "平台登录", true
	case method == http.MethodPost && route == "/api/auth/logout":
		return "退出平台登录", true
	case method == http.MethodPost && route == "/api/accounts":
		return "创建账户", true
	case method == http.MethodDelete && route == "/api/accounts/:id":
		return "删除账户", true
	case method == http.MethodPost && route == "/api/accounts/:id/password":
		return "更新 App 专用密码", true
	case method == http.MethodPut && route == "/api/accounts/:id/cookies":
		return "更新 Cookie", true
	case method == http.MethodPost && route == "/api/accounts/:id/login/start":
		return "发起 Apple 登录", true
	case method == http.MethodPost && route == "/api/accounts/:id/login/verify":
		return "验证 Apple 登录", true
	case method == http.MethodPut && route == "/api/accounts/:id/alias-automation":
		return "更新别名自动化规则", true
	case method == http.MethodPost && route == "/api/accounts/:id/alias-automation/pause":
		return "暂停别名自动化", true
	case method == http.MethodPost && route == "/api/accounts/:id/alias-automation/resume":
		return "恢复别名自动化", true
	case method == http.MethodPost && route == "/api/accounts/:id/alias-automation/run":
		return "执行别名自动化", true
	case method == http.MethodGet && route == "/api/accounts/:id/alias-creation-history":
		return "查看别名创建历史", true
	case method == http.MethodGet && route == "/api/accounts/:id/alias-creation-history.csv":
		return "导出别名创建历史", true
	case method == http.MethodPost && route == "/api/accounts/:id/aliases/batch":
		return "批量创建别名", true
	case method == http.MethodPost && route == "/api/create":
		return "创建别名", true
	case method == http.MethodGet && route == "/api/inbox":
		return "读取收件箱", true
	case method == http.MethodGet && route == "/api/inbox/messages/:id":
		return "读取邮件预览", true
	case method == http.MethodPost && route == "/api/aliases/:id/deactivate":
		return "停用别名", true
	case method == http.MethodPost && route == "/api/aliases/:id/reactivate":
		return "启用别名", true
	case method == http.MethodDelete && route == "/api/aliases/:id":
		return "删除别名", true
	case method == http.MethodPost && route == "/api/reload":
		return "重载配置", true
	case method == http.MethodPut && route == "/api/notifications/email":
		return "163 email notification settings", true
	case method == http.MethodPost && route == "/api/notifications/email/test":
		return "163 email notification test", true
	case method == http.MethodPut && route == "/api/notifications/webhook":
		return "webhook notification settings", true
	case method == http.MethodPost && route == "/api/notifications/webhook/test":
		return "webhook notification test", true
	default:
		return "", false
	}
}
