import { cleanup } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { afterAll, afterEach, beforeAll } from "vitest";

import { clearApiToken } from "../api/apiTokenSession";
import {
  mockAccounts,
  mockAliasAutomation,
  mockAliases,
  mockScenario,
  server,
} from "./mocks/server";

beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
  cleanup();
  clearApiToken();
  mockAccounts.reset();
  mockAliasAutomation.reset();
  mockAliases.reset();
  mockScenario.reset();
  server.resetHandlers();
});

afterAll(() => {
  server.close();
});
