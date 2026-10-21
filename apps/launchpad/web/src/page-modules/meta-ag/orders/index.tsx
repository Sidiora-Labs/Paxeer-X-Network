'use client';

import { useState, useCallback } from 'react';
import OrderForm from '@/widgets/meta-ag/order-form';
import OpenOrdersList from '@/widgets/meta-ag/open-orders-list';

export default function MetaAgOrdersPage() {
  // Bumping this counter on a successful place tells OpenOrdersList to
  // refetch its index lists immediately rather than waiting for wagmi's
  // default polling interval. Keeps the placed-→listed loop tight.
  const [refreshCounter, setRefreshCounter] = useState(0);
  const handlePlaced = useCallback(() => {
    setRefreshCounter((n) => n + 1);
  }, []);

  return (
    <div className="min-h-[calc(100vh-64px)] px-4 pt-6 pb-20 max-w-[1100px] mx-auto">
      <div className="mb-5">
        <div className="flex items-baseline gap-3">
          <h1 className="text-lg font-bold text-text">Orders</h1>
        </div>
        <p className="text-xs text-text-faint mt-1">
          Set limit and stop orders. They trade automatically when the price
          hits your trigger
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[420px_1fr] gap-5">
        <div>
          <OrderForm onPlaced={handlePlaced} />
          <KeeperHealthHint />
        </div>
        <div>
          <OpenOrdersList refreshSignal={refreshCounter} />
        </div>
      </div>
    </div>
  );
}

function KeeperHealthHint() {
  return (
    <div className="mt-3 px-3 py-2.5 rounded-xl bg-surface-2 text-2xs text-text-faint space-y-1">
      <div className="font-bold uppercase tracking-wider text-text mb-1">How it works</div>
      <p>
        Your order waits on-chain until the price hits your trigger, then it
        trades automatically. Cancel anytime, your funds stay locked in the
        order until it trades
      </p>
      <div className="pt-1.5 mt-1.5 flex justify-between">
        <span>Order bot</span>
        <span className="font-mono text-text">0xaA39…36aB</span>
      </div>
      <div className="flex justify-between">
        <span>Orders contract</span>
        <span className="font-mono text-text">0x16bC…5f6E</span>
      </div>
    </div>
  );
}
