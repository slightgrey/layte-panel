import { useCallback, useEffect, useState } from "react";

export type Theme = "light" | "dark" | "system";
const KEY = "layte-theme";

function read(): Theme {
  try {
    const t = localStorage.getItem(KEY);
    return t === "light" || t === "dark" ? t : "system";
  } catch {
    return "system";
  }
}

function apply(t: Theme) {
  const dark = t === "dark" || (t === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
}

/** Current theme + setter. Saved in localStorage; "system" follows the OS. */
export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(read);

  useEffect(() => {
    apply(theme);
    if (theme !== "system") return;
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => apply("system");
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [theme]);

  const setTheme = useCallback((t: Theme) => {
    try {
      if (t === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, t);
    } catch {
      /* storage blocked: still switch for this page view */
    }
    setThemeState(t);
  }, []);

  return { theme, setTheme };
}
