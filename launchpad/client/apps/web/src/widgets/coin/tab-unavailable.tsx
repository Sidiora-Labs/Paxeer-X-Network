'use client';
import { UnavailableState } from '@/ui/primitives';

/**
 * Frame for a coin tab whose pool snapshot could not be read. The tab keeps its body height (a
 * surface-2 well under the tab strip) with the shared compact notice inside, instead of a full error
 * panel with an icon and prose. The operation and status stay developer detail on the chip.
 */
export default function TabUnavailable({ title, error, onRetry }: { title: string; error: unknown; onRetry: () => void }) {
  const status = (error as { status?: unknown } | null)?.status;
  return (
    <div data-tab-unavailable={title} className="flex min-h-40 items-center justify-center rounded-md bg-surface-2 p-4">
      <UnavailableState variant="chip" wrap surface={2} title={title} plural="auto" operation="IF_POOL_DETAIL" status={typeof status === 'number' ? status : null} onRetry={onRetry} />
    </div>
  );
}
