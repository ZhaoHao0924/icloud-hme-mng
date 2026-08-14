export const mockScenarios = [
  "success",
  "empty",
  "error",
  "mixed",
  "offline",
  "otp",
  "expired",
  "alias-error",
  "alias-forbidden",
  "web-api",
  "inbox-error",
  "inbox-timeout",
  "inbox-long",
  "inbox-html",
  "inbox-paged",
  "inbox-scroll",
  "platform-login",
  "platform-setup",
] as const;

export type MockScenario = (typeof mockScenarios)[number];

let activeBrowserScenario: MockScenario | null = null;

export function parseMockScenario(value: string | null | undefined): MockScenario {
  return mockScenarios.includes(value as MockScenario) ? (value as MockScenario) : "success";
}

export function getBrowserMockScenario() {
  const requestedScenario = new URLSearchParams(window.location.search).get("mock");
  if (requestedScenario !== null) {
    activeBrowserScenario = parseMockScenario(requestedScenario);
  }
  return activeBrowserScenario ?? "success";
}

export function createMockScenarioState(initialScenario: MockScenario = "success") {
  let scenario = initialScenario;

  return {
    get: () => scenario,
    reset: () => {
      scenario = initialScenario;
    },
    set: (nextScenario: MockScenario) => {
      scenario = nextScenario;
    },
  };
}
