package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
)

const (
	loginChallengeTTL         = 5 * time.Minute
	maxLoginChallengeIDLength = 128
)

type startPasswordLoginFunc func(id, password string) (account.AccountDTO, *account.PasswordLoginSession, error)
type verifyPasswordLoginFunc func(session *account.PasswordLoginSession, otp string) (account.AccountDTO, error)

type loginChallengeEntry struct {
	accountID string
	session   *account.PasswordLoginSession
	expiresAt time.Time
}

type loginChallengeStore struct {
	mu      sync.Mutex
	entries map[string]loginChallengeEntry
	now     func() time.Time
	newID   func() (string, error)
}

func newLoginChallengeStore() *loginChallengeStore {
	return &loginChallengeStore{
		entries: make(map[string]loginChallengeEntry),
		now:     time.Now,
		newID:   newLoginChallengeID,
	}
}

func newLoginChallengeID() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成登录 challenge: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *loginChallengeStore) put(accountID string, session *account.PasswordLoginSession) (string, time.Time, error) {
	id, err := s.newID()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	expiresAt := now.Add(loginChallengeTTL)

	s.mu.Lock()
	toClose := s.removeLocked(func(key string, entry loginChallengeEntry) bool {
		return !now.Before(entry.expiresAt) || entry.accountID == accountID || key == id
	})
	s.entries[id] = loginChallengeEntry{accountID: accountID, session: session, expiresAt: expiresAt}
	s.mu.Unlock()
	closeLoginSessions(toClose)
	return id, expiresAt, nil
}

func (s *loginChallengeStore) take(accountID, id string) *account.PasswordLoginSession {
	now := s.now()
	s.mu.Lock()
	expired := s.removeLocked(func(_ string, entry loginChallengeEntry) bool {
		return !now.Before(entry.expiresAt)
	})
	entry, ok := s.entries[id]
	if ok && entry.accountID == accountID {
		delete(s.entries, id)
	} else {
		ok = false
	}
	s.mu.Unlock()
	closeLoginSessions(expired)
	if !ok {
		return nil
	}
	return entry.session
}

func (s *loginChallengeStore) invalidateAccount(accountID string) {
	s.mu.Lock()
	removed := s.removeLocked(func(_ string, entry loginChallengeEntry) bool {
		return entry.accountID == accountID
	})
	s.mu.Unlock()
	closeLoginSessions(removed)
}

func (s *loginChallengeStore) invalidateAll() {
	s.mu.Lock()
	removed := s.removeLocked(func(string, loginChallengeEntry) bool { return true })
	s.mu.Unlock()
	closeLoginSessions(removed)
}

func (s *loginChallengeStore) removeLocked(remove func(string, loginChallengeEntry) bool) []*account.PasswordLoginSession {
	var sessions []*account.PasswordLoginSession
	for id, entry := range s.entries {
		if remove(id, entry) {
			delete(s.entries, id)
			sessions = append(sessions, entry.session)
		}
	}
	return sessions
}

func closeLoginSessions(sessions []*account.PasswordLoginSession) {
	for _, session := range sessions {
		session.Close()
	}
}

type loginStartReq struct {
	Password string `json:"password" binding:"required"`
}

type loginVerifyReq struct {
	ChallengeID string `json:"challenge_id" binding:"required"`
	OTPCode     string `json:"otp_code" binding:"required"`
}

func (s *Server) startAccountLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req loginStartReq
	if !bindJSON(c, &req, "password 必填") {
		return
	}

	accountID := c.Param("id")
	s.loginChallenges.invalidateAccount(accountID)
	dto, session, err := s.startPasswordLogin(accountID, req.Password)
	if err != nil {
		failPasswordLogin(c, "登录失败: ", err)
		return
	}
	if session == nil {
		s.loginChallenges.invalidateAccount(accountID)
		ok(c, dto)
		return
	}

	challengeID, _, err := s.loginChallenges.put(accountID, session)
	if err != nil {
		session.Close()
		fail(c, http.StatusInternalServerError, "创建登录 challenge 失败")
		return
	}
	ok(c, gin.H{
		"status":       "otp_required",
		"challenge_id": challengeID,
		"expires_in":   int(loginChallengeTTL / time.Second),
	})
}

func (s *Server) verifyAccountLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req loginVerifyReq
	if !bindJSON(c, &req, "challenge_id, otp_code 必填") {
		return
	}
	req.ChallengeID = strings.TrimSpace(req.ChallengeID)
	req.OTPCode = strings.TrimSpace(req.OTPCode)
	if req.ChallengeID == "" || len(req.ChallengeID) > maxLoginChallengeIDLength {
		fail(c, http.StatusBadRequest, "参数错误: challenge_id 格式无效")
		return
	}
	if !isSixDigitOTP(req.OTPCode) {
		fail(c, http.StatusBadRequest, "参数错误: otp_code 必须是 6 位数字")
		return
	}

	session := s.loginChallenges.take(c.Param("id"), req.ChallengeID)
	if session == nil {
		fail(c, http.StatusGone, "登录 challenge 无效或已过期，请重新提交密码")
		return
	}
	dto, err := s.verifyPasswordLogin(session, req.OTPCode)
	if err != nil {
		failPasswordLogin(c, "验证码验证失败: ", err)
		return
	}
	ok(c, dto)
}

func isSixDigitOTP(code string) bool {
	if len(code) != 6 {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

func failPasswordLogin(c *gin.Context, prefix string, err error) {
	switch {
	case errors.Is(err, account.ErrPersistence):
		fail(c, http.StatusInternalServerError, prefix+err.Error())
	case errors.Is(err, account.ErrAccountNotFound):
		fail(c, http.StatusNotFound, err.Error())
	case errors.Is(err, account.ErrLoginEmailMissing):
		fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, account.ErrLoginSessionInvalid):
		fail(c, http.StatusGone, "登录 challenge 无效或已过期，请重新提交密码")
	case errors.Is(err, hme.ErrLoginChallengeInvalid):
		fail(c, http.StatusGone, "登录 challenge 无效或已过期，请重新提交密码")
	case errors.Is(err, hme.ErrInvalidCredentials), errors.Is(err, hme.ErrInvalidOTP):
		fail(c, http.StatusUnauthorized, prefix+err.Error())
	case errors.Is(err, hme.ErrPrivacyTermsRequired):
		fail(c, http.StatusForbidden, err.Error())
	default:
		fail(c, http.StatusBadGateway, prefix+err.Error())
	}
}
