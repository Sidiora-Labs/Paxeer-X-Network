'use client';

import { useState } from 'react';
import Image from 'next/image';
import { Flame, Zap, Sparkles, BarChart3, Activity, RefreshCw } from 'lucide-react';
import { useRankingsQuery, type RankingCategory } from '@/lib/queries/rankings';
import type { AppRoute } from '@/widgets/shell/useAppRoute';
import { openExternalUrl, openExplorerPath } from '@/lib/security/navigation';
import { DiscoverRankingSkeleton } from '@/components/ui/Skeletons';
import { RankedTokenList } from './RankedTokenList';
import { DiscoverCarousel } from './DiscoverCarousel';
import Link from 'next/link';
import { SURFACE_ROUTES } from '@/surfaces/routes';

const BANNER_SLIDES = [
  { src: '/wallet/1c56896f-202b-4da0-a145-5e469abf0f85.png', alt: 'Paxeer Banner 1' },
  { src: '/wallet/6751fc7c-8b71-48f5-b454-c34299955eb3.png', alt: 'Paxeer Banner 2' },
];

interface DiscoverWidgetProps {
  onNavigate: (route: AppRoute) => void;
  onTokenTrade?: (poolAddress: string, symbol?: string) => void;
}

const RANKING_TABS: { id: RankingCategory; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { id: 'trending', label: 'Trending', icon: Flame },
  { id: 'breakout', label: 'Breakout', icon: Zap },
  { id: 'new', label: 'New', icon: Sparkles },
  { id: 'top_volume', label: 'Volume', icon: BarChart3 },
  { id: 'movers', label: 'Movers', icon: Activity },
];

export function DiscoverWidget({ onNavigate, onTokenTrade }: DiscoverWidgetProps) {
  const [rankingCategory, setRankingCategory] = useState<RankingCategory>('trending');

  const { data: ranked = [], isFetching, isError, refetch } = useRankingsQuery(rankingCategory);

  return (
    <div className="grid grid-cols-2 gap-2.5 px-3 pt-3 pb-24">

      <div className="col-span-2 px-1 pt-1">
        <h3 className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">Apps</h3>
      </div>

      <button onClick={() => openExternalUrl('https://www.kindlelaunch.com')} className="row-span-2 bg-pax-surface rounded-[20px] p-4 flex flex-col justify-between text-left press-scale min-h-[200px]">
        <div className="w-[48px] h-[48px] rounded-2xl overflow-hidden shrink-0">
          <Image src="/wallet/Kindle-Launch-logo-dark.webp" alt="Sidiora.Fun" width={48} height={48} className="w-full h-full object-contain" />
        </div>
        <div className="mt-auto pt-3">
          <p className="text-base font-extrabold leading-tight">KindleLaunch</p>
          <p className="text-[11px] text-pax-muted mt-1">Launch and trade tokens</p>
        </div>
      </button>

      <button onClick={() => openExplorerPath()} className="bg-pax-surface rounded-[20px] p-4 flex flex-col justify-between text-left press-scale min-h-[95px]">
        <div className="w-[36px] h-[36px] rounded-xl overflow-hidden shrink-0">
          <Image src="/wallet/paxscan.svg" alt="PaxScan" width={36} height={36} className="w-full h-full object-contain" />
        </div>
        <div className="mt-auto pt-2"><p className="text-[13px] font-bold">PaxScan</p></div>
      </button>

      <button onClick={() => onNavigate('pns')} className="bg-pax-surface rounded-[20px] p-4 flex flex-col justify-between text-left press-scale min-h-[95px]">
        <div className="w-[36px] h-[36px] rounded-xl overflow-hidden shrink-0">
          <Image src="/wallet/pns.svg" alt="PNS" width={36} height={36} className="w-full h-full object-contain" />
        </div>
        <div className="mt-auto pt-2"><p className="text-[13px] font-bold">PNS</p></div>
      </button>

      <div className="col-span-2 px-1 pt-1">
        <h3 className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">Paxeer X</h3>
      </div>
      {SURFACE_ROUTES.map((route) => (
        <Link key={route.id} href={route.href} className="bg-pax-surface rounded-[20px] p-4 flex flex-col justify-between text-left press-scale min-h-[80px]">
          <p className="text-[13px] font-bold">{route.label}</p>
          <p className="text-[11px] text-pax-muted mt-1">{route.description}</p>
        </Link>
      ))}

      <DiscoverCarousel slides={BANNER_SLIDES} />

      <div className="col-span-2 flex items-center gap-2 px-1 pt-1">
        <Flame className="w-4 h-4 text-orange-400" />
        <h3 className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">Discover Tokens</h3>
      </div>

      <div className="col-span-2 flex gap-1.5 overflow-x-auto no-scrollbar">
        {RANKING_TABS.map((tab) => {
          const active = rankingCategory === tab.id;
          const TabIcon = tab.icon;
          return (
            <button key={tab.id} onClick={() => setRankingCategory(tab.id)}
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-[11px] font-medium whitespace-nowrap press-scale transition-all ${
                active ? 'bg-pax-accent/15 text-pax-accent' : 'bg-white/5 text-pax-muted'
              }`}>
              <TabIcon className="w-3 h-3" />
              {tab.label}
            </button>
          );
        })}
      </div>

      {isError && ranked.length === 0 ? (
        <div className="col-span-2 flex flex-col items-center justify-center py-10 gap-3 text-center">
          <p className="text-sm text-pax-muted">Could not load token rankings</p>
          <button
            onClick={() => refetch()}
            className="flex items-center gap-2 px-4 py-2 rounded-xl bg-white/[0.06] text-xs font-medium press-scale"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            Retry
          </button>
        </div>
      ) : isFetching && ranked.length === 0 ? (
        <div className="col-span-2">
          <DiscoverRankingSkeleton rows={6} />
        </div>
      ) : (
        <RankedTokenList ranked={ranked} loading={false} onTokenTrade={onTokenTrade} />
      )}
    </div>
  );
}
