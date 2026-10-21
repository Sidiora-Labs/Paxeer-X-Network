"use client";

import { LegalLayout } from "./LegalLayout";
import { useCookieConsent } from "@/hooks/ui/useCookieConsent";

function ConsentControls() {
  const { value, accept, decline } = useCookieConsent();

  return (
    <div className="rounded-xl border border-border bg-surface-1 p-4 my-6">
      <p className="text-xs text-muted-foreground mb-3">
        <strong className="text-foreground">Your current preference: </strong>
        {value === "accepted" && <span className="text-emerald-400">Accepting all cookies</span>}
        {value === "declined" && <span className="text-amber-400">Essential cookies only</span>}
        {value === null && <span className="text-muted-foreground">Not yet set</span>}
      </p>
      <div className="flex gap-2">
        <button
          onClick={decline}
          className="px-4 py-1.5 rounded-lg text-xs font-medium text-muted-foreground border border-border hover:border-foreground/30 hover:text-foreground transition-colors"
        >
          Essential only
        </button>
        <button
          onClick={accept}
          className="px-4 py-1.5 rounded-lg text-xs font-semibold text-white bg-[var(--brand-blue)] hover:bg-[#0046DC] transition-colors"
        >
          Accept all cookies
        </button>
      </div>
    </div>
  );
}

export default function CookiePolicy() {
  return (
    <LegalLayout title="Cookie Policy" lastUpdated="12 May 2026">
      <p>
        This Cookie Policy explains what cookies are, how <strong>Paxeer Network Foundation</strong>{" "}
        ("we", "us", or "our") uses them on the HyperPax Interface, and how you can control them.
        By using the Interface you consent to cookies in accordance with this policy.
      </p>

      <h2>What Are Cookies?</h2>
      <p>
        Cookies are small text files placed on your device by a website when you visit it. They allow
        the site to remember information about your visit, such as your preferences or authentication
        state. Cookies can be "session" cookies (deleted when you close your browser) or "persistent"
        cookies (retained for a set period or until deleted).
      </p>
      <p>
        We also use similar technologies such as <strong>localStorage</strong> and
        {" "}<strong>sessionStorage</strong> in your browser — these work in a similar way to cookies
        and are covered by this policy.
      </p>

      <h2>Categories of Cookies We Use</h2>

      <h2>1. Strictly Necessary Cookies</h2>
      <p>
        These cookies are essential for the Interface to function. They cannot be disabled. They do
        not store any personally identifying information.
      </p>
      <ul>
        <li>
          <strong>Wallet session state.</strong> Stored in localStorage to remember which wallet
          provider you connected and the network you selected, so you don't need to reconnect on
          every page visit.
        </li>
        <li>
          <strong>Theme preference.</strong> Stores your chosen colour theme (dark, light, etc.) in
          localStorage.
        </li>
        <li>
          <strong>Cookie consent preference.</strong> Stores your response to this cookie banner in
          localStorage (key: <code className="text-xs bg-surface-2 px-1.5 py-0.5 rounded">hyperpax_cookie_consent</code>).
        </li>
        <li>
          <strong>Trading settings.</strong> Stores locally-configured preferences such as default
          leverage, slippage tolerance, and interface layout in localStorage.
        </li>
      </ul>

      <h2>2. Performance &amp; Analytics Cookies</h2>
      <p>
        With your consent, we use analytics cookies to understand how users interact with the
        Interface. This helps us prioritise improvements and fix bugs. All data collected is
        anonymised and aggregated — no wallet addresses or identifying information is included.
      </p>
      <ul>
        <li>
          <strong>Page view analytics.</strong> Records which pages and features are visited and for
          how long. Retention: up to 24 months.
        </li>
        <li>
          <strong>Error tracking.</strong> Captures anonymised information about JavaScript errors and
          performance issues to help us diagnose problems.
        </li>
      </ul>
      <p>These cookies are only placed if you click "Accept all cookies" below or in the banner.</p>

      <h2>3. Third-Party Cookies</h2>
      <p>
        Some third-party services embedded in the Interface may set their own cookies:
      </p>
      <ul>
        <li>
          <strong>TradingView.</strong> The charting widget embedded on the trading page may set
          session and preference cookies managed by TradingView. These are subject to TradingView's
          own cookie and privacy policy.
        </li>
        <li>
          <strong>StatusPage.io (Atlassian).</strong> The system status embed script may set cookies
          to cache status data. Subject to Atlassian's privacy policy.
        </li>
        <li>
          <strong>WalletConnect.</strong> If you connect via WalletConnect, their relay service may
          set cookies related to the connection session.
        </li>
      </ul>
      <p>
        We have no control over third-party cookies. You should review the respective providers'
        cookie policies for details.
      </p>

      <h2>Manage Your Preferences</h2>
      <p>
        You can update your cookie consent at any time using the controls below. Changes take effect
        immediately. Selecting "Essential only" will not delete cookies already placed — you may need
        to clear them manually via your browser settings.
      </p>

      <ConsentControls />

      <h2>How to Control Cookies via Your Browser</h2>
      <p>
        Most browsers allow you to refuse or delete cookies through their settings. The links below
        provide guidance for common browsers:
      </p>
      <ul>
        <li><a href="https://support.google.com/chrome/answer/95647" target="_blank" rel="noopener noreferrer">Google Chrome</a></li>
        <li><a href="https://support.mozilla.org/en-US/kb/cookies-information-websites-store-on-your-computer" target="_blank" rel="noopener noreferrer">Mozilla Firefox</a></li>
        <li><a href="https://support.apple.com/guide/safari/manage-cookies-sfri11471/mac" target="_blank" rel="noopener noreferrer">Apple Safari</a></li>
        <li><a href="https://support.microsoft.com/en-us/microsoft-edge/delete-cookies-in-microsoft-edge-63947406" target="_blank" rel="noopener noreferrer">Microsoft Edge</a></li>
      </ul>
      <p>
        Note that disabling cookies may affect the functionality of the Interface, particularly wallet
        connection and trading settings.
      </p>

      <h2>Changes to This Policy</h2>
      <p>
        We may update this Cookie Policy from time to time. The "last updated" date at the top of
        this page reflects the most recent changes. Continued use of the Interface after an update
        constitutes acceptance of the revised policy.
      </p>

      <h2>Contact</h2>
      <p>
        For cookie-related questions, contact us at:{" "}
        <a href="mailto:privacy@paxeer.network">privacy@paxeer.network</a>.
      </p>
    </LegalLayout>
  );
}
