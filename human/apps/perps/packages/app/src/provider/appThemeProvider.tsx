import {
  FC,
  PropsWithChildren,
  useEffect,
  useLayoutEffect,
  useMemo,
} from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import {
  PaxeerNetworkThemeProvider,
  type PaxeerNetworkThemeProviderProps,
  DARK_THEME_CSS_VARS,
  LIGHT_THEME_CSS_VARS,
  type ThemeCssVars,
} from "@paxeer-network/ui";

export type AppThemeProviderProps =
  PropsWithChildren<PaxeerNetworkThemeProviderProps>;

export const PAXEER_NETWORK_THEME_STORAGE_KEY = "paxeerNetwork_theme_id";

const useIsomorphicLayoutEffect =
  typeof window === "undefined" ? useEffect : useLayoutEffect;

export const AppThemeProvider: FC<AppThemeProviderProps> = (props) => {
  const { children, themes, ...rest } = props;

  const defaultTheme = useMemo(() => {
    return themes?.find((theme) => theme.isDefault) ?? themes?.[0];
  }, [themes]);

  const [storedThemeId, setStoredThemeId] = useLocalStorage<string | undefined>(
    PAXEER_NETWORK_THEME_STORAGE_KEY,
    undefined,
  );

  const currentTheme = useMemo(() => {
    return themes?.find((theme) => theme.id === storedThemeId) ?? defaultTheme;
  }, [themes, storedThemeId, defaultTheme]);

  const currentThemeId = currentTheme?.id;

  // Persist the fallback when the stored theme no longer exists.
  useEffect(() => {
    if (!currentThemeId || currentThemeId === storedThemeId) {
      return;
    }

    setStoredThemeId(currentThemeId);
  }, [currentThemeId, storedThemeId, setStoredThemeId]);

  // Apply theme to DOM via data-oui-theme and optional cssVars.
  useIsomorphicLayoutEffect(() => {
    if (typeof document === "undefined") return;

    const root = document.documentElement;

    if (!currentThemeId) {
      root.removeAttribute("data-oui-theme");
      return;
    }

    root.setAttribute("data-oui-theme", currentThemeId);

    const baseThemeVars =
      currentTheme?.mode === "light"
        ? LIGHT_THEME_CSS_VARS
        : DARK_THEME_CSS_VARS;

    // override default theme css vars with current theme css vars
    Object.entries(baseThemeVars).forEach(([key, defaultValue]) => {
      const newValue =
        currentTheme?.cssVars?.[key as keyof ThemeCssVars] ?? defaultValue;
      root.style.setProperty(key, newValue);
    });
  }, [currentThemeId, currentTheme]);

  return (
    <PaxeerNetworkThemeProvider
      themes={themes}
      currentThemeId={currentThemeId}
      currentTheme={currentTheme}
      setCurrentThemeId={setStoredThemeId}
      {...rest}
    >
      {children}
    </PaxeerNetworkThemeProvider>
  );
};
