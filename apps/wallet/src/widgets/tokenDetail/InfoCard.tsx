'use client';

/**
 * Info card — name, symbol, network, contract (with copy), decimals, supply,
 * market cap, holders, created-at. Last block of static info on the page.
 */

import { useState } from 'react';
import { shortenAddress, formatUsd, formatCompactNumber, formatCompactUsd } from '@/lib/format';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { SectionLabel, InfoRow } from './Atoms';

export interface InfoCardProps {
  isPax: boolean;
  tokenId: string;
  tokenName: string;
  tokenSymbol: string;
  tokenDecimals: number;
  totalSupply: number | null;
  marketCap: number;
  holderCount: number;
  createdAt: number | null;
}

export function InfoCard({
  isPax,
  tokenId,
  tokenName,
  tokenSymbol,
  tokenDecimals,
  totalSupply,
  marketCap,
  holderCount,
  createdAt,
}: InfoCardProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard rejected — silent fail */
    }
  };

  return (
    <>
      <div className="col-span-2 px-1 pt-1">
        <SectionLabel text="Info" />
      </div>
      <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
        <InfoRow label="Name" value={<span className="font-semibold">{tokenName}</span>} />
        <InfoRow label="Symbol" value={<span className="font-semibold">{tokenSymbol}</span>} />
        <InfoRow label="Network" value={<span className="font-semibold">Paxeer</span>} />
        {!isPax && tokenId && (
          <InfoRow
            label="Contract"
            value={
              <button
                onClick={() => handleCopy(tokenId)}
                className="flex items-center gap-1.5 press-scale"
              >
                <span className="font-mono font-semibold">{shortenAddress(tokenId, 5)}</span>
                {copied ? (
                  <SvgIcon
                    name="check"
                    className="w-3.5 h-3.5"
                    style={{
                      filter:
                        'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)',
                    }}
                  />
                ) : (
                  <SvgIcon
                    name="copy"
                    className="w-3.5 h-3.5"
                    style={{ filter: 'brightness(0) invert(0.5)' }}
                  />
                )}
              </button>
            }
          />
        )}
        {!isPax && (
          <InfoRow label="Decimals" value={<span className="font-semibold">{tokenDecimals}</span>} />
        )}
        {totalSupply != null && totalSupply > 0 && (
          <InfoRow
            label="Total Supply"
            value={<span className="font-semibold">{formatCompactNumber(totalSupply)}</span>}
          />
        )}
        {marketCap > 0 && (
          <InfoRow
            label="Market Cap"
            value={
              <span className="font-semibold">
                {marketCap >= 1000 ? formatCompactUsd(marketCap) : formatUsd(marketCap)}
              </span>
            }
          />
        )}
        {holderCount > 0 && (
          <InfoRow
            label="Holders"
            value={<span className="font-semibold">{holderCount.toLocaleString()}</span>}
          />
        )}
        {createdAt && (
          <InfoRow
            label="Created"
            value={
              <span className="font-semibold">
                {new Date(createdAt * 1000).toLocaleDateString('en-US', {
                  year: 'numeric',
                  month: 'short',
                  day: 'numeric',
                })}
              </span>
            }
          />
        )}
      </div>
    </>
  );
}
