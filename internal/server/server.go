// Package server 提供 HTTP API,基于 Gin。
//
// 两个核心接口:
//
//	POST /api/create  — 在指定账号下创建一个 Hide My Email 别名
//	GET  /api/inbox   — 读取指定账号(或指定别名)收到的邮件
//
// 辅助接口(用于多账号管理):账号增删查、别名列表、设置 App 密码。
package server

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"icloud-hme/internal/account"
	"icloud-hme/internal/auditlog"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
	"icloud-hme/internal/notification"
	"icloud-hme/internal/platformauth"
	"icloud-hme/internal/webui"
)

const (
	maxRequestBodyBytes      int64 = 1 << 20
	minInboxLimit                  = 1
	maxInboxLimit                  = 100
	defaultInboxLimit              = 20
	minInboxDays                   = 1
	maxInboxDays                   = 365
	defaultInboxDays               = 7
	maxAliasLabelRunes             = 200
	defaultAliasHistoryLimit       = 100
	defaultServiceVersion          = "dev"
	apiTokenInvalidCode            = "api_token_invalid"
)

// Server 封装 Gin 引擎和账号管理器。
type Server struct {
	mgr                 *account.Manager
	r                   *gin.Engine
	authEnabled         bool
	apiTokenHash        [sha256.Size]byte
	version             string
	loginChallenges     *loginChallengeStore
	inboxIMAPSessions   *inboxIMAPSessionPool
	inboxPreviews       *inboxPreviewCache
	newInboxIMAPClient  func(accountID string) (inboxIMAPClient, error)
	startPasswordLogin  startPasswordLoginFunc
	verifyPasswordLogin verifyPasswordLoginFunc
	aliasOperations     *aliasOperationService
	aliasAutomation     *aliasAutomationService
	operationLogs       *auditlog.Store
	notifications       *notification.Notifier
	webhooks            *notification.WebhookNotifier
	platformAuth        *platformauth.Store
	platformSessions    *platformSessionStore
}

// New 创建 Server。apiToken 非空时,所有 /api 路由都需要 Bearer Token。
func New(mgr *account.Manager, debug bool, apiToken string) *Server {
	s, err := newServer(mgr, debug, apiToken, defaultServiceVersion, false)
	if err != nil {
		panic(err)
	}
	return s
}

// NewWithVersion 创建带显式构建版本的 Server。
func NewWithVersion(mgr *account.Manager, debug bool, apiToken, version string) *Server {
	s, err := newServer(mgr, debug, apiToken, version, false)
	if err != nil {
		panic(err)
	}
	return s
}

// NewWithPlatformAuth creates the production server with browser login protection enabled.
func NewWithPlatformAuth(mgr *account.Manager, debug bool, apiToken, version string) (*Server, error) {
	return newServer(mgr, debug, apiToken, version, true)
}

func newServer(
	mgr *account.Manager,
	debug bool,
	apiToken, version string,
	enablePlatformAuth bool,
) (*Server, error) {
	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = defaultServiceVersion
	}
	operationLogs, _ := auditlog.New(mgr.DataDir())
	notifications, err := notification.New(mgr.DataDir())
	if err != nil {
		if operationLogs != nil {
			operationLogs.Close()
		}
		return nil, fmt.Errorf("initialize 163 email notifications: %w", err)
	}
	webhooks, err := notification.NewWebhook(mgr.DataDir())
	if err != nil {
		if operationLogs != nil {
			operationLogs.Close()
		}
		notifications.Close()
		return nil, fmt.Errorf("initialize webhook notifications: %w", err)
	}
	var authStore *platformauth.Store
	if enablePlatformAuth {
		var err error
		authStore, err = platformauth.NewStore(mgr.DataDir())
		if err != nil {
			if operationLogs != nil {
				operationLogs.Close()
			}
			notifications.Close()
			webhooks.Close()
			return nil, fmt.Errorf("initialize platform authentication: %w", err)
		}
	}
	s := &Server{
		mgr:               mgr,
		authEnabled:       apiToken != "",
		version:           version,
		loginChallenges:   newLoginChallengeStore(),
		inboxIMAPSessions: newInboxIMAPSessionPool(),
		inboxPreviews:     newInboxPreviewCache(),
		operationLogs:     operationLogs,
		notifications:     notifications,
		webhooks:          webhooks,
		platformAuth:      authStore,
	}
	if authStore != nil {
		s.platformSessions = newPlatformSessionStore()
	}
	s.newInboxIMAPClient = func(accountID string) (inboxIMAPClient, error) {
		return mgr.MailClient(accountID)
	}
	s.startPasswordLogin = mgr.StartPasswordLogin
	s.verifyPasswordLogin = func(session *account.PasswordLoginSession, otp string) (account.AccountDTO, error) {
		return session.Verify(otp)
	}
	s.aliasOperations = newAliasOperationService(mgr)
	s.aliasAutomation = newAliasAutomationService(mgr, s.aliasOperations)
	s.aliasAutomation.onScheduledRun = s.recordScheduledAliasAutomationRun
	s.aliasAutomation.onRun = s.notifyAliasAutomationRun
	if s.authEnabled {
		s.apiTokenHash = sha256.Sum256([]byte(apiToken))
	}
	s.r = gin.New()
	s.r.Use(gin.Recovery(), securityResponseHeaders, requestIDMiddleware, s.operationLogMiddleware)
	s.register()
	s.aliasAutomation.Start()
	return s, nil
}

// Run 启动 HTTP 服务。
func (s *Server) Run(addr string) error {
	return s.r.Run(addr)
}

// Handler 返回底层 gin 引擎(便于测试)。
func (s *Server) Handler() http.Handler { return s.r }

// Close 停止后台自动化调度器。Run 退出时不需要显式调用；测试和嵌入式使用可调用它释放资源。
func (s *Server) Close() {
	if s.aliasAutomation != nil {
		s.aliasAutomation.Stop()
	}
	if s.aliasOperations != nil {
		s.aliasOperations.close()
	}
	s.inboxIMAPSessions.Clear()
	s.inboxPreviews.Clear()
	if s.platformSessions != nil {
		s.platformSessions.Clear()
	}
	if s.operationLogs != nil {
		s.operationLogs.Close()
	}
	if s.notifications != nil {
		s.notifications.Close()
	}
	if s.webhooks != nil {
		s.webhooks.Close()
	}
}

func (s *Server) register() {
	api := s.r.Group("/api")
	api.Use(noStoreAPIResponses, limitRequestBody)
	platformAuth := api.Group("/auth")
	{
		platformAuth.GET("/session", s.platformAuthSession)
		platformAuth.POST("/setup", s.setupPlatformAuth)
		platformAuth.POST("/login", s.loginPlatform)
		platformAuth.POST("/logout", s.logoutPlatform)
	}

	protected := api.Group("")
	if s.authEnabled || s.platformAuth != nil {
		protected.Use(s.requirePlatformAccess)
	}
	{
		// ===== 账号管理 =====
		protected.GET("/accounts", s.listAccounts)
		protected.POST("/accounts", s.addAccount)
		protected.DELETE("/accounts/:id", s.removeAccount)
		protected.POST("/accounts/:id/password", s.setAppPassword)
		protected.PUT("/accounts/:id/cookies", s.updateCookies)
		protected.POST("/accounts/:id/login/start", s.startAccountLogin)
		protected.POST("/accounts/:id/login/verify", s.verifyAccountLogin)
		protected.GET("/accounts/:id/alias-automation", s.getAliasAutomation)
		protected.PUT("/accounts/:id/alias-automation", s.updateAliasAutomation)
		protected.POST("/accounts/:id/alias-automation/pause", s.pauseAliasAutomation)
		protected.POST("/accounts/:id/alias-automation/resume", s.resumeAliasAutomation)
		protected.POST("/accounts/:id/alias-automation/preview", s.previewAliasAutomation)
		protected.POST("/accounts/:id/alias-automation/run", s.runAliasAutomation)
		protected.GET("/accounts/:id/alias-creation-history", s.listAliasCreationHistory)
		protected.GET("/accounts/:id/alias-creation-history.csv", s.exportAliasCreationHistoryCSV)
		protected.POST("/accounts/:id/aliases/batch", s.createAliasesBatch)

		// ===== 核心接口 1: 创建邮箱 =====
		protected.POST("/create", s.createAlias)

		// ===== 核心接口 2: 读取邮件 =====
		protected.GET("/inbox", s.listInbox)
		protected.GET("/inbox/messages/:id", s.getInboxMessage)

		// ===== 别名管理 =====
		protected.GET("/aliases", s.listAliases)
		protected.POST("/aliases/:id/deactivate", s.deactivateAlias)
		protected.POST("/aliases/:id/reactivate", s.reactivateAlias)
		protected.DELETE("/aliases/:id", s.deleteAlias)

		// ===== 系统 =====
		protected.GET("/health", s.health)
		protected.GET("/logs", s.listOperationLogs)
		protected.GET("/notifications/email", s.getEmailNotification)
		protected.PUT("/notifications/email", s.updateEmailNotification)
		protected.POST("/notifications/email/test", s.testEmailNotification)
		protected.GET("/notifications/webhook", s.getWebhookNotification)
		protected.PUT("/notifications/webhook", s.updateWebhookNotification)
		protected.POST("/notifications/webhook/test", s.testWebhookNotification)
		protected.POST("/reload", s.reloadConfig)
	}

	staticHandler := http.FileServer(http.FS(webui.Assets()))
	s.r.GET("/assets/*filepath", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})
	s.r.NoRoute(func(c *gin.Context) {
		if !shouldServeSPAFallback(c.Request) {
			c.Status(http.StatusNotFound)
			return
		}

		serveEmbeddedIndex(c)
	})
}

func serveEmbeddedIndex(c *gin.Context) {
	c.Header("Cache-Control", "no-cache")
	file, err := webui.Assets().Open("index.html")
	if err != nil {
		c.Data(http.StatusOK, "text/html; charset=utf-8", webui.FallbackIndexHTML())
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.Data(http.StatusOK, "text/html; charset=utf-8", webui.FallbackIndexHTML())
		return
	}
	reader, ok := file.(io.ReadSeeker)
	if !ok {
		c.Data(http.StatusOK, "text/html; charset=utf-8", webui.FallbackIndexHTML())
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), reader)
}

func shouldServeSPAFallback(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}

	requestPath := request.URL.Path
	if requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") || requestPath == "/assets" || strings.HasPrefix(requestPath, "/assets/") {
		return false
	}
	if path.Ext(requestPath) != "" {
		return false
	}

	accept := request.Header.Get("Accept")
	return accept == "" || strings.Contains(strings.ToLower(accept), "text/html")
}

func securityResponseHeaders(c *gin.Context) {
	c.Header(
		"Content-Security-Policy",
		"default-src 'self'; base-uri 'self'; connect-src 'self'; font-src 'self' data: http: https:; form-action 'self'; frame-ancestors 'none'; frame-src 'self'; img-src 'self' data: http: https:; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline' http: https:",
	)
	c.Header("Cross-Origin-Opener-Policy", "same-origin")
	c.Header("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Next()
}

func noStoreAPIResponses(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Next()
}

func limitRequestBody(c *gin.Context) {
	if c.Request.Body == nil {
		c.Next()
		return
	}
	if c.Request.ContentLength > maxRequestBodyBytes {
		fail(c, http.StatusRequestEntityTooLarge, requestBodyTooLargeMessage())
		c.Abort()
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBodyBytes)
	c.Next()
}

func (s *Server) requireAPIToken(c *gin.Context) {
	if s.hasValidAPIToken(c) {
		c.Next()
		return
	}
	s.abortAPIToken(c)
}

// ---- 统一响应 ----

type apiResp struct {
	Success bool        `json:"success"`
	Code    string      `json:"code,omitempty"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, apiResp{Success: true, Data: data})
}

func fail(c *gin.Context, code int, msg string) {
	setOperationLogErrorCode(c, auditlog.ErrorCodeForStatus(code))
	failWithCode(c, code, "", msg)
}

func failWithAuditCode(c *gin.Context, status int, auditCode, message string) {
	setOperationLogErrorCode(c, auditCode)
	failWithCode(c, status, "", message)
}

func requestBodyTooLargeMessage() string {
	return fmt.Sprintf("请求体不能超过 %d 字节", maxRequestBodyBytes)
}

func bindJSON(c *gin.Context, dst any, requiredFields string) bool {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			fail(c, http.StatusRequestEntityTooLarge, requestBodyTooLargeMessage())
			return false
		}
		fail(c, http.StatusBadRequest, "参数错误: "+requiredFields+" — "+err.Error())
		return false
	}
	if err := binding.JSON.BindBody(raw, dst); err != nil {
		fail(c, http.StatusBadRequest, "参数错误: "+requiredFields+" — "+err.Error())
		return false
	}
	return true
}

func parseBoundedInt(raw string, defaultValue, minValue, maxValue int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return 0, fmt.Errorf("必须是 %d 到 %d 之间的整数", minValue, maxValue)
	}
	return value, nil
}

func boundedIntQuery(c *gin.Context, name string, defaultValue, minValue, maxValue int) (int, bool) {
	value, err := parseBoundedInt(c.Query(name), defaultValue, minValue, maxValue)
	if err != nil {
		fail(c, http.StatusBadRequest, "参数错误: "+name+" "+err.Error())
		return 0, false
	}
	return value, true
}

func boolQuery(c *gin.Context, name string, defaultValue bool) (bool, bool) {
	raw, exists := c.GetQuery(name)
	if !exists || strings.TrimSpace(raw) == "" {
		return defaultValue, true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		fail(c, http.StatusBadRequest, "参数错误: "+name+" 必须是 true 或 false")
		return false, false
	}
	return value, true
}

func positiveUint32Query(c *gin.Context, name string) (uint32, bool) {
	raw, exists := c.GetQuery(name)
	if !exists || strings.TrimSpace(raw) == "" {
		return 0, true
	}
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || value == 0 {
		fail(c, http.StatusBadRequest, "参数错误: "+name+" 必须是正整数")
		return 0, false
	}
	return uint32(value), true
}

func failAccountOperation(c *gin.Context, fallbackStatus int, prefix string, err error) {
	if errors.Is(err, account.ErrPersistence) {
		fallbackStatus = http.StatusInternalServerError
	}
	fail(c, fallbackStatus, prefix+err.Error())
}

func (s *Server) persistClientCookies(c *gin.Context, accountID string, cookies map[string]string) bool {
	if err := s.mgr.SaveCookies(accountID, cookies); err != nil {
		fail(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: "+err.Error())
		return false
	}
	return true
}

// ====================================================================
// 核心接口 1: 创建邮箱
//   POST /api/create
//   body: {"account_id": "acc_xxx", "label": "可选标签"}
//   返回: 新创建的 HME 邮箱地址
// ====================================================================

type createReq struct {
	AccountID string `json:"account_id" binding:"required"`
	Label     string `json:"label"`
}

func (s *Server) createAlias(c *gin.Context) {
	var req createReq
	if !bindJSON(c, &req, "account_id 必填") {
		return
	}
	if utf8.RuneCountInString(req.Label) > maxAliasLabelRunes {
		fail(c, http.StatusBadRequest, fmt.Sprintf("参数错误: label 不能超过 %d 个字符", maxAliasLabelRunes))
		return
	}

	var result *hme.CreateResult
	err := s.aliasOperations.withClient(req.AccountID, func(client aliasOperationClient) error {
		var createErr error
		result, createErr = client.CreateAlias(req.Label, 5)
		return createErr
	})
	if errors.Is(err, account.ErrAccountNotFound) {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	created := createdAliasData{AccountID: req.AccountID}
	if result != nil {
		created.CreatedAt = result.CreatedAt
		created.Email = result.Email
		created.Label = result.Label
	}
	status := account.AliasAutomationStatusSuccess
	complete := true
	failed := 0
	if err != nil {
		complete = false
		failed = 1
		status = account.AliasAutomationStatusError
		if result != nil {
			status = account.AliasAutomationStatusPartial
		}
	}
	historyError := ""
	if err != nil {
		historyError = aliasOperationErrorSummary(err)
	}
	historyAliases := make([]createdAliasData, 0, 1)
	if result != nil {
		historyAliases = append(historyAliases, created)
	}
	history, historyErr := s.aliasOperations.recordAliasCreation(req.AccountID, account.AliasCreationHistory{
		Aliases:     aliasCreationHistoryAliases(historyAliases),
		Complete:    complete,
		Created:     len(historyAliases),
		Error:       historyError,
		Failed:      failed,
		LabelPrefix: req.Label,
		Requested:   1,
		Status:      status,
		Trigger:     account.AliasCreationTriggerManual,
	}, time.Now())
	if historyErr != nil {
		failAccountOperation(c, http.StatusInternalServerError, "保存别名创建历史失败: ", historyErr)
		return
	}
	created.BatchID = history.BatchID

	if err != nil {
		if errors.Is(err, account.ErrPersistence) {
			fail(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: "+err.Error())
			return
		}
		// 区分会话失效(需重新登录)与临时失败
		msg := err.Error()
		if isSessionError(msg) {
			s.notifySessionExpired(req.AccountID)
			fail(c, http.StatusUnauthorized, "iCloud 会话失效，请更新 Cookie")
		} else {
			failWithAuditCode(c, http.StatusBadGateway, auditCodeForUpstreamError(err), "创建邮箱失败，请稍后重试")
		}
		return
	}

	ok(c, created)
}

// ====================================================================
// 核心接口 2: 读取邮件
//   GET /api/inbox?account_id=acc_xxx[&alias=xxx@icloud.com][&limit=20][&days=7][&before_uid=123]
//
//   - 不传 alias: 返回该账号收件箱最近邮件
//   - 传 alias:   只返回发给该 HME 别名的邮件
//
//   认证优先级: IMAP (App Password) 优先 > Web API (Cookie) 回退
//   - IMAP: 支持服务端按收件人搜索 (FindByRecipient)
//   - Web API: 仅使用响应中的明确收件人做精确过滤;收件人缺失时显式失败
// ====================================================================

func (s *Server) listInbox(c *gin.Context) {
	accountID := c.Query("account_id")
	if accountID == "" {
		fail(c, http.StatusBadRequest, "参数缺失: account_id")
		return
	}
	alias := strings.TrimSpace(c.Query("alias"))
	limit, valid := boundedIntQuery(c, "limit", defaultInboxLimit, minInboxLimit, maxInboxLimit)
	if !valid {
		return
	}
	days, valid := boundedIntQuery(c, "days", defaultInboxDays, minInboxDays, maxInboxDays)
	if !valid {
		return
	}
	beforeUID, valid := positiveUint32Query(c, "before_uid")
	if !valid {
		return
	}
	includePreview, valid := boolQuery(c, "include_preview", true)
	if !valid {
		return
	}
	firstPreview, valid := boolQuery(c, "first_preview", false)
	if !valid {
		return
	}

	// 优先使用 IMAP (App Password 认证)。成功连接会在短时间内按账号复用。
	var page mail.MessagePage
	var imapOperationErr error
	err := s.withInboxIMAPClient(accountID, func(mc inboxIMAPClient) error {
		if alias != "" {
			if includePreview {
				page, imapOperationErr = mc.FindByRecipientPage(alias, limit, days, beforeUID)
			} else {
				page, imapOperationErr = mc.FindByRecipientSummariesPage(alias, limit, days, beforeUID)
			}
		} else if includePreview {
			page, imapOperationErr = mc.ListInboxPage(limit, days, beforeUID)
		} else {
			page, imapOperationErr = mc.ListInboxSummariesPage(limit, days, beforeUID)
		}
		if imapOperationErr == nil && firstPreview && !includePreview {
			s.loadFirstInboxPreview(accountID, page.Messages, mc)
		}
		return imapOperationErr
	})
	if err == nil {
		ok(c, inboxPageData(accountID, alias, page, "imap"))
		return
	}

	// 回退到 Web API (Cookie 认证，无需 App Password)
	wmc, err := s.mgr.WebMailClient(accountID)
	if err != nil {
		fail(c, http.StatusBadRequest, "无可用邮件客户端: 需要 App Password 或 Cookie")
		return
	}
	if beforeUID > 0 {
		fail(c, http.StatusBadRequest, "当前 Web API 回退不支持继续加载，请配置 App Password 后重试")
		return
	}

	var messages []mail.Message
	if alias != "" {
		messages, err = wmc.FindByAlias(alias, limit)
		if err != nil {
			s.failInboxRead(c, accountID, err)
			return
		}
		ok(c, inboxPageData(accountID, alias, mail.MessagePage{Messages: messages}, "web_api"))
	} else {
		messages, err = wmc.ListInbox(limit)
		if err != nil {
			s.failInboxRead(c, accountID, err)
			return
		}
		ok(c, inboxPageData(accountID, alias, mail.MessagePage{Messages: messages}, "web_api"))
	}
}

func inboxPageData(accountID, alias string, page mail.MessagePage, method string) gin.H {
	nextCursor := ""
	if page.NextBeforeUID > 0 {
		nextCursor = strconv.FormatUint(uint64(page.NextBeforeUID), 10)
	}
	return gin.H{
		"account_id":  accountID,
		"alias":       alias,
		"count":       len(page.Messages),
		"has_more":    nextCursor != "",
		"messages":    page.Messages,
		"method":      method,
		"next_cursor": nextCursor,
	}
}

func (s *Server) withInboxIMAPClient(accountID string, operation func(inboxIMAPClient) error) error {
	return s.inboxIMAPSessions.Use(accountID, func() (inboxIMAPClient, error) {
		if s.newInboxIMAPClient == nil {
			return nil, errors.New("IMAP 客户端工厂不可用")
		}
		return s.newInboxIMAPClient(accountID)
	}, operation)
}

type inboxPreviewLoader interface {
	GetPreview(uid uint32) (*mail.Message, error)
}

// loadFirstInboxPreview reuses the list request's authenticated IMAP session.
// Preview failures are intentionally ignored so a slow or malformed message cannot fail the list.
func (s *Server) loadFirstInboxPreview(accountID string, messages []mail.Message, loader inboxPreviewLoader) {
	if len(messages) == 0 {
		return
	}
	first := &messages[0]
	if cached, found := s.inboxPreviews.Get(accountID, first.ID); found {
		first.Preview = cached.Preview
		return
	}
	uid, err := strconv.ParseUint(first.ID, 10, 32)
	if err != nil || uid == 0 {
		return
	}
	if loader == nil {
		return
	}
	message, err := loader.GetPreview(uint32(uid))
	if err != nil || message == nil {
		return
	}
	*first = *message
	s.inboxPreviews.Set(accountID, *message)
}

// getInboxMessage 按 IMAP UID 读取单封邮件完整正文，供详情视图按需加载。
func (s *Server) getInboxMessage(c *gin.Context) {
	accountID := strings.TrimSpace(c.Query("account_id"))
	if accountID == "" {
		fail(c, http.StatusBadRequest, "参数缺失: account_id")
		return
	}
	uid, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || uid == 0 {
		fail(c, http.StatusBadRequest, "参数错误: id 必须是有效的邮件 UID")
		return
	}
	messageID := strconv.FormatUint(uid, 10)
	if message, found := s.inboxPreviews.GetFull(accountID, messageID); found {
		ok(c, message)
		return
	}

	var message *mail.FullMessage
	var imapOperationErr error
	err = s.withInboxIMAPClient(accountID, func(mc inboxIMAPClient) error {
		message, imapOperationErr = mc.GetFull(uint32(uid))
		if imapOperationErr == nil && message == nil {
			imapOperationErr = errors.New("IMAP 未返回邮件内容")
		}
		return imapOperationErr
	})
	if err != nil {
		var factoryErr *inboxIMAPClientFactoryError
		if errors.As(err, &factoryErr) {
			fail(c, http.StatusBadRequest, "无可用 IMAP 客户端: "+err.Error())
			return
		}
		if strings.Contains(err.Error(), "邮件不存在") {
			fail(c, http.StatusNotFound, "邮件不存在或已被删除")
			return
		}
		failWithAuditCode(c, http.StatusBadGateway, auditCodeForUpstreamError(err), "读取邮件失败，请稍后重试")
		return
	}
	s.inboxPreviews.SetFull(accountID, *message)
	ok(c, message)
}

func (s *Server) failInboxRead(c *gin.Context, accountID string, err error) {
	msg := err.Error()
	if isSessionError(msg) {
		s.notifySessionExpired(accountID)
		fail(c, http.StatusUnauthorized, "iCloud 会话失效，请更新 Cookie")
		return
	}
	failWithAuditCode(c, http.StatusBadGateway, auditCodeForUpstreamError(err), "读取邮件失败，请稍后重试")
}

func auditCodeForUpstreamError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return auditlog.ErrorCodeUpstreamTimeout
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded") {
		return auditlog.ErrorCodeUpstreamTimeout
	}
	return auditlog.ErrorCodeUpstreamRejected
}

// ====================================================================
// 辅助接口
// ====================================================================

func (s *Server) listAccounts(c *gin.Context) {
	ok(c, s.mgr.ListAccounts())
}

type addAccountReq struct {
	Name        string `json:"name" binding:"required"`
	ICloudEmail string `json:"icloud_email"`
	Cookies     string `json:"cookies"` // 可选,后续可通过 /login 获取
	Host        string `json:"host"`
	Proxy       string `json:"proxy"` // HTTP/SOCKS5 代理
}

func (s *Server) addAccount(c *gin.Context) {
	var req addAccountReq
	if !bindJSON(c, &req, "name 必填") {
		return
	}
	acc, err := s.mgr.AddAccount(req.Name, req.ICloudEmail, req.Cookies, req.Host, req.Proxy)
	if err != nil {
		failAccountOperation(c, http.StatusBadRequest, "", err)
		return
	}
	c.JSON(http.StatusCreated, apiResp{Success: true, Data: acc})
}

func (s *Server) removeAccount(c *gin.Context) {
	id := c.Param("id")
	removed, err := s.mgr.RemoveAccount(id)
	if err != nil {
		failAccountOperation(c, http.StatusInternalServerError, "删除账号失败: ", err)
		return
	}
	if !removed {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	s.loginChallenges.invalidateAccount(id)
	s.aliasOperations.invalidateAccount(id)
	s.inboxIMAPSessions.InvalidateAccount(id)
	s.inboxPreviews.InvalidateAccount(id)
	ok(c, gin.H{"id": id})
}

type setPwdReq struct {
	ICloudEmail string `json:"icloud_email" binding:"required"`
	AppPassword string `json:"app_password" binding:"required"`
}

func (s *Server) setAppPassword(c *gin.Context) {
	id := c.Param("id")
	var req setPwdReq
	if !bindJSON(c, &req, "icloud_email, app_password 必填") {
		return
	}
	acc, err := s.mgr.SetAppPassword(id, req.ICloudEmail, req.AppPassword)
	if err != nil {
		failAccountOperation(c, http.StatusBadRequest, "", err)
		return
	}
	s.inboxIMAPSessions.InvalidateAccount(id)
	s.inboxPreviews.InvalidateAccount(id)
	ok(c, acc)
}

type updateCookiesReq struct {
	Cookies map[string]string `json:"cookies" binding:"required"`
}

func (s *Server) updateCookies(c *gin.Context) {
	id := c.Param("id")
	var req updateCookiesReq
	if !bindJSON(c, &req, "cookies 必填") {
		return
	}
	if len(req.Cookies) > account.MaxCookieCount {
		fail(c, http.StatusBadRequest, fmt.Sprintf("参数错误: Cookie 数量不能超过 %d", account.MaxCookieCount))
		return
	}
	var acc account.AccountDTO
	err := s.aliasOperations.withCredentialUpdate(id, func() error {
		var err error
		acc, err = s.mgr.UpdateCookies(id, req.Cookies)
		return err
	})
	if err != nil {
		failAccountOperation(c, http.StatusBadRequest, "", err)
		return
	}
	ok(c, acc)
}

type aliasAutomationReq struct {
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
}

func (s *Server) getAliasAutomation(c *gin.Context) {
	automation, err := s.mgr.GetAliasAutomation(c.Param("id"))
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, "账号不存在")
			return
		}
		failAccountOperation(c, http.StatusInternalServerError, "读取自动化规则失败: ", err)
		return
	}
	ok(c, automation)
}

func (s *Server) updateAliasAutomation(c *gin.Context) {
	var req aliasAutomationReq
	if !bindJSON(c, &req, "自动化规则参数无效") {
		return
	}
	automation, err := s.mgr.SetAliasAutomation(c.Param("id"), account.AliasAutomation{
		Enabled:              req.Enabled,
		IntervalMinutes:      req.IntervalMinutes,
		AllowedWeekdays:      req.AllowedWeekdays,
		ExecutionWindowStart: req.ExecutionWindowStart,
		ExecutionWindowEnd:   req.ExecutionWindowEnd,
		ScheduledBatchSize:   req.ScheduledBatchSize,
		MinimumActive:        req.MinimumActive,
		TargetActive:         req.TargetActive,
		MaxBatchSize:         req.MaxBatchSize,
		MaxTotalAliases:      req.MaxTotalAliases,
		MaxFailureCount:      req.MaxFailureCount,
		DailyCreationLimit:   req.DailyCreationLimit,
		TargetCreated:        req.TargetCreated,
		LabelPrefix:          req.LabelPrefix,
	}, time.Now())
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, "账号不存在")
			return
		}
		failAccountOperation(c, http.StatusBadRequest, "", err)
		return
	}
	s.aliasAutomation.Start()
	ok(c, automation)
}

func (s *Server) pauseAliasAutomation(c *gin.Context) {
	automation, err := s.mgr.PauseAliasAutomation(c.Param("id"), time.Now())
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, "账号不存在")
			return
		}
		failAccountOperation(c, http.StatusBadRequest, "暂停自动化规则失败: ", err)
		return
	}
	s.aliasAutomation.Start()
	ok(c, automation)
}

func (s *Server) resumeAliasAutomation(c *gin.Context) {
	automation, err := s.mgr.ResumeAliasAutomation(c.Param("id"), time.Now())
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, "账号不存在")
			return
		}
		failAccountOperation(c, http.StatusBadRequest, "恢复自动化规则失败: ", err)
		return
	}
	s.aliasAutomation.Start()
	ok(c, automation)
}

func (s *Server) runAliasAutomation(c *gin.Context) {
	result, err := s.aliasAutomation.RunNow(c.Param("id"))
	if errors.Is(err, account.ErrPersistence) {
		failAccountOperation(c, http.StatusInternalServerError, "保存自动化运行状态失败: ", err)
		return
	}
	if err == nil || result.Created > 0 {
		ok(c, result)
		return
	}
	if errors.Is(err, account.ErrAccountNotFound) {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	if errors.Is(err, errAliasAutomationRuleMissing) {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, errAliasAutomationPaused) {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if isSessionError(err.Error()) {
		s.notifySessionExpired(c.Param("id"))
		fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		return
	}
	fail(c, http.StatusBadGateway, "执行自动化规则失败: "+err.Error())
}

func (s *Server) previewAliasAutomation(c *gin.Context) {
	preview, err := s.aliasAutomation.Preview(c.Param("id"))
	if err == nil {
		ok(c, preview)
		return
	}
	if errors.Is(err, account.ErrAccountNotFound) {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	if errors.Is(err, errAliasAutomationRuleMissing) {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if isSessionError(err.Error()) {
		s.notifySessionExpired(c.Param("id"))
		fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		return
	}
	fail(c, http.StatusBadGateway, "预览自动化规则失败: "+err.Error())
}

type createAliasesBatchReq struct {
	Count       int    `json:"count"`
	LabelPrefix string `json:"label_prefix"`
}

func (s *Server) createAliasesBatch(c *gin.Context) {
	var req createAliasesBatchReq
	if !bindJSON(c, &req, "count 必填") {
		return
	}
	if req.Count < account.MinAliasAutomationBatchSize || req.Count > account.MaxAliasAutomationBatchSize {
		fail(c, http.StatusBadRequest, fmt.Sprintf("参数错误: count 必须是 %d 到 %d 之间的整数", account.MinAliasAutomationBatchSize, account.MaxAliasAutomationBatchSize))
		return
	}
	if utf8.RuneCountInString(strings.TrimSpace(req.LabelPrefix)) > account.MaxAliasAutomationLabelPrefixRunes {
		fail(c, http.StatusBadRequest, fmt.Sprintf("参数错误: label_prefix 不能超过 %d 个字符", account.MaxAliasAutomationLabelPrefixRunes))
		return
	}

	batch, err := s.aliasOperations.createBatch(c.Param("id"), req.Count, req.LabelPrefix)
	if errors.Is(err, account.ErrAccountNotFound) {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	status := account.AliasAutomationStatusSuccess
	if err != nil {
		status = account.AliasAutomationStatusError
		if batch.Created > 0 {
			status = account.AliasAutomationStatusPartial
		}
	}
	history, historyErr := s.aliasOperations.recordAliasCreation(c.Param("id"), account.AliasCreationHistory{
		Aliases:     aliasCreationHistoryAliases(batch.Aliases),
		Complete:    batch.Complete,
		Created:     batch.Created,
		Error:       batch.Error,
		Failed:      batch.Failed,
		LabelPrefix: req.LabelPrefix,
		Requested:   batch.Requested,
		Status:      status,
		Trigger:     account.AliasCreationTriggerBatch,
	}, time.Now())
	if historyErr != nil {
		failAccountOperation(c, http.StatusInternalServerError, "保存别名创建历史失败: ", historyErr)
		return
	}
	batch.BatchID = history.BatchID
	applyAliasCreationBatchID(batch.Aliases, history.BatchID)
	if errors.Is(err, account.ErrPersistence) {
		failAccountOperation(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: ", err)
		return
	}
	if err == nil || batch.Created > 0 {
		ok(c, batch)
		return
	}
	if isSessionError(err.Error()) {
		s.notifySessionExpired(c.Param("id"))
		fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		return
	}
	fail(c, http.StatusBadGateway, "批量创建邮箱失败: "+err.Error())
}

type aliasCreationHistoryData struct {
	AccountID string                         `json:"account_id"`
	Count     int                            `json:"count"`
	Entries   []account.AliasCreationHistory `json:"entries"`
}

func (s *Server) listAliasCreationHistory(c *gin.Context) {
	limit, valid := boundedIntQuery(c, "limit", defaultAliasHistoryLimit, 1, account.MaxAliasCreationHistory)
	if !valid {
		return
	}
	entries, err := s.mgr.ListAliasCreationHistory(c.Param("id"), limit)
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, "账号不存在")
			return
		}
		failAccountOperation(c, http.StatusInternalServerError, "读取别名创建历史失败: ", err)
		return
	}
	ok(c, aliasCreationHistoryData{
		AccountID: c.Param("id"),
		Count:     len(entries),
		Entries:   entries,
	})
}

func (s *Server) exportAliasCreationHistoryCSV(c *gin.Context) {
	entries, err := s.mgr.ListAliasCreationHistory(c.Param("id"), account.MaxAliasCreationHistory)
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, "账号不存在")
			return
		}
		failAccountOperation(c, http.StatusInternalServerError, "导出别名创建历史失败: ", err)
		return
	}

	c.Header("Content-Disposition", `attachment; filename="alias-creation-history.csv"`)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write([]string{
		"batch_id",
		"created_at",
		"trigger",
		"status",
		"requested",
		"created",
		"failed",
		"complete",
		"label_prefix",
		"error",
		"email",
		"alias_label",
		"alias_created_at",
	})
	for _, entry := range entries {
		if len(entry.Aliases) == 0 {
			_ = writer.Write(aliasCreationHistoryCSVRow(entry, account.AliasCreationHistoryAlias{}))
			continue
		}
		for _, alias := range entry.Aliases {
			_ = writer.Write(aliasCreationHistoryCSVRow(entry, alias))
		}
	}
	writer.Flush()
}

func aliasCreationHistoryCSVRow(entry account.AliasCreationHistory, alias account.AliasCreationHistoryAlias) []string {
	return []string{
		entry.BatchID,
		entry.CreatedAt,
		entry.Trigger,
		entry.Status,
		strconv.Itoa(entry.Requested),
		strconv.Itoa(entry.Created),
		strconv.Itoa(entry.Failed),
		strconv.FormatBool(entry.Complete),
		entry.LabelPrefix,
		entry.Error,
		alias.Email,
		alias.Label,
		alias.CreatedAt,
	}
}

func (s *Server) listAliases(c *gin.Context) {
	accountID := c.Query("account_id")
	if accountID == "" {
		fail(c, http.StatusBadRequest, "参数缺失: account_id")
		return
	}
	var aliases []hme.Alias
	err := s.aliasOperations.withClient(accountID, func(client aliasOperationClient) error {
		var listErr error
		aliases, listErr = client.ListAliases()
		return listErr
	})
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, account.ErrPersistence) {
			failAccountOperation(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: ", err)
			return
		}
		if isSessionError(err.Error()) {
			s.notifySessionExpired(accountID)
			fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		} else {
			fail(c, http.StatusBadGateway, err.Error())
		}
		return
	}
	history, historyErr := s.mgr.ListAliasCreationHistory(accountID, account.MaxAliasCreationHistory)
	if historyErr == nil {
		applyAliasCreationHistory(aliases, history)
		hme.SortAliasesByCreatedAt(aliases)
	}
	ok(c, gin.H{
		"account_id": accountID,
		"count":      len(aliases),
		"aliases":    aliases,
	})
}

type aliasActionReq struct {
	AccountID string `json:"account_id" binding:"required"`
}

func (s *Server) deactivateAlias(c *gin.Context) {
	anonymousID := c.Param("id")
	var req aliasActionReq
	if !bindJSON(c, &req, "account_id 必填") {
		return
	}

	var success bool
	err := s.aliasOperations.withClient(req.AccountID, func(client aliasOperationClient) error {
		var actionErr error
		success, actionErr = client.DeactivateHME(anonymousID)
		return actionErr
	})
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, account.ErrPersistence) {
			failAccountOperation(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: ", err)
			return
		}
		fail(c, http.StatusBadGateway, "停用失败: "+err.Error())
		return
	}
	ok(c, gin.H{"anonymous_id": anonymousID, "success": success})
}

func (s *Server) reactivateAlias(c *gin.Context) {
	anonymousID := c.Param("id")
	var req aliasActionReq
	if !bindJSON(c, &req, "account_id 必填") {
		return
	}

	var success bool
	err := s.aliasOperations.withClient(req.AccountID, func(client aliasOperationClient) error {
		var actionErr error
		success, actionErr = client.ReactivateHME(anonymousID)
		return actionErr
	})
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, account.ErrPersistence) {
			failAccountOperation(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: ", err)
			return
		}
		fail(c, http.StatusBadGateway, "激活失败: "+err.Error())
		return
	}
	ok(c, gin.H{"anonymous_id": anonymousID, "success": success})
}

func (s *Server) deleteAlias(c *gin.Context) {
	anonymousID := c.Param("id")
	var req aliasActionReq
	if !bindJSON(c, &req, "account_id 必填") {
		return
	}

	err := s.aliasOperations.withClient(req.AccountID, func(client aliasOperationClient) error {
		return client.Delete(anonymousID)
	})
	if err != nil {
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, account.ErrPersistence) {
			failAccountOperation(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: ", err)
			return
		}
		fail(c, http.StatusBadGateway, "删除失败: "+err.Error())
		return
	}
	ok(c, gin.H{"anonymous_id": anonymousID})
}

// isSessionError 判断错误是否由会话失效引起。
func isSessionError(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "401") || strings.Contains(m, "403") ||
		strings.Contains(m, "session") || strings.Contains(m, "cookie") ||
		strings.Contains(m, "unauthorized") || strings.Contains(m, "认证") ||
		strings.Contains(m, "会话校验失败")
}

// reloadConfig 重新加载 accounts.json 配置文件。
func (s *Server) reloadConfig(c *gin.Context) {
	if err := s.mgr.Reload(); err != nil {
		fail(c, http.StatusInternalServerError, "重新加载配置失败: "+err.Error())
		return
	}
	s.aliasOperations.invalidateAll()
	s.loginChallenges.invalidateAll()
	s.inboxIMAPSessions.Clear()
	s.inboxPreviews.Clear()
	s.aliasAutomation.Start()
	ok(c, gin.H{"message": "配置已重新加载"})
}
