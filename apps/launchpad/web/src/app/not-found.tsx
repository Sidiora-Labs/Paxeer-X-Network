import Link from 'next/link';

export default function NotFound() {
  return (
    <div className="flex min-h-[70vh] flex-col items-center justify-center gap-6 p-8">
      <div className="relative">
        <div className="text-[120px] font-extrabold leading-none text-surface-3 select-none">
          404
        </div>
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="w-16 h-16 rounded-2xl bg-surface-2 flex items-center justify-center">
            <svg width="28" height="28" viewBox="0 0 28 28" fill="none" className="text-text-faint">
              <circle cx="12" cy="12" r="8" stroke="currentColor" strokeWidth="1.5" />
              <path d="M18 18L24 24" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              <path d="M9 12H15" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </div>
        </div>
      </div>

      <div className="text-center space-y-2">
        <h2 className="text-md font-bold text-text">Page not found</h2>
        <p className="text-xs text-text-faint max-w-xs">
          We couldn't find that page
        </p>
      </div>

      <Link
        href="/"
        className="px-6 py-2.5 rounded-xl bg-accent-soft text-accent font-bold text-sm hover:bg-accent-soft-strong transition-all active:scale-[0.97]"
      >
        Go home
      </Link>
    </div>
  );
}
