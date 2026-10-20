import { useEffect, useState } from "react";
import { PaxSpinner } from "@/ui/atoms/PaxSpinner";

const SPLASH_SESSION_KEY = "sidiora.splashShown";
const VISIBLE_MS = 3000;
const FADE_MS = 400;

export function SplashScreen() {
  // Render immediately on first mount if the splash has not yet been shown
  // in this browser session. Using lazy init avoids a "flash" from `false`
  // → `true` on mount.
  const [shouldRender, setShouldRender] = useState<boolean>(() => {
    if (typeof window === "undefined") return false;
    try {
      return window.sessionStorage.getItem(SPLASH_SESSION_KEY) !== "1";
    } catch {
      return true;
    }
  });
  const [visible, setVisible] = useState<boolean>(true);

  useEffect(() => {
    if (!shouldRender) return;

    // Mark shown right away so HMR / route changes don't replay.
    try { window.sessionStorage.setItem(SPLASH_SESSION_KEY, "1"); } catch {}

    const fadeTimer = window.setTimeout(() => setVisible(false), VISIBLE_MS);
    const unmountTimer = window.setTimeout(() => setShouldRender(false), VISIBLE_MS + FADE_MS);
    return () => {
      window.clearTimeout(fadeTimer);
      window.clearTimeout(unmountTimer);
    };
  }, [shouldRender]);

  if (!shouldRender) return null;

  return (
    <div
      aria-hidden="true"
      className="fixed inset-0 z-[100] flex items-center justify-center bg-background"
      style={{
        opacity: visible ? 1 : 0,
        transition: `opacity ${FADE_MS}ms ease-out`,
        pointerEvents: visible ? "auto" : "none",
      }}
    >
      <PaxSpinner
        speed={0.9}
        active={visible}
        style={{
          width: "min(80vw, 520px)",
          height: "min(80vh, 520px)",
        }}
      />
    </div>
  );
}
