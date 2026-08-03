import { setupWorker } from "msw/browser";

import { createMockAccountStore } from "./accountStore";
import { createMockAliasStore } from "./aliasStore";
import { createMockHandlers } from "./handlers";
import { createMockPlatformAuthStore } from "./platformAuthStore";
import { createMockWebhookNotificationStore } from "./webhookNotificationStore";
import { getBrowserMockScenario } from "./scenario";

const mockAccounts = createMockAccountStore();
const mockAliases = createMockAliasStore();
const mockPlatformAuth = createMockPlatformAuthStore();
const mockWebhookNotification = createMockWebhookNotificationStore();

export const worker = setupWorker(
  ...createMockHandlers(
    getBrowserMockScenario,
    mockAccounts,
    mockAliases,
    undefined,
    mockPlatformAuth,
    undefined,
    undefined,
    mockWebhookNotification,
  ),
);
