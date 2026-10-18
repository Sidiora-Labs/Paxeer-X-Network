import { useCallback, useEffect, useRef, useState } from "react";

type Opts = {
  onStatusChange?: (was: boolean, is: boolean) => void;
  hideDelay?: number; // Delay in ms to hide UI after network recovery, default 3000
};

export const useOfflineInfoScript = ({ opts }: { opts?: Opts }) => {
  // Init
  const [offline, setOffline] = useState<boolean | undefined>(undefined);
  const lastRef = useRef(offline);
  const cbRef = useRef(opts?.onStatusChange);
  cbRef.current = opts?.onStatusChange;

  // Probe state and hide timer
  const probingRef = useRef(false);
  const hideTimerRef = useRef<NodeJS.Timeout | null>(null);
  const hideDelayRef = useRef(opts?.hideDelay ?? 3000);

  // Keep ref in sync
  hideDelayRef.current = opts?.hideDelay ?? 3000;

  // Lightweight liveness probe
  const probe = useCallback(async () => {
    try {
      await fetch("/favicon.ico", {
        method: "HEAD",
        mode: "no-cors",
        cache: "no-cache",
      });
      return true;
    } catch {
      return false;
    }
  }, []);

  useEffect(() => {
    let alive = true;

    const update = (next: boolean) => {
      if (!alive || lastRef.current === next) return;
      cbRef.current?.(lastRef.current ?? false, next);
      lastRef.current = next;
      setOffline(next);
    };

    const onOffline = () => {
      // Clear hide timer when going offline
      if (hideTimerRef.current) {
        clearTimeout(hideTimerRef.current);
        hideTimerRef.current = null;
      }
      update(true);
    };

    const onOnline = async () => {
      // Use ref to avoid duplicate probes
      if (probingRef.current) return;
      try {
        probingRef.current = true;
        const ok = await probe();
        if (ok && alive) {
          // Only delay hide when recovering from offline to online
          const wasOffline = lastRef.current === true;

          if (wasOffline) {
            // Network recovered; delay hide so user can refresh if needed
            const delay = hideDelayRef.current;
            hideTimerRef.current = setTimeout(() => {
              update(false); // Ensure UI is hidden
            }, delay);
          }
        } else {
          update(true);
        }
      } catch (e) {
        update(true);
      } finally {
        probingRef.current = false;
      }
    };

    window.addEventListener("offline", onOffline);
    window.addEventListener("online", onOnline);

    if (navigator.onLine) {
      onOnline();
    } else {
      onOffline();
    }

    return () => {
      alive = false;
      window.removeEventListener("offline", onOffline);
      window.removeEventListener("online", onOnline);
      // Clear hide timer
      if (hideTimerRef.current) {
        clearTimeout(hideTimerRef.current);
        hideTimerRef.current = null;
      }
    };
  }, [probe]);

  return { offline };
};

export type OfflineInfoState = ReturnType<typeof useOfflineInfoScript>;
