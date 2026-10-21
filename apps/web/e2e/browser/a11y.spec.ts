import { expect, test } from "@playwright/test";

import { copyEntry } from "../../copy/catalog.ts";

test("@a11y public content exposes its semantic structure", async ({ page }) => {
  await page.goto("/explorer", { waitUntil: "networkidle" });

  await expect(page.locator("html")).toHaveAttribute("lang", "en");
  await expect(page.getByRole("main")).toHaveCount(1);
  await expect(
    page.getByRole("heading", { level: 1, name: copyEntry("explorer.title").message }),
  ).toBeVisible();
  await expect(page.getByText(copyEntry("explorer.summary").message)).toBeVisible();
});

test("@a11y public content remains usable at 1.5x text expansion", async ({ page }) => {
  await page.goto("/explorer", { waitUntil: "networkidle" });
  await page.addStyleTag({ content: "html { font-size: 150% !important; }" });

  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  const overflow = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: document.documentElement.clientWidth,
  }));
  expect(overflow.documentWidth).toBeLessThanOrEqual(overflow.viewportWidth);
});

test("@a11y coarse pointers activate the package touch-target contract", async ({ page }) => {
  await page.goto("/explorer", { waitUntil: "networkidle" });
  test.skip(
    !(await page.evaluate(() => window.matchMedia("(pointer: coarse)").matches)),
    "This assertion applies to touch-pointer projects",
  );

  const activeTouchTargetRule = await page.evaluate(() => {
    const walk = (rules: CSSRuleList, withinCoarsePointerQuery = false): boolean => {
      for (const rule of Array.from(rules)) {
        if (rule instanceof CSSStyleRule && withinCoarsePointerQuery) {
          if (
            rule.selectorText.includes("button") &&
            rule.style.minWidth === "44px" &&
            rule.style.minHeight === "44px"
          ) {
            return true;
          }
        }
        if (rule instanceof CSSMediaRule) {
          const isActiveCoarsePointerQuery =
            withinCoarsePointerQuery ||
            (rule.conditionText.includes("pointer: coarse") &&
              window.matchMedia(rule.conditionText).matches);
          if (walk(rule.cssRules, isActiveCoarsePointerQuery)) {
            return true;
          }
        } else if ("cssRules" in rule) {
          const groupedRules = (rule as CSSRule & { readonly cssRules: CSSRuleList }).cssRules;
          if (walk(groupedRules, withinCoarsePointerQuery)) {
            return true;
          }
        }
      }
      return false;
    };

    return Array.from(document.styleSheets).some((stylesheet) => walk(stylesheet.cssRules));
  });
  expect(activeTouchTargetRule).toBe(true);
});
