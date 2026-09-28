import { ConnectButton } from '@/components/ConnectButton';

/**
 * Reference landing page. Shows what a partner Paxeer app integration looks
 * like with the brand v2.0 monochrome system.
 */
export default function Home() {
  return (
    <main className="relative min-h-dvh overflow-hidden">
      <BackgroundGrid />

      <header className="relative z-10 flex items-center justify-between px-8 py-6">
        <div className="flex items-center gap-2.5">
          <PaxeerLogo />
          <span className="text-[15px] tracking-[-0.01em]">Paxeer</span>
          <span className="ml-2 hidden rounded-md border border-neutral-700 bg-neutral-800 px-2 py-0.5 text-[11px] uppercase tracking-wider text-neutral-400 sm:inline">
            Wallet · Demo
          </span>
        </div>
        <ConnectButton />
      </header>

      <section className="relative z-10 mx-auto flex max-w-5xl flex-col items-center px-6 py-24 text-center">
        <span
          className="
            mb-8 rounded-full border border-neutral-700 bg-neutral-800
            px-3 py-1 text-[12px] text-neutral-400
          "
        >
          Universal Embedded Wallet · HyperPaxeer · Chain 125
        </span>
        <h1 className="font-[var(--font-display)] text-[54px] leading-[1.1] tracking-[-0.02em] sm:text-[72px]">
          One sign-in.
          <br />
          One wallet.
          <br />
          <span className="text-neutral-500">Every Paxeer app.</span>
        </h1>
        <p className="mt-8 max-w-xl text-[18px] leading-[1.6] text-neutral-400">
          No seed phrase. No browser extension. No popup. Sign in with email or
          social, get a wallet on HyperPaxeer in under a second, and use the same
          identity across the entire Paxeer Network.
        </p>

        <div className="mt-10 flex items-center gap-3">
          <ConnectButton />
          <a
            href="https://github.com/paxeer-network/paxeer-embedded-wallet"
            target="_blank"
            rel="noreferrer"
            className="
              text-[14px] text-neutral-400
              transition-colors duration-[var(--duration-snappy)]
              ease-[var(--ease-standard)]
              hover:text-neutral-100
            "
          >
            View on GitHub →
          </a>
        </div>
      </section>

      <section className="relative z-10 mx-auto grid max-w-5xl gap-3 px-6 pb-24 sm:grid-cols-3">
        <FeatureCard
          title="Server-side signing"
          body="Keys never reach the browser. AES-256-GCM at rest, decrypted in memory only when a request authenticates."
        />
        <FeatureCard
          title="Same wallet, every app"
          body="One Supabase identity, one EVM address, persisted across every paxeer.network surface."
        />
        <FeatureCard
          title="Drop-in for Next.js"
          body="Three components: ConnectButton, WalletModal, SendTxForm. Copy them straight into your app."
        />
      </section>

      <footer className="relative z-10 mx-auto max-w-5xl px-6 pb-10 text-center text-[12px] text-neutral-500">
        Aligned with Paxeer Brand Identity System v2.0 · 2026
      </footer>
    </main>
  );
}

/* -------------------------------------------------------------------------- */
/* Decorative                                                                  */
/* -------------------------------------------------------------------------- */

function FeatureCard({ title, body }: { title: string; body: string }) {
  return (
    <article
      className="
        rounded-2xl border border-neutral-700 bg-neutral-800
        p-5 text-left
      "
    >
      <h3 className="text-[15px] text-neutral-100">{title}</h3>
      <p className="mt-2 text-[14px] leading-[1.55] text-neutral-400">{body}</p>
    </article>
  );
}

function PaxeerLogo() {
  return (
    <svg width="22" height="22" viewBox="0 0 22 22" fill="none" aria-hidden="true">
      <rect x="1" y="1" width="20" height="20" rx="5" fill="#004CED" />
      <path
        d="M5.5 16.5L16.5 5.5"
        stroke="#FFFFFF"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  );
}

function BackgroundGrid() {
  // Subtle dot field. Brand v2.0 — monochrome, no gradients, no chroma.
  return (
    <div
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 [mask-image:radial-gradient(ellipse_at_center,black_20%,transparent_70%)]"
      style={{
        backgroundImage:
          'radial-gradient(circle at 1px 1px, rgba(255,255,255,0.05) 1px, transparent 0)',
        backgroundSize: '24px 24px',
      }}
    />
  );
}
