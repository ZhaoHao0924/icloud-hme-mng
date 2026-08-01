package server

import (
	"fmt"
	"strings"
	"sync"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
)

type aliasOperationClient interface {
	CreateAlias(label string, maxRetries int) (*hme.CreateResult, error)
	DeactivateHME(anonymousID string) (bool, error)
	Delete(anonymousID string) error
	ListAliases() ([]hme.Alias, error)
	ReactivateHME(anonymousID string) (bool, error)
	SessionCookies() map[string]string
}

type aliasOperationClientFactory func(accountID string) (aliasOperationClient, error)

type accountOperationLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *accountOperationLocks) lockFor(accountID string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = make(map[string]*sync.Mutex)
	}
	lock := l.locks[accountID]
	if lock == nil {
		lock = &sync.Mutex{}
		l.locks[accountID] = lock
	}
	return lock
}

// aliasOperationService serializes all HME operations for the same account.
// A HME client carries mutable session cookies, so concurrent client operations
// could otherwise persist stale cookies over a fresher session.
type aliasOperationService struct {
	mgr       *account.Manager
	locks     accountOperationLocks
	newClient aliasOperationClientFactory
}

func newAliasOperationService(mgr *account.Manager) *aliasOperationService {
	service := &aliasOperationService{mgr: mgr}
	service.newClient = func(accountID string) (aliasOperationClient, error) {
		return mgr.HMEClient(accountID, false)
	}
	return service
}

func (s *aliasOperationService) withClient(accountID string, operation func(aliasOperationClient) error) error {
	lock := s.locks.lockFor(accountID)
	lock.Lock()
	defer lock.Unlock()

	client, err := s.newClient(accountID)
	if err != nil {
		return err
	}
	operationErr := operation(client)
	if err := s.mgr.SaveCookies(accountID, client.SessionCookies()); err != nil {
		return fmt.Errorf("保存刷新后的 Cookie 失败: %w", err)
	}
	return operationErr
}

type createdAliasData struct {
	AccountID string `json:"account_id"`
	CreatedAt string `json:"created_at"`
	Email     string `json:"email"`
	Label     string `json:"label"`
}

type aliasBatchResult struct {
	AccountID string             `json:"account_id"`
	Aliases   []createdAliasData `json:"aliases"`
	Complete  bool               `json:"complete"`
	Created   int                `json:"created"`
	Error     string             `json:"error,omitempty"`
	Failed    int                `json:"failed"`
	Requested int                `json:"requested"`
}

func newAliasBatchResult(accountID string, count int) aliasBatchResult {
	return aliasBatchResult{
		AccountID: accountID,
		Aliases:   make([]createdAliasData, 0, count),
		Complete:  true,
		Requested: count,
	}
}

func (s *aliasOperationService) createBatch(accountID string, count int, labelPrefix string) (aliasBatchResult, error) {
	batch := newAliasBatchResult(accountID, count)
	labelPrefix = strings.TrimSpace(labelPrefix)
	err := s.withClient(accountID, func(client aliasOperationClient) error {
		for index := 0; index < count; index++ {
			result, err := client.CreateAlias(aliasBatchLabel(labelPrefix, index, count), 5)
			if err != nil {
				batch.Complete = false
				batch.Failed = 1
				batch.Error = aliasOperationErrorSummary(err)
				return err
			}
			batch.Created++
			batch.Aliases = append(batch.Aliases, createdAliasData{
				AccountID: accountID,
				CreatedAt: result.CreatedAt,
				Email:     result.Email,
				Label:     result.Label,
			})
		}
		return nil
	})
	if err != nil && batch.Failed == 0 {
		batch.Complete = false
		batch.Failed = 1
		batch.Error = aliasOperationErrorSummary(err)
	}
	return batch, err
}

func aliasBatchLabel(prefix string, index int, count int) string {
	if prefix == "" {
		return ""
	}
	if count == 1 {
		return prefix
	}
	return fmt.Sprintf("%s %d", prefix, index+1)
}

func aliasOperationErrorSummary(err error) string {
	if err != nil && isSessionError(err.Error()) {
		return "iCloud 会话失效，请更新 Cookie"
	}
	return "创建别名失败，请稍后重试"
}
