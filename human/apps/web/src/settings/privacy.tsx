"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { copyEntry } from "../../copy/runtime";

const STORAGE_PREFIX = "layerx.privacy-mode.v1";
const CHANGE_EVENT = "layerx:privacy-mode-change";

interface PrivacyModeState {
  readonly key: string;
  readonly masked: boolean;
}

interface PrivacyModeContextValue {
  readonly masked: boolean;
  readonly setMasked: (masked: boolean) => void;
}

const PrivacyModeContext = createContext<PrivacyModeContextValue | undefined>(undefined);

function storageKey(principalScope: string): string {
  return `${STORAGE_PREFIX}.${principalScope}`;
}

export function PrivacyModeProvider({
  principalScope,
  children,
}: Readonly<{ principalScope: string; children: ReactNode }>) {
  const key = useMemo(() => storageKey(principalScope), [principalScope]);
  const [state, setMaskedState] = useState<PrivacyModeState>(() => ({ key, masked: true }));
  const masked = state.key === key ? state.masked : true;

  useEffect(() => {
    const readPreference = () => {
      try {
        const stored = window.localStorage.getItem(key);
        setMaskedState({ key, masked: stored !== null && stored !== "visible" });
      } catch {
        setMaskedState({ key, masked: true });
      }
    };
    const sync = (event: StorageEvent) => {
      if (event.key !== key && event.key !== null) return;
      try {
        if (event.storageArea !== window.localStorage) return;
      } catch {
        setMaskedState({ key, masked: true });
        return;
      }
      readPreference();
    };
    const syncLocal = (event: Event) => {
      if (!(event instanceof CustomEvent)) return;
      const detail: unknown = event.detail;
      if (
        typeof detail === "object" && detail !== null &&
        "key" in detail && detail.key === key &&
        "masked" in detail && typeof detail.masked === "boolean"
      ) {
        setMaskedState({ key, masked: detail.masked });
      }
    };
    window.addEventListener("storage", sync);
    window.addEventListener(CHANGE_EVENT, syncLocal);
    readPreference();
    return () => {
      window.removeEventListener("storage", sync);
      window.removeEventListener(CHANGE_EVENT, syncLocal);
    };
  }, [key]);

  const setMasked = useCallback((next: boolean) => {
    const nextState = { key, masked: next };
    setMaskedState(nextState);
    try {
      window.localStorage.setItem(key, next ? "masked" : "visible");
    } catch {
      setMaskedState(nextState);
    }
    window.dispatchEvent(new CustomEvent<PrivacyModeState>(CHANGE_EVENT, { detail: nextState }));
  }, [key]);

  const value = useMemo(() => ({ masked, setMasked }), [masked, setMasked]);
  return (
    <PrivacyModeContext.Provider value={value}>
      <div data-privacy-mode={masked ? "masked" : "visible"}>{children}</div>
    </PrivacyModeContext.Provider>
  );
}

export function usePrivacyMode(): PrivacyModeContextValue {
  const value = useContext(PrivacyModeContext);
  if (value === undefined) {
    throw new Error("PrivacyModeProvider is required on authenticated surfaces");
  }
  return value;
}

export function PrivateFigure({
  children,
  className,
}: Readonly<{ children: ReactNode; className?: string }>) {
  const { masked } = usePrivacyMode();
  if (masked) {
    return (
      <span
        className={className}
        data-private-figure="masked"
        aria-label={copyEntry("privacy.hidden").message}
      >
        ••••
      </span>
    );
  }
  return <span className={className} data-private-figure="visible">{children}</span>;
}
