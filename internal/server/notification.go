package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/notification"
)

type emailNotificationUpdateReq struct {
	Enabled           bool   `json:"enabled"`
	SenderEmail       string `json:"sender_email"`
	AuthorizationCode string `json:"authorization_code"`
	RecipientEmail    string `json:"recipient_email"`
}

type webhookNotificationUpdateReq struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
}

func (s *Server) getEmailNotification(c *gin.Context) {
	if s.notifications == nil {
		fail(c, http.StatusServiceUnavailable, "邮件通知服务暂不可用")
		return
	}
	ok(c, s.notifications.PublicConfig())
}

func (s *Server) updateEmailNotification(c *gin.Context) {
	if s.notifications == nil {
		fail(c, http.StatusServiceUnavailable, "邮件通知服务暂不可用")
		return
	}
	var req emailNotificationUpdateReq
	if !bindJSON(c, &req, "邮件通知参数无效") {
		return
	}
	public, err := s.notifications.Update(notification.UpdateInput{
		Enabled:           req.Enabled,
		SenderEmail:       strings.TrimSpace(req.SenderEmail),
		AuthorizationCode: req.AuthorizationCode,
		RecipientEmail:    strings.TrimSpace(req.RecipientEmail),
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, notification.ErrPersistence) {
			status = http.StatusInternalServerError
		}
		fail(c, status, err.Error())
		return
	}
	ok(c, public)
}

func (s *Server) testEmailNotification(c *gin.Context) {
	if s.notifications == nil {
		fail(c, http.StatusServiceUnavailable, "邮件通知服务暂不可用")
		return
	}
	if err := s.notifications.SendTest(); err != nil {
		if errors.Is(err, notification.ErrNotConfigured) {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		fail(c, http.StatusBadGateway, "邮件测试发送失败: "+err.Error())
		return
	}
	ok(c, gin.H{"message": "邮件测试邮件已发送"})
}

func (s *Server) getWebhookNotification(c *gin.Context) {
	if s.webhooks == nil {
		fail(c, http.StatusServiceUnavailable, "webhook notification service is unavailable")
		return
	}
	ok(c, s.webhooks.PublicConfig())
}

func (s *Server) updateWebhookNotification(c *gin.Context) {
	if s.webhooks == nil {
		fail(c, http.StatusServiceUnavailable, "webhook notification service is unavailable")
		return
	}
	var req webhookNotificationUpdateReq
	if !bindJSON(c, &req, "webhook notification parameters are invalid") {
		return
	}
	public, err := s.webhooks.Update(notification.WebhookUpdateInput{
		Enabled: req.Enabled,
		URL:     strings.TrimSpace(req.URL),
		Secret:  req.Secret,
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, notification.ErrWebhookPersistence) {
			status = http.StatusInternalServerError
		}
		fail(c, status, err.Error())
		return
	}
	ok(c, public)
}

func (s *Server) testWebhookNotification(c *gin.Context) {
	if s.webhooks == nil {
		fail(c, http.StatusServiceUnavailable, "webhook notification service is unavailable")
		return
	}
	if err := s.webhooks.SendTest(); err != nil {
		if errors.Is(err, notification.ErrWebhookNotConfigured) {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		fail(c, http.StatusBadGateway, "webhook test delivery failed: "+err.Error())
		return
	}
	ok(c, gin.H{"message": "webhook test delivery completed"})
}

func (s *Server) notifyExternal(event notification.Event) {
	if s.notifications != nil {
		s.notifications.Notify(event)
	}
	if s.webhooks != nil {
		s.webhooks.Notify(event)
	}
}

func (s *Server) notifyAliasAutomationRun(run aliasAutomationRunNotification) {
	kind := "alias_automation"
	if run.PauseReason != "" {
		kind = "alias_automation_paused"
	}
	s.notifyExternal(notification.Event{
		AccountID:   run.AccountID,
		Complete:    run.Complete,
		Created:     run.Created,
		Error:       run.Error,
		Failed:      run.Failed,
		Kind:        kind,
		PauseReason: run.PauseReason,
		Requested:   run.Requested,
		Status:      run.Status,
		Trigger:     run.Trigger,
	})
}

func (s *Server) notifySessionExpired(accountID string) {
	s.notifyExternal(notification.Event{
		AccountID: accountID,
		Complete:  false,
		Error:     "iCloud session is no longer valid",
		Kind:      "session_expired",
		Status:    "error",
		Trigger:   "system",
	})
}
