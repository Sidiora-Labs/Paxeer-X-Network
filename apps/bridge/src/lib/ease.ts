/**
 * Shared motion curves and springs for DX UI motion components.
 * Cubic-bezier arrays work with `motion/react` transitions; the `_CSS` strings
 * are for plain CSS animations.
 */
export const EASE_OUT = [0.23, 1, 0.32, 1] as const;
export const EASE_IN_OUT = [0.77, 0, 0.175, 1] as const;
export const EASE_DRAWER = [0.32, 0.72, 0, 1] as const;

export const EASE_OUT_CSS = "cubic-bezier(0.23, 1, 0.32, 1)";
export const EASE_IN_OUT_CSS = "cubic-bezier(0.77, 0, 0.175, 1)";
export const EASE_DRAWER_CSS = "cubic-bezier(0.32, 0.72, 0, 1)";

/** Layout and shared-element morphs. */
export const SPRING_LAYOUT = { type: "spring", stiffness: 420, damping: 38, mass: 0.9 } as const;
/** Press / tap feedback — quick and slightly bouncy. */
export const SPRING_PRESS = { type: "spring", stiffness: 600, damping: 30, mass: 0.6 } as const;
/** Panels, sheets and popovers opening. */
export const SPRING_PANEL = { type: "spring", stiffness: 380, damping: 34, mass: 1 } as const;
/** Content swaps — icons, labels, digits. */
export const SPRING_SWAP = { type: "spring", stiffness: 520, damping: 36, mass: 0.7 } as const;
/** Pointer-follow effects (magnetic, tilt). */
export const SPRING_MOUSE = { stiffness: 260, damping: 22, mass: 0.5 } as const;
/** Smoothed values gliding to a target (sliders, charts). */
export const SPRING_GLIDE = { stiffness: 340, damping: 34, mass: 0.6 } as const;
