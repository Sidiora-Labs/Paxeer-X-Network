import { LegalLayout } from "./LegalLayout";

export default function PrivacyPolicy() {
  return (
    <LegalLayout title="Privacy Policy" lastUpdated="12 May 2026">
      <p>
        This Privacy Policy explains how <strong>Paxeer Network Foundation</strong> ("we", "us", or "our")
        collects, uses, and protects information when you access or use the HyperPax decentralised
        exchange interface (the "Interface") available at hyperpax.io and associated subdomains
        (collectively, the "Services").
      </p>
      <p>
        <strong>HyperPax is a non-custodial protocol.</strong> We do not hold, control, or have access
        to your private keys, funds, or on-chain assets at any time. Transactions are executed
        directly on the Paxeer Network blockchain via smart contracts.
      </p>

      <h2>1. Information We Collect</h2>
      <p>We collect the minimum information necessary to operate the Interface:</p>
      <ul>
        <li>
          <strong>Blockchain address.</strong> When you connect a wallet, your public wallet address is
          read from your wallet provider and used to display your balances, positions, and transaction
          history. Your private key is never transmitted to us.
        </li>
        <li>
          <strong>Usage data.</strong> We collect anonymised information about how the Interface is used,
          including pages visited, features interacted with, browser type, and approximate geographic
          region (country level). This data is used solely to improve the Interface.
        </li>
        <li>
          <strong>Cookies and local storage.</strong> We use essential cookies required for the Interface
          to function and optional analytics cookies if you consent. See our{" "}
          <a href="/cookies">Cookie Policy</a> for full details.
        </li>
        <li>
          <strong>Communications.</strong> If you contact us via email or our community channels, we may
          retain the content of your message and your contact details to respond to you.
        </li>
      </ul>
      <p>
        We do <strong>not</strong> collect names, email addresses, government-issued identification, or
        any other personally identifying information unless you voluntarily provide it. The Interface
        does not require account registration.
      </p>

      <h2>2. On-Chain Data</h2>
      <p>
        All transactions executed through the Interface are recorded on the Paxeer Network public
        blockchain. By nature of blockchain technology, this data is publicly visible, immutable, and
        outside our control. We are not responsible for the privacy implications of publicly visible
        on-chain activity.
      </p>

      <h2>3. How We Use Information</h2>
      <ul>
        <li>Provide, operate, and improve the Interface.</li>
        <li>Diagnose and fix bugs and performance issues.</li>
        <li>Understand how traders and builders use the platform to prioritise features.</li>
        <li>Comply with applicable laws and regulations where required.</li>
        <li>Detect, prevent, and investigate fraudulent or prohibited activity.</li>
      </ul>
      <p>We do not sell, rent, or trade any information about you to third parties for commercial gain.</p>

      <h2>4. Third-Party Services</h2>
      <p>The Interface integrates with the following third-party services that may process data:</p>
      <ul>
        <li>
          <strong>TradingView.</strong> Charting widgets embedded in the Interface. TradingView's own
          privacy policy governs data processed through their charting library.
        </li>
        <li>
          <strong>Pyth Network &amp; Ojo Network (oracle providers).</strong> On-chain price oracles;
          no personal data is transmitted.
        </li>
        <li>
          <strong>Wallet providers (MetaMask, WalletConnect, etc.).</strong> Third-party wallet software
          is subject to each provider's own privacy policy. We receive only the public address you
          authorise.
        </li>
        <li>
          <strong>StatusPage.io.</strong> Used for system status monitoring. A small status embed script
          may load from their CDN. See Atlassian's privacy policy for details.
        </li>
      </ul>

      <h2>5. Data Retention</h2>
      <p>
        Anonymised analytics data is retained for up to 24 months before being aggregated or deleted.
        Cookie consent preferences stored in your browser persist until you clear them or withdraw
        consent. We do not retain wallet addresses or transaction data beyond what is visible on-chain.
      </p>

      <h2>6. International Transfers</h2>
      <p>
        Our analytics infrastructure may process data in jurisdictions outside your home country,
        including the United States and European Union. Where required by law, we implement appropriate
        safeguards such as Standard Contractual Clauses for transfers outside the EEA.
      </p>

      <h2>7. Your Rights</h2>
      <p>
        Depending on your jurisdiction, you may have the following rights with respect to your personal data:
      </p>
      <ul>
        <li><strong>Right of access</strong> — request a copy of the data we hold about you.</li>
        <li><strong>Right to rectification</strong> — request correction of inaccurate data.</li>
        <li><strong>Right to erasure</strong> — request deletion of your personal data.</li>
        <li><strong>Right to restrict processing</strong> — request that we limit how we use your data.</li>
        <li><strong>Right to data portability</strong> — receive your data in a structured, machine-readable format.</li>
        <li><strong>Right to object</strong> — object to processing based on our legitimate interests.</li>
        <li><strong>Right to withdraw consent</strong> — withdraw cookie consent at any time via the Cookie Settings link in the footer.</li>
      </ul>
      <p>
        Because HyperPax is a non-custodial protocol and we do not link wallet addresses to real-world
        identities, exercising some of the above rights may be limited in practice. To make a request,
        contact us at <a href="mailto:privacy@paxeer.network">privacy@paxeer.network</a>.
      </p>

      <h2>8. Security</h2>
      <p>
        We implement commercially reasonable technical and organisational measures to protect the
        information we collect. However, no Internet transmission or electronic storage is perfectly
        secure. We encourage you to take appropriate precautions to protect your wallet credentials and
        private keys, which are entirely within your control.
      </p>

      <h2>9. Children's Privacy</h2>
      <p>
        The Services are not directed to individuals under the age of 18. We do not knowingly collect
        personal information from minors. If you believe a minor has provided us with personal data,
        please contact us immediately.
      </p>

      <h2>10. Changes to This Policy</h2>
      <p>
        We may update this Privacy Policy from time to time. The "last updated" date at the top of this
        page indicates when the most recent changes were made. Continued use of the Interface after a
        policy update constitutes your acceptance of the revised policy.
      </p>

      <h2>11. Contact Us</h2>
      <p>
        For privacy-related enquiries, please contact us at:{" "}
        <a href="mailto:privacy@paxeer.network">privacy@paxeer.network</a>.
      </p>
    </LegalLayout>
  );
}
