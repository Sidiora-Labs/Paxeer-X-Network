export default function Loading() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <div role="status" className="flex flex-col items-center gap-4">
        <div className="relative h-10 w-10 animate-spin rounded-full bg-surface-2">
          <div className="absolute left-1/2 top-0 h-3 w-3 -translate-x-1/2 rounded-full bg-accent" />
        </div>
        <p className="text-xs text-text-faint animate-pulse">Loading...</p>
      </div>
    </div>
  );
}
