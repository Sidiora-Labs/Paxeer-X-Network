import Image from "next/image";

export type MobileGlobalTab = "markets" | "trade" | "account";

interface MobileBottomNavProps {
  active: MobileGlobalTab;
  onChange: (tab: MobileGlobalTab) => void;
}

const TABS: { key: MobileGlobalTab; label: string; icon: string }[] = [
  { key: "markets", label: "Markets", icon: "/assets/graph.svg" },
  { key: "trade", label: "Trade", icon: "/assets/exchange.svg" },
  { key: "account", label: "Account", icon: "/assets/avatar.svg" },
];

export function MobileBottomNav({ active, onChange }: MobileBottomNavProps) {
  return (
    <nav
      className="fixed bottom-0 left-0 right-0 z-50 flex items-stretch border-t safe-area-bottom md:hidden"
      style={{
        background: "var(--bg)",
        borderColor: "var(--border-soft)",
        height: 48,
      }}
    >
      {TABS.map(({ key, label, icon }) => {
        const isActive = active === key;
        return (
          <button
            key={key}
            type="button"
            onClick={() => onChange(key)}
            className="flex-1 flex items-center justify-center gap-1.5 transition-colors"
            style={{
              color: isActive ? "var(--accent-solid)" : "var(--fg-muted)",
            }}
          >
            <Image
              src={icon}
              alt=""
              width={16}
              height={16}
              className="transition-transform"
              style={{
                filter: isActive
                  ? "brightness(1.4) saturate(1.6)"
                  : "brightness(0.7) saturate(0.3)",
                transform: isActive ? "scale(1.1)" : "scale(1)",
              }}
            />
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                letterSpacing: "0.08em",
                fontWeight: isActive ? 600 : 400,
              }}
            >
              {label}
            </span>
          </button>
        );
      })}
    </nav>
  );
}
