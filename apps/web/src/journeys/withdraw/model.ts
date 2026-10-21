import type { HumanApiClient, Journey } from "../../api/index.ts";
import { presentedJourneyState, withdrawPaidOut } from "../custody/evidence.ts";
import { WalletHandOff, type PaxeerWalletBridge, type WalletHandOffPhase } from "../custody/handoff.ts";
import {
  journeyTimeline,
  newIdempotencyKey,
  refusalPresentation,
  validateDestinationAddress,
  validatePositiveAmount,
  walletPanel,
  type CustodyShell,
  type RandomBytes,
  type RefusalPresentation,
  type TimelineRow,
  type WalletPanelPlan,
} from "../custody/model.ts";
import {
  JourneyOutcomeUnknownError,
  mutationOutcomeIsUnknown,
  type UnknownOutcomeRecovery,
} from "../custody/recovery.ts";
import {
  plainDuration,
  settlementExpectationSeconds,
  type CustodyTiming,
} from "../custody/time.ts";

export const WITHDRAW_FINAL_STAGE = "withdraw.stage.paying-out";
export const WITHDRAW_HOLD_STAGE = "withdraw.stage.challenge-hold";

export interface SettlementPresentation {
  readonly bodyKey: "withdraw.settlement.expectation" | "withdraw.settlement.undeclared";
  readonly duration?: string;
}

export function settlementPresentation(timing: CustodyTiming): SettlementPresentation {
  if (timing.settlement === undefined) {
    return Object.freeze({ bodyKey: "withdraw.settlement.undeclared" as const });
  }
  return Object.freeze({
    bodyKey: "withdraw.settlement.expectation" as const,
    duration: plainDuration(settlementExpectationSeconds(timing.settlement)),
  });
}

export interface ChallengeHoldPresentation {
  readonly titleKey: "withdraw.hold.title";
  readonly bodyKey: "withdraw.hold.body";
  readonly expectation?: Readonly<{ bodyKey: "withdraw.hold.expectation"; duration: string }>;
  readonly cancelledKey?: "withdraw.hold.cancelled";
}

export function challengeHoldPresentation(
  journey: Journey,
  timing: CustodyTiming,
): ChallengeHoldPresentation | undefined {
  const stage = journey.stages.find((candidate) => candidate.copy_key === WITHDRAW_HOLD_STAGE);
  if (stage === undefined || stage.state === "done" || stage.state === "done-finalised") {
    return undefined;
  }
  return Object.freeze({
    titleKey: "withdraw.hold.title" as const,
    bodyKey: "withdraw.hold.body" as const,
    ...(timing.challengeWindowSeconds === undefined
      ? {}
      : {
          expectation: Object.freeze({
            bodyKey: "withdraw.hold.expectation" as const,
            duration: plainDuration(timing.challengeWindowSeconds),
          }),
        }),
    ...(stage.state === "refused" ? { cancelledKey: "withdraw.hold.cancelled" as const } : {}),
  });
}

export interface WithdrawPlanInput {
  readonly shell: CustodyShell;
  readonly timing: CustodyTiming;
  readonly nowMs: number;
  readonly amountInput: string;
  readonly destinationInput: string;
  readonly journey?: Journey;
  readonly walletPhase: WalletHandOffPhase;
}

export interface WithdrawPlan {
  readonly shell: CustodyShell;
  readonly titleKey: "withdraw.title";
  readonly summaryKey: "withdraw.summary";
  readonly phase: "form" | "journey";
  readonly amount: Readonly<{ labelKey: "withdraw.amount.label"; value: string; errorKey?: "withdraw.amount.invalid" }>;
  readonly destination: Readonly<{
    labelKey: "withdraw.destination.label";
    value: string;
    errorKey?: "withdraw.destination.invalid";
  }>;
  readonly review: Readonly<{
    titleKey: "withdraw.review.title";
    irreversibleKey: "withdraw.irreversible";
    commitKey: "withdraw.commit";
    ready: boolean;
  }>;
  readonly settlement: SettlementPresentation;
  readonly summaryItems: readonly Readonly<{ labelKey: string; value: string }>[];
  readonly timeline?: readonly TimelineRow[];
  readonly claim?: Readonly<{ titleKey: "withdraw.claim.title"; bodyKey: "withdraw.claim.body" }>;
  readonly wallet?: WalletPanelPlan;
  readonly hold?: ChallengeHoldPresentation;
  readonly refusal?: RefusalPresentation;
  readonly complete?: Readonly<{ titleKey: "status.paid_out"; bodyKey: "withdraw.paid_out.body" }>;
  readonly duplicateLocked: boolean;
}

export function withdrawPlan(input: WithdrawPlanInput): WithdrawPlan {
  const amount = validatePositiveAmount(input.amountInput);
  const destination = validateDestinationAddress(input.destinationInput);
  const settlement = settlementPresentation(input.timing);
  const base = {
    shell: input.shell,
    titleKey: "withdraw.title" as const,
    summaryKey: "withdraw.summary" as const,
    amount: Object.freeze({
      labelKey: "withdraw.amount.label" as const,
      value: input.amountInput,
      ...(amount === undefined && input.amountInput.length > 0
        ? { errorKey: "withdraw.amount.invalid" as const }
        : {}),
    }),
    destination: Object.freeze({
      labelKey: "withdraw.destination.label" as const,
      value: input.destinationInput,
      ...(destination === undefined && input.destinationInput.length > 0
        ? { errorKey: "withdraw.destination.invalid" as const }
        : {}),
    }),
    settlement,
    summaryItems:
      input.shell === "desktop"
        ? Object.freeze([
            Object.freeze({ labelKey: "withdraw.amount.label", value: input.amountInput }),
            Object.freeze({ labelKey: "withdraw.destination.label", value: input.destinationInput }),
          ])
        : Object.freeze([]),
  };
  if (input.journey === undefined) {
    return Object.freeze({
      ...base,
      phase: "form",
      review: Object.freeze({
        titleKey: "withdraw.review.title" as const,
        irreversibleKey: "withdraw.irreversible" as const,
        commitKey: "withdraw.commit" as const,
        ready: amount !== undefined && destination !== undefined,
      }),
      duplicateLocked: false,
    });
  }

  const journey = input.journey;
  const presented = presentedJourneyState(journey, WITHDRAW_FINAL_STAGE);
  const complete = withdrawPaidOut(journey);
  const wallet =
    journey.wallet_request === undefined || journey.refusal !== undefined || presented === "still-checking"
      ? undefined
      : walletPanel(journey.wallet_request, input.walletPhase, "withdraw.sign.claim");
  const hold = challengeHoldPresentation(journey, input.timing);
  return Object.freeze({
    ...base,
    phase: "journey",
    review: Object.freeze({
      titleKey: "withdraw.review.title" as const,
      irreversibleKey: "withdraw.irreversible" as const,
      commitKey: "withdraw.commit" as const,
      ready: false,
    }),
    timeline: journeyTimeline(journey),
    ...(wallet === undefined
      ? {}
      : {
          claim: Object.freeze({
            titleKey: "withdraw.claim.title" as const,
            bodyKey: "withdraw.claim.body" as const,
          }),
          wallet,
        }),
    ...(hold === undefined ? {} : { hold }),
    ...(journey.refusal === undefined ? {} : { refusal: refusalPresentation(journey.refusal) }),
    ...(complete
      ? {
          complete: Object.freeze({
            titleKey: "status.paid_out" as const,
            bodyKey: "withdraw.paid_out.body" as const,
          }),
        }
      : {}),
    duplicateLocked: presented === "still-checking",
  });
}

export interface WithdrawControllerOptions {
  readonly api: HumanApiClient;
  readonly bridge: PaxeerWalletBridge;
  readonly randomBytes?: RandomBytes;
}

export class WithdrawController {
  readonly #api: HumanApiClient;
  readonly #handOff: WalletHandOff;
  readonly #randomBytes: RandomBytes | undefined;
  #idempotencyKey: string | undefined;
  #journey: Journey | undefined;
  #unknownRecovery: UnknownOutcomeRecovery | undefined;
  #unknownRecoveryMode: "start" | "handoff" | undefined;

  constructor(options: WithdrawControllerOptions) {
    this.#api = options.api;
    this.#handOff = new WalletHandOff(options.bridge);
    this.#randomBytes = options.randomBytes;
  }

  get journey(): Journey | undefined {
    return this.#journey;
  }

  get walletPhase(): WalletHandOffPhase {
    return this.#handOff.phase;
  }

  get idempotencyKey(): string | undefined {
    return this.#idempotencyKey;
  }

  get outcomeUnknown(): boolean {
    return this.#unknownRecovery !== undefined;
  }

  get unknownRecoveryMode(): "start" | "handoff" | undefined {
    return this.#unknownRecoveryMode;
  }

  walletOpens(stageId: string): number {
    return this.#handOff.opens(stageId);
  }

  cancelWallet(): void {
    this.#handOff.cancel();
  }

  async commit(amount: bigint, currency: string, destination: string): Promise<Journey> {
    this.#idempotencyKey ??=
      this.#randomBytes === undefined ? newIdempotencyKey() : newIdempotencyKey(this.#randomBytes);
    const request = { money: { amount, currency }, destination } as const;
    const start = async () => this.#api.withdrawStart(request, this.#idempotencyKey as string);
    try {
      this.#journey = await start();
      this.#unknownRecovery = undefined;
      this.#unknownRecoveryMode = undefined;
      return this.#journey;
    } catch (error) {
      if (!mutationOutcomeIsUnknown(error)) {
        throw error;
      }
      this.#unknownRecovery = async () => {
        const journey = await start();
        this.#journey = journey;
        return { journey, resolved: true };
      };
      this.#unknownRecoveryMode = "start";
      throw new JourneyOutcomeUnknownError(error);
    }
  }

  async openWalletToClaim(): Promise<Journey | undefined> {
    const journey = this.#journey;
    const request = journey?.wallet_request;
    if (journey === undefined || request === undefined) {
      throw new Error("No claim is ready for this withdrawal");
    }
    const outcome = await this.#handOff.open(request);
    if (outcome.outcome !== "approved") {
      return undefined;
    }
    try {
      this.#journey = await this.#api.withdrawClaim(journey.journey_id, {
        claim_signature: outcome.reference,
      });
      this.#unknownRecovery = undefined;
      this.#unknownRecoveryMode = undefined;
      return this.#journey;
    } catch (error) {
      const signedStage = request.stage_id;
      this.#unknownRecovery = async () => {
        const updated = await this.#api.journeyGet(journey.journey_id);
        this.#journey = updated;
        return {
          journey: updated,
          resolved:
            updated.state !== "still-checking" && updated.wallet_request?.stage_id !== signedStage,
        };
      };
      this.#unknownRecoveryMode = "handoff";
      throw new JourneyOutcomeUnknownError(error);
    }
  }

  async recoverUnknown(): Promise<Readonly<{ journey: Journey; resolved: boolean }>> {
    const recovery = this.#unknownRecovery;
    if (recovery === undefined) {
      const journey = await this.refresh();
      return { journey, resolved: journey.state !== "still-checking" };
    }
    const outcome = await recovery();
    if (outcome.resolved) {
      this.#unknownRecovery = undefined;
      this.#unknownRecoveryMode = undefined;
    }
    return outcome;
  }

  async refresh(): Promise<Journey> {
    const journey = this.#journey;
    if (journey === undefined) {
      throw new Error("No withdrawal journey has been started");
    }
    this.#journey = await this.#api.journeyGet(journey.journey_id);
    return this.#journey;
  }

  adopt(journey: Journey): void {
    this.#journey = journey;
  }
}
