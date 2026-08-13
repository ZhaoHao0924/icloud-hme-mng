package server

import (
	"fmt"
	"strings"
	"sync"
	"time"

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
	clientsMu sync.Mutex
	clients   map[string]aliasOperationClient
}

func newAliasOperationService(mgr *account.Manager) *aliasOperationService {
	service := &aliasOperationService{
		mgr:     mgr,
		clients: make(map[string]aliasOperationClient),
	}
	service.newClient = func(accountID string) (aliasOperationClient, error) {
		return mgr.HMEClient(accountID, false)
	}
	return service
}

type closeableAliasOperationClient interface {
	Close()
}

func closeAliasOperationClient(client aliasOperationClient) {
	if closeable, ok := client.(closeableAliasOperationClient); ok {
		closeable.Close()
	}
}

// clientForLocked reuses the validated HME client for an account. The caller
// must hold the account operation lock because the client owns mutable state.
func (s *aliasOperationService) clientForLocked(accountID string) (aliasOperationClient, error) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if client := s.clients[accountID]; client != nil {
		return client, nil
	}
	client, err := s.newClient(accountID)
	if err != nil {
		return nil, err
	}
	s.clients[accountID] = client
	return client, nil
}

func (s *aliasOperationService) forgetClientLocked(accountID string) {
	s.clientsMu.Lock()
	client := s.clients[accountID]
	delete(s.clients, accountID)
	s.clientsMu.Unlock()
	closeAliasOperationClient(client)
}

func (s *aliasOperationService) invalidateAccount(accountID string) {
	lock := s.locks.lockFor(accountID)
	lock.Lock()
	defer lock.Unlock()
	s.forgetClientLocked(accountID)
}

// withCredentialUpdate serializes a credential replacement with every HME
// operation for the account. The cached client must be discarded before the
// replacement so a completed operation cannot persist its old session after
// the new credentials have been validated.
func (s *aliasOperationService) withCredentialUpdate(accountID string, update func() error) error {
	lock := s.locks.lockFor(accountID)
	lock.Lock()
	defer lock.Unlock()

	s.forgetClientLocked(accountID)
	return update()
}

func (s *aliasOperationService) invalidateAll() {
	s.clientsMu.Lock()
	accountIDs := make([]string, 0, len(s.clients))
	for accountID := range s.clients {
		accountIDs = append(accountIDs, accountID)
	}
	s.clientsMu.Unlock()
	for _, accountID := range accountIDs {
		s.invalidateAccount(accountID)
	}
}

func (s *aliasOperationService) close() {
	s.invalidateAll()
}

func (s *aliasOperationService) withClient(accountID string, operation func(aliasOperationClient) error) error {
	lock := s.locks.lockFor(accountID)
	lock.Lock()
	defer lock.Unlock()

	client, err := s.clientForLocked(accountID)
	if err != nil {
		return err
	}
	operationErr := operation(client)
	if err := s.mgr.SaveCookies(accountID, client.SessionCookies()); err != nil {
		return fmt.Errorf("保存刷新后的 Cookie 失败: %w", err)
	}
	if operationErr != nil && isSessionError(operationErr.Error()) {
		s.forgetClientLocked(accountID)
	}
	return operationErr
}

// withReadOnlyClient serializes a non-mutating upstream call without saving
// refreshed session cookies. It is used by previews that must not alter state.
func (s *aliasOperationService) withReadOnlyClient(accountID string, operation func(aliasOperationClient) error) error {
	lock := s.locks.lockFor(accountID)
	lock.Lock()
	defer lock.Unlock()

	client, err := s.newClient(accountID)
	if err != nil {
		return err
	}
	defer closeAliasOperationClient(client)
	return operation(client)
}

type createdAliasData struct {
	AccountID string `json:"account_id"`
	BatchID   string `json:"batch_id,omitempty"`
	CreatedAt string `json:"created_at"`
	Email     string `json:"email"`
	Label     string `json:"label"`
}

type aliasBatchResult struct {
	AccountID string             `json:"account_id"`
	Aliases   []createdAliasData `json:"aliases"`
	BatchID   string             `json:"batch_id,omitempty"`
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

func aliasCreationHistoryAliases(aliases []createdAliasData) []account.AliasCreationHistoryAlias {
	historyAliases := make([]account.AliasCreationHistoryAlias, 0, len(aliases))
	for _, alias := range aliases {
		historyAliases = append(historyAliases, account.AliasCreationHistoryAlias{
			CreatedAt: alias.CreatedAt,
			Email:     alias.Email,
			Label:     alias.Label,
		})
	}
	return historyAliases
}

func applyAliasCreationHistory(aliases []hme.Alias, history []account.AliasCreationHistory) {
	createdAtByEmail := make(map[string]string)
	for _, entry := range history {
		for _, createdAlias := range entry.Aliases {
			email := strings.ToLower(strings.TrimSpace(createdAlias.Email))
			if email == "" || strings.TrimSpace(createdAlias.CreatedAt) == "" {
				continue
			}
			if _, exists := createdAtByEmail[email]; !exists {
				createdAtByEmail[email] = createdAlias.CreatedAt
			}
		}
	}

	for index := range aliases {
		if strings.TrimSpace(aliases[index].CreatedAt) != "" {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(aliases[index].Email))
		aliases[index].CreatedAt = createdAtByEmail[email]
	}
}

func applyAliasCreationBatchID(aliases []createdAliasData, batchID string) {
	for index := range aliases {
		aliases[index].BatchID = batchID
	}
}

func (s *aliasOperationService) recordAliasCreation(
	accountID string,
	entry account.AliasCreationHistory,
	now time.Time,
) (account.AliasCreationHistory, error) {
	return s.mgr.RecordAliasCreation(accountID, entry, now)
}

func aliasOperationErrorSummary(err error) string {
	if err != nil && isSessionError(err.Error()) {
		return "iCloud 会话失效，请更新 Cookie"
	}
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "429") {
			return "iCloud 请求频率受限，已延后重试"
		}
		if strings.Contains(message, "409") || strings.Contains(message, "507") {
			return "iCloud 可能已达到别名上限，请检查账户"
		}
	}
	return "创建别名失败，请稍后重试"
}
