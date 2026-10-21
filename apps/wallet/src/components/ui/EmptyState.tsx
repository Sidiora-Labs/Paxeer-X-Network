'use client';

import { motion } from 'framer-motion';
import { SvgIcon } from '@/components/ui/SvgIcon';

type EmptyIcon =
  | 'inbox'
  | 'activity'
  | 'search'
  | 'contacts'
  | 'wallet'
  | 'swap'
  | 'bell'
  | 'chart';

const ICON_MAP: Record<EmptyIcon, { name: string; filter: string }> = {
  inbox: { name: 'arrow-up-right', filter: 'brightness(0) invert(0.3)' },
  activity: { name: 'activity', filter: 'brightness(0) invert(0.3)' },
  search: { name: 'search', filter: 'brightness(0) invert(0.3)' },
  contacts: { name: 'user', filter: 'brightness(0) invert(0.3)' },
  wallet: { name: 'wallet', filter: 'brightness(0) invert(0.3)' },
  swap: { name: 'refresh', filter: 'brightness(0) invert(0.3)' },
  bell: { name: 'bell', filter: 'brightness(0) invert(0.3)' },
  chart: { name: 'chart', filter: 'brightness(0) invert(0.3)' },
};

interface EmptyStateProps {
  icon?: EmptyIcon;
  title: string;
  subtitle?: string;
  action?: {
    label: string;
    onClick: () => void;
  };
  className?: string;
  compact?: boolean;
}

export function EmptyState({
  icon = 'inbox',
  title,
  subtitle,
  action,
  className = '',
  compact = false,
}: EmptyStateProps) {
  const ic = ICON_MAP[icon];

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: [0.22, 1, 0.36, 1] }}
      className={`flex flex-col items-center justify-center text-center ${compact ? 'py-10' : 'py-16'} px-6 ${className}`}
    >
      <div
        className={`flex items-center justify-center rounded-2xl bg-white/[0.06] mb-4 ${
          compact ? 'w-12 h-12' : 'w-16 h-16'
        }`}
      >
        <SvgIcon
          name={ic.name}
          className={compact ? 'w-5 h-5' : 'w-7 h-7'}
          style={{ filter: ic.filter }}
        />
      </div>

      <p className={`font-semibold text-white/70 ${compact ? 'text-sm' : 'text-base'}`}>
        {title}
      </p>

      {subtitle && (
        <p className={`text-pax-muted mt-1 max-w-[260px] leading-relaxed ${compact ? 'text-xs' : 'text-sm'}`}>
          {subtitle}
        </p>
      )}

      {action && (
        <button
          onClick={action.onClick}
          className="mt-5 px-5 py-2.5 rounded-xl bg-pax-accent text-black text-sm font-semibold press-scale transition-all hover:brightness-110"
        >
          {action.label}
        </button>
      )}
    </motion.div>
  );
}
