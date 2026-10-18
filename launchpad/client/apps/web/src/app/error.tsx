'use client';

import { useEffect } from 'react';
import { motion } from 'framer-motion';
import { TriangleAlert } from 'lucide-react';

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error('Unhandled error:', error);
  }, [error]);

  return (
    <div className="flex min-h-[70vh] flex-col items-center justify-center gap-6 p-8" role="alert">
      <motion.div
        initial={{ scale: 0.8, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={{ type: 'spring', stiffness: 200, damping: 20 }}
        className="flex h-20 w-20 items-center justify-center rounded-lg bg-danger-soft text-danger"
      >
        <TriangleAlert size={36} aria-hidden="true" />
      </motion.div>

      <motion.div
        initial={{ y: 16, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ delay: 0.1 }}
        className="space-y-2 text-center"
      >
        <h2 className="text-xl font-bold text-text">Something went wrong</h2>
        <p className="max-w-sm text-sm text-text-muted">
          Your funds are safe. Try again or go home
        </p>
      </motion.div>

      <motion.div
        initial={{ y: 16, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ delay: 0.2 }}
        className="flex gap-3"
      >
        <button
          type="button"
          onClick={reset}
          className="h-control-height-md rounded-sm bg-accent px-4 text-base font-bold text-on-accent transition-colors hover:bg-accent-hover active:bg-accent-active"
        >
          Try again
        </button>
        <button
          type="button"
          onClick={() => { window.location.href = '/'; }}
          className="h-control-height-md rounded-sm bg-surface-2 px-4 text-base font-bold text-text transition-colors hover:bg-surface-hover active:bg-surface-active"
        >
          Go home
        </button>
      </motion.div>
    </div>
  );
}
