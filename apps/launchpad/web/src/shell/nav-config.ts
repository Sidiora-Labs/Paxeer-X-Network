import type { LucideIcon } from 'lucide-react';
import {
  House,
  Compass,
  Trophy,
  Radio,
  Target,
  Coins,
  Terminal,
  Gift,
  Star,
  User,
  MessageSquare,
  LifeBuoy,
  Scale,
  Plus,
} from 'lucide-react';

export type NavSection = 'main' | 'signed-in' | 'bottom';
export type NavRequirement = 'public' | 'wallet' | 'session';

export interface NavItem {
  id: string;
  href: string;
  label: string;
  icon: LucideIcon;
  section: NavSection;
  requires: NavRequirement;
  badge?: 'rewards';
  /**
   * Rail grouping inside the main section. The reference rail separates its
   * primary destinations from the secondary ones with a gap; ours does the same
   * with spacing only (no divider line).
   */
  group?: 'primary' | 'secondary';
}

export const navItems: readonly NavItem[] = [
  { id: 'home', group: 'primary', href: '/', label: 'Home', icon: House, section: 'main', requires: 'public' },
  { id: 'explore', group: 'primary', href: '/explore', label: 'Explore', icon: Compass, section: 'main', requires: 'public' },
  { id: 'leaderboard', group: 'primary', href: '/leaderboard', label: 'Leaderboard', icon: Trophy, section: 'main', requires: 'public' },
  { id: 'live', group: 'primary', href: '/live', label: 'Live', icon: Radio, section: 'main', requires: 'public' },
  { id: 'bounties', group: 'secondary', href: '/bounties', label: 'Bounties', icon: Target, section: 'main', requires: 'public' },
  { id: 'holder-rewards', group: 'secondary', href: '/holder-rewards', label: 'Holder rewards', icon: Coins, section: 'main', requires: 'public' },
  { id: 'terminal', group: 'secondary', href: '/terminal', label: 'Terminal', icon: Terminal, section: 'main', requires: 'public' },
  { id: 'rewards', href: '/rewards', label: 'Rewards', icon: Gift, section: 'signed-in', requires: 'wallet', badge: 'rewards' },
  { id: 'watchlist', href: '/watchlist', label: 'Watchlist', icon: Star, section: 'signed-in', requires: 'wallet' },
  { id: 'profile', href: '/profile', label: 'Profile', icon: User, section: 'signed-in', requires: 'wallet' },
  { id: 'chat', href: '/chat', label: 'Chat', icon: MessageSquare, section: 'signed-in', requires: 'session' },
  // Rail bottom utility cluster (help, legal); the sidebar adds its own collapse toggle after these.
  { id: 'docs', href: '/docs', label: 'Help and docs', icon: LifeBuoy, section: 'bottom', requires: 'public' },
  { id: 'legal', href: '/docs/terms', label: 'Terms and privacy', icon: Scale, section: 'bottom', requires: 'public' },
] as const;

export const createItem: NavItem = {
  id: 'create',
  href: '/create',
  label: 'Create',
  icon: Plus,
  section: 'bottom',
  requires: 'wallet',
};

/**
 * Header stack height for the whole app. --shell-top is the CSS default (top
 * bar, plus the ticker from md up) until the stack is measured. The shared
 * tokens --sl-shell-top-total / --spacing-shell-top-total, which pages use for
 * sticky offsets and viewport-height columns, are pointed at it so they match
 * the real stack instead of a fixed 108px that assumed a banner slot.
 */
export const SHELL_TOP_VARS = '[--shell-top:var(--sl-header-height)] md:[--shell-top:calc(var(--sl-header-height)+var(--sl-ticker-height))] [--sl-shell-top-total:var(--shell-top)] [--spacing-shell-top-total:var(--shell-top)]';
