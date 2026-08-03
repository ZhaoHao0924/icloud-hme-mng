// Package platformauth stores the single local administrator credential used
// to enter the browser platform. It never persists a plaintext password.
package platformauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	configFileName    = "platform-auth.json"
	minPasswordRunes  = 12
	maxPasswordBytes  = 72
	minUsernameLength = 3
	maxUsernameLength = 32
)

var (
	ErrAlreadyConfigured = errors.New("platform administrator is already configured")
	ErrNotConfigured     = errors.New("platform administrator is not configured")
)

type config struct {
	PasswordHash string `json:"password_hash"`
	Username     string `json:"username"`
}

// Store persists one administrator username and a bcrypt password hash.
type Store struct {
	mu           sync.RWMutex
	passwordHash []byte
	path         string
	username     string
}

// NewStore opens the administrator credential stored below dataDir.
func NewStore(dataDir string) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("platform auth data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create platform auth data directory: %w", err)
	}

	store := &Store{path: filepath.Join(dataDir, configFileName)}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// Configured reports whether an administrator credential has been created.
func (s *Store) Configured() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.username != "" && len(s.passwordHash) > 0
}

// Username returns the configured administrator username, if any.
func (s *Store) Username() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.username
}

// Setup creates the initial administrator credential. It can only succeed once.
func (s *Store) Setup(username, password string) error {
	username, err := normalizeUsername(username)
	if err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash administrator password: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.username != "" || len(s.passwordHash) > 0 {
		return ErrAlreadyConfigured
	}

	encoded, err := json.Marshal(config{PasswordHash: string(passwordHash), Username: username})
	if err != nil {
		return fmt.Errorf("encode platform auth configuration: %w", err)
	}
	if err := writeFileAtomic(s.path, encoded); err != nil {
		return err
	}

	s.username = username
	s.passwordHash = append(s.passwordHash[:0], passwordHash...)
	return nil
}

// Authenticate verifies an administrator username and password.
func (s *Store) Authenticate(username, password string) bool {
	if s == nil {
		return false
	}
	username = strings.TrimSpace(username)
	s.mu.RLock()
	configuredUsername := s.username
	passwordHash := append([]byte(nil), s.passwordHash...)
	s.mu.RUnlock()

	if configuredUsername == "" || len(passwordHash) == 0 || username != configuredUsername {
		return false
	}
	return bcrypt.CompareHashAndPassword(passwordHash, []byte(password)) == nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read platform auth configuration: %w", err)
	}

	var stored config
	if err := json.Unmarshal(raw, &stored); err != nil {
		return fmt.Errorf("decode platform auth configuration: %w", err)
	}
	username, err := normalizeUsername(stored.Username)
	if err != nil {
		return fmt.Errorf("invalid platform auth username: %w", err)
	}
	passwordHash := []byte(stored.PasswordHash)
	if _, err := bcrypt.Cost(passwordHash); err != nil {
		return fmt.Errorf("invalid platform auth password hash: %w", err)
	}

	s.username = username
	s.passwordHash = append(s.passwordHash[:0], passwordHash...)
	return nil
}

func normalizeUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < minUsernameLength || len(value) > maxUsernameLength {
		return "", fmt.Errorf("username must be %d to %d characters", minUsernameLength, maxUsernameLength)
	}
	for index, char := range value {
		letter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
		digit := char >= '0' && char <= '9'
		allowed := letter || digit || char == '.' || char == '_' || char == '-'
		if !allowed || (index == 0 && !letter && !digit) {
			return "", errors.New("username may contain only letters, numbers, dots, underscores, and hyphens")
		}
	}
	return value, nil
}

func validatePassword(value string) error {
	if utf8.RuneCountInString(value) < minPasswordRunes {
		return fmt.Errorf("password must contain at least %d characters", minPasswordRunes)
	}
	if len([]byte(value)) > maxPasswordBytes {
		return fmt.Errorf("password must not exceed %d bytes", maxPasswordBytes)
	}
	if strings.TrimSpace(value) == "" {
		return errors.New("password must not be blank")
	}
	return nil
}

func writeFileAtomic(filename string, content []byte) (err error) {
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".platform-auth-*.tmp")
	if err != nil {
		return fmt.Errorf("create platform auth temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temporary.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close platform auth temporary file: %w", closeErr))
			}
		}
		if err != nil {
			if removeErr := os.Remove(temporaryName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove platform auth temporary file: %w", removeErr))
			}
		}
	}()

	if err = temporary.Chmod(0600); err != nil {
		return fmt.Errorf("set platform auth temporary file permissions: %w", err)
	}
	if _, err = temporary.Write(content); err != nil {
		return fmt.Errorf("write platform auth temporary file: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("sync platform auth temporary file: %w", err)
	}
	if err = temporary.Close(); err != nil {
		closed = true
		return fmt.Errorf("close platform auth temporary file: %w", err)
	}
	closed = true
	if err = os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace platform auth configuration: %w", err)
	}
	return nil
}
