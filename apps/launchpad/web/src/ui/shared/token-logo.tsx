'use client';

import { useState, useEffect } from 'react';
import { metadataApiUrl } from '@/core/sdk-config';

interface TokenLogoProps {
  tokenAddress: string;
  symbol?: string;
  size?: number;
  className?: string;
}

export default function TokenLogo({ tokenAddress, symbol, size = 32, className = '' }: TokenLogoProps) {
  const [src, setSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!tokenAddress) return;
    setSrc(metadataApiUrl(`/logo/${tokenAddress}.png`));
  }, [tokenAddress]);

  if (!src) {
    return (
      <img
        src="/shadcn.png"
        alt={symbol || ''}
        width={size}
        height={size}
        className={`rounded-full object-cover ${className}`}
      />
    );
  }

  return (
    <img
      src={src}
      alt={symbol || ''}
      width={size}
      height={size}
      className={`rounded-full object-cover ${className}`}
      onError={() => setSrc(null)}
    />
  );
}
