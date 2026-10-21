import type { Metadata } from 'next';

export const metadata: Metadata = { title: 'DMCA Guidelines — Sidiora' };

export default function DMCAPage() {
  return (
    <article className="prose-legal">
      <h1>DMCA Guidelines</h1>
      <p className="updated">Last Updated: 02 May 2026</p>

      <p>Sidiora respects the intellectual property rights of others and expects users of the Sidiora Platform and Sidiora Services to do the same. In accordance with the Digital Millennium Copyright Act (&ldquo;DMCA&rdquo;) and other applicable intellectual property laws, we will respond to properly submitted notices of alleged copyright infringement that comply with the requirements set out below.</p>

      <Section title="Reporting Copyright Infringement">
        <p>If you believe that content accessible on or through the Sidiora Platform infringes your copyright, you may submit a DMCA takedown notice (&ldquo;Notice&rdquo;) to our designated agent. Your Notice must include all of the following information:</p>
        <ul>
          <li><strong>Identification of the copyrighted work:</strong> A description of the copyrighted work you believe has been infringed, or, if multiple works on a single site are covered by a single notification, a representative list of such works;</li>
          <li><strong>Identification of the infringing material:</strong> A description of the material you claim is infringing and sufficient information to allow us to locate it on the Sidiora Platform (e.g., a URL or specific page reference);</li>
          <li><strong>Your contact information:</strong> Your name, address, telephone number, and email address;</li>
          <li><strong>Good faith statement:</strong> A statement that you have a good faith belief that use of the material in the manner complained of is not authorised by the copyright owner, its agent, or the law;</li>
          <li><strong>Accuracy statement:</strong> A statement that the information in the notification is accurate, and under penalty of perjury, that you are authorised to act on behalf of the copyright owner; and</li>
          <li><strong>Signature:</strong> Your physical or electronic signature (or the signature of a person authorised to act on behalf of the copyright owner).</li>
        </ul>
        <p>Notices that do not meet these requirements may not be acted upon. Please note that under 17 U.S.C. § 512(f), any person who knowingly materially misrepresents that material is infringing may be subject to liability.</p>
      </Section>

      <Section title="Where to Send Notices">
        <p>Please submit your DMCA Notice to our Support team via:</p>
        <ul>
          <li>Telegram Support:{' '}
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
        <p>Please include &ldquo;DMCA Notice&rdquo; in the subject line or the beginning of your message to ensure your notice is routed correctly and handled promptly.</p>
      </Section>

      <Section title="Counter-Notification Procedure">
        <p>If you believe that content you posted was wrongfully removed or disabled as a result of a mistaken or misidentified DMCA Notice, you may submit a counter-notification. Your counter-notification must include:</p>
        <ul>
          <li>Your physical or electronic signature;</li>
          <li>Identification of the content that has been removed or disabled and the location where it appeared before removal;</li>
          <li>A statement under penalty of perjury that you have a good faith belief that the content was removed or disabled as a result of mistake or misidentification;</li>
          <li>Your name, address, and telephone number, and a statement that you consent to the jurisdiction of the federal court in your district, or if outside the United States, any judicial district in which Sidiora may be found, and that you will accept service of process from the person who provided the original DMCA Notice or an agent of such person.</li>
        </ul>
        <p>Upon receipt of a valid counter-notification, we may restore the removed content in our sole discretion, subject to the requirements of the DMCA and any other applicable law. Note that if a copyright owner files legal action against you, the removed content will not be restored until the legal dispute is resolved.</p>
      </Section>

      <Section title="Repeat Infringers">
        <p>In accordance with the DMCA and other applicable law, Sidiora has adopted a policy of terminating, in appropriate circumstances, the access of users who are deemed to be repeat infringers. We may also, at our sole discretion, limit access to the Sidiora Platform and/or terminate access of any user who infringes any intellectual property rights of others, whether or not there is any repeat infringement.</p>
      </Section>

      <Section title="Limitations">
        <p>Please note that the Sidiora Platform enables users to interact with public blockchains. Certain content created or recorded on-chain (such as token names, metadata stored on-chain, or transaction records) may not be removable due to the immutable nature of public distributed ledger technology. We will work with you to the extent technically and legally feasible, but we cannot guarantee the removal of on-chain content.</p>
        <p>These DMCA Guidelines apply only to copyright claims. If you have concerns about other forms of intellectual property infringement (e.g., trademark infringement), please refer to our Trademark Guidelines or contact our Support team directly.</p>
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
