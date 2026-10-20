import Link from "next/link";
import Script from "next/script";

function statusPageUrl(): string | null {
  const raw = process.env.NEXT_PUBLIC_STATUS_PAGE_URL;
  if (!raw) return null;
  try {
    const url = new URL(raw);
    return url.protocol === "https:" && url.origin === raw ? raw : null;
  } catch {
    return null;
  }
}

export function Footer() {
  const statusPage = statusPageUrl();
  return (
    <>
      <footer className="border-t border-border py-1.5 px-4 flex items-center justify-between text-[10px] text-muted-foreground">
        {/* StatusPage.io live indicator — script populates .statuspage-status-description
            and sets a colour class on .statuspage-indicator. Falls back to Operational
            until the script resolves. */}
        {statusPage ? (
        <a
          href={statusPage}
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-1.5 hover:text-foreground transition-colors"
        >
          <span className="statuspage-indicator inline-block w-1.5 h-1.5 rounded-full bg-emerald-500" />
          <span className="statuspage-status-description">Operational</span>
        </a>
        ) : (
          <span />
        )}

        <div className="flex items-center gap-3 flex-wrap justify-end">
          <a
            href="https://t.me/paxeernetwork"
            target="_blank"
            rel="noopener noreferrer"
            className="hover:text-foreground transition-colors"
          >
            Join our community
          </a>
          <span className="text-muted-foreground/40">·</span>
          <Link href="/privacy" className="hover:text-foreground transition-colors">
            Privacy Policy
          </Link>
          <span className="text-muted-foreground/40">·</span>
          <Link href="/terms" className="hover:text-foreground transition-colors">
            Terms of Service
          </Link>
          <span className="text-muted-foreground/40">·</span>
          <Link href="/cookies" className="hover:text-foreground transition-colors">
            Cookies
          </Link>
          <span className="text-muted-foreground/40">·</span>
          <span>Charts by <span className="font-medium text-foreground/70">TradingView</span></span>
        </div>
      </footer>

      {statusPage && (
        <Script src={`${statusPage}/embed/script.js`} strategy="afterInteractive" />
      )}
    </>
  );
}
