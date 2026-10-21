'use client';

import { cn } from '@/lib/cn';
import { SvgIcon } from '@/components/ui/SvgIcon';
import type { AppRoute } from '@/widgets/shell/useAppRoute';

interface BottomNavProps {
  active: AppRoute;
  onNavigate: (route: AppRoute) => void;
}

const tabs: { id: AppRoute; label: string; icon: string }[] = [
  { id: 'portfolio', label: 'Wallet', icon: 'wallet' },
  { id: 'transactions', label: 'Activity', icon: 'clock' },
  { id: 'swap', label: 'Swap', icon: 'swap' },
  { id: 'discover', label: 'Discover', icon: 'compass' },
  { id: 'settings', label: 'Settings', icon: 'settings' },
];

function haptic() {
  if (typeof navigator !== 'undefined' && 'vibrate' in navigator) {
    navigator.vibrate(8);
  }
}

export function BottomNav({ active, onNavigate }: BottomNavProps) {
  return (
    <nav className="fixed bottom-0 inset-x-0 glass-nav safe-area-pb z-50">
      <div className="flex justify-around items-center h-16 max-w-lg mx-auto px-2">
        {tabs.map(({ id, label, icon }) => {
          const isActive = active === id;
          return (
            <button
              key={id}
              onClick={() => { haptic(); onNavigate(id); }}
              className={cn(
                'flex flex-col items-center justify-center gap-0.5 py-1 px-2 min-w-[48px] transition-all press-scale',
                isActive ? 'text-pax-accent' : 'text-pax-muted',
              )}
            >
              <SvgIcon
                name={icon}
                className="w-5 h-5"
                style={isActive ? { filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' } : { filter: 'brightness(0) invert(0.6)' }}
              />
              <span className="text-[10px] font-medium">{label}</span>
            </button>
          );
        })}
      </div>
    </nav>
  );
}
