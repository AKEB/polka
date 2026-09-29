import { describe, expect, it } from "vitest";
import { getFilterLang, setFilterLang } from "./filters";

describe("book language filter", () => {
  it("stores and clears the chosen language", () => {
    localStorage.clear();
    expect(getFilterLang()).toBe("");
    setFilterLang("en");
    expect(getFilterLang()).toBe("en");
    setFilterLang("");
    expect(getFilterLang()).toBe("");
  });
});
