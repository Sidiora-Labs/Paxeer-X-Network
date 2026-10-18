export const arenaRuleMap = {
  "120_campaign_rule": {
    rule: [
      {
        content: "**How to participate:**",
        type: "text" as const,
        listStyle: "none" as const, // Use numbers for the 5 steps
        className: "oui-text-[20px] oui-font-semibold",
        children: [
          {
            content:
              "**Join now:** To be eligible for the campaign, kindly enable trading and click 'Join Now' to participate.",
            type: "markdown" as const,
            listStyle: "decimal" as const,
          },
          {
            content:
              "**Get your referral link**: Visit the [referral page](https://woofi.com/en/rewards/affiliate) and share your unique link/code with friends.",
            type: "markdown" as const,
            listStyle: "decimal" as const,
          },
          {
            content:
              "**Successful referrals**: Each time a new user you invite during the campaign period generates a trading volume of at least 1,000 USDC on WOOFi Pro during the campaign, it counts as a successful referral.",
            type: "markdown" as const,
            listStyle: "decimal" as const,
          },
          {
            content:
              "**Win prizes**: The top 10 users with the most successful referrals under their belts will earn rewards. A minimum number of successful referrals is required to qualify for a prize.",
            type: "markdown" as const,
            listStyle: "decimal" as const,
          },
          {
            content:
              "**Tie-breaker**: If there's a tie in referral numbers, the user whose invited friends have a higher total trading volume will rank higher.",
            type: "markdown" as const,
            listStyle: "decimal" as const,
          },
          {
            content: "**Open to all**: This event is open to everyone.",
            type: "markdown" as const,
            listStyle: "decimal" as const,
          },
        ],
      },
      {
        content: "/leaderboard/rule_120.png",
        type: "image" as const,
        listStyle: "none" as const,
        children: [
          {
            content:
              "Note: Should a user meet the qualification criteria for a rank for which all rewards have been allocated, they will automatically be assigned to the subsequent rank and will receive its designated rewards.",
            type: "markdown" as const,
            listStyle: "none" as const, // Use bullet points for bonus details
          },
        ],
      },
      {
        content: "**Welcome bonus**",
        type: "markdown" as const,
        listStyle: "none" as const, // Use bullet points for bonus details
        className: "oui-text-[20px] oui-font-semibold",
        children: [
          {
            content:
              "New users registering on WOOFi during the event with a trading volume of at least 1,000 USDC may receive a $25 USDC reward.",
            type: "text" as const,
          },
          {
            content:
              "The total prize pool is $3,750 USDC, available on a first-come, first-served basis, limited to the first 150 new users.",
            type: "text" as const,
          },
          {
            content:
              "To be eligible as a new user, you must connect your wallet to WOOFi Pro for the first time during the campaign and have no previous history of deposits or trades.",
            type: "text" as const,
          },
        ],
      },
    ],
    terms: [
      {
        content:
          'We reserve the right to modify, suspend, or terminate the Recruit & Reign ("Campaign") at any time without prior notice, including adjustments to the campaign timeline and prize distribution.',
        type: "text" as const,
      },
      {
        content:
          "Participation in the Campaign is open only to WOOFi account holders who are in compliance with the campaign rules. Participants must be 18 years of age or older, or the age of majority in their jurisdiction, whichever is higher.",
        type: "text" as const,
      },
      {
        content:
          "We do not provide warranties of any kind regarding the expected outcomes of the Campaign. Participation is at the user's own risk.",
        type: "text" as const,
      },
      {
        content:
          "Participants agree to indemnify and hold harmless we and our affiliates from any claims, damages, losses, or liabilities arising from or related to their participation in the Campaign.",
        type: "text" as const,
      },
      {
        content:
          "Users are strictly prohibited from manipulating the campaign outcomes. This includes, but is not limited to, registering multiple accounts or engaging in any other fraudulent activity to increase chances of winning. WOOFi reserves the right to disqualify any participant that is found to be tampering with the entry process or the operation of the campaign or violating these Terms and Conditions. WOOFi reserves the right to refuse participation to any applicant or participant at any time at its sole discretion.",
        type: "text" as const,
      },
      {
        content:
          "We reserve the right of final interpretation of all terms and conditions of this campaign. Any decision made by us in relation to this campaign, including but not limited to rule amendments, prize distribution, and eligibility of participants, is final and binding on all participants.",
        type: "text" as const,
      },
      {
        content:
          "The content above is neither a recommendation for investment and trading strategies nor does it constitute an investment offer, solicitation, or recommendation of any product or service. The content is for informational sharing purposes only. Anyone who makes or changes the investment decision based on the content shall undertake the result or loss by himself/herself.",
        type: "text" as const,
      },
      {
        content:
          "The content of this document may be translated into different languages and shared throughout different platforms. In case of any discrepancy or inconsistency between different posts caused by mistranslations, the English version on our official website shall prevail.",
        type: "text" as const,
      },
      {
        content:
          "Cryptocurrencies involve significant risk and may not be suitable for all investors. The value of digital currencies can be extremely volatile, and you should carefully consider your investment objectives, level of experience, and risk appetite before participating in any investment activities.",
        type: "text" as const,
      },
      {
        content:
          "We strongly recommend that you seek independent advice from a qualified professional before making any investment or financial decisions related to cryptocurrencies. We shall in NO case be liable for any loss or damage arising directly or indirectly from the use of or reliance on the information contained in this article.",
        type: "text" as const,
      },
    ],
    ruleConfig: {},
  },
};
