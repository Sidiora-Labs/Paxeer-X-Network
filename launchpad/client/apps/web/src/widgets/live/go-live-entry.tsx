'use client';
import { useState } from 'react';
import { Radio } from 'lucide-react';
import { useAccount } from 'wagmi';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { Button, ModalFrame, StatusBadge } from '@/ui/primitives';
import { CreatorConsoleView } from './creator-console';
import { isPoolCreator, useCreatorStream } from './use-creator-stream';

export interface GoLiveEntryProps {
  /** Pool or token address; the snapshot resolves it to the pool the broadcast belongs to. */
  poolAddress: string;
  className?: string;
}

/** Go live button plus console dialog. Renders nothing unless the connected wallet is the pool creator. */
export function GoLiveEntry({ poolAddress, className }: GoLiveEntryProps) {
  const wallet = useAccount();
  const snapshot = usePoolSnapshot(poolAddress);
  const data = snapshot.data;
  if (!wallet.isConnected || !data || !isPoolCreator(wallet.address, data.creator)) return null;
  return <CreatorEntry poolAddress={data.pool.poolAddress} className={className} />;
}

function CreatorEntry({ poolAddress, className }: { poolAddress: string; className?: string }) {
  const [open, setOpen] = useState(false);
  const stream = useCreatorStream(poolAddress);
  const live = stream.operation?.status === 'live';
  return (
    <>
      <Button
        variant={live ? 'secondary' : 'primary'}
        icon={live ? undefined : <Radio />}
        onClick={() => setOpen(true)}
        aria-haspopup="dialog"
        aria-expanded={open}
        className={className}
      >
        {live ? <StatusBadge status="live" dot size="sm" /> : null}
        {live ? 'Manage stream' : 'Go live'}
      </Button>
      <ModalFrame
        open={open}
        onClose={() => setOpen(false)}
        title="Go live"
        subtitle="Stream to everyone trading this coin"
        size="lg"
        closeLabel="Close"
      >
        <CreatorConsoleView poolAddress={poolAddress} stream={stream} />
      </ModalFrame>
    </>
  );
}

export default GoLiveEntry;
