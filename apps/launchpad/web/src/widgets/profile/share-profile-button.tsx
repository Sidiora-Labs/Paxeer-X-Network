'use client';

import { useState } from 'react';
import { Check, Share2 } from 'lucide-react';
import { toast } from 'sonner';
import { IconButton } from '@/ui/primitives';

export interface ShareProfileButtonProps {
  wallet: string;
  displayName: string | null;
  size?: 'sm' | 'md';
  className?: string;
}

function profileUrl(wallet: string, origin: string): string {
  return `${origin.replace(/\/$/, '')}/profile/${wallet.toLowerCase()}`;
}

export default function ShareProfileButton({ wallet, displayName, size = 'md', className }: ShareProfileButtonProps) {
  const [copied, setCopied] = useState(false);

  async function share() {
    const url = profileUrl(wallet, window.location.origin);
    const title = displayName ? `${displayName} on Sidiora Launch` : 'Profile on Sidiora Launch';
    if (typeof navigator.share === 'function') {
      try {
        await navigator.share({ title, url });
        return;
      } catch (error) {
        if (error instanceof DOMException && error.name === 'AbortError') return;
      }
    }
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      toast.success('Profile link copied');
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Couldn't copy link");
    }
  }

  return (
    <IconButton
      label={copied ? 'Profile link copied' : 'Share profile'}
      icon={copied ? <Check /> : <Share2 />}
      variant="raised"
      size={size}
      onClick={() => { void share(); }}
      className={className}
    />
  );
}
