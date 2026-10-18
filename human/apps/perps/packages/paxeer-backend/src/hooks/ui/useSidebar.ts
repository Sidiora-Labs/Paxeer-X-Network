import { create } from "zustand";
import { persist } from "zustand/middleware";

interface SidebarStore {
  /** Collapsed = icon-only rail (48px); expanded = labelled (220px). */
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
  toggle: () => void;
}

/**
 * Desktop sidebar collapse state. Persists across reloads.
 * Not consumed on mobile surfaces.
 */
export const useSidebar = create<SidebarStore>()(
  persist(
    (set) => ({
      collapsed: false,
      setCollapsed: (collapsed) => set({ collapsed }),
      toggle: () => set((s) => ({ collapsed: !s.collapsed })),
    }),
    { name: "paxeer-sidebar-collapsed" }
  )
);
