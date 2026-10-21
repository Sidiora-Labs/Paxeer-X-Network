import type { Journey, Refusal, WalletSignRequest } from "../../api/index.ts";
import type { StatusKey } from "../../kit/model.ts";
import { custodyCopyKey } from "./copy.ts";
import { presentedStageState, stageEvidenceBacked, statusKeyForState } from "./evidence.ts";
import type { WalletHandOffPhase } from "./handoff.ts";

export type CustodyShell = "mobile" | "desktop";

export const CUSTODY_CURRENCY = "LXP";

export interface TimelineRow {
  readonly stageId: string;
  readonly nameKey: string;
  readonly status: StatusKey;
  readonly backed: boolean;
}

export function journeyTimeline(journey: Journey): readonly TimelineRow[] {
  return Object.freeze(
    journey.stages.map((stage) =>
      Object.freeze({
        stageId: stage.stage_id,
        nameKey: custodyCopyKey(stage.copy_key, "journey.stage.unnamed"),
        status: statusKeyForState(presentedStageState(stage)),
        backed: stageEvidenceBacked(stage),
      }),
    ),
  );
}

export interface WalletPanelPlan {
  readonly phase: WalletHandOffPhase;
  readonly titleKey: string;
  readonly bodyKey: string;
  readonly signKey: string;
  readonly actionKey?: string;
}

export function walletPanel(
  request: WalletSignRequest,
  phase: WalletHandOffPhase,
  fallbackSignKey: string,
): WalletPanelPlan | undefined {
  const signKey = custodyCopyKey(request.copy_key, fallbackSignKey);
  switch (phase) {
    case "idle":
      return Object.freeze({
        phase,
        titleKey: "wallet.handoff.ready",
        bodyKey: signKey,
        signKey,
        actionKey: "wallet.handoff.open",
      });
    case "waiting":
      return Object.freeze({
        phase,
        titleKey: "wallet.handoff.in_progress",
        bodyKey: "wallet.handoff.in_progress.body",
        signKey,
      });
    case "cancelled":
      return Object.freeze({
        phase,
        titleKey: "wallet.handoff.cancelled",
        bodyKey: "wallet.handoff.cancelled.body",
        signKey,
        actionKey: "wallet.handoff.retry",
      });
    case "rejected":
      return Object.freeze({
        phase,
        titleKey: "wallet.handoff.rejected",
        bodyKey: "wallet.handoff.rejected.body",
        signKey,
        actionKey: "wallet.handoff.retry",
      });
    case "unavailable":
      return Object.freeze({
        phase,
        titleKey: "wallet.handoff.unavailable",
        bodyKey: "wallet.handoff.unavailable.body",
        signKey,
        actionKey: "wallet.handoff.retry",
      });
    case "failed":
      return Object.freeze({
        phase,
        titleKey: "wallet.handoff.failed",
        bodyKey: "wallet.handoff.failed.body",
        signKey,
        actionKey: "wallet.handoff.retry",
      });
    case "approved":
      return undefined;
  }
}

export interface RefusalPresentation {
  readonly bodyKey: string;
  readonly moneyKey: "journey.failure.money_moved" | "journey.failure.money_not_moved";
  readonly changePath?: string;
  readonly changeActionKey?: string;
}

export function custodyApplicationPath(path: string): string | undefined {
  if (!path.startsWith("/app/") || path.includes("\\") || /[\u0000-\u001f\u007f]/u.test(path)) {
    return undefined;
  }
  const parsed = new URL(path, "https://layerx.invalid");
  if (parsed.origin !== "https://layerx.invalid" || !parsed.pathname.startsWith("/app/")) {
    return undefined;
  }
  return `${parsed.pathname}${parsed.search}${parsed.hash}`;
}

export function refusalPresentation(refusal: Refusal): RefusalPresentation {
  const changePath = refusal.change_path === undefined
    ? undefined
    : custodyApplicationPath(refusal.change_path);
  return Object.freeze({
    bodyKey: custodyCopyKey(refusal.copy_key, "journey.refused.generic"),
    moneyKey: refusal.money_left
      ? ("journey.failure.money_moved" as const)
      : ("journey.failure.money_not_moved" as const),
    ...(changePath === undefined
      ? {}
      : { changePath, changeActionKey: "journey.refused.change_limit" }),
  });
}

export type RandomBytes = (length: number) => Uint8Array;

function defaultRandomBytes(length: number): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(length));
}

export function newIdempotencyKey(randomBytes: RandomBytes = defaultRandomBytes): string {
  const bytes = randomBytes(16);
  if (bytes.length !== 16) {
    throw new Error("An idempotency key needs 16 random bytes");
  }
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export function validatePositiveAmount(input: string): bigint | undefined {
  if (!/^\d+$/.test(input)) {
    return undefined;
  }
  const amount = BigInt(input);
  return amount > 0n ? amount : undefined;
}

export function validateDestinationAddress(input: string): string | undefined {
  return /^0x[0-9a-fA-F]{40}$/.test(input) ? input : undefined;
}
