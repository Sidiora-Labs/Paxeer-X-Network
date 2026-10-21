'use client';

/**
 * Social pill row — website / X / Telegram. Source URLs come from public
 * metadata; the X and Telegram values may be either raw handles or full URLs.
 */

import { Globe } from 'lucide-react';
import { SocialPill } from './Atoms';

export interface SocialLinksProps {
  website?: string | null;
  twitter?: string | null;
  telegram?: string | null;
}

const ensureUrl = (value: string, prefix: string): string =>
  value.startsWith('http') ? value : `${prefix}${value}`;

export function SocialLinks({ website, twitter, telegram }: SocialLinksProps) {
  if (!website && !twitter && !telegram) return null;

  return (
    <div className="col-span-2 flex items-center gap-2 flex-wrap">
      {website && (
        <SocialPill
          icon={<Globe className="w-3.5 h-3.5" />}
          label="Website"
          href={website}
        />
      )}
      {twitter && (
        <SocialPill
          icon={<span className="text-xs font-bold">X</span>}
          label="X"
          href={ensureUrl(twitter, 'https://x.com/')}
        />
      )}
      {telegram && (
        <SocialPill
          icon={<span className="text-xs">TG</span>}
          label="Telegram"
          href={ensureUrl(telegram, 'https://t.me/')}
        />
      )}
    </div>
  );
}
