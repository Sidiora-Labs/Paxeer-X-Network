import assert from "node:assert/strict";
import test from "node:test";

import type { ConfirmationProps } from "../src/kit/confirm.tsx";
import {
  PATTERN_PAIRS,
  confirmationVariant,
  directionWord,
  feeMathTotal,
  protocolAmount,
  statusPresentation,
  typedConfirmationReady,
} from "../src/kit/model.ts";

const confirmationBase = {
  open: true,
  onOpenChange: (_open: boolean) => undefined,
  title: "Confirm action",
  consequence: "This changes your account.",
  confirmLabel: "Confirm",
  onConfirm: () => undefined,
} as const;

const reversibleConfirmation: ConfirmationProps = {
  ...confirmationBase,
  kind: "reversible",
};

const typedConfirmation = {
  expectedValue: "ARCHIVE",
  value: "ARCHIVE",
  onValueChange: (_value: string) => undefined,
} as const;

const irreversibleConfirmation: ConfirmationProps = {
  ...confirmationBase,
  kind: "irreversible",
  typedConfirmation,
};

// @ts-expect-error reversible confirmations cannot ask for typed confirmation
const invalidReversibleConfirmation: ConfirmationProps = {
  ...confirmationBase,
  kind: "reversible",
  typedConfirmation,
};

// @ts-expect-error irreversible confirmations require typed confirmation
const invalidIrreversibleConfirmation: ConfirmationProps = {
  ...confirmationBase,
  kind: "irreversible",
};

void reversibleConfirmation;
void invalidReversibleConfirmation;
void invalidIrreversibleConfirmation;

test("the kit declares every mobile and desktop pattern pair", () => {
  assert.deepEqual(PATTERN_PAIRS, [
    "navigation",
    "primary-action",
    "confirmation",
    "detail",
    "filters",
    "money-lists",
    "wizards",
    "search",
    "code-entry",
    "notifications",
  ]);
});

test("money presentation combines normative words with semantic status tones", () => {
  assert.equal(directionWord("inbound"), "Money in");
  assert.equal(directionWord("outbound"), "Money out");
  assert.deepEqual(statusPresentation("done"), { label: "Done", tone: "success" });
  assert.deepEqual(statusPresentation("still_checking"), {
    label: "Still checking — don't send again",
    tone: "warning",
  });
});

test("confirmation appearance and typed readiness follow the fixed grammar", () => {
  assert.equal(confirmationVariant("reversible"), "primary");
  assert.equal(confirmationVariant("destructive"), "destructive");
  assert.equal(confirmationVariant("irreversible"), "destructive");
  assert.equal(typedConfirmationReady("destructive"), true);
  assert.equal(
    typedConfirmationReady("irreversible", { expectedValue: "ARCHIVE", value: "archive" }),
    false,
  );
  assert.equal(
    typedConfirmationReady("irreversible", { expectedValue: "ARCHIVE", value: "ARCHIVE" }),
    true,
  );
});

test("fee math walks declared inputs to one computed total", () => {
  assert.equal(
    feeMathTotal([
      { amount: protocolAmount(250) },
      { amount: protocolAmount(25) },
      { amount: protocolAmount(-10) },
    ]),
    protocolAmount(265),
  );
  assert.throws(() => protocolAmount(0.1), /safe integers/u);
  assert.throws(() => protocolAmount(Number.MAX_SAFE_INTEGER + 1), /safe integers/u);
});
