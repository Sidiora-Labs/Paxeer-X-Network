'use client';

import { useState, useEffect, useMemo } from 'react';
import { useAccount } from 'wagmi';
import { Coins } from 'lucide-react';
import { useReadQuoterGetPoolsByCreator } from '@/core/network/contracts';
import { formatAddress } from '@/utils/format';
import { metadataApiUrl } from '@/core/sdk-config';
import TxFeed from '@/widgets/trading/tx-feed';
import HoldersPanel from '@/widgets/trading/holders-panel';
import { Button, EmptyState, ErrorState, Skeleton } from '@/ui/primitives';
import { SignedOutState } from '@/ui/shared/signed-out-state';

interface PoolMeta {
  name?: string;
  symbol?: string;
}

export default function MyTokenView() {
  const { address } = useAccount();
  const [selectedPool, setSelectedPool] = useState<string | null>(null);
  const [poolMetas, setPoolMetas] = useState<Record<string, PoolMeta>>({});

  const { data: pools, isPending, isError, error, refetch } = useReadQuoterGetPoolsByCreator(
    { creator: address! },
  );

  const poolList = useMemo<string[]>(() => (Array.isArray(pools) ? pools.map(String) : []), [pools]);

  useEffect(() => {
    poolList.forEach((addr) => {
      if (poolMetas[addr]) return;
      fetch(metadataApiUrl(`/metadata/${addr}`))
        .then((r) => (r.ok ? r.json() : null))
        .then((d) => {
          if (d) setPoolMetas((prev) => ({ ...prev, [addr]: { name: d.name, symbol: d.symbol } }));
        })
        .catch(() => {});
    });
  }, [poolList.length]);

  useEffect(() => {
    if (poolList.length > 0 && !selectedPool) {
      setSelectedPool(poolList[0]);
    }
  }, [poolList, selectedPool]);

  return (
    <div className="p-6 text-text space-y-4">
      <h1 className="text-md font-bold">My coins</h1>

      {!address ? (
        <SignedOutState icon={Coins} title="Sign in to see your coins" body="Coins you launch with your wallet show up here with their holders and trades." className="rounded-lg bg-surface-1" />
      ) : isPending ? (
        <Skeleton variant="rows" rows={3} label="your coins" className="rounded-lg bg-surface-1 p-2" />
      ) : isError ? (
        <ErrorState title="Your coins can't load right now" message="Something went wrong, try again" onRetry={() => { void refetch(); }} className="rounded-lg bg-surface-1" />
      ) : poolList.length === 0 ? (
        <EmptyState
          icon={Coins}
          title="No coins created yet"
          body="Launch a coin and it shows up here with its holders and trades."
          action={<Button href="/create" variant="primary" size="sm">Create coin</Button>}
          className="rounded-lg bg-surface-1"
        />
      ) : (
        <>
          <div className="flex gap-2 overflow-x-auto scrollbar-none pb-2">
            {poolList.map((addr) => {
              const m = poolMetas[addr];
              return (
                <button
                  key={addr}
                  onClick={() => setSelectedPool(addr)}
                  className={`px-3 py-2 rounded-lg text-2xs font-bold whitespace-nowrap transition ${
                    selectedPool === addr
                      ? 'bg-info-soft text-info'
                      : 'bg-surface-2 text-text-faint hover:bg-surface-hover hover:text-text'
                  }`}
                >
                  {m?.symbol || formatAddress(addr, 4)}
                </button>
              );
            })}
          </div>

          {selectedPool && (
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <div className="space-y-4">
                <div className="rounded-lg bg-surface-2 p-4">
                  <h3 className="text-sm font-bold mb-2">Coin info</h3>
                  <div className="grid grid-cols-2 gap-2 text-2xs">
                    <span className="text-text-faint">Address</span>
                    <span className="truncate">{selectedPool}</span>
                    <span className="text-text-faint">Name</span>
                    <span>{poolMetas[selectedPool]?.name || '...'}</span>
                    <span className="text-text-faint">Ticker</span>
                    <span>{poolMetas[selectedPool]?.symbol || '...'}</span>
                  </div>
                </div>
                <HoldersPanel poolAddress={selectedPool} />
              </div>
              <TxFeed poolAddress={selectedPool} />
            </div>
          )}
        </>
      )}
    </div>
  );
}
