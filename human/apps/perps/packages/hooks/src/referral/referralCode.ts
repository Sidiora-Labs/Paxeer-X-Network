export const REFERRAL_CODE_MIN_LENGTH = 4;
export const REFERRAL_CODE_MAX_LENGTH = 15;

export function formatReferralCodeInput(raw: string): string {
  return String(raw)
    .replace(/[a-z]/g, (c) => c.toUpperCase())
    .replace(/[^A-Z0-9]/g, "");
}

export function isReferralCodeLengthValid(code: string): boolean {
  return (
    code.length >= REFERRAL_CODE_MIN_LENGTH &&
    code.length <= REFERRAL_CODE_MAX_LENGTH
  );
}
