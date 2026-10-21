'use client';

import { motion } from 'framer-motion';
import { WifiOff, RefreshCw, AlertTriangle, Clock } from 'lucide-react';

type NetworkErrorVariant = 'offline' | 'api' | 'ratelimit' | 'timeout';

interface NetworkErrorScreenProps {
  variant?: NetworkErrorVariant;
  onRetry?: () => void;
  retryLabel?: string;
  message?: string;
  countdownSeconds?: number;
  fullScreen?: boolean;
}

const VARIANT_CONFIG: Record<
  NetworkErrorVariant,
  { icon: React.ComponentType<{ className?: string }>; title: string; subtitle: string; iconClass: string; bgClass: string }
> = {
  offline: {
    icon: WifiOff,
    title: 'No connection',
    subtitle: 'Check your internet connection and try again.',
    iconClass: 'text-amber-400',
    bgClass: 'bg-amber-500/10',
  },
  api: {
    icon: AlertTriangle,
    title: 'Something went wrong',
    subtitle: 'Could not load data. Cached info may be shown.',
    iconClass: 'text-red-400',
    bgClass: 'bg-red-500/10',
  },
  ratelimit: {
    icon: Clock,
    title: 'Too many requests',
    subtitle: 'Please wait a moment before trying again.',
    iconClass: 'text-amber-400',
    bgClass: 'bg-amber-500/10',
  },
  timeout: {
    icon: Clock,
    title: 'Request timed out',
    subtitle: 'The server took too long to respond.',
    iconClass: 'text-pax-muted',
    bgClass: 'bg-white/[0.06]',
  },
};

export function NetworkErrorScreen({
  variant = 'api',
  onRetry,
  retryLabel = 'Try Again',
  message,
  countdownSeconds,
  fullScreen = false,
}: NetworkErrorScreenProps) {
  const cfg = VARIANT_CONFIG[variant];
  const Icon = cfg.icon;

  const content = (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35, ease: [0.22, 1, 0.36, 1] }}
      className="flex flex-col items-center justify-center text-center px-6 py-12 gap-4"
    >
      <div className={`w-16 h-16 rounded-2xl flex items-center justify-center ${cfg.bgClass}`}>
        <Icon className={`w-7 h-7 ${cfg.iconClass}`} />
      </div>

      <div className="space-y-1">
        <p className="text-base font-semibold text-white/80">{cfg.title}</p>
        <p className="text-sm text-pax-muted max-w-[260px] leading-relaxed">
          {message || cfg.subtitle}
        </p>
        {countdownSeconds !== undefined && countdownSeconds > 0 && (
          <p className="text-xs font-mono text-amber-400 mt-1">
            Retry in {countdownSeconds}s
          </p>
        )}
      </div>

      {onRetry && (!countdownSeconds || countdownSeconds === 0) && (
        <button
          onClick={onRetry}
          className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-white/[0.08] text-sm font-medium press-scale hover:bg-white/[0.12] transition-colors"
        >
          <RefreshCw className="w-4 h-4" />
          {retryLabel}
        </button>
      )}
    </motion.div>
  );

  if (fullScreen) {
    return (
      <div className="fixed inset-0 z-40 flex items-center justify-center bg-pax-bg">
        {content}
      </div>
    );
  }

  return content;
}

// ── Inline mini error banner (for list refresh failures) ────────────────────
export function ErrorBanner({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <motion.div
      initial={{ opacity: 0, height: 0 }}
      animate={{ opacity: 1, height: 'auto' }}
      exit={{ opacity: 0, height: 0 }}
      className="mx-4 my-2 px-4 py-2.5 rounded-xl bg-red-500/8   flex items-center gap-3"
    >
      <AlertTriangle className="w-4 h-4 text-red-400 shrink-0" />
      <p className="flex-1 text-xs text-red-300">{message}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="text-xs text-red-400 font-medium press-scale shrink-0"
        >
          Retry
        </button>
      )}
    </motion.div>
  );
}
