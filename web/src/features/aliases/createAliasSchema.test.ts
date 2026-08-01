import { describe, expect, it } from "vitest";

import { createAliasSchema, maxAliasLabelCharacters } from "./createAliasSchema";

describe("createAliasSchema", () => {
  it("accepts an empty optional label and trims entered text", () => {
    expect(createAliasSchema.parse({ label: "" })).toEqual({ label: "" });
    expect(createAliasSchema.parse({ label: "  新闻订阅  " })).toEqual({ label: "新闻订阅" });
  });

  it("counts Unicode code points using the backend label limit", () => {
    expect(
      createAliasSchema.safeParse({ label: "😀".repeat(maxAliasLabelCharacters) }).success,
    ).toBe(true);
    expect(
      createAliasSchema.safeParse({ label: "😀".repeat(maxAliasLabelCharacters + 1) }).success,
    ).toBe(false);
  });
});
