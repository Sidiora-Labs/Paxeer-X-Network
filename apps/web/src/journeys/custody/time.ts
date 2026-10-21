import { formatCopy } from "../../../copy/format.ts";

export interface SettlementDeclaration {
  readonly checkpointIntervalSeconds: number;
  readonly paxeerBlockSeconds: number;
  readonly requiredConfirmations: number;
}

export interface CustodyTiming {
  readonly settlement?: SettlementDeclaration;
  readonly challengeWindowSeconds?: number;
  readonly depositDelayedAfterSeconds?: number;
}

type EnvSource = Readonly<Record<string, string | undefined>>;

function declaredPositiveInteger(env: EnvSource, name: string): number | undefined {
  const raw = env[name];
  if (raw === undefined || !/^\d+$/.test(raw)) {
    return undefined;
  }
  const value = Number.parseInt(raw, 10);
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

export function custodyTimingFromEnv(env: EnvSource): CustodyTiming {
  const checkpointIntervalSeconds = declaredPositiveInteger(env, "HUMAN_SETTLEMENT_CHECKPOINT_INTERVAL_SECONDS");
  const paxeerBlockSeconds = declaredPositiveInteger(env, "HUMAN_SETTLEMENT_PAXEER_BLOCK_SECONDS");
  const requiredConfirmations = declaredPositiveInteger(env, "HUMAN_SETTLEMENT_REQUIRED_CONFIRMATIONS");
  const challengeWindowSeconds = declaredPositiveInteger(env, "HUMAN_CHALLENGE_WINDOW_SECONDS");
  const depositDelayedAfterSeconds = declaredPositiveInteger(env, "HUMAN_DEPOSIT_DELAYED_AFTER_SECONDS");
  const settlement =
    checkpointIntervalSeconds !== undefined &&
    paxeerBlockSeconds !== undefined &&
    requiredConfirmations !== undefined
      ? Object.freeze({ checkpointIntervalSeconds, paxeerBlockSeconds, requiredConfirmations })
      : undefined;
  return Object.freeze({
    ...(settlement === undefined ? {} : { settlement }),
    ...(challengeWindowSeconds === undefined ? {} : { challengeWindowSeconds }),
    ...(depositDelayedAfterSeconds === undefined ? {} : { depositDelayedAfterSeconds }),
  });
}

export function settlementExpectationSeconds(settlement: SettlementDeclaration): number {
  return (
    settlement.checkpointIntervalSeconds +
    settlement.paxeerBlockSeconds * settlement.requiredConfirmations
  );
}

export function plainDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    throw new RangeError("A duration must be a positive number of seconds");
  }
  if (seconds >= 86_400) {
    return formatCopy("time.duration.days", { count: Math.ceil(seconds / 86_400) });
  }
  if (seconds >= 3_600) {
    return formatCopy("time.duration.hours", { count: Math.ceil(seconds / 3_600) });
  }
  if (seconds >= 60) {
    return formatCopy("time.duration.minutes", { count: Math.ceil(seconds / 60) });
  }
  return formatCopy("time.duration.seconds", { count: Math.ceil(seconds) });
}

export function elapsedSeconds(nowMs: number, sinceIso: string): number {
  const since = Date.parse(sinceIso);
  if (Number.isNaN(since)) {
    return 0;
  }
  return Math.max(0, (nowMs - since) / 1_000);
}

export function stageDelayed(nowMs: number, sinceIso: string, expectationSeconds: number): boolean {
  return elapsedSeconds(nowMs, sinceIso) > expectationSeconds;
}
