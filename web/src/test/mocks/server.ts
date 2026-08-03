import { setupServer } from "msw/node";

import { createMockHandlers } from "./handlers";
import { createMockAccountStore } from "./accountStore";
import { createMockAliasStore } from "./aliasStore";
import { createMockAliasAutomationStore } from "./aliasAutomationStore";
import { createMockAliasCreationHistoryStore } from "./aliasCreationHistoryStore";
import { createMockPlatformAuthStore } from "./platformAuthStore";
import { createMockEmailNotificationStore } from "./emailNotificationStore";
import { createMockWebhookNotificationStore } from "./webhookNotificationStore";
import { createMockScenarioState } from "./scenario";

export const mockScenario = createMockScenarioState();
export const mockAccounts = createMockAccountStore();
export const mockAliases = createMockAliasStore();
export const mockAliasAutomation = createMockAliasAutomationStore();
export const mockAliasCreationHistory = createMockAliasCreationHistoryStore();
export const mockPlatformAuth = createMockPlatformAuthStore();
export const mockEmailNotification = createMockEmailNotificationStore();
export const mockWebhookNotification = createMockWebhookNotificationStore();
export const server = setupServer(
  ...createMockHandlers(
    mockScenario.get,
    mockAccounts,
    mockAliases,
    mockAliasAutomation,
    mockPlatformAuth,
    mockAliasCreationHistory,
    mockEmailNotification,
    mockWebhookNotification,
  ),
);
