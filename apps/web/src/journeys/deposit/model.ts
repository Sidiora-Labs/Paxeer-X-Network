import type { HumanApiClient, Journey, WalletBinding } from "../../api/index.ts";
import { depositComplete, presentedJourneyState } from "../custody/evidence.ts";
import { WalletHandOff, type PaxeerWalletBridge, type WalletHandOffPhase } from "../custody/handoff.ts";
import {
  journeyTimeline,
  newIdempotencyKey,
  refusalPresentation,
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
import { plainDuration, stageDelayed, type CustodyTiming } from "../custody/time.ts";

export const DEPOSIT_FINAL_STAGE = "deposit.stage.crediting";

export interface DepositPlanInput {
  readonly shell: CustodyShell;
  readonly timing: CustodyTiming;
  readonly nowMs: number;
  readonly amountInput: string;
  readonly binding?: WalletBinding;
  readonly journey?: Journey;
  readonly walletPhase: WalletHandOffPhase;
}

export interface DepositPlan {
  readonly shell: CustodyShell;
  readonly titleKey: "deposit.title";
  readonly summaryKey: "deposit.summary";
  readonly phase: "form" | "journey";
  readonly bindingFolded: boolean;
  readonly amount: Readonly<{ labelKey: "deposit.amount.label"; value: string; errorKey?: "deposit.amount.invalid" }>;
  readonly primaryAction: Readonly<{
    labelKey: string;
    disabled: boolean;
    disabledReasonKey?: string;
  }>;
  readonly summaryItems: readonly Readonly<{ labelKey: string; value: string }>[];
  readonly timeline?: readonly TimelineRow[];
  readonly wallet?: WalletPanelPlan;
  readonly safeToCloseKey?: "deposit.safe_to_close";
  readonly pendingHonestyKey?: "deposit.pending.not_counted";
  readonly delayed?: Readonly<{ titleKey: "journey.delayed"; bodyKey: "journey.delayed.expectation"; duration: string }>;
  readonly refusal?: RefusalPresentation;
  readonly complete?: Readonly<{ titleKey: "deposit.complete"; bodyKey: "deposit.complete.body" }>;
  readonly duplicateLocked: boolean;
}

export function depositPlan(input: DepositPlanInput): DepositPlan {
  const bindingFolded = input.binding?.state !== "bound";
  if (input.journey === undefined) {
    const amount = validatePositiveAmount(input.amountInput);
    const invalid = amount === undefined;
    return Object.freeze({
      shell: input.shell,
      titleKey: "deposit.title",
      summaryKey: "deposit.summary",
      phase: "form",
      bindingFolded,
      amount: Object.freeze({
        labelKey: "deposit.amount.label" as const,
        value: input.amountInput,
        ...(invalid && input.amountInput.length > 0 ? { errorKey: "deposit.amount.invalid" as const } : {}),
      }),
      primaryAction: Object.freeze({
        labelKey: "deposit.start",
        disabled: invalid,
        ...(invalid ? { disabledReasonKey: "deposit.amount.invalid" } : {}),
      }),
      summaryItems:
        input.shell === "desktop"
          ? Object.freeze([
              Object.freeze({ labelKey: "deposit.amount.label", value: input.amountInput }),
              ...(bindingFolded
                ? [Object.freeze({ labelKey: "deposit.wallet.label", value: "" })]
                : [Object.freeze({ labelKey: "deposit.wallet.label", value: input.binding.address ?? "" })]),
            ])
          : Object.freeze([]),
      duplicateLocked: false,
    });
  }

  const journey = input.journey;
  const presented = presentedJourneyState(journey, DEPOSIT_FINAL_STAGE);
  const complete = depositComplete(journey);
  const wallet =
    journey.wallet_request === undefined || journey.refusal !== undefined || presented === "still-checking"
      ? undefined
      : walletPanel(journey.wallet_request, input.walletPhase, "deposit.sign.custody-transaction");
  const active = !complete && journey.refusal === undefined;
  const settled = journey.wallet_request === undefined || input.walletPhase === "approved";
  const delayedAfter = input.timing.depositDelayedAfterSeconds;
  const delayed =
    active &&
    settled &&
    delayedAfter !== undefined &&
    stageDelayed(input.nowMs, journey.updated_at, delayedAfter)
      ? Object.freeze({
          titleKey: "journey.delayed" as const,
          bodyKey: "journey.delayed.expectation" as const,
          duration: plainDuration(delayedAfter),
        })
      : undefined;
  return Object.freeze({
    shell: input.shell,
    titleKey: "deposit.title",
    summaryKey: "deposit.summary",
    phase: "journey",
    bindingFolded,
    amount: Object.freeze({ labelKey: "deposit.amount.label" as const, value: input.amountInput }),
    primaryAction: Object.freeze(
      wallet?.actionKey !== undefined
        ? { labelKey: wallet.actionKey, disabled: false }
        : {
            labelKey: "deposit.start",
            disabled: true,
            disabledReasonKey:
              presented === "still-checking" ? "state.still_checking.locked" : "deposit.safe_to_close",
          },
    ),
    summaryItems:
      input.shell === "desktop"
        ? Object.freeze([Object.freeze({ labelKey: "deposit.amount.label", value: input.amountInput })])
        : Object.freeze([]),
    timeline: journeyTimeline(journey),
    ...(wallet === undefined ? {} : { wallet }),
    ...(active && settled ? { safeToCloseKey: "deposit.safe_to_close" as const } : {}),
    ...(active && settled ? { pendingHonestyKey: "deposit.pending.not_counted" as const } : {}),
    ...(delayed === undefined ? {} : { delayed }),
    ...(journey.refusal === undefined ? {} : { refusal: refusalPresentation(journey.refusal) }),
    ...(complete
      ? { complete: Object.freeze({ titleKey: "deposit.complete" as const, bodyKey: "deposit.complete.body" as const }) }
      : {}),
    duplicateLocked: presented === "still-checking",
  });
}

export interface DepositControllerOptions {
  readonly api: HumanApiClient;
  readonly bridge: PaxeerWalletBridge;
  readonly randomBytes?: RandomBytes;
}

export class DepositController {
  readonly #api: HumanApiClient;
  readonly #handOff: WalletHandOff;
  readonly #randomBytes: RandomBytes | undefined;
  #idempotencyKey: string | undefined;
  #journey: Journey | undefined;
  #unknownRecovery: UnknownOutcomeRecovery | undefined;
  #unknownRecoveryMode: "start" | "handoff" | undefined;

  constructor(options: DepositControllerOptions) {
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

  async start(amount: bigint, currency: string): Promise<Journey> {
    this.#idempotencyKey ??=
      this.#randomBytes === undefined ? newIdempotencyKey() : newIdempotencyKey(this.#randomBytes);
    const request = { money: { amount, currency } } as const;
    const start = async () => this.#api.depositStart(request, this.#idempotencyKey as string);
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

  async openWallet(): Promise<Journey | undefined> {
    const journey = this.#journey;
    const request = journey?.wallet_request;
    if (journey === undefined || request === undefined) {
      throw new Error("No signing moment is pending for this deposit");
    }
    const outcome = await this.#handOff.open(request);
    if (outcome.outcome !== "approved") {
      return undefined;
    }
    try {
      this.#journey = await this.#api.depositConfirm(journey.journey_id, {
        wallet_transaction: outcome.reference,
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
      throw new Error("No deposit journey has been started");
    }
    this.#journey = await this.#api.journeyGet(journey.journey_id);
    return this.#journey;
  }

  adopt(journey: Journey): void {
    this.#journey = journey;
  }
}
