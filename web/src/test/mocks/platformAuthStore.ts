import type { PlatformAuthStatus } from "../../api/schemas";
import type { MockScenario } from "./scenario";

type Credentials = {
  password?: unknown;
  username?: unknown;
};

export type MockPlatformAuthStore = {
  login: (credentials: Credentials, scenario: MockScenario) => PlatformAuthStatus | null;
  logout: () => PlatformAuthStatus;
  reset: () => void;
  setup: (credentials: Credentials, scenario: MockScenario) => PlatformAuthStatus | null;
  status: (scenario: MockScenario) => PlatformAuthStatus;
};

const validPassword = "correct-horse-battery-staple";

function authStatus(configured: boolean, authenticated: boolean): PlatformAuthStatus {
  return {
    authenticated,
    configured,
    expires_at: authenticated ? "2026-08-03T08:30:00Z" : "",
    username: authenticated ? "admin" : "",
  };
}

function isValidCredential(credentials: Credentials) {
  return credentials.username === "admin" && credentials.password === validPassword;
}

export function createMockPlatformAuthStore(): MockPlatformAuthStore {
  let authenticated = false;
  let setupComplete = false;

  function setupRequired(scenario: MockScenario) {
    return scenario === "platform-setup" && !setupComplete;
  }

  return {
    login(credentials, scenario) {
      if (setupRequired(scenario) || !isValidCredential(credentials)) return null;
      authenticated = true;
      return authStatus(true, true);
    },
    logout() {
      authenticated = false;
      return authStatus(true, false);
    },
    reset() {
      authenticated = false;
      setupComplete = false;
    },
    setup(credentials, scenario) {
      if (scenario !== "platform-setup" || !isValidCredential(credentials)) return null;
      setupComplete = true;
      authenticated = true;
      return authStatus(true, true);
    },
    status(scenario) {
      if (setupRequired(scenario)) return authStatus(false, false);
      if (scenario === "platform-login" || scenario === "platform-setup") {
        return authStatus(true, authenticated);
      }
      return authStatus(true, true);
    },
  };
}
