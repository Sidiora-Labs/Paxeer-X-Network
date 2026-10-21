import { useEffect, useRef, useState } from "react";

/**
 * Smoothly interpolate a numeric value toward a target using requestAnimationFrame
 * with an ease-out-cubic curve. Returns the current in-flight value.
 *
 * Ported from src/components/design/shared.jsx:53-72 (Phase 1, 2026-04-23)
 * — converted to TS, cleaned up raf cleanup, stable ref handling.
 *
 * The effect re-anchors whenever `target` changes, so chained updates compose:
 * each new target animates from wherever the previous animation left off.
 *
 * @param target   The value to animate toward.
 * @param duration Animation duration in milliseconds. Default 600.
 * @returns The current interpolated value (re-renders on each frame).
 *
 * @example
 * const portfolioValue = useAnimatedNumber(realValue, 900);
 * return <div>{fmt(portfolioValue)}</div>;
 */
export function useAnimatedNumber(target: number, duration = 600): number {
  const [val, setVal] = useState<number>(target);
  const fromRef = useRef<number>(target);
  const startRef = useRef<number>(0);

  useEffect(() => {
    // Anchor the starting value at whatever `val` is right now.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    fromRef.current = val;
    startRef.current = performance.now();
    let raf = 0;

    const tick = (t: number) => {
      const e = Math.min(1, (t - startRef.current) / duration);
      const eased = 1 - Math.pow(1 - e, 3); // ease-out-cubic
      setVal(fromRef.current + (target - fromRef.current) * eased);
      if (e < 1) raf = requestAnimationFrame(tick);
    };

    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
    // `val` is intentionally excluded — the effect captures its current value
    // only at the moment `target` changes, which is the designed behavior.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target, duration]);

  return val;
}
