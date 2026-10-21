import { HumanApiError, type Journey } from "../../api/index.ts";

export type UnknownOutcomeRecovery = () => Promise<
  Readonly<{ journey: Journey; resolved: boolean }>
>;

export class JourneyOutcomeUnknownError extends Error {
  readonly cause: unknown;

  constructor(cause: unknown) {
    super("The journey outcome is still being checked");
    this.name = "JourneyOutcomeUnknownError";
    this.cause = cause;
  }
}

export function mutationOutcomeIsUnknown(error: unknown): boolean {
  if (!(error instanceof HumanApiError)) {
    return true;
  }
  return error.status === 408 || error.status >= 500;
}

export function isJourneyOutcomeUnknown(error: unknown): error is JourneyOutcomeUnknownError {
  return error instanceof JourneyOutcomeUnknownError;
}
