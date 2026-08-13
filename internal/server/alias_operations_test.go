package server

import (
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
)

func TestApplyAliasCreationHistoryFillsMissingCreatedAt(t *testing.T) {
	aliases := []hme.Alias{
		{Email: "KNOWN@icloud.com"},
		{Email: "already@icloud.com", CreatedAt: "2026-08-01T10:00:00Z"},
		{Email: "untracked@icloud.com"},
	}
	history := []account.AliasCreationHistory{{
		Aliases: []account.AliasCreationHistoryAlias{
			{CreatedAt: "2026-08-02T10:00:00Z", Email: "known@icloud.com"},
			{CreatedAt: "2026-07-31T10:00:00Z", Email: "already@icloud.com"},
		},
	}}

	applyAliasCreationHistory(aliases, history)

	if got := aliases[0].CreatedAt; got != "2026-08-02T10:00:00Z" {
		t.Errorf("known alias CreatedAt = %q, want history timestamp", got)
	}
	if got := aliases[1].CreatedAt; got != "2026-08-01T10:00:00Z" {
		t.Errorf("existing alias CreatedAt = %q, want upstream timestamp", got)
	}
	if got := aliases[2].CreatedAt; got != "" {
		t.Errorf("untracked alias CreatedAt = %q, want empty", got)
	}
}

func TestAliasOperationServiceReusesAndInvalidatesClient(t *testing.T) {
	mgr := newAutomationTestManager(t)
	service := newAliasOperationService(mgr)
	client := &fakeAliasOperationClient{}
	newClientCalls := 0
	service.newClient = func(string) (aliasOperationClient, error) {
		newClientCalls++
		return client, nil
	}

	for i := 0; i < 2; i++ {
		if err := service.withClient("acc_automation", func(aliasOperationClient) error { return nil }); err != nil {
			t.Fatalf("withClient() error = %v", err)
		}
	}
	if newClientCalls != 1 {
		t.Fatalf("new client calls before invalidation = %d, want 1", newClientCalls)
	}

	service.invalidateAccount("acc_automation")
	if err := service.withClient("acc_automation", func(aliasOperationClient) error { return nil }); err != nil {
		t.Fatalf("withClient() after invalidation error = %v", err)
	}
	if newClientCalls != 2 {
		t.Fatalf("new client calls after invalidation = %d, want 2", newClientCalls)
	}
}

func TestAliasOperationServiceReadOnlyClientIsNotReused(t *testing.T) {
	mgr := newAutomationTestManager(t)
	service := newAliasOperationService(mgr)
	client := &fakeAliasOperationClient{}
	newClientCalls := 0
	service.newClient = func(string) (aliasOperationClient, error) {
		newClientCalls++
		return client, nil
	}

	for i := 0; i < 2; i++ {
		if err := service.withReadOnlyClient("acc_automation", func(aliasOperationClient) error { return nil }); err != nil {
			t.Fatalf("withReadOnlyClient() error = %v", err)
		}
	}
	if newClientCalls != 2 {
		t.Fatalf("new client calls = %d, want 2", newClientCalls)
	}
}

func TestAliasOperationServiceSerializesCredentialUpdateWithOperation(t *testing.T) {
	mgr := newAutomationTestManager(t)
	service := newAliasOperationService(mgr)
	client := &fakeAliasOperationClient{}
	service.newClient = func(string) (aliasOperationClient, error) {
		return client, nil
	}

	operationEntered := make(chan struct{})
	releaseOperation := make(chan struct{})
	operationDone := make(chan error, 1)
	go func() {
		operationDone <- service.withClient("acc_automation", func(aliasOperationClient) error {
			close(operationEntered)
			<-releaseOperation
			return nil
		})
	}()
	<-operationEntered

	updateAttempted := make(chan struct{})
	updateEntered := make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		close(updateAttempted)
		updateDone <- service.withCredentialUpdate("acc_automation", func() error {
			close(updateEntered)
			return mgr.SaveCookies("acc_automation", map[string]string{"session": "replacement"})
		})
	}()
	<-updateAttempted

	select {
	case <-updateEntered:
		t.Fatal("credential update ran before the active operation completed")
	case <-time.After(40 * time.Millisecond):
	}
	close(releaseOperation)
	if err := <-operationDone; err != nil {
		t.Fatalf("withClient() error = %v", err)
	}
	if err := <-updateDone; err != nil {
		t.Fatalf("withCredentialUpdate() error = %v", err)
	}
	persistedClient, err := mgr.HMEClient("acc_automation", false)
	if err != nil {
		t.Fatalf("HMEClient() after credential update error = %v", err)
	}
	if got := persistedClient.Cookies["session"]; got != "replacement" {
		t.Fatalf("persisted session cookie = %q, want replacement", got)
	}
	persistedClient.Close()

	gotClient := make(chan aliasOperationClient, 1)
	service.newClient = func(string) (aliasOperationClient, error) {
		fresh := &fakeAliasOperationClient{}
		gotClient <- fresh
		return fresh, nil
	}
	if err := service.withClient("acc_automation", func(aliasOperationClient) error { return nil }); err != nil {
		t.Fatalf("withClient() after credential update error = %v", err)
	}
	if fresh := <-gotClient; fresh == client {
		t.Fatal("credential update reused the previous cached client")
	}
}
