import * as React from "react";

type Theme = "light" | "dark" | "system";
type Accent = "ink" | "dx-gradient";

type ThemeContextValue = {
  theme: Theme;
  resolvedTheme: "light" | "dark";
  accent: Accent;
  setTheme: (t: Theme) => void;
  setAccent: (a: Accent) => void;
  toggleTheme: () => void;
};

const ThemeContext = React.createContext<ThemeContextValue | null>(null);

function getSystem(): "light" | "dark" {
  if (typeof window === "undefined") return "light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = React.useState<Theme>(() => (localStorage.getItem("dx-theme") as Theme) || "system");
  const [accent, setAccentState] = React.useState<Accent>(() => (localStorage.getItem("dx-accent") as Accent) || "ink");
  const [system, setSystem] = React.useState<"light" | "dark">(getSystem);

  React.useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const fn = () => setSystem(mq.matches ? "dark" : "light");
    mq.addEventListener("change", fn);
    return () => mq.removeEventListener("change", fn);
  }, []);

  // Keep every same-origin document (e.g. block preview iframes) in sync.
  React.useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === "dx-theme" && e.newValue) setThemeState(e.newValue as Theme);
      if (e.key === "dx-accent" && e.newValue) setAccentState(e.newValue as Accent);
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  const resolvedTheme = theme === "system" ? system : theme;

  React.useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("dark", resolvedTheme === "dark");
    root.classList.toggle("accent-dx-gradient", accent === "dx-gradient");
  }, [resolvedTheme, accent]);

  const setTheme = React.useCallback((t: Theme) => {
    localStorage.setItem("dx-theme", t);
    setThemeState(t);
  }, []);
  const setAccent = React.useCallback((a: Accent) => {
    localStorage.setItem("dx-accent", a);
    setAccentState(a);
  }, []);
  const toggleTheme = React.useCallback(() => setTheme(resolvedTheme === "dark" ? "light" : "dark"), [resolvedTheme, setTheme]);

  return (
    <ThemeContext.Provider value={{ theme, resolvedTheme, accent, setTheme, setAccent, toggleTheme }}>{children}</ThemeContext.Provider>
  );
}

export function useTheme() {
  const ctx = React.useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider");
  return ctx;
}
