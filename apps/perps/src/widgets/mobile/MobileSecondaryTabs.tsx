import { useRef, useEffect } from "react";

const TABS = [
  "Balances",
  "Positions",
  "Outcomes",
  "Open Orders",
  "TWAP",
  "Trade History",
  "Funding",
];

interface MobileSecondaryTabsProps {
  active: string;
  onChange: (tab: string) => void;
}

export function MobileSecondaryTabs({ active, onChange }: MobileSecondaryTabsProps) {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!scrollRef.current) return;
    const el = scrollRef.current.querySelector(`[data-tab="${active}"]`) as HTMLElement | null;
    if (el) el.scrollIntoView({ behavior: "smooth", inline: "center", block: "nearest" });
  }, [active]);

  return (
    <div
      ref={scrollRef}
      className="flex items-stretch overflow-x-auto border-t border-b scrollbar-none md:hidden"
      style={{ borderColor: "var(--border-soft)", WebkitOverflowScrolling: "touch" }}
    >
      {TABS.map((tab) => {
        const isActive = active === tab;
        return (
          <button
            key={tab}
            data-tab={tab}
            type="button"
            onClick={() => onChange(tab)}
            className="shrink-0 px-3 py-2 transition-colors relative whitespace-nowrap"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              letterSpacing: "0.06em",
              color: isActive ? "var(--accent-solid)" : "var(--fg-muted)",
              fontWeight: isActive ? 600 : 400,
            }}
          >
            {tab}
            {isActive && (
              <div
                className="absolute bottom-0 left-2 right-2 h-[2px] rounded-full"
                style={{ background: "var(--accent-solid)" }}
              />
            )}
          </button>
        );
      })}
    </div>
  );
}
