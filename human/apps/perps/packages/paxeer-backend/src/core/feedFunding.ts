/** Chainflow next_funding_ms is a remaining duration, anchored to the source update time. */
export function feedFundingDeadline(
  updatedAt: number,
  remainingMs: number,
): number | undefined {
  if (
    !Number.isFinite(updatedAt) ||
    updatedAt <= 0 ||
    !Number.isFinite(remainingMs) ||
    remainingMs <= 0
  )
    return undefined;
  return updatedAt + remainingMs;
}
