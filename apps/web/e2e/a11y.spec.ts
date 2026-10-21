import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { formatMoney } from "../packages/layerx-ui/src/lib/format.ts";
import {
  MINIMUM_TEXT_CONTRAST,
  a11y,
  assertSemanticTokenContrast,
  formatExplicitCurrencyAmount,
  semanticContrastResults,
} from "../src/kit/a11y.ts";

const tokensUrl = new URL("../packages/layerx-ui/src/styles/tokens.css", import.meta.url);

test("the authoritative semantic token combinations meet WCAG AA text contrast", async () => {
  const stylesheet = await readFile(tokensUrl, "utf8");
  assert.doesNotThrow(() => assertSemanticTokenContrast(stylesheet));
  for (const result of semanticContrastResults(stylesheet)) {
    assert.ok(
      result.ratio >= MINIMUM_TEXT_CONTRAST,
      `${result.foreground} on ${result.background} is ${result.ratio.toFixed(2)}:1`,
    );
  }
});

test("application styling imports the owner package without redefining its primitives", async () => {
  const stylesheet = await readFile(new URL("../src/app/globals.css", import.meta.url), "utf8");
  assert.match(stylesheet, /@import "@layerx\/ui\/styles\.css";/u);
  assert.doesNotMatch(stylesheet, /--(?:background|foreground|primary|accent|success|destructive|warning)\s*:/u);
});

test("amount formatting is locale-aware and always names an explicit currency code", () => {
  assert.equal(
    formatExplicitCurrencyAmount(1234.5, { currency: "EUR", locale: "de-DE" }),
    "+1.234,50 EUR",
  );
  assert.equal(
    formatExplicitCurrencyAmount(-1234.5, { currency: "USD", locale: "en-US" }),
    "-USD 1,234.50",
  );
  assert.equal(
    formatMoney(1234.5, { currency: "EUR", locale: "de-DE" }),
    "+ 1.234,50 EUR",
  );
  assert.throws(
    () => formatExplicitCurrencyAmount(1, { currency: "eur", locale: "de-DE" }),
    /uppercase ISO 4217/u,
  );
});

test("the accessibility contract fixes touch targets and text expansion", () => {
  assert.equal(a11y().minimumTouchTargetCssPixels, 44);
  assert.equal(a11y().supportedTextExpansion, 1.5);
});

test("the browser CI suite selects accessibility specs and a coarse-pointer tablet", async () => {
  const [playwrightConfig, workflow] = await Promise.all([
    readFile(new URL("../playwright.config.ts", import.meta.url), "utf8"),
    readFile(new URL("../../../.github/workflows/human.yml", import.meta.url), "utf8"),
  ]);
  assert.match(playwrightConfig, /testMatch: \["\*\*\/\*\.spec\.ts"\]/u);
  assert.match(playwrightConfig, /name: "touch-tablet-shell"/u);
  assert.match(playwrightConfig, /testMatch: "\*\*\/a11y\.spec\.ts"/u);
  assert.match(workflow, /run: make human-test-e2e/u);
});

test("layered surfaces retain focus trapping, Escape dismissal and mobile avoidance", async () => {
  const [modal, sheet, drawer, viewport] = await Promise.all([
    readFile(new URL("../packages/layerx-ui/src/components/modal.tsx", import.meta.url), "utf8"),
    readFile(new URL("../packages/layerx-ui/src/components/sheet.tsx", import.meta.url), "utf8"),
    readFile(new URL("../packages/layerx-ui/src/components/drawer.tsx", import.meta.url), "utf8"),
    readFile(new URL("../src/app/layout.tsx", import.meta.url), "utf8"),
  ]);
  for (const surface of [modal, sheet, drawer]) {
    assert.match(surface, /@radix-ui\/react-dialog/u);
    assert.match(surface, /<Dialog\.Overlay/u);
    assert.match(surface, /<Dialog\.Content/u);
  }
  assert.match(sheet, /data-sheet-drag-handle/u);
  assert.match(sheet, /event\.clientY - startY >= 72/u);
  assert.match(sheet, /safe-area-inset-bottom/u);
  assert.match(viewport, /interactiveWidget: "resizes-content"/u);
});
