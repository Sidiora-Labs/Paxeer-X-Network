'use client';

import { Info } from 'lucide-react';
import { Tooltip } from '@/ui/primitives';

/** A focusable info glyph whose tooltip carries the long explainer copy. */
export function InfoTip({ label, content }: { label: string; content: string }) {
  return (
    <Tooltip content={content} delay={150}>
      <button type="button" aria-label={label} className="inline-flex size-4 items-center justify-center rounded-pill text-text-faint hover:text-text focus-visible:outline-2 focus-visible:outline-focus">
        <Info aria-hidden="true" className="size-icon-sm" />
      </button>
    </Tooltip>
  );
}
