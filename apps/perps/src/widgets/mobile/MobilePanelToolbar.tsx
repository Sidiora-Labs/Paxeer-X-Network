interface MobilePanelToolbarProps {
  sortValue?: string;
  onSortChange?: (value: string) => void;
  onExpandAll?: () => void;
}

export function MobilePanelToolbar({ sortValue = "Market", onSortChange, onExpandAll }: MobilePanelToolbarProps) {
  return (
    <div
      className="flex items-center justify-between px-3 py-2 md:hidden"
      style={{
        background: "var(--bg)",
        borderBottom: "1px solid var(--border-soft)",
      }}
    >
      <div className="flex items-center gap-1.5">
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            color: "var(--fg-subtle)",
            letterSpacing: "0.06em",
          }}
        >
          Sort by
        </span>
        <select
          value={sortValue}
          onChange={(e) => onSortChange?.(e.target.value)}
          className="rounded px-1.5 py-0.5 text-xs outline-none"
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            color: "var(--fg)",
            background: "var(--bg-subtle)",
            border: "1px solid var(--border-soft)",
          }}
        >
          <option value="Market">Market</option>
          <option value="PnL">PnL</option>
          <option value="Size">Size</option>
          <option value="Time">Time</option>
        </select>
      </div>
      <button
        onClick={onExpandAll}
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          color: "var(--accent-solid)",
          letterSpacing: "0.04em",
        }}
      >
        Expand All Outcomes
      </button>
    </div>
  );
}
