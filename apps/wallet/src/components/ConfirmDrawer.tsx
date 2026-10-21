'use client';

import { useCallback, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Loader2 } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';

interface ConfirmDrawerProps {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  confirmLabel?: string;
  cancelLabel?: string;
  loading?: boolean;
  error?: string;
  children: React.ReactNode;
}

const backdropVariants = {
  hidden: { opacity: 0 },
  visible: { opacity: 1 },
};

const drawerVariants = {
  hidden: {
    y: '100%',
    opacity: 0,
    transition: { type: 'spring', stiffness: 300, damping: 30 },
  },
  visible: {
    y: 0,
    opacity: 1,
    transition: {
      type: 'spring',
      stiffness: 300,
      damping: 30,
      mass: 0.8,
      staggerChildren: 0.07,
      delayChildren: 0.15,
    },
  },
};

const itemVariants = {
  hidden: { y: 20, opacity: 0 },
  visible: {
    y: 0,
    opacity: 1,
    transition: { type: 'spring', stiffness: 300, damping: 30, mass: 0.8 },
  },
};

export function ConfirmDrawer({
  open,
  onClose,
  onConfirm,
  title,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  loading = false,
  error,
  children,
}: ConfirmDrawerProps) {
  // Close on Escape
  const handleKey = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !loading) onClose();
    },
    [onClose, loading],
  );

  useEffect(() => {
    if (open) window.addEventListener('keydown', handleKey);
    return () => window.removeEventListener('keydown', handleKey);
  }, [open, handleKey]);

  return (
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-50 flex items-end justify-center">
          {/* Backdrop */}
          <motion.div
            key="confirm-backdrop"
            variants={backdropVariants}
            initial="hidden"
            animate="visible"
            exit="hidden"
            transition={{ duration: 0.2 }}
            className="absolute inset-0 bg-black/60 backdrop-blur-sm"
            onClick={() => !loading && onClose()}
          />

          {/* Drawer */}
          <motion.div
            key="confirm-drawer"
            variants={drawerVariants}
            initial="hidden"
            animate="visible"
            exit="hidden"
            className="relative w-full max-w-md bg-pax-card rounded-t-3xl p-5 safe-area-pb"
            style={{ paddingBottom: 'max(2rem, calc(env(safe-area-inset-bottom, 0px) + 5rem))' }}
          >
            {/* Handle + header */}
            <motion.div variants={itemVariants} className="flex items-center justify-between mb-4">
              <div className="w-10 h-1 rounded-full bg-white/10 absolute top-2.5 left-1/2 -translate-x-1/2" />
              <h3 className="text-base font-bold">{title}</h3>
              <button
                onClick={() => !loading && onClose()}
                className="p-1.5 rounded-full bg-white/5 press-scale"
                disabled={loading}
              >
                <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
              </button>
            </motion.div>

            {/* Content */}
            <motion.div variants={itemVariants} className="space-y-3 mb-5">
              {children}
            </motion.div>

            {/* Error */}
            {error && (
              <motion.p
                variants={itemVariants}
                className="text-red-400 text-xs mb-3"
              >
                {error}
              </motion.p>
            )}

            {/* Actions */}
            <motion.div variants={itemVariants} className="flex flex-col gap-2.5">
              <button
                onClick={onConfirm}
                disabled={loading}
                className="w-full flex items-center justify-center gap-2 py-3.5 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale disabled:opacity-40 transition-all"
              >
                {loading ? (
                  <Loader2 className="w-5 h-5 animate-spin" />
                ) : (
                  confirmLabel
                )}
              </button>
              <button
                onClick={() => !loading && onClose()}
                disabled={loading}
                className="w-full py-3 rounded-2xl bg-white/5 text-sm font-medium text-pax-muted press-scale disabled:opacity-30 transition-all"
              >
                {cancelLabel}
              </button>
            </motion.div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}

export { itemVariants as drawerItemVariants };
