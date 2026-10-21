/**
 * Plain-language copy for everything the creation flow reports back to the user.
 *
 * The controller, adapters and wallet throw precise internal messages (and viem
 * errors carry RPC detail). None of that is shown: every message is turned into
 * one short sentence here, and the original is logged for diagnostics.
 */

export const GENERIC_LAUNCH_ERROR = 'Something went wrong, try again';

/** Shown for every capability/admission refusal (CapabilityAdmissionError, KnownCapabilityAdmissionError). */
export const CREATION_PAUSED = 'Creating coins is paused right now. Try again in a few minutes';

/** Internal admission wording that must never reach the screen. */
const CAPABILITY_PATTERN = /capabilit|admission|admitted|creation authority|committed creation|reviewed deployment|deployment capability|wallet dispatch|optical schema|pool creation is unavailable|is currently disabled/i;

function isCapabilityError(cause: unknown): boolean {
  let current: unknown = cause;
  for (let depth = 0; depth < 8 && current && typeof current === 'object'; depth++) {
    const name = (current as { name?: unknown }).name;
    if (name === 'KnownCapabilityAdmissionError' || name === 'CapabilityAdmissionError') return true;
    current = (current as { cause?: unknown }).cause;
  }
  return false;
}

/** Status lines the controller sets while a launch moves forward. '' hides the line. */
const statusCopy: Record<string, string> = {
  'Launch parameters changed. Review them again.': '',
  'Wallet connection changed.': '',
  'Invalid first purchase amount.': '',
  'Creation review expired. Fees, balances and gas are refreshed before signing.': '',
  'Wallet account or chain changed. The launch is reviewed again with the creator.': 'Your wallet changed. Try again',
  'Launch dependency is unavailable. Review again before signing.': GENERIC_LAUNCH_ERROR,
  'Launch action failed.': GENERIC_LAUNCH_ERROR,
  'Approve the exact USDL amount. Creation is a separate transaction.': 'Approve the 1 USDL fee in your wallet',
  'Approval confirmed. Review the fresh creation gas and launch details before creating.': 'Fee approved. Now create your coin',
  'Confirm one createMarket transaction in your wallet.': 'Confirm in your wallet',
  'Creation review expired.': 'This took too long. Try again',
  'Submission outcome is unknown. Recover this attempt before starting another.': "We couldn't confirm your coin was created. Check the steps above before trying again",
  'Submission outcome is unknown. Check your wallet transaction history and attach the exact hash.': "We couldn't tell if your wallet sent this. Check your wallet's activity",
  'Checking the saved transaction. No new transaction will be sent.': 'Checking on your coin...',
  'Creation is submitted; pool resolution is delayed. Resume this same attempt.': 'Confirming... this is taking longer than usual',
  'Token created. Checking metadata publication status.': 'Your coin is created. Saving its details...',
  'Token created. Image validation is pending; retry keeps the same image operation.': 'Your coin is created. Your image is still uploading',
  'Pool image approved. Review and publish metadata with this image.': 'Image uploaded. Save your coin details to finish',
  'Metadata service unavailable; token remains created.': "Your coin is live, but its details didn't save yet. Try again",
  'Token created. Reconnect and authenticate the original creator to resume metadata status.': 'Your coin is live. Reconnect the wallet you created it with to finish',
  'First purchase approval confirmed. Request and review a fresh quote.': 'Approved. Get a quote to buy',
  'Review this separate first purchase. Creation is already confirmed.': 'Your coin is live. Check your first buy below',
  'Token created. Purchase outcome is unknown; recover it before retrying.': "Your coin is live. We couldn't confirm your first buy, check it before trying again",
  'Token created. First purchase skipped by your choice.': 'Your coin is live',
  'Purchase slippage changed. Request a new quote.': 'Slippage changed. Get a new quote',
  'A separate new launch is allowed. Review its new intent before signing.': 'You can create a new coin now',
  'Wallet rejected creation. No transaction was submitted.': 'You cancelled in your wallet',
};

/** Ordered: the first matching rule wins. */
const errorRules: ReadonlyArray<readonly [RegExp, string]> = [
  [/user rejected|user denied|rejected the request|wallet rejected|denied transaction|cancel/i, 'You cancelled in your wallet'],
  [CAPABILITY_PATTERN, CREATION_PAUSED],
  [/insufficient funds|exceeds balance|not enough/i, 'Not enough balance to cover this. Top up and try again'],
  [/another tab/i, 'This launch is open in another tab. Close it and try again'],
  [/web locks/i, "This browser can't create coins. Try another browser"],
  [/image|logo|media/i, "Your image couldn't be uploaded. Try again"],
  [/metadata|publication|publish/i, "Your coin is live, but its details didn't save yet. Try again"],
  [/purchase|router\.buy|quote/i, "Your first buy didn't go through. Try again"],
  [/recover|existing attempt|saved attempt|saved launch|history is full/i, 'You have a launch in progress above. Finish it first'],
  [/network switch|switch to chain|chain 125|wrong network/i, 'Switch your wallet to Paxeer and try again'],
  [/wallet changed|account or chain|creator wallet|original creator|reconnect/i, 'Your wallet changed. Reconnect and try again'],
  [/draft/i, "Your saved draft couldn't be loaded. Start fresh"],
  [/expired|changed|review again|stale|reorganiz|timeout|timed out/i, 'Something changed. Try again'],
];

/** Status lines that carry a hash, revision or code: matched by shape, never shown as is. */
const statusPatterns: ReadonlyArray<readonly [RegExp, string]> = [
  [/^Approval submitted:/, 'Approval sent. Confirming...'],
  [/^Creation submitted:/, 'Confirming...'],
  [/^Purchase approval submitted:/, 'Approval sent. Confirming...'],
  [/^Token created\. Purchase submitted:/, 'Your coin is live. Confirming your first buy...'],
  [/^Token created\. Purchase recovery storage failed/, "Your coin is live. This browser couldn't save your first buy, so don't buy again until it shows up"],
  [/^Recovery storage failed/, "This browser couldn't save your progress. Don't create the coin again, check your wallet's activity first"],
  [/^Token created\. First purchase confirmed/, 'Your coin is live and your first buy went through!'],
  [/^Token created\. First purchase reverted/, "Your coin is live, but your first buy didn't go through"],
  [/^Token created\. Metadata published/, 'Your coin is live!'],
  [/^Token created\. Metadata needs fresh/, 'Your coin is live. Sign in again with the wallet you created it with to save its details'],
  [/^Token created\. Metadata publication stopped/, "Your coin is live, but its details didn't save. Try again"],
  [/^Token created\./, 'Your coin is live'],
];

const passThrough = new Set<string>(Object.values(statusCopy).filter(Boolean));

/** Registers sentences that are already plain copy (e.g. funding notes) so they pass through unchanged. */
export function plainCopy(...sentences: string[]): void {
  for (const sentence of sentences) passThrough.add(sentence);
}

function rawText(cause: unknown): string {
  if (cause instanceof Error) return cause.message;
  if (typeof cause === 'string') return cause;
  return '';
}

function walletCode(cause: unknown): number | null {
  let current: unknown = cause;
  for (let depth = 0; depth < 8 && current && typeof current === 'object'; depth++) {
    const known = current as { code?: unknown; cause?: unknown };
    if (typeof known.code === 'number') return known.code;
    current = known.cause;
  }
  return null;
}

/** A launch status line in plain words; '' means nothing is shown. */
export function plainLaunchStatus(message: string | undefined): string {
  if (!message) return '';
  if (message in statusCopy) return statusCopy[message];
  for (const [pattern, copy] of statusPatterns) if (pattern.test(message)) return copy;
  return plainLaunchError(message);
}

/** One plain sentence for any failure in the creation flow. The original detail goes to the console. */
export function plainLaunchError(cause: unknown, fallback: string = GENERIC_LAUNCH_ERROR): string {
  const text = rawText(cause);
  if (passThrough.has(text)) return text;
  if (text in statusCopy) return statusCopy[text] || fallback;
  if (cause && typeof cause === 'object') console.error('[create]', cause);
  if (walletCode(cause) === 4001) return 'You cancelled in your wallet';
  if (isCapabilityError(cause)) return CREATION_PAUSED;
  for (const [pattern, copy] of errorRules) if (pattern.test(text)) return copy;
  return fallback;
}
