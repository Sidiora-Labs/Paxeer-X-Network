import Image from "next/image";
import { useState } from "react";
import { useRouter } from "next/router";
import { cn } from "@/ui/utils/cn";
import { Menu, X } from "lucide-react";
import { NAV_ITEMS } from "@/core/nav";

export function MobileNavTrigger({ onClick }: { onClick: () => void }) {
  return (
    <button
      className="flex md:hidden items-center justify-center h-8 w-8 rounded-md hover:bg-muted/30 transition-colors"
      onClick={onClick}
      aria-label="Open navigation menu"
    >
      <Menu className="h-4 w-4" />
    </button>
  );
}

export function MobileNavDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const router = useRouter();

  if (!open) return null;

  const navItems = NAV_ITEMS.filter((item) => item.surfaces.includes("mobileDrawer"));

  return (
    <div className="fixed inset-0 z-50 flex md:hidden">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <nav
        aria-label="Main navigation"
        className="relative w-64 bg-surface-1 h-full flex flex-col p-4 animate-slide-in-left"
      >
        {/* Header */}
        <div className="flex items-center justify-between mb-3 shrink-0">
          <span className="text-xs text-muted-foreground uppercase tracking-wider font-medium">Navigation</span>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground transition-colors">
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Nav links */}
        <div className="flex-1 overflow-y-auto space-y-1">
          {navItems.map((item) => (
            <button
              key={item.href}
              className={cn(
                "w-full flex items-center gap-2 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors text-left",
                router.pathname === item.href
                  ? "bg-primary/10 text-primary"
                  : "hover:bg-muted/30"
              )}
              onClick={() => { router.push(item.href); onClose(); }}
            >
              {item.label}
            </button>
          ))}
        </div>

        {/* Brand banner — pinned to bottom */}
        <div className="pt-4 shrink-0">
          <a
            href="https://paxeer.network"
            target="_blank"
            rel="noopener noreferrer"
            className="block rounded-lg overflow-hidden opacity-80 hover:opacity-100 transition-opacity"
          >
            <Image
              src="/image.png"
              alt="HyperPax — From Fast to Instant"
              width={232}
              height={116}
              className="w-full h-auto object-cover rounded-lg"
              priority={false}
            />
          </a>
        </div>
      </nav>
    </div>
  );
}

export function useMobileNav() {
  const [open, setOpen] = useState(false);
  return {
    open,
    openNav: () => setOpen(true),
    closeNav: () => setOpen(false),
  };
}
