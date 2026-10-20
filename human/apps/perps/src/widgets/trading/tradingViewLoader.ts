// Lazy loader for the TradingView Charting Library standalone bundle.
//
// Previously the script was injected globally in `_app.tsx` with
// `strategy="beforeInteractive"`, which parser-blocked every route on
// the ~1 MB bundle even though only the perps trading page renders a
// chart. This loader injects the script tag on demand and caches the
// resolved promise so concurrent Chart mounts share a single download.
//
// SSR-safe: callers must invoke from inside `useEffect` (we treat any
// call without `window` as a no-op rejection).

const TRADING_VIEW_SRC = "/static/charting_library/charting_library.standalone.js";

// We deliberately do NOT redeclare `window.TradingView` here. The
// charting library's runtime shape is declared in `Chart.tsx` (which
// owns the typed widget calls). Reading the global through a local
// type cast keeps this module self-contained without conflicting with
// that declaration.
type WindowWithTV = Window & { TradingView?: unknown };

let inflight: Promise<void> | null = null;

export function loadTradingView(): Promise<void> {
  if (typeof window === "undefined") {
    return Promise.reject(new Error("TradingView loader called outside browser"));
  }
  const w = window as WindowWithTV;
  // Already loaded by an earlier mount (or by a server-rendered tag).
  if (w.TradingView) return Promise.resolve();
  if (inflight) return inflight;

  inflight = new Promise<void>((resolve, reject) => {
    // Some prior mount might already have appended the tag without
    // finishing — reuse it instead of stacking another <script>.
    let script = document.querySelector<HTMLScriptElement>(
      `script[data-tradingview-loader="1"]`,
    );
    const onLoad = () => {
      if (w.TradingView) resolve();
      else reject(new Error("TradingView global missing after load"));
    };
    const onError = () => {
      inflight = null;
      reject(new Error("Failed to load TradingView charting library"));
    };

    if (!script) {
      script = document.createElement("script");
      script.src = TRADING_VIEW_SRC;
      script.async = true;
      script.defer = true;
      script.dataset.tradingviewLoader = "1";
      script.addEventListener("load", onLoad, { once: true });
      script.addEventListener("error", onError, { once: true });
      document.head.appendChild(script);
    } else if (w.TradingView) {
      resolve();
    } else {
      script.addEventListener("load", onLoad, { once: true });
      script.addEventListener("error", onError, { once: true });
    }
  });

  return inflight;
}
