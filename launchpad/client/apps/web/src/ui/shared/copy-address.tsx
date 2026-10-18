'use client';

import { useState, type MouseEvent } from 'react';
import { Check, Copy } from 'lucide-react';
import { cn } from '@/lib/utils';
import { shortAddress } from '@/utils/format';

interface CopyAddressProps {
  address: string;
  chars?: number;
  /** `icon` renders only the copy glyph; `text` (default) shows the shortened address with a copy glyph. */
  variant?: 'text' | 'icon';
  className?: string;
}

export default function CopyAddress({ address, chars = 4, variant = 'text', className = '' }: CopyAddressProps) {
  const [copied, setCopied] = useState(false);
  const short = shortAddress(address, chars, chars);

  const handleCopy = async (event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    event.preventDefault();
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  };

  const Icon = copied ? Check : Copy;

  return (
    <button
      type="button"
      onClick={handleCopy}
      title={address}
      aria-label={`Copy address ${short}`}
      className={cn(
        'inline-flex items-center gap-1 rounded-xs font-mono text-xs text-text-faint transition-colors hover:text-text',
        variant === 'icon' && 'size-5 justify-center hover:bg-surface-hover',
        copied && 'text-accent',
        className,
      )}
    >
      {variant === 'text' ? <span aria-hidden="true">{short}</span> : null}
      <Icon aria-hidden="true" className="size-icon-sm" />
      <span className="sr-only" role="status">{copied ? 'Address copied' : ''}</span>
    </button>
  );
}
