import Link from "next/link";
import Script from "next/script";

export function Footer() {
  return (
    <>
      <footer className="border-t border-border py-1.5 px-4 flex items-center justify-between text-[10px] text-muted-foreground">
        {/* StatusPage.io live indicator — script populates .statuspage-status-description
            and sets a colour class on .statuspage-indicator. Falls back to Operational
            until the script resolves. */}
        <a
          href="https://hyperpaxstatus.statuspage.io"
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-1.5 hover:text-foreground transition-colors"
        >
          <span className="statuspage-indicator inline-block w-1.5 h-1.5 rounded-full bg-emerald-500" />
          <span className="statuspage-status-description">Operational</span>
        </a>

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

      <Script
        src="https://hyperpaxstatus.statuspage.io/embed/script.js"
        strategy="afterInteractive"
      />
    </>
  );
}
