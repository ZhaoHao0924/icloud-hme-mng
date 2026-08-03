package platformauth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSetupPersistsOnlyABcryptHash(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if store.Configured() {
		t.Fatal("new store unexpectedly reports configured")
	}

	const username = "admin-user"
	const password = "correct-horse-battery-staple"
	if err := store.Setup(username, password); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if !store.Configured() || store.Username() != username {
		t.Fatalf("configured store = configured:%t username:%q", store.Configured(), store.Username())
	}
	if !store.Authenticate(username, password) {
		t.Fatal("Authenticate() rejected the configured credential")
	}
	if store.Authenticate(username, "incorrect-password") {
		t.Fatal("Authenticate() accepted an invalid password")
	}

	raw, err := os.ReadFile(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatalf("read stored configuration: %v", err)
	}
	if strings.Contains(string(raw), password) || !strings.Contains(string(raw), "password_hash") {
		t.Fatalf("stored configuration leaked or omitted password hash: %s", raw)
	}

	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if !reopened.Authenticate(username, password) {
		t.Fatal("reopened store rejected the configured credential")
	}
}

func TestStoreRejectsInvalidOrRepeatedSetup(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Setup("a", "correct-horse-battery-staple"); err == nil {
		t.Fatal("Setup() accepted an invalid username")
	}
	if err := store.Setup("admin", "short"); err == nil {
		t.Fatal("Setup() accepted a short password")
	}
	if err := store.Setup("admin", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if err := store.Setup("other", "another-correct-password"); !errors.Is(err, ErrAlreadyConfigured) {
		t.Fatalf("repeated Setup() error = %v, want ErrAlreadyConfigured", err)
	}
}

func TestStoreRejectsMalformedPersistedConfiguration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(`{"username":"admin","password_hash":"not-a-bcrypt-hash"}`), 0600); err != nil {
		t.Fatalf("write malformed configuration: %v", err)
	}
	if _, err := NewStore(dir); err == nil {
		t.Fatal("NewStore() accepted a malformed stored hash")
	}
}
