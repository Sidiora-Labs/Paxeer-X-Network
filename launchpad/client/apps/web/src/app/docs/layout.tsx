import Link from 'next/link';
import type { ReactNode } from 'react';
import { DOC_LINKS } from './doc-links';


export default function DocsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="bg-canvas">
      <div className="max-w-5xl mx-auto px-4 sm:px-6 py-8">
        <div className="mb-7">
          <Link
            href="/"
            className="inline-flex items-center gap-1.5 text-xs text-text-faint hover:text-text transition"
          >
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
              <path
                d="M9 2L4 7l5 5"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            Back to Sidiora
          </Link>
        </div>

        <div className="flex flex-col lg:flex-row gap-8">
          <aside className="lg:w-52 flex-shrink-0">
            <nav className="lg:sticky lg:top-8">
              <p className="text-2xs text-text-faint font-bold uppercase tracking-wider mb-3 px-2">
                <Link href="/docs" className="hover:text-text">Documentation</Link>
              </p>
              <div className="space-y-0.5">
                {DOC_LINKS.map(({ label, href }) => (
                  <Link
                    key={href}
                    href={href}
                    className="block text-xs text-text-muted hover:text-text py-1.5 px-2 rounded-lg hover:bg-surface-3 transition"
                  >
                    {label}
                  </Link>
                ))}
              </div>
            </nav>
          </aside>

          <div className="flex-1 min-w-0">{children}</div>
        </div>
      </div>
    </div>
  );
}
