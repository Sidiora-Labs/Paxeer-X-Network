'use client';

// ── Shared shimmer base ──────────────────────────────────────────────────────
// Uses the .shimmer utility class from globals.css

function Shimmer({ className = '' }: { className?: string }) {
  return <div className={`shimmer rounded-lg ${className}`} />;
}

// ── Portfolio skeleton ───────────────────────────────────────────────────────
export function PortfolioSkeleton() {
  return (
    <div className="px-4 pt-4 space-y-4 animate-fade-in">
      {/* Balance hero */}
      <div className="flex flex-col items-center gap-2 py-6">
        <Shimmer className="h-5 w-28 rounded-full" />
        <Shimmer className="h-10 w-48 rounded-xl" />
        <Shimmer className="h-4 w-24 rounded-full" />
      </div>

      {/* Action buttons */}
      <div className="grid grid-cols-4 gap-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="flex flex-col items-center gap-1.5">
            <Shimmer className="h-11 w-11 rounded-2xl" />
            <Shimmer className="h-3 w-10 rounded-full" />
          </div>
        ))}
      </div>

      {/* Token list */}
      <div className="glass-card overflow-hidden">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 px-4 py-3.5   ">
            <Shimmer className="h-10 w-10 rounded-full shrink-0" />
            <div className="flex-1 space-y-1.5">
              <Shimmer className="h-3.5 w-20 rounded" />
              <Shimmer className="h-2.5 w-14 rounded" />
            </div>
            <div className="flex flex-col items-end gap-1.5">
              <Shimmer className="h-3.5 w-16 rounded" />
              <Shimmer className="h-2.5 w-10 rounded" />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Transaction list skeleton ────────────────────────────────────────────────
export function TransactionListSkeleton({ rows = 8 }: { rows?: number }) {
  return (
    <div className="glass-card overflow-hidden animate-fade-in">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3.5   ">
          <Shimmer className="h-9 w-9 rounded-full shrink-0" />
          <div className="flex-1 space-y-1.5">
            <Shimmer className="h-3.5 w-16 rounded" />
            <Shimmer className="h-2.5 w-24 rounded" />
          </div>
          <div className="flex flex-col items-end gap-1.5">
            <Shimmer className="h-3.5 w-14 rounded" />
            <Shimmer className="h-2.5 w-10 rounded" />
          </div>
        </div>
      ))}
    </div>
  );
}

// ── Token detail skeleton ────────────────────────────────────────────────────
export function TokenDetailSkeleton() {
  return (
    <div className="px-4 pt-4 space-y-4 animate-fade-in">
      {/* Hero */}
      <div className="flex flex-col items-center gap-2 py-4">
        <Shimmer className="h-16 w-16 rounded-full" />
        <Shimmer className="h-4 w-20 rounded" />
        <Shimmer className="h-8 w-36 rounded-xl" />
        <Shimmer className="h-3.5 w-24 rounded" />
      </div>

      {/* Chart placeholder */}
      <Shimmer className="h-40 w-full rounded-2xl" />

      {/* Stats */}
      <div className="glass-card p-4 grid grid-cols-2 gap-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="space-y-1.5">
            <Shimmer className="h-2.5 w-16 rounded" />
            <Shimmer className="h-4 w-24 rounded" />
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Swap quote skeleton ──────────────────────────────────────────────────────
export function SwapQuoteSkeleton() {
  return (
    <div className="space-y-2 animate-fade-in">
      {Array.from({ length: 2 }).map((_, i) => (
        <div key={i} className="glass-card p-3 flex items-center gap-3">
          <Shimmer className="h-8 w-8 rounded-full shrink-0" />
          <div className="flex-1 space-y-1.5">
            <Shimmer className="h-3 w-20 rounded" />
            <Shimmer className="h-2.5 w-28 rounded" />
          </div>
          <Shimmer className="h-5 w-16 rounded" />
        </div>
      ))}
    </div>
  );
}

// ── Contact list skeleton ────────────────────────────────────────────────────
export function ContactListSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="glass-card overflow-hidden animate-fade-in">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3.5   ">
          <Shimmer className="h-10 w-10 rounded-full shrink-0" />
          <div className="flex-1 space-y-1.5">
            <Shimmer className="h-3.5 w-24 rounded" />
            <Shimmer className="h-2.5 w-32 rounded" />
          </div>
        </div>
      ))}
    </div>
  );
}

// ── Discover / token ranking skeleton ───────────────────────────────────────
export function DiscoverRankingSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="space-y-2 animate-fade-in">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="glass-card px-4 py-3 flex items-center gap-3">
          <Shimmer className="h-4 w-4 rounded" />
          <Shimmer className="h-10 w-10 rounded-full shrink-0" />
          <div className="flex-1 space-y-1.5">
            <Shimmer className="h-3.5 w-20 rounded" />
            <Shimmer className="h-2.5 w-14 rounded" />
          </div>
          <div className="flex flex-col items-end gap-1.5">
            <Shimmer className="h-3.5 w-16 rounded" />
            <Shimmer className="h-2.5 w-12 rounded" />
          </div>
        </div>
      ))}
    </div>
  );
}

// ── Single-line text skeleton (inline) ──────────────────────────────────────
export function TextSkeleton({ width = 'w-24', height = 'h-3.5' }: { width?: string; height?: string }) {
  return <div className={`shimmer rounded ${width} ${height}`} />;
}
