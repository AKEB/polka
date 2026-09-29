import { beforeEach, describe, expect, it, vi } from "vitest";

describe("theme", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.className = "";
    document.documentElement.style.colorScheme = "";
    vi.resetModules();
  });

  it("defaults to system preference when nothing is stored", async () => {
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      value: vi.fn().mockImplementation((query) => ({
        matches: query.includes("dark"),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    });
    const { getTheme } = await import("./theme");
    expect(getTheme()).toBe("dark");
    expect(document.documentElement.classList.contains("theme-dark")).toBe(true);
  });

  it("persists and toggles light/dark", async () => {
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: "",
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    });
    const { getTheme, setTheme, toggleTheme } = await import("./theme");
    expect(getTheme()).toBe("light");
    expect(toggleTheme()).toBe("dark");
    expect(localStorage.getItem("polka-theme")).toBe("dark");
    expect(document.documentElement.classList.contains("theme-dark")).toBe(true);
    expect(setTheme("light")).toBe("light");
    expect(document.documentElement.classList.contains("theme-light")).toBe(true);
  });
});
