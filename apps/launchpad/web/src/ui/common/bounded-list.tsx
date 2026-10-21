'use client';
import { useState, type ReactNode, type Key } from 'react';
import { listWindow } from '@/core/app-foundation/window';
interface VirtualListProps<T> {
  items: T[]; estimateSize?: number; renderItem: (item: T, index: number) => ReactNode;
  maxHeight?: number; emptyMessage?: string; className?: string;
  getItemKey?: (item: T, index: number) => Key;
}
function defaultKey(item: unknown, index: number): Key {
  if (item && typeof item === 'object') {
    const row = item as Record<string, unknown>;
    for (const field of ['id', 'poolAddress', 'pool_address', 'address', 'tokenAddress']) {
      if (typeof row[field] === 'string' || typeof row[field] === 'number') return row[field] as Key;
    }
  }
  return index;
}
/** Fixed-height rows. Custom identities use getItemKey; legacy address rows remain stable. */
export function VirtualList<T>({ items, estimateSize = 40, renderItem, maxHeight = 500, emptyMessage = 'No items', className, getItemKey = defaultKey }: VirtualListProps<T>) {
  const [offset, setOffset] = useState(0);
  const window = listWindow(items.length, estimateSize, maxHeight, offset);
  if (!items.length) return <div role="status" className="p-4 text-center text-xs text-text-disabled">{emptyMessage}</div>;
  return <div data-foundation-bounded-list role="list" aria-label="Results" tabIndex={0} className={`overflow-y-auto ${className ?? ''}`} style={{ height: Math.min(maxHeight, window.totalHeight), overflowAnchor: 'none' }} onScroll={e => setOffset(e.currentTarget.scrollTop)}>
    <div style={{ height: window.totalHeight, position: 'relative', width: '100%' }}>
      {items.slice(window.start, window.end).map((item, localIndex) => {
        const index = window.start + localIndex;
        return <div role="listitem" aria-posinset={index + 1} aria-setsize={items.length} key={getItemKey(item, index)} data-index={index} style={{ position: 'absolute', top: index * estimateSize, height: estimateSize, width: '100%', overflow: 'hidden' }}>{renderItem(item, index)}</div>;
      })}
    </div>
  </div>;
}
