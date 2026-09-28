import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'Privacy Policy — Paxport',
  description: 'Privacy policy for the Paxport browser extension and web application.',
};

export default function PrivacyPolicyPage() {
  return (
    <div className="min-h-screen bg-pax-bg text-white">
      <div className="max-w-2xl mx-auto px-6 py-16">
        <h1 className="text-3xl font-bold mb-2">Privacy Policy</h1>
        <p className="text-sm text-gray-400 mb-10">Last updated: March 15, 2026</p>

        <div className="space-y-8 text-sm leading-relaxed text-gray-300">
          <section>
            <h2 className="text-lg font-semibold text-white mb-3">1. Introduction</h2>
            <p>
              Paxport (&quot;we&quot;, &quot;our&quot;, or &quot;the Extension&quot;) is a self-custody
              cryptocurrency wallet for Paxeer Network. This privacy policy explains what data
              the Extension collects, how it is used, and your rights regarding that data.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">2. Data We Collect</h2>
            <p className="mb-3">
              Paxport is designed with privacy as a core principle. We collect the absolute
              minimum data necessary for the wallet to function.
            </p>
            <h3 className="text-base font-medium text-white mb-2">2.1 Data Stored Locally on Your Device</h3>
            <ul className="list-disc list-inside space-y-1.5 ml-2">
              <li><strong>Encrypted wallet data</strong> — Your private keys and recovery phrase are encrypted with a PIN-derived key using AES-256 and stored exclusively in your browser wallet vault. They never leave your device.</li>
              <li><strong>Account addresses</strong> — Your public wallet addresses are stored locally to display balances and transaction history.</li>
              <li><strong>Contacts</strong> — Saved wallet addresses you choose to label for convenience.</li>
              <li><strong>Connected sites</strong> — A list of website origins you have approved for wallet connection.</li>
              <li><strong>Session state</strong> — A timestamp tracking your session for auto-lock functionality.</li>
            </ul>

            <h3 className="text-base font-medium text-white mb-2 mt-4">2.2 Data Transmitted to External Services</h3>
            <ul className="list-disc list-inside space-y-1.5 ml-2">
              <li><strong>Blockchain RPC requests</strong> — To read balances and send transactions, the Extension sends your public wallet address to the Paxeer Network RPC node (<code>public-rpc.paxeer.app</code>). This is required for any blockchain wallet to function.</li>
              <li><strong>Portfolio and price data</strong> — Your public wallet address is sent to our indexer API to fetch token balances, transaction history, and price data. No private keys or personal information are transmitted.</li>
              <li><strong>Block explorer queries</strong> — Your public address may be sent to PaxScan (<code>paxscan.paxeer.app</code>) when viewing transaction details.</li>
            </ul>

            <h3 className="text-base font-medium text-white mb-2 mt-4">2.3 Data We Do NOT Collect</h3>
            <ul className="list-disc list-inside space-y-1.5 ml-2">
              <li>We do <strong>not</strong> collect your private keys, recovery phrase, or PIN.</li>
              <li>We do <strong>not</strong> collect analytics, telemetry, or usage tracking data.</li>
              <li>We do <strong>not</strong> collect your browsing history, cookies, or personal information.</li>
              <li>We do <strong>not</strong> use any third-party analytics or advertising SDKs.</li>
              <li>We do <strong>not</strong> transmit data to any party other than the blockchain RPC and indexer APIs listed above.</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">3. How Your Data Is Used</h2>
            <ul className="list-disc list-inside space-y-1.5 ml-2">
              <li>Encrypted wallet data is used solely to derive signing keys when you authorize a transaction.</li>
              <li>Public addresses are used to query the blockchain for balances and transaction history.</li>
              <li>Connected site origins are used to determine which websites may request wallet actions.</li>
              <li>Session timestamps are used to auto-lock the wallet after 15 minutes of inactivity.</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">4. Data Storage and Security</h2>
            <ul className="list-disc list-inside space-y-1.5 ml-2">
              <li>All sensitive data (private keys, mnemonic) is encrypted with AES-256 before storage.</li>
              <li>Encryption keys are derived from your PIN using PBKDF2 with a unique salt, durable attempt throttling, and optional device-bound biometric protection.</li>
              <li>Data is stored in <code>chrome.storage.local</code>, which is sandboxed to the extension and inaccessible to websites.</li>
              <li>No data is stored on our servers. The wallet is entirely self-custody.</li>
              <li>The Extension&apos;s Content Security Policy restricts script execution to trusted sources only.</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">5. Third-Party Services</h2>
            <p>The Extension communicates with the following services, all operated by the Paxeer Network team:</p>
            <div className="mt-3 rounded-xl bg-white/5 overflow-hidden">
              <table className="w-full text-xs">
                <thead>
                  <tr className=" ">
                    <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Service</th>
                    <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Purpose</th>
                    <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Data Sent</th>
                  </tr>
                </thead>
                <tbody className=" divide-white/5">
                  <tr>
                    <td className="px-4 py-2.5 font-mono">public-rpc.paxeer.app</td>
                    <td className="px-4 py-2.5">Blockchain RPC</td>
                    <td className="px-4 py-2.5">Public address, signed transactions</td>
                  </tr>
                  <tr>
                    <td className="px-4 py-2.5 font-mono">paxscan.paxeer.app</td>
                    <td className="px-4 py-2.5">Block explorer / Indexer API</td>
                    <td className="px-4 py-2.5">Public address</td>
                  </tr>
                  <tr>
                    <td className="px-4 py-2.5 font-mono">us-east-1.user-stats.sidiora.exchange</td>
                    <td className="px-4 py-2.5">Portfolio & price data</td>
                    <td className="px-4 py-2.5">Public address</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p className="mt-3">No third-party analytics, advertising, or tracking services are used.</p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">6. Permissions Justification</h2>
            <ul className="list-disc list-inside space-y-1.5 ml-2">
              <li><strong>storage</strong> — Required to persist encrypted wallet data across browser sessions.</li>
              <li><strong>activeTab</strong> — Required to detect the current website&apos;s URL for connection status display.</li>
              <li><strong>scripting</strong> — Required to inject the wallet provider (<code>window.ethereum</code>) into web pages for dApp connectivity.</li>
              <li><strong>sidePanel</strong> — Required to provide an expanded wallet view in Chrome&apos;s side panel.</li>
              <li><strong>alarms</strong> — Required for reliable session timeout in Manifest V3 (service worker timers are unreliable).</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">7. Your Rights</h2>
            <ul className="list-disc list-inside space-y-1.5 ml-2">
              <li><strong>Full control</strong> — You can export your private keys and recovery phrase at any time from Settings.</li>
              <li><strong>Data deletion</strong> — Use &quot;Erase Wallet&quot; in Settings to permanently delete all wallet data from your device.</li>
              <li><strong>Connection management</strong> — You can view and revoke dApp connections at any time from the Connected Sites page.</li>
              <li><strong>No account required</strong> — The wallet does not require registration, email, or any personal information.</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">8. Children&apos;s Privacy</h2>
            <p>
              The Extension is not directed at children under the age of 13. We do not knowingly
              collect personal information from children.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">9. Changes to This Policy</h2>
            <p>
              We may update this privacy policy from time to time. Changes will be reflected by
              updating the &quot;Last updated&quot; date at the top of this page. Continued use of the
              Extension after changes constitutes acceptance of the updated policy.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-white mb-3">10. Contact</h2>
            <p>
              If you have questions about this privacy policy or the Extension&apos;s data practices,
              contact us at <a href="mailto:privacy@paxeer.app" className="text-pax-accent hover:underline">privacy@paxeer.app</a>.
            </p>
          </section>
        </div>

        <div className="mt-16 pt-8   text-center">
          <p className="text-xs text-gray-500">Paxport v1.0.0 — Paxeer Network</p>
        </div>
      </div>
    </div>
  );
}
