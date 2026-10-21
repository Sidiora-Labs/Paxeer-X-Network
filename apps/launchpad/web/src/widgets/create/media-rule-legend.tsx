'use client';

import { Crop, FileImage } from 'lucide-react';
import { cn } from '@/lib/utils';
import { MEDIA_RULES } from './media-validation';

export function MediaRuleLegend({ className }: { className?: string }) {
  return (
    <dl className={cn('grid gap-3 text-xs text-text-faint sm:grid-cols-2', className)}>
      <div className="flex gap-2">
        <FileImage aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0" />
        <div>
          <dt className="font-bold text-text-muted">File size and type</dt>
          <dd>{MEDIA_RULES.format}</dd>
          <dd>{MEDIA_RULES.size}</dd>
          <dd>{MEDIA_RULES.video}</dd>
        </div>
      </div>
      <div className="flex gap-2">
        <Crop aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0" />
        <div>
          <dt className="font-bold text-text-muted">Resolution and aspect ratio</dt>
          <dd>{MEDIA_RULES.shape}</dd>
          <dd>{MEDIA_RULES.resolution}</dd>
        </div>
      </div>
    </dl>
  );
}
