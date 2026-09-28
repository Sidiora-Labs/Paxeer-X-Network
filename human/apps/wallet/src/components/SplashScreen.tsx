'use client';

import { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';

// ── Splash ────────────────────────────────────────────────────────────────────

const SPLASH_MS = 3000;
const EXIT_MS = 600;

interface SplashScreenProps {
    children: React.ReactNode;
}

export function SplashScreen({ children }: SplashScreenProps) {
    const [phase, setPhase] = useState<'visible' | 'fading' | 'gone'>('visible');

    useEffect(() => {
        const showTimer = setTimeout(() => setPhase('fading'), SPLASH_MS);
        const goneTimer = setTimeout(() => setPhase('gone'), SPLASH_MS + EXIT_MS);
        return () => { clearTimeout(showTimer); clearTimeout(goneTimer); };
    }, []);

    return (
        <>
            <AnimatePresence>
                {phase !== 'gone' && (
                    <motion.div
                        key="splash"
                        initial={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: EXIT_MS / 1000, ease: [0.22, 1, 0.36, 1] }}
                        className="fixed inset-0 z-[9999] overflow-hidden"
                    >
                        {/* Full-bleed brand image */}
                        <motion.img
                            src="/PAXPORT.png"
                            alt="Paxport"
                            className="absolute inset-0 w-full h-full object-cover"
                            initial={{ opacity: 0, scale: 1.04 }}
                            animate={{ opacity: 1, scale: 1 }}
                            transition={{ duration: 0.7, ease: [0.22, 1, 0.36, 1] }}
                        />

                        {/* Progress bar */}
                        <motion.div
                            className="absolute bottom-14 left-1/2 -translate-x-1/2 h-[2px] rounded-full overflow-hidden"
                            style={{ width: 80, background: 'rgba(255,255,255,0.12)' }}
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            transition={{ delay: 0.6, duration: 0.4 }}
                        >
                            <motion.div
                                className="h-full rounded-full"
                                style={{ background: 'var(--color-action-primary)' }}
                                initial={{ width: '0%' }}
                                animate={{ width: '100%' }}
                                transition={{ delay: 0.8, duration: (SPLASH_MS - 800) / 1000, ease: 'linear' }}
                            />
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>
            {children}
        </>
    );
}
