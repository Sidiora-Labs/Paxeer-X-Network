import { LegalLayout } from "./LegalLayout";

export default function TermsOfService() {
  return (
    <LegalLayout title="Terms of Service" lastUpdated="12 May 2026">
      <p>
        These Terms of Service ("Terms") govern your access to and use of the HyperPax interface
        (the "Interface") provided by <strong>Paxeer Network Foundation</strong> ("Foundation", "we",
        "us", or "our"). By accessing or using the Interface, you agree to be bound by these Terms.
        If you do not agree, do not use the Interface.
      </p>
      <p>
        Please read these Terms carefully. They contain important information about your legal rights,
        including mandatory risk disclosures, limitations of liability, and a prohibition on use by
        residents of certain jurisdictions.
      </p>

      <h2>1. The Interface and the Protocol</h2>
      <p>
        The Interface is a web application that provides a graphical front-end through which users may
        interact with the Hyperpax-os decentralised perpetual futures protocol (the "Protocol") deployed
        on the Paxeer Network blockchain (Chain ID 125). The Foundation does not operate, control, or
        have custody over the Protocol. The Protocol consists of open-source, audited smart contracts
        running autonomously on the blockchain.
      </p>
      <p>
        <strong>The Interface is one of many means by which the Protocol can be accessed.</strong> You
        may interact with the Protocol directly via its smart contract addresses, through other
        interfaces, or via the Paxeer Network command-line tools. These Terms apply only to your use
        of this Interface, not to your direct interaction with the Protocol's smart contracts.
      </p>

      <h2>2. Eligibility and Prohibited Use</h2>
      <p>You may only use the Interface if you:</p>
      <ul>
        <li>Are at least 18 years old (or the age of majority in your jurisdiction).</li>
        <li>Have full legal capacity to enter into a binding agreement.</li>
        <li>Are not a resident or citizen of a Restricted Jurisdiction (defined below).</li>
        <li>Are not accessing the Interface on behalf of a person in a Restricted Jurisdiction.</li>
        <li>Are not a Specially Designated National or subject to applicable sanctions lists.</li>
      </ul>
      <p>
        <strong>Restricted Jurisdictions</strong> include, without limitation: the United States of
        America, Cuba, Iran, North Korea, Syria, Russia (in respect of sanctioned activities),
        and any other jurisdiction in which the use of decentralised perpetual trading is prohibited
        or would require regulatory authorisation that has not been obtained. This list may be updated
        from time to time without notice.
      </p>
      <p>
        By using the Interface you represent and warrant that you meet these eligibility requirements.
        We reserve the right to restrict access to the Interface based on geographic or other criteria
        at any time in our sole discretion.
      </p>

      <h2>3. Non-Custodial Nature</h2>
      <p>
        The Interface is strictly non-custodial. We do not take custody, control, or possession of
        your digital assets at any time. Your private keys, seed phrases, and wallet credentials remain
        solely in your possession. We have no ability to recover assets, reverse transactions, or
        freeze funds. You are solely responsible for the security of your wallet.
      </p>

      <h2>4. Risk Disclosures</h2>
      <p>
        Trading perpetual futures contracts involves <strong>substantial risk of loss</strong>. By
        using the Interface you acknowledge and accept the following risks:
      </p>
      <ul>
        <li>
          <strong>Leverage risk.</strong> Perpetual futures may be traded with significant leverage.
          Leveraged positions can result in losses that exceed your deposited margin. You may lose
          your entire deposited collateral.
        </li>
        <li>
          <strong>Liquidation risk.</strong> Positions may be automatically liquidated by the Protocol
          if your margin ratio falls below the maintenance threshold. Liquidation may occur rapidly
          in volatile market conditions.
        </li>
        <li>
          <strong>Smart contract risk.</strong> Despite audits, the Protocol's smart contracts may
          contain bugs, vulnerabilities, or be subject to exploits that could result in total loss of
          funds.
        </li>
        <li>
          <strong>Oracle risk.</strong> The Protocol relies on oracle price feeds that may be
          manipulated, become stale, or diverge from true market prices, resulting in adverse fills
          or liquidations.
        </li>
        <li>
          <strong>Market risk.</strong> Digital asset markets are highly volatile and subject to
          sudden price movements, thin liquidity, and regulatory uncertainty.
        </li>
        <li>
          <strong>Regulatory risk.</strong> The regulatory treatment of decentralised protocols and
          digital assets is uncertain and subject to rapid change. Future regulation may restrict your
          ability to use the Protocol.
        </li>
        <li>
          <strong>Network risk.</strong> The Paxeer Network may experience downtime, congestion, or
          forks that prevent transaction processing or lead to unexpected outcomes.
        </li>
        <li>
          <strong>Frontend risk.</strong> The Interface may become unavailable, be subject to DNS
          attacks, or serve malicious content if its delivery infrastructure is compromised. Always
          verify the URL and consider interacting directly with smart contracts for critical operations.
        </li>
      </ul>
      <p>
        <strong>Nothing on this Interface constitutes financial, investment, legal, or tax advice.</strong>{" "}
        You should consult independent professional advisors before making any trading or investment
        decisions.
      </p>

      <h2>5. No Warranties</h2>
      <p>
        The Interface is provided on an <strong>"AS IS" and "AS AVAILABLE"</strong> basis without
        warranties of any kind, express or implied, including but not limited to warranties of
        merchantability, fitness for a particular purpose, accuracy, completeness, or non-infringement.
        We do not warrant that the Interface will be uninterrupted, error-free, or free of harmful
        components.
      </p>
      <p>
        We make no representations about the accuracy of pricing data, funding rate calculations,
        oracle values, or any other information displayed on the Interface. Market data is provided
        for informational purposes only.
      </p>

      <h2>6. Limitation of Liability</h2>
      <p>
        To the maximum extent permitted by applicable law, the Foundation and its officers, directors,
        employees, agents, and affiliates shall not be liable for:
      </p>
      <ul>
        <li>Any loss of funds resulting from trading activity, liquidation, or smart contract failure.</li>
        <li>Any indirect, incidental, special, consequential, or punitive damages.</li>
        <li>Loss of profits, revenue, data, goodwill, or business opportunities.</li>
        <li>Damages arising from reliance on information displayed on the Interface.</li>
        <li>Failures, delays, or errors in the blockchain, oracles, or third-party services.</li>
      </ul>
      <p>
        In jurisdictions that do not permit the exclusion or limitation of certain liabilities, our
        liability is limited to the maximum extent permitted by law.
      </p>

      <h2>7. Indemnification</h2>
      <p>
        You agree to defend, indemnify, and hold harmless the Foundation and its affiliates from any
        claims, damages, losses, liabilities, costs, or expenses (including reasonable legal fees)
        arising from or related to your use of the Interface, your violation of these Terms, your
        violation of applicable laws, or your infringement of third-party rights.
      </p>

      <h2>8. Intellectual Property</h2>
      <p>
        The Interface, including its design, code (to the extent not open-sourced), trademarks, and
        content, is the property of Paxeer Network Foundation and is protected by applicable
        intellectual property laws. The underlying Protocol smart contracts are open source and
        governed by their respective licences. Nothing in these Terms transfers any intellectual
        property rights to you.
      </p>

      <h2>9. Fees</h2>
      <p>
        The Protocol charges trading fees in accordance with its published fee schedule. The Foundation
        may receive a portion of Protocol fees as described in the tokenomics documentation. Network
        gas fees may also apply. All fees are disclosed within the Interface before you confirm a
        transaction. Fees are non-refundable once a transaction is confirmed on-chain.
      </p>

      <h2>10. Modifications and Termination</h2>
      <p>
        We may modify, suspend, or discontinue the Interface (or any part of it) at any time without
        notice. We may also update these Terms at any time. Continued use of the Interface after an
        update constitutes acceptance of the revised Terms. It is your responsibility to review these
        Terms regularly.
      </p>

      <h2>11. Governing Law and Disputes</h2>
      <p>
        These Terms are governed by and construed in accordance with the laws of the British Virgin
        Islands, without regard to conflict-of-law principles. Any dispute arising from or relating to
        these Terms or the Interface shall be resolved by binding arbitration administered by the BVI
        International Arbitration Centre, with proceedings conducted in English. You waive any right to
        a jury trial or to participate in a class action.
      </p>

      <h2>12. Entire Agreement and Severability</h2>
      <p>
        These Terms, together with the Privacy Policy and Cookie Policy, constitute the entire
        agreement between you and the Foundation regarding the Interface. If any provision of these
        Terms is found to be unenforceable, the remaining provisions shall continue in full force and
        effect. Our failure to enforce any provision shall not be deemed a waiver of our right to do
        so in the future.
      </p>

      <h2>13. Contact</h2>
      <p>
        Questions regarding these Terms may be directed to:{" "}
        <a href="mailto:legal@paxeer.network">legal@paxeer.network</a>.
      </p>
    </LegalLayout>
  );
}
