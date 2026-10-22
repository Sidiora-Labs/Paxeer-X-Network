"use client";

import { useEffect } from "react";
import { create } from "zustand";

const STORAGE_KEY = "hyperpax_cookie_consent";

export type CookieConsentValue = "accepted" | "declined" | null;

export interface CookieConsent {
  /** null = not yet decided, "accepted" = all cookies, "declined" = essential only */
  value: CookieConsentValue;
  /** True while we haven't read localStorage yet (prevents flash of banner on SSR). */
  loading: boolean;
  accept: () => void;
  decline: () => void;
}

interface CookieConsentStore extends CookieConsent {
  hydrate: () => void;
}

function store(value: "accepted" | "declined") {
  try { localStorage.setItem(STORAGE_KEY, value); } catch { /* ignore */ }
}

/** One consent state shared by every consumer (policy page controls and the global banner). */
export const cookieConsentStore = create<CookieConsentStore>()((set, get) => ({
  value: null,
  loading: true,
  hydrate: () => {
    if (!get().loading) return;
    let value: CookieConsentValue = null;
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored === "accepted" || stored === "declined") {
        value = stored;
      }
    } catch {
      // localStorage unavailable — treat as not-yet-decided
    }
    set({ value, loading: false });
  },
  accept: () => {
    set({ value: "accepted", loading: false });
    store("accepted");
  },
  decline: () => {
    set({ value: "declined", loading: false });
    store("declined");
  },
}));

export function useCookieConsent(): CookieConsent {
  const value = cookieConsentStore((s) => s.value);
  const loading = cookieConsentStore((s) => s.loading);
  const accept = cookieConsentStore((s) => s.accept);
  const decline = cookieConsentStore((s) => s.decline);
  const hydrate = cookieConsentStore((s) => s.hydrate);

  useEffect(() => {
    hydrate();
  }, [hydrate]);

  return { value, loading, accept, decline };
}
