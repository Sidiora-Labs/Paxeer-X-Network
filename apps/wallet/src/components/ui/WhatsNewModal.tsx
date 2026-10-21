'use client';

import { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  ArrowRight,
  ChartNoAxesCombined,
  Globe2,
  Search,
  ShieldCheck,
  Sparkles,
  X,
  Zap,
  type LucideIcon,
} from 'lucide-react';
import { announcementRepository } from '@/platform/storage/repositories';

const CURRENT_VERSION = '1.2.0';

interface WhatsNewItem {
  icon: LucideIcon;
  title: string;
  description: string;
}

const WHATS_NEW: WhatsNewItem[] = [
  { icon: Zap, title: 'Instant Swap', description: 'Swap tokens with the updated route engine' },
  { icon: Search, title: 'Discover Tokens', description: 'Find trending, new, and breakout tokens in one place' },
  { icon: Globe2, title: 'PNS Names', description: 'Register your .pax name and send to human-readable addresses' },
  { icon: ChartNoAxesCombined, title: 'Portfolio Charts', description: 'Track price history and holding performance over time' },
  { icon: ShieldCheck, title: 'Safer Approvals', description: 'Review the account, network, destination, amount, and fee before signing' },
];

export function WhatsNewModal() {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const seen = announcementRepository.read();
    if (seen !== CURRENT_VERSION) setOpen(true);
  }, []);

  const handleDismiss = () => {
    announcementRepository.write(CURRENT_VERSION);
    setOpen(false);
  };

  return (
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-[60] flex items-end justify-center">
          <motion.div
            key="whats-new-backdrop"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="absolute inset-0 bg-black/70 backdrop-blur-sm z-[60]"
            onClick={handleDismiss}
          />
          <motion.div
            key="whats-new-sheet"
            initial={{ y: '100%', opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: '100%', opacity: 0 }}
            transition={{ type: 'spring', stiffness: 280, damping: 28, mass: 0.9 }}
            className="relative w-full max-w-md bg-pax-card rounded-t-3xl overflow-hidden"
            style={{ paddingBottom: 'max(5.5rem, calc(env(safe-area-inset-bottom, 0px) + 5.5rem))' }}
          >
            {/* Handle */}
            <div className="w-10 h-1 rounded-full bg-white/10 absolute top-2.5 left-1/2 -translate-x-1/2" />

            {/* Header */}
            <div className="px-5 pt-7 pb-4 flex items-start justify-between">
              <div className="flex items-center gap-3">
                <div className="w-12 h-12 rounded-2xl bg-pax-accent/10 flex items-center justify-center">
                  <Sparkles className="w-6 h-6 text-pax-accent" />
                </div>
                <div>
                  <p className="text-base font-bold">What's New</p>
                  <p className="text-xs text-pax-muted">Version {CURRENT_VERSION}</p>
                </div>
              </div>
              <button
                onClick={handleDismiss}
                className="p-1.5 rounded-full bg-white/5 press-scale"
              >
                <X className="w-4 h-4 text-pax-muted" />
              </button>
            </div>

            {/* Items */}
            <div className="px-5 space-y-3 mb-5">
              {WHATS_NEW.map((item, i) => (
                <motion.div
                  key={item.title}
                  initial={{ opacity: 0, x: -12 }}
                  animate={{ opacity: 1, x: 0 }}
                  transition={{ delay: i * 0.06, duration: 0.3 }}
                  className="flex items-start gap-3"
                >
                  <item.icon
                    aria-hidden="true"
                    className="w-5 h-5 shrink-0 mt-0.5 text-pax-accent"
                  />
                  <div>
                    <p className="text-sm font-semibold">{item.title}</p>
                    <p className="text-xs text-pax-muted leading-relaxed">{item.description}</p>
                  </div>
                </motion.div>
              ))}
            </div>

            {/* CTA */}
            <div className="px-5">
              <button
                onClick={handleDismiss}
                className="w-full flex items-center justify-center gap-2 py-3.5 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale"
              >
                Get Started
                <ArrowRight className="w-4 h-4" />
              </button>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
