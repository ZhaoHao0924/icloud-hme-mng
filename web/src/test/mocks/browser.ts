import { setupWorker } from "msw/browser";

import { createMockAccountStore } from "./accountStore";
import { createMockAliasStore } from "./aliasStore";
import { createMockHandlers } from "./handlers";
import { getBrowserMockScenario } from "./scenario";

const mockAccounts = createMockAccountStore();
const mockAliases = createMockAliasStore();

export const worker = setupWorker(
  ...createMockHandlers(getBrowserMockScenario, mockAccounts, mockAliases),
);
