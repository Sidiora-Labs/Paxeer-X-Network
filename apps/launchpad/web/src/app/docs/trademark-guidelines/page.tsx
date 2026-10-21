import type { Metadata } from 'next';

export const metadata: Metadata = { title: 'Trademark Guidelines — Sidiora' };

export default function TrademarkGuidelinesPage() {
  return (
    <article className="prose-legal">
      <h1>Trademark Guidelines</h1>
      <p className="updated">Last Updated: 02 May 2026</p>

      <p>These Trademark Guidelines describe the rules for using Sidiora&rsquo;s trademarks, logos, and brand assets (&ldquo;Sidiora Marks&rdquo;). These guidelines apply to anyone who wishes to reference or use the Sidiora Marks in connection with their own products, services, communications, or publications.</p>

      <Section title="Our Trademarks">
        <p>The following are trademarks owned by Sidiora (&ldquo;Sidiora Marks&rdquo;):</p>
        <ul>
          <li>The word marks &ldquo;Sidiora&rdquo; and &ldquo;Sidiora.fun&rdquo;;</li>
          <li>The Sidiora logo (the offwhite wordmark and icon variants);</li>
          <li>Any other logos, slogans, or brand assets associated with the Sidiora Platform or Sidiora Services.</li>
        </ul>
        <p>All Sidiora Marks are the exclusive property of the Sidiora Entities and their affiliates. These guidelines do not grant you any ownership interest in the Sidiora Marks.</p>
      </Section>

      <Section title="Permitted Uses">
        <p>You may use the Sidiora Marks only in the following circumstances and only in strict compliance with these guidelines:</p>
        <ul>
          <li><strong>Referential use:</strong> You may use the Sidiora Marks to accurately refer to the Sidiora Platform or Sidiora Services (e.g., &ldquo;Built on Sidiora&rdquo; or &ldquo;Integrated with Sidiora.fun&rdquo;), provided the reference is truthful and not misleading.</li>
          <li><strong>Press and editorial use:</strong> Journalists, analysts, and media outlets may use the Sidiora Marks when writing about the Sidiora Platform for informational or editorial purposes.</li>
          <li><strong>Community content:</strong> Community members may use the Sidiora Marks to discuss, promote, or educate others about the Sidiora Platform, provided there is no suggestion of official affiliation, endorsement, or sponsorship unless expressly authorised in writing by Sidiora.</li>
        </ul>
      </Section>

      <Section title="Prohibited Uses">
        <p>You may not use the Sidiora Marks in any way that:</p>
        <ul>
          <li>Suggests or implies that your product, service, project, or organisation is officially affiliated with, endorsed by, sponsored by, or otherwise approved by Sidiora, unless you have received prior written authorisation from Sidiora;</li>
          <li>Could be likely to cause confusion with the Sidiora Platform or Sidiora Services, or with any of the Sidiora Entities or affiliates;</li>
          <li>Disparages, defames, or tarnishes Sidiora or the Sidiora Marks;</li>
          <li>Is used in connection with any illegal, fraudulent, deceptive, or harmful activity;</li>
          <li>Incorporates the Sidiora Marks into your own product or company name, logo, domain name, social media handle, or username in a manner likely to cause confusion;</li>
          <li>Modifies, distorts, or creates derivative versions of any Sidiora Mark without prior written consent;</li>
          <li>Uses the Sidiora Marks in any way that violates these guidelines, our Terms of Service, or Applicable Law.</li>
        </ul>
      </Section>

      <Section title="Logo Usage">
        <p>When using the Sidiora logo you must:</p>
        <ul>
          <li>Use only official, unmodified versions of the Sidiora logo as provided by Sidiora;</li>
          <li>Maintain clear space around the logo — do not crowd it with other design elements;</li>
          <li>Not alter the logo&rsquo;s colours, proportions, orientation, or composition;</li>
          <li>Not place the logo on backgrounds that reduce its legibility or distinctiveness;</li>
          <li>Not animate, distort, or add effects to the logo without express written consent.</li>
        </ul>
      </Section>

      <Section title="Third-Party Tokens and Projects">
        <p>If you are a token creator or project builder using the Sidiora Platform, you may indicate that your project was &ldquo;launched on Sidiora&rdquo; or &ldquo;built on Sidiora.fun&rdquo; in a plain, factual manner. You may not, however:</p>
        <ul>
          <li>Imply that Sidiora endorses, backs, or is affiliated with your token or project;</li>
          <li>Use the Sidiora name or logo as part of your token name, project name, or brand identity in a way that is confusing or misleading;</li>
          <li>Make any representations about the Sidiora Entities or affiliates in connection with your project without prior written consent.</li>
        </ul>
      </Section>

      <Section title="Requesting Permission">
        <p>If you would like to use the Sidiora Marks in a manner not expressly permitted by these guidelines — including for co-branding, partnerships, merchandise, or official integrations — please contact us through our Support channel at{' '}
          <a href="https://t.me/Sidiorafunsupport" className="text-accent underline" target="_blank" rel="noopener noreferrer">
            https://t.me/Sidiorafunsupport
          </a>.
        </p>
        <p>Any permission granted will be subject to additional terms and conditions determined by Sidiora in its sole discretion, and may be revoked at any time.</p>
      </Section>

      <Section title="Enforcement">
        <p>Sidiora reserves the right to enforce these Trademark Guidelines to the fullest extent permitted by law. Unauthorised use of the Sidiora Marks may result in a demand to cease and desist, legal action for trademark infringement, and/or suspension or termination of your access to the Sidiora Platform and Sidiora Services.</p>
        <p>These Trademark Guidelines do not constitute a waiver of any rights that the Sidiora Entities may have with respect to the Sidiora Marks under applicable trademark law, contract law, or any other area of law.</p>
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
