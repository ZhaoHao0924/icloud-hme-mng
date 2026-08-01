import { describe, expect, it } from "vitest";

import { getBrowserMockScenario, parseMockScenario } from "./scenario";

describe("mock scenarios", () => {
  it("parses known scenarios and falls back to success", () => {
    expect(parseMockScenario("otp")).toBe("otp");
    expect(parseMockScenario("expired")).toBe("expired");
    expect(parseMockScenario("alias-error")).toBe("alias-error");
    expect(parseMockScenario("alias-forbidden")).toBe("alias-forbidden");
    expect(parseMockScenario("web-api")).toBe("web-api");
    expect(parseMockScenario("inbox-error")).toBe("inbox-error");
    expect(parseMockScenario("inbox-timeout")).toBe("inbox-timeout");
    expect(parseMockScenario("unknown")).toBe("success");
    expect(parseMockScenario(null)).toBe("success");
  });

  it("keeps an explicit browser scenario during SPA navigation", () => {
    window.history.replaceState(null, "", "/accounts?mock=mixed");
    expect(getBrowserMockScenario()).toBe("mixed");

    window.history.replaceState(null, "", "/accounts/acc_error/security");
    expect(getBrowserMockScenario()).toBe("mixed");
  });
});
