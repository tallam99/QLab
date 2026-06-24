import { useState } from "react";
import { type Theme, applyTheme, getTheme } from "../theme";

// ThemeToggle flips light/dark and persists the choice. Local state mirrors the
// applied theme so the label updates immediately.
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(getTheme);

  function toggle() {
    const next: Theme = theme === "dark" ? "light" : "dark";
    applyTheme(next);
    setTheme(next);
  }

  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
      className="rounded-md border border-edge bg-surface px-2.5 py-1 text-fg text-sm hover:opacity-90"
    >
      {theme === "dark" ? "☀ Light" : "☾ Dark"}
    </button>
  );
}
