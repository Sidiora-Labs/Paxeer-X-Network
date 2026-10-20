export function gasWithMargin(estimate: bigint): bigint {
  return (estimate * 12n) / 10n;
}
