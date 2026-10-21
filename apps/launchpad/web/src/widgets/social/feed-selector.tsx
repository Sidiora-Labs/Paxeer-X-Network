'use client';
import { ChevronDown, Clock, Coins, Flame } from 'lucide-react';
import { DropdownMenu } from '@/ui/primitives';

export type CalloutFeedKind = 'hot' | 'latest' | 'coin';

export const feedTitles: Record<CalloutFeedKind, string> = {
  hot: 'Hot callouts',
  latest: 'Latest callouts',
  coin: 'This coin',
};

export interface FeedSelectorProps {
  value: CalloutFeedKind;
  onChange: (value: CalloutFeedKind) => void;
  /** Whether the "This coin" feed is offered (only on a coin page). */
  coinAvailable: boolean;
  className?: string;
}

/** Title-caret feed switcher for the callouts panel (components.md 14, feed selector variant). */
export function FeedSelector({ value, onChange, coinAvailable, className }: FeedSelectorProps) {
  const items = [
    { id: 'hot', label: feedTitles.hot, icon: <Flame />, selected: value === 'hot', onSelect: () => onChange('hot') },
    { id: 'latest', label: feedTitles.latest, icon: <Clock />, selected: value === 'latest', onSelect: () => onChange('latest') },
    ...(coinAvailable ? [{ id: 'coin', label: feedTitles.coin, icon: <Coins />, selected: value === 'coin', onSelect: () => onChange('coin') }] : []),
  ];
  return (
    <DropdownMenu
      label="Choose callout feed"
      menuLabel="Callout feeds"
      showLabel={false}
      trigger={<><span className="text-md font-bold text-text">{feedTitles[value]}</span><ChevronDown aria-hidden="true" className="size-icon-sm text-text-faint" /></>}
      items={items}
      size="sm"
      triggerClassName="px-1 -ml-1"
      className={className}
    />
  );
}
