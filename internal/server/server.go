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
	"crypto/sha256"
	"crypto/subtle"
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
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
	"icloud-hme/internal/webui"
)

const (
	maxRequestBodyBytes   int64 = 1 << 20
	minInboxLimit               = 1
	maxInboxLimit               = 100
	defaultInboxLimit           = 20
	minInboxDays                = 1
	maxInboxDays                = 365
	defaultInboxDays            = 7
	maxAliasLabelRunes          = 200
	defaultServiceVersion       = "dev"
	apiTokenInvalidCode         = "api_token_invalid"
)

// Server 封装 Gin 引擎和账号管理器。
type Server struct {
	mgr                 *account.Manager
	r                   *gin.Engine
	authEnabled         bool
	apiTokenHash        [sha256.Size]byte
	version             string
	loginChallenges     *loginChallengeStore
	startPasswordLogin  startPasswordLoginFunc
	verifyPasswordLogin verifyPasswordLoginFunc
	aliasOperations     *aliasOperationService
	aliasAutomation     *aliasAutomationService
}

// New 创建 Server。apiToken 非空时,所有 /api 路由都需要 Bearer Token。
func New(mgr *account.Manager, debug bool, apiToken string) *Server {
	return NewWithVersion(mgr, debug, apiToken, defaultServiceVersion)
}

// NewWithVersion 创建带显式构建版本的 Server。
func NewWithVersion(mgr *account.Manager, debug bool, apiToken, version string) *Server {
	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = defaultServiceVersion
	}
	s := &Server{
		mgr:             mgr,
		authEnabled:     apiToken != "",
		version:         version,
		loginChallenges: newLoginChallengeStore(),
	}
	s.startPasswordLogin = mgr.StartPasswordLogin
	s.verifyPasswordLogin = func(session *account.PasswordLoginSession, otp string) (account.AccountDTO, error) {
		return session.Verify(otp)
	}
	s.aliasOperations = newAliasOperationService(mgr)
	s.aliasAutomation = newAliasAutomationService(mgr, s.aliasOperations)
	if s.authEnabled {
		s.apiTokenHash = sha256.Sum256([]byte(apiToken))
	}
	s.r = gin.Default() // 自带 Logger + Recovery 中间件
	s.r.Use(securityResponseHeaders)
	s.register()
	s.aliasAutomation.Start()
	return s
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
}

func (s *Server) register() {
	api := s.r.Group("/api")
	api.Use(noStoreAPIResponses)
	if s.authEnabled {
		api.Use(s.requireAPIToken)
	}
	api.Use(limitRequestBody)
	{
		// ===== 账号管理 =====
		api.GET("/accounts", s.listAccounts)
		api.POST("/accounts", s.addAccount)
		api.DELETE("/accounts/:id", s.removeAccount)
		api.POST("/accounts/:id/password", s.setAppPassword)
		api.PUT("/accounts/:id/cookies", s.updateCookies)
		api.POST("/accounts/:id/login/start", s.startAccountLogin)
		api.POST("/accounts/:id/login/verify", s.verifyAccountLogin)
		api.GET("/accounts/:id/alias-automation", s.getAliasAutomation)
		api.PUT("/accounts/:id/alias-automation", s.updateAliasAutomation)
		api.POST("/accounts/:id/alias-automation/run", s.runAliasAutomation)
		api.POST("/accounts/:id/aliases/batch", s.createAliasesBatch)

		// ===== 核心接口 1: 创建邮箱 =====
		api.POST("/create", s.createAlias)

		// ===== 核心接口 2: 读取邮件 =====
		api.GET("/inbox", s.listInbox)

		// ===== 别名管理 =====
		api.GET("/aliases", s.listAliases)
		api.POST("/aliases/:id/deactivate", s.deactivateAlias)
		api.POST("/aliases/:id/reactivate", s.reactivateAlias)
		api.DELETE("/aliases/:id", s.deleteAlias)

		// ===== 系统 =====
		api.GET("/health", s.health)
		api.POST("/reload", s.reloadConfig)
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
		"default-src 'self'; base-uri 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'",
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
	parts := strings.Fields(c.GetHeader("Authorization"))
	valid := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer")
	if valid {
		providedHash := sha256.Sum256([]byte(parts[1]))
		valid = subtle.ConstantTimeCompare(providedHash[:], s.apiTokenHash[:]) == 1
	}
	if !valid {
		c.Header("WWW-Authenticate", "Bearer")
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusUnauthorized, apiResp{
			Success: false,
			Code:    apiTokenInvalidCode,
			Message: "API 访问令牌无效或缺失",
		})
		return
	}
	c.Next()
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
	c.JSON(code, apiResp{Success: false, Message: msg})
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

	if err != nil {
		if errors.Is(err, account.ErrPersistence) {
			fail(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: "+err.Error())
			return
		}
		if errors.Is(err, account.ErrAccountNotFound) {
			fail(c, http.StatusNotFound, err.Error())
			return
		}
		// 区分会话失效(需重新登录)与临时失败
		msg := err.Error()
		if isSessionError(msg) {
			fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+msg)
		} else {
			fail(c, http.StatusBadGateway, "创建邮箱失败: "+msg)
		}
		return
	}

	ok(c, createdAliasData{
		AccountID: req.AccountID,
		CreatedAt: result.CreatedAt,
		Email:     result.Email,
		Label:     result.Label,
	})
}

// ====================================================================
// 核心接口 2: 读取邮件
//   GET /api/inbox?account_id=acc_xxx[&alias=xxx@icloud.com][&limit=20][&days=7]
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

	// 优先使用 IMAP (App Password 认证)
	mc, err := s.mgr.MailClient(accountID)
	if err == nil {
		if connErr := mc.Connect(); connErr == nil {
			defer mc.Disconnect()
			var messages []mail.Message
			if alias != "" {
				messages, err = mc.FindByRecipient(alias, limit, days)
			} else {
				messages, err = mc.ListInbox(limit, days)
			}
			if err == nil {
				ok(c, gin.H{
					"account_id": accountID,
					"alias":      alias,
					"count":      len(messages),
					"messages":   messages,
					"method":     "imap",
				})
				return
			}
			// IMAP 失败，继续尝试 Web API
		}
	}

	// 回退到 Web API (Cookie 认证，无需 App Password)
	wmc, err := s.mgr.WebMailClient(accountID)
	if err != nil {
		fail(c, http.StatusBadRequest, "无可用邮件客户端: 需要 App Password 或 Cookie")
		return
	}

	if alias != "" {
		messages, err := wmc.FindByAlias(alias, limit)
		if err != nil {
			failInboxRead(c, err)
			return
		}
		ok(c, gin.H{
			"account_id": accountID,
			"alias":      alias,
			"count":      len(messages),
			"messages":   messages,
			"method":     "web_api",
		})
	} else {
		messages, err := wmc.ListInbox(limit)
		if err != nil {
			failInboxRead(c, err)
			return
		}
		ok(c, gin.H{
			"account_id": accountID,
			"count":      len(messages),
			"messages":   messages,
			"method":     "web_api",
		})
	}
}

func failInboxRead(c *gin.Context, err error) {
	msg := err.Error()
	if isSessionError(msg) {
		fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+msg)
		return
	}
	fail(c, http.StatusBadGateway, "读取邮件失败: "+msg)
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
	acc, err := s.mgr.UpdateCookies(id, req.Cookies)
	if err != nil {
		failAccountOperation(c, http.StatusBadRequest, "", err)
		return
	}
	ok(c, acc)
}

type aliasAutomationReq struct {
	Enabled            bool   `json:"enabled"`
	IntervalMinutes    int    `json:"interval_minutes"`
	ScheduledBatchSize int    `json:"scheduled_batch_size"`
	MinimumActive      int    `json:"minimum_active"`
	TargetActive       int    `json:"target_active"`
	MaxBatchSize       int    `json:"max_batch_size"`
	LabelPrefix        string `json:"label_prefix"`
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
		Enabled:            req.Enabled,
		IntervalMinutes:    req.IntervalMinutes,
		ScheduledBatchSize: req.ScheduledBatchSize,
		MinimumActive:      req.MinimumActive,
		TargetActive:       req.TargetActive,
		MaxBatchSize:       req.MaxBatchSize,
		LabelPrefix:        req.LabelPrefix,
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

func (s *Server) runAliasAutomation(c *gin.Context) {
	result, err := s.aliasAutomation.RunNow(c.Param("id"))
	if err == nil || result.Created > 0 {
		ok(c, result)
		return
	}
	if errors.Is(err, account.ErrAccountNotFound) {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	if errors.Is(err, account.ErrPersistence) {
		failAccountOperation(c, http.StatusInternalServerError, "保存自动化运行状态失败: ", err)
		return
	}
	if errors.Is(err, errAliasAutomationRuleMissing) {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if isSessionError(err.Error()) {
		fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		return
	}
	fail(c, http.StatusBadGateway, "执行自动化规则失败: "+err.Error())
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
	if err == nil || batch.Created > 0 {
		ok(c, batch)
		return
	}
	if errors.Is(err, account.ErrAccountNotFound) {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	if errors.Is(err, account.ErrPersistence) {
		failAccountOperation(c, http.StatusInternalServerError, "保存刷新后的 Cookie 失败: ", err)
		return
	}
	if isSessionError(err.Error()) {
		fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		return
	}
	fail(c, http.StatusBadGateway, "批量创建邮箱失败: "+err.Error())
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
			fail(c, http.StatusUnauthorized, "iCloud 会话失效,请更新 Cookie: "+err.Error())
		} else {
			fail(c, http.StatusBadGateway, err.Error())
		}
		return
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
	s.loginChallenges.invalidateAll()
	s.aliasAutomation.Start()
	ok(c, gin.H{"message": "配置已重新加载"})
}
