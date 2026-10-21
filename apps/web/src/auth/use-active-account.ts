"use client";

import { useEffect, useState } from "react";

import { ACTIVE_ACCOUNT_STORAGE_KEY } from "./session.ts";

function storedAccountId(): string | undefined {
  try {
    const value = window.localStorage.getItem(ACTIVE_ACCOUNT_STORAGE_KEY)?.trim();
    return value === undefined || value.length === 0 || value.length > 256 ? undefined : value;
  } catch {
    return undefined;
  }
}

export function useActiveAccountId(initial?: string): string | undefined {
  const [accountId, setAccountId] = useState(initial);

  useEffect(() => {
    if (initial !== undefined) {
      setAccountId(initial);
      return;
    }
    setAccountId(storedAccountId());
    const sync = (event: StorageEvent) => {
      if (event.key === ACTIVE_ACCOUNT_STORAGE_KEY) {
        setAccountId(storedAccountId());
      }
    };
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener("storage", sync);
    };
  }, [initial]);

  return accountId;
}
