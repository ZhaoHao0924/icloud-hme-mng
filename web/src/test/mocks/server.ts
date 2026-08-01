import { setupServer } from "msw/node";

import { createMockHandlers } from "./handlers";
import { createMockAccountStore } from "./accountStore";
import { createMockAliasStore } from "./aliasStore";
import { createMockAliasAutomationStore } from "./aliasAutomationStore";
import { createMockScenarioState } from "./scenario";

export const mockScenario = createMockScenarioState();
export const mockAccounts = createMockAccountStore();
export const mockAliases = createMockAliasStore();
export const mockAliasAutomation = createMockAliasAutomationStore();
export const server = setupServer(
  ...createMockHandlers(mockScenario.get, mockAccounts, mockAliases, mockAliasAutomation),
);
