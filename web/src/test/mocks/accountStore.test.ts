import { describe, expect, it } from "vitest";

import { createMockAccountStore } from "./accountStore";

describe("mock account login challenges", () => {
  it("binds a challenge to one account and consumes it once", () => {
    const store = createMockAccountStore();

    store.issueLoginChallenge("account-a", "challenge-a");

    expect(store.consumeLoginChallenge("account-b", "challenge-a")).toBe(false);
    expect(store.consumeLoginChallenge("account-a", "challenge-a")).toBe(true);
    expect(store.consumeLoginChallenge("account-a", "challenge-a")).toBe(false);
  });

  it("replaces an account challenge and clears challenges on deletion or reset", () => {
    const store = createMockAccountStore();

    store.issueLoginChallenge("account-a", "challenge-old");
    store.issueLoginChallenge("account-a", "challenge-new");
    expect(store.consumeLoginChallenge("account-a", "challenge-old")).toBe(false);
    expect(store.consumeLoginChallenge("account-a", "challenge-new")).toBe(true);

    store.issueLoginChallenge("account-a", "challenge-delete");
    store.delete("account-a");
    expect(store.consumeLoginChallenge("account-a", "challenge-delete")).toBe(false);

    store.issueLoginChallenge("account-b", "challenge-reset");
    store.reset();
    expect(store.consumeLoginChallenge("account-b", "challenge-reset")).toBe(false);
  });

  it("tracks restored sessions separately from account capability flags", () => {
    const store = createMockAccountStore();

    expect(store.hasRestoredSession("account-a")).toBe(false);
    store.markSessionRestored("account-a");
    expect(store.hasRestoredSession("account-a")).toBe(true);
    expect(store.hasRestoredSession("account-b")).toBe(false);

    store.delete("account-a");
    expect(store.hasRestoredSession("account-a")).toBe(false);

    store.markSessionRestored("account-b");
    store.reset();
    expect(store.hasRestoredSession("account-b")).toBe(false);
  });
});
