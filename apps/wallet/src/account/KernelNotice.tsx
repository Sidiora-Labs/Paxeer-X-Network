'use client';

import { cn } from '@/lib/cn';
import type { KernelGate } from './hooks';

export function KernelNotice({ gate, className }: { gate: KernelGate; className?: string }) {
    if (gate.open) {
        return (
            <p data-kernel="available" className={cn('text-xs text-pax-muted', className)}>
                The LayerX kernel is available
            </p>
        );
    }
    if (gate.checking) {
        return (
            <p data-kernel="checking" role="status" className={cn('text-xs text-pax-muted', className)}>
                {gate.reason}
            </p>
        );
    }
    return (
        <p data-kernel="unavailable" role="status" className={cn('rounded-xl bg-white/[0.06] px-3 py-2 text-xs text-pax-light', className)}>
            {gate.reason}
        </p>
    );
}
