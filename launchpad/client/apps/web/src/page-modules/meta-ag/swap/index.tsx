import BestRouteSwapPanel from '@/widgets/meta-ag/best-route-swap';
import Link from 'next/link';

export default function MetaAgSwapModule() {
  return (
    <div className="min-h-[calc(100vh-64px)] flex flex-col items-center px-4 pt-6 pb-20 gap-6">
      <div className="w-full max-w-[480px]">
        <div className="flex items-baseline justify-between mb-1">
          <h1 className="text-lg font-bold text-text">Swap</h1>
          <Link
            href="/swap"
            className="text-2xs text-text-faint hover:text-text transition underline-offset-2 hover:underline"
          >
            Classic swap →
          </Link>
        </div>
        <p className="text-xs text-text-faint mb-5">
          Finds the best price across Sidiora coins and the vault
        </p>
      </div>
      <BestRouteSwapPanel />
      <div className="w-full max-w-[480px] text-2xs text-text-faint space-y-1">
        <div className="flex justify-between">
          <span>Swap contract</span>
          <span className="font-bold text-text">0x732A…6449</span>
        </div>
      </div>
    </div>
  );
}
