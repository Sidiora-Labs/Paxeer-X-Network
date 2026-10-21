import type { Metadata } from 'next';

export const metadata: Metadata = { title: 'Do Not Sell My Data — Sidiora' };

export default function DoNotSellPage() {
  return (
    <article className="prose-legal">
      <h1>Do Not Sell or Share My Personal Information</h1>
      <p className="updated">Last Updated: 02 May 2026</p>

      <p>This page is provided pursuant to the California Consumer Privacy Act of 2018 (&ldquo;CCPA&rdquo;), as amended by the California Privacy Rights Act of 2020 (&ldquo;CPRA&rdquo;), and other applicable privacy laws that provide individuals with the right to opt out of the sale or sharing of their personal information.</p>
      <p>Sidiora is committed to respecting the privacy rights of all users, including California residents. This page describes how you can exercise your right to opt out.</p>

      <Section title="What Does 'Sell' or 'Share' Mean?">
        <p>Under the CCPA/CPRA, &ldquo;sell&rdquo; means disclosing personal information to a third party for monetary or other valuable consideration. &ldquo;Share&rdquo; means disclosing personal information to a third party for cross-context behavioural advertising purposes, whether or not for monetary consideration.</p>
        <p>We do not currently sell your personal information to third parties for monetary consideration. However, certain data practices — such as the use of analytics and advertising cookies — may qualify as &ldquo;sharing&rdquo; personal information under the CCPA/CPRA. This page gives you the ability to opt out of such practices.</p>
      </Section>

      <Section title="Your Rights">
        <p>If you are a California resident (or a resident of another state with similar opt-out rights), you have the right to:</p>
        <ul>
          <li><strong>Opt out of the sale or sharing of your personal information</strong> with third parties for cross-context behavioural advertising or other purposes as defined by applicable law;</li>
          <li><strong>Know</strong> what categories of personal information we collect, use, disclose, and share;</li>
          <li><strong>Delete</strong> personal information we have collected about you, subject to certain exceptions;</li>
          <li><strong>Correct</strong> inaccurate personal information we hold about you;</li>
          <li><strong>Non-discrimination</strong> — we will not discriminate against you for exercising any of your CCPA/CPRA rights.</li>
        </ul>
      </Section>

      <Section title="How to Opt Out">
        <p>To opt out of the sale or sharing of your personal information, or to exercise any of the other rights described above, please contact us through one of the following methods:</p>
        <ul>
          <li><strong>Telegram Support:</strong>{' '}
            <a
              href="https://t.me/Sidiorafunsupport"
              className="text-accent underline"
              target="_blank"
              rel="noopener noreferrer"
            >
              https://t.me/Sidiorafunsupport
            </a>
          </li>
        </ul>
        <p>Please include &ldquo;Do Not Sell/Share My Data&rdquo; in the subject line or the beginning of your message, along with sufficient information to identify you (such as your wallet address or registered email) so we can process your request.</p>
        <p>We will respond to verifiable consumer requests within 45 days, as required by applicable law. If we require additional time (up to a further 45 days), we will inform you of the reason and extension period in writing.</p>
      </Section>

      <Section title="Cookie-Based Opt Out">
        <p>Some sharing of personal information occurs through cookies and similar tracking technologies. You can manage your cookie preferences at any time:</p>
        <ul>
          <li>Use the cookie consent banner displayed when you first visit the Sidiora Platform;</li>
          <li>Adjust your browser settings to block or delete cookies — see your browser&rsquo;s &ldquo;Help&rdquo; section for instructions;</li>
          <li>Visit{' '}
            <a
              href="https://www.allaboutcookies.org"
              className="text-accent underline"
              target="_blank"
              rel="noopener noreferrer"
            >
              allaboutcookies.org
            </a>{' '}
            for more information on managing cookies.</li>
        </ul>
        <p>Please be aware that opting out of certain cookies may affect the functionality of the Sidiora Platform.</p>
      </Section>

      <Section title="Blockchain Data">
        <p>Please note that certain data associated with your use of the Sidiora Platform may be recorded on public blockchains. This on-chain data (such as wallet addresses and transaction history) is inherently public and immutable. We are not able to delete or modify on-chain records, and this data does not fall within the scope of your CCPA/CPRA opt-out rights as it is not data that we &ldquo;sell&rdquo; or &ldquo;share&rdquo; — it is broadcast to a public network by the nature of blockchain technology.</p>
      </Section>

      <Section title="Additional Information">
        <p>For more information about how we collect, use, and share your personal information, please review our{' '}
          <a href="/docs/privacy" className="text-accent underline">Privacy Policy</a>.
        </p>
        <p>For general questions, feedback, or other privacy-related requests, please contact us through our Support channel at{' '}
          <a
            href="https://t.me/Sidiorafunsupport"
            className="text-accent underline"
            target="_blank"
            rel="noopener noreferrer"
          >
            https://t.me/Sidiorafunsupport
          </a>.
        </p>
      </Section>
    </article>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-8">
      <h2 className="text-base font-bold text-text mb-3 pb-2">
        {title}
      </h2>
      <div className="space-y-3 text-sm text-text-muted leading-relaxed [&_ul]:space-y-1.5 [&_ul]:pl-0 [&_li]:flex [&_li]:items-start [&_li]:gap-2 [&_li]:before:content-['–'] [&_li]:before:text-text-faint [&_li]:before:shrink-0 [&_strong]:text-text [&_a]:text-accent [&_a]:underline">
        {children}
      </div>
    </section>
  );
}
