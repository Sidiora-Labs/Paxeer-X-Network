import { expect, test } from "@playwright/test";

test("@journey @visual renders the real root surface", async ({ page, request }, testInfo) => {
  const serviceResponse = await request.get("/");
  expect(serviceResponse.ok()).toBe(true);

  await page.goto("/", { waitUntil: "networkidle" });
  await expect(page.locator("main")).toHaveCount(1);
  expect(process.env.HUMAN_VISUAL_BASELINE_REVIEWED).toBe("1");
  await expect(page).toHaveScreenshot(`${testInfo.project.name}-root.png`, {
    fullPage: true,
    animations: "disabled",
  });
});
