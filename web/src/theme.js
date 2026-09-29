// Light / dark theme. Choice is stored in localStorage; first visit follows
// the system preference.
const KEY = "polka-theme";

export const getTheme = () => {
  try {
    const stored = localStorage.getItem(KEY);
    if (stored === "light" || stored === "dark") return stored;
  } catch {
    /* ignore */
  }
  try {
    if (window.matchMedia("(prefers-color-scheme: dark)").matches) return "dark";
  } catch {
    /* ignore */
  }
  return "light";
};

export const applyTheme = (theme) => {
  const next = theme === "dark" ? "dark" : "light";
  const root = document.documentElement;
  root.classList.remove("theme-light", "theme-dark", "theme-auto");
  root.classList.add(`theme-${next}`);
  root.style.colorScheme = next;
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) {
    meta.setAttribute("content", next === "dark" ? "#15120e" : "#f6f4ee");
  }
};

export const setTheme = (theme) => {
  const next = theme === "dark" ? "dark" : "light";
  try {
    localStorage.setItem(KEY, next);
  } catch {
    /* ignore */
  }
  applyTheme(next);
  return next;
};

export const toggleTheme = () => setTheme(getTheme() === "dark" ? "light" : "dark");

applyTheme(getTheme());
