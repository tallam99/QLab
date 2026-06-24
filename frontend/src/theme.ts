// Light/dark theme, persisted to localStorage and applied as a `.dark` class on
// <html> (the CSS in index.css flips the palette off that class). Dark-first to match
// the brand; the toggle just overrides per-user.

export type Theme = "light" | "dark";

const STORAGE_KEY = "qlab-theme";

export function getTheme(): Theme {
  const saved = localStorage.getItem(STORAGE_KEY);
  return saved === "light" || saved === "dark" ? saved : "dark";
}

export function applyTheme(theme: Theme): void {
  document.documentElement.classList.toggle("dark", theme === "dark");
  localStorage.setItem(STORAGE_KEY, theme);
}
