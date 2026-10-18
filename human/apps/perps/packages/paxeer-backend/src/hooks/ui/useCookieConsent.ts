"use client";

import { useEffect, useState } from "react";

const STORAGE_KEY = "paxeerNetwork_cookie_consent";

export type CookieConsentValue = "accepted" | "declined" | null;

export interface CookieConsent {
  /** null = not yet decided, "accepted" = all cookies, "declined" = essential only */
  value: CookieConsentValue;
  /** True while we haven't read localStorage yet (prevents flash of banner on SSR). */
  loading: boolean;
  accept: () => void;
  decline: () => void;
}

export function useCookieConsent(): CookieConsent {
  const [value, setValue] = useState<CookieConsentValue>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY) as CookieConsentValue | null;
      if (stored === "accepted" || stored === "declined") {
        setValue(stored);
      }
    } catch {
      // localStorage unavailable — treat as not-yet-decided
    }
    setLoading(false);
  }, []);

  const accept = () => {
    setValue("accepted");
    try { localStorage.setItem(STORAGE_KEY, "accepted"); } catch { /* ignore */ }
  };

  const decline = () => {
    setValue("declined");
    try { localStorage.setItem(STORAGE_KEY, "declined"); } catch { /* ignore */ }
  };

  return { value, loading, accept, decline };
}
