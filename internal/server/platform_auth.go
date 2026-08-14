package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/auditlog"
	"icloud-hme/internal/platformauth"
)

const (
	platformAuthInvalidCode       = "platform_auth_invalid"
	platformAuthRequiredCode      = "platform_auth_required"
	platformAuthSetupRequiredCode = "platform_auth_setup_required"
	platformSessionCookieName     = "icloud_hme_platform_session"
	platformSessionDuration       = 12 * time.Hour
	maxPlatformSessions           = 256
)

type platformAuthCredentials struct {
	Password string `json:"password" binding:"required"`
	Username string `json:"username" binding:"required"`
}

type platformAuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Configured    bool   `json:"configured"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	Username      string `json:"username,omitempty"`
}

type platformSession struct {
	expiresAt time.Time
	username  string
}

type platformSessionStore struct {
	mu       sync.Mutex
	now      func() time.Time
	sessions map[[sha256.Size]byte]platformSession
}

func newPlatformSessionStore() *platformSessionStore {
	return &platformSessionStore{
		now:      time.Now,
		sessions: make(map[[sha256.Size]byte]platformSession),
	}
}

func (s *platformSessionStore) Create(username string) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, errors.New("platform session store is unavailable")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := s.now().UTC().Add(platformSessionDuration)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(s.now().UTC())
	if len(s.sessions) >= maxPlatformSessions {
		s.removeOldestLocked()
	}
	s.sessions[hashPlatformSession(token)] = platformSession{
		expiresAt: expiresAt,
		username:  username,
	}
	return token, expiresAt, nil
}

func (s *platformSessionStore) Lookup(token string) (string, time.Time, bool) {
	if s == nil || token == "" {
		return "", time.Time{}, false
	}
	now := s.now().UTC()
	key := hashPlatformSession(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	session, ok := s.sessions[key]
	if !ok {
		return "", time.Time{}, false
	}
	return session.username, session.expiresAt, true
}

func (s *platformSessionStore) Delete(token string) {
	if s == nil || token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, hashPlatformSession(token))
}

func (s *platformSessionStore) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.sessions)
}

func (s *platformSessionStore) cleanupLocked(now time.Time) {
	for key, session := range s.sessions {
		if !session.expiresAt.After(now) {
			delete(s.sessions, key)
		}
	}
}

func (s *platformSessionStore) removeOldestLocked() {
	var oldestKey [sha256.Size]byte
	var oldestExpiry time.Time
	for key, session := range s.sessions {
		if oldestExpiry.IsZero() || session.expiresAt.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = session.expiresAt
		}
	}
	if !oldestExpiry.IsZero() {
		delete(s.sessions, oldestKey)
	}
}

func hashPlatformSession(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

func (s *Server) platformAuthSession(c *gin.Context) {
	if s.platformAuth == nil {
		ok(c, platformAuthStatus{Authenticated: true, Configured: true})
		return
	}
	if !s.platformAuth.Configured() {
		ok(c, platformAuthStatus{})
		return
	}

	username, expiresAt, authenticated := s.currentPlatformSession(c.Request)
	status := platformAuthStatus{Authenticated: authenticated, Configured: true}
	if authenticated {
		status.ExpiresAt = expiresAt.Format(time.RFC3339)
		status.Username = username
	}
	ok(c, status)
}

func (s *Server) setupPlatformAuth(c *gin.Context) {
	if s.platformAuth == nil {
		fail(c, http.StatusNotFound, "当前服务未启用平台登录认证")
		return
	}
	if s.platformAuth.Configured() {
		failWithCode(c, http.StatusConflict, platformAuthRequiredCode, "管理员账户已初始化，请直接登录")
		return
	}
	if s.authEnabled && !s.hasValidAPIToken(c) {
		s.abortAPIToken(c)
		return
	}

	var credentials platformAuthCredentials
	if !bindJSON(c, &credentials, "username 和 password 必填") {
		return
	}
	if err := s.platformAuth.Setup(credentials.Username, credentials.Password); err != nil {
		if errors.Is(err, platformauth.ErrAlreadyConfigured) {
			failWithCode(c, http.StatusConflict, platformAuthRequiredCode, "管理员账户已初始化，请直接登录")
			return
		}
		fail(c, http.StatusBadRequest, "管理员账户初始化失败："+err.Error())
		return
	}
	s.issuePlatformSession(c, s.platformAuth.Username())
}

func (s *Server) loginPlatform(c *gin.Context) {
	if s.platformAuth == nil {
		fail(c, http.StatusNotFound, "当前服务未启用平台登录认证")
		return
	}
	if !s.platformAuth.Configured() {
		failWithCode(c, http.StatusConflict, platformAuthSetupRequiredCode, "请先初始化管理员账户")
		return
	}

	var credentials platformAuthCredentials
	if !bindJSON(c, &credentials, "username 和 password 必填") {
		return
	}
	if !s.platformAuth.Authenticate(credentials.Username, credentials.Password) {
		failWithCode(c, http.StatusUnauthorized, platformAuthInvalidCode, "用户名或密码错误")
		return
	}
	s.issuePlatformSession(c, s.platformAuth.Username())
}

func (s *Server) logoutPlatform(c *gin.Context) {
	if token, ok := platformSessionToken(c.Request); ok && s.platformSessions != nil {
		s.platformSessions.Delete(token)
	}
	s.clearPlatformSessionCookie(c)
	ok(c, platformAuthStatus{Configured: s.platformAuth != nil && s.platformAuth.Configured()})
}

func (s *Server) issuePlatformSession(c *gin.Context, username string) {
	if s.platformSessions == nil {
		fail(c, http.StatusServiceUnavailable, "登录会话暂不可用")
		return
	}
	token, expiresAt, err := s.platformSessions.Create(username)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建登录会话失败")
		return
	}
	http.SetCookie(c.Writer, platformSessionCookie(c.Request, token, expiresAt, int(platformSessionDuration.Seconds())))
	ok(c, platformAuthStatus{
		Authenticated: true,
		Configured:    true,
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		Username:      username,
	})
}

func (s *Server) currentPlatformSession(request *http.Request) (string, time.Time, bool) {
	if s.platformSessions == nil {
		return "", time.Time{}, false
	}
	token, ok := platformSessionToken(request)
	if !ok {
		return "", time.Time{}, false
	}
	return s.platformSessions.Lookup(token)
}

func platformSessionToken(request *http.Request) (string, bool) {
	cookie, err := request.Cookie(platformSessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

func (s *Server) clearPlatformSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, platformSessionCookie(c.Request, "", time.Unix(0, 0).UTC(), -1))
}

func platformSessionCookie(request *http.Request, value string, expiresAt time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{
		Expires:  expiresAt,
		HttpOnly: true,
		MaxAge:   maxAge,
		Name:     platformSessionCookieName,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		Secure:   requestUsesHTTPS(request),
		Value:    value,
	}
}

func requestUsesHTTPS(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	for _, value := range strings.Split(request.Header.Get("X-Forwarded-Proto"), ",") {
		if strings.EqualFold(strings.TrimSpace(value), "https") {
			return true
		}
	}
	return false
}

func (s *Server) requirePlatformAccess(c *gin.Context) {
	if s.hasValidAPIToken(c) {
		c.Next()
		return
	}
	if s.platformAuth != nil {
		if s.authEnabled && strings.TrimSpace(c.GetHeader("Authorization")) != "" {
			s.abortAPIToken(c)
			return
		}
		if !s.platformAuth.Configured() {
			c.AbortWithStatusJSON(http.StatusUnauthorized, apiResp{
				Success: false,
				Code:    platformAuthSetupRequiredCode,
				Message: "请先初始化管理员账户",
			})
			return
		}
		if _, _, authenticated := s.currentPlatformSession(c.Request); authenticated {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, apiResp{
			Success: false,
			Code:    platformAuthRequiredCode,
			Message: "请先登录后再继续操作",
		})
		return
	}
	if s.authEnabled {
		s.abortAPIToken(c)
		return
	}
	c.Next()
}

func (s *Server) hasValidAPIToken(c *gin.Context) bool {
	if !s.authEnabled {
		return false
	}
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	providedHash := sha256.Sum256([]byte(parts[1]))
	return subtle.ConstantTimeCompare(providedHash[:], s.apiTokenHash[:]) == 1
}

func (s *Server) abortAPIToken(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusUnauthorized, apiResp{
		Success: false,
		Code:    apiTokenInvalidCode,
		Message: "API 访问令牌无效或缺失",
	})
}

func failWithCode(c *gin.Context, status int, code, message string) {
	if _, found := c.Get(auditErrorCodeContextKey); !found {
		setOperationLogErrorCode(c, auditlog.ErrorCodeForStatus(status))
	}
	c.JSON(status, apiResp{Success: false, Code: code, Message: message})
}
