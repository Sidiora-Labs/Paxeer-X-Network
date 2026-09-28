'use client';

import { motion } from 'framer-motion';
import { X, CheckCircle2, AlertTriangle, Info } from 'lucide-react';
import { cn } from '@/lib/cn';
import type { ToasterToast } from './use-toast';

interface ToastItemProps {
  toast: ToasterToast;
  onClose: () => void;
}

const VARIANT_STYLES: Record<NonNullable<ToasterToast['variant']>, string> = {
  default: 'bg-[var(--color-surface-overlay)]',
  success: 'bg-emerald-950',
  destructive: 'bg-red-950',
};

const VARIANT_ICON = {
  default: Info,
  success: CheckCircle2,
  destructive: AlertTriangle,
} as const;

const VARIANT_ICON_CLASS: Record<NonNullable<ToasterToast['variant']>, string> = {
  default: 'text-pax-accent',
  success: 'text-pax-success',
  destructive: 'text-pax-error',
};

export function ToastItem({ toast, onClose }: ToastItemProps) {
  const variant = toast.variant ?? 'default';
  const Icon = VARIANT_ICON[variant];

  return (
    <motion.div
      layout
      initial={{ opacity: 0, y: -12, scale: 0.96 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, x: 24, scale: 0.96, transition: { duration: 0.18 } }}
      transition={{ type: 'spring', stiffness: 320, damping: 28 }}
      className={cn(
        'pointer-events-auto flex items-start gap-2.5 rounded-2xl px-3.5 py-3 shadow-2xl backdrop-blur-sm',
        VARIANT_STYLES[variant],
      )}
      role={variant === 'destructive' ? 'alert' : 'status'}
    >
      <Icon className={cn('mt-0.5 w-4 h-4 shrink-0', VARIANT_ICON_CLASS[variant])} />
      <div className="flex-1 min-w-0 space-y-0.5">
        {toast.title && (
          <div className="text-[13px] font-semibold leading-tight text-white">
            {toast.title}
          </div>
        )}
        {toast.description && (
          <div className="text-[11px] leading-snug text-pax-muted break-words">
            {toast.description}
          </div>
        )}
        {toast.action && <div className="pt-1">{toast.action}</div>}
      </div>
      <button
        type="button"
        onClick={onClose}
        aria-label="Dismiss notification"
        className="p-1 -mr-1 rounded-md text-pax-muted hover:text-white hover:bg-white/[0.06] transition-colors press-scale"
      >
        <X className="w-3.5 h-3.5" />
      </button>
    </motion.div>
  );
}
