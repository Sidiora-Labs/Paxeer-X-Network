/** Preset dropdown; use inside GridPresetProvider. */
import React from "react";
import { useGridPresetContext } from "./GridPresetContext";

export interface GridLayoutSwitcherProps {
  className?: string;
  style?: React.CSSProperties;
  "aria-label"?: string;
}

export function GridLayoutSwitcher({
  className,
  style,
  "aria-label": ariaLabel = "Grid layout preset",
}: GridLayoutSwitcherProps): React.ReactElement | null {
  const ctx = useGridPresetContext();
  if (!ctx || ctx.presets.length === 0) return null;

  return (
    <select
      className={className}
      style={style}
      aria-label={ariaLabel}
      value={ctx.selectedPresetId}
      onChange={(e) => ctx.setSelectedPresetId(e.target.value)}
    >
      {ctx.presets.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </select>
  );
}
