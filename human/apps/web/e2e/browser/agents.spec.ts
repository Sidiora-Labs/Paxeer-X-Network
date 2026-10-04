import { randomUUID } from "node:crypto";
import { expect, test, type APIResponse, type Page, type Response } from "@playwright/test";

import { copyEntry } from "../../copy/catalog.ts";
import { formatCopy } from "../../copy/format.ts";
import {
  decodeAgent, decodeAgentPage, decodeJourney, decodeNativeFeeAsset,
  type Agent, type JsonValue, type Journey,
} from "../../src/api/generated/index.ts";
import {
  agentStateVerified, archiveDispositionReady, journeyProgress,
} from "../../src/journeys/agents/model.ts";
import { establishPublicSession } from "../public-session.ts";

const API = "/human/v1";

function required(name: string): string {
  const value = process.env[name]?.trim();
  if (value === undefined || value.length === 0) throw new Error(`${name} is required for real agent lifecycle qualification`);
  return value;
}

async function result<T>(response: APIResponse | Response, decode: (value: JsonValue | undefined, at: string) => T): Promise<T> {
  expect(response.ok()).toBe(true);
  const envelope: unknown = await response.json();
  expect(typeof envelope).toBe("object");
  expect(envelope).not.toBeNull();
  const wire = envelope as Record<string, JsonValue>;
  expect(wire["ok"]).toBe(true);
  expect(typeof wire["trace"]).toBe("string");
  return decode(wire["result"], "actual agent lifecycle result");
}

function mutation(page: Page, path: string): Promise<Response> {
  return page.waitForResponse((response) => response.request().method() === "POST"
    && new URL(response.url()).pathname === `${API}${path}`);
}

async function settled(page: Page, initial: Journey): Promise<void> {
  expect(initial.refusal).toBeUndefined();
  await expect.poll(async () => {
    const response = await page.context().request.get(`${API}/journeys/${encodeURIComponent(initial.journey_id)}`);
    try {
      const current = await result(response, decodeJourney);
      expect(current.journey_id).toBe(initial.journey_id);
      expect(current.refusal).toBeUndefined();
      return journeyProgress(current).complete;
    } finally {
      await response.dispose();
    }
  }, { timeout: 150_000, intervals: [500, 1_000, 2_000] }).toBe(true);
}

async function currentAgent(page: Page, agentId: string): Promise<Agent> {
  const response = await page.context().request.get(`${API}/agents/${encodeURIComponent(agentId)}`);
  try {
    return await result(response, decodeAgent);
  } finally {
    await response.dispose();
  }
}

test("@agents create pause resume reclaim and archive use genuine receipt-backed state", async ({ page, context }, testInfo) => {
  test.setTimeout(600_000);
  const origin = new URL(required("HUMAN_E2E_BASE_URL")).origin;
  await establishPublicSession(context, origin);
  const name = `Lifecycle ${testInfo.project.name} ${randomUUID()}`;
  await page.goto("/app/agents/new", { waitUntil: "networkidle" });
  await page.getByRole("textbox", { name: copyEntry("agent.create.name.label").message, exact: true }).fill(name);
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await page.getByRole("textbox", { name: copyEntry("agent.create.purpose.label").message, exact: true }).fill("Production lifecycle qualification");
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await page.getByRole("textbox", { name: copyEntry("agent.create.limit.label").message, exact: true }).fill(required("HUMAN_E2E_AGENT_MONTHLY_LIMIT"));
  const feeResponse = await context.request.get(`${API}/sessions/fee-policy`);
  let feeCurrency: string;
  try {
    feeCurrency = (await result(feeResponse, decodeNativeFeeAsset)).currency;
  } finally {
    await feeResponse.dispose();
  }
  for (const [field, environment] of [
    ["perAction", "HUMAN_E2E_AGENT_FEE_PER_ACTION"],
    ["total", "HUMAN_E2E_AGENT_FEE_TOTAL"],
    ["perPeriod", "HUMAN_E2E_AGENT_FEE_PER_PERIOD"],
  ] as const) {
    await page.getByRole("textbox", { name: formatCopy(`fees.limits.${field}`, { currency: feeCurrency }), exact: true }).fill(required(environment));
  }
  const createdResponse = mutation(page, "/agents");
  await page.getByRole("button", { name: copyEntry("agent.create.submit").message, exact: true }).click();
  const creation = await result(await createdResponse, decodeJourney);
  expect(creation.kind).toBe("agent-create");
  await settled(page, creation);
  await expect(page.getByText(copyEntry("agent.create.ready").message, { exact: true })).toBeVisible({ timeout: 30_000 });

  const listResponse = await context.request.get(`${API}/agents`);
  let agent: Agent;
  try {
    const matches = (await result(listResponse, decodeAgentPage)).agents.filter((item) => item.name === name);
    expect(matches).toHaveLength(1);
    agent = matches[0]!;
  } finally {
    await listResponse.dispose();
  }
  expect(agent.state).toBe("active");
  expect(agentStateVerified(agent)).toBe(true);
  await page.goto("/app/agents", { waitUntil: "networkidle" });
  await page.getByRole("listitem").filter({ hasText: name }).click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
  if (testInfo.project.name === "mobile-shell") {
    await expect(page).toHaveURL(`${origin}/app/agents/${encodeURIComponent(agent.agent_id)}`);
  } else {
    await expect(page).toHaveURL(`${origin}/app/agents`);
  }

  for (const [control, state] of [["pause", "paused"], ["resume", "active"]] as const) {
    await page.getByRole("button", { name: copyEntry(`agent.control.${control}`).message, exact: true }).click();
    const response = mutation(page, `/agents/${encodeURIComponent(agent.agent_id)}/${control}`);
    await page.getByRole("dialog").getByRole("button", { name: copyEntry(`agent.control.${control}`).message, exact: true }).click();
    const updated = await result(await response, decodeAgent);
    expect(updated.agent_id).toBe(agent.agent_id);
    expect(updated.state).toBe(state);
    expect(agentStateVerified(updated)).toBe(true);
    await expect(page.getByRole("button", { name: copyEntry(`agent.control.${control === "pause" ? "resume" : "pause"}`).message, exact: true })).toBeEnabled();
  }

  agent = await currentAgent(page, agent.agent_id);
  expect(agent.spend.remaining.amount).toBeGreaterThan(0n);
  await page.getByRole("button", { name: copyEntry("agent.control.archive").message, exact: true }).click();
  const archiveDialog = page.getByRole("dialog");
  await expect(archiveDialog.getByText(copyEntry("agent.archive.disposition").message, { exact: true })).toBeVisible();
  await archiveDialog.getByRole("button", { name: copyEntry("agent.archive.continue").message, exact: true }).click();
  await expect(archiveDialog.getByText(copyEntry("error.agent.archive-needs-disposition").message, { exact: true })).toBeVisible();
  await expect(archiveDialog.getByRole("textbox")).toHaveCount(0);
  await archiveDialog.getByRole("button", { name: copyEntry("agent.control.reclaim").message, exact: true }).click();
  await archiveDialog.getByRole("textbox", { name: copyEntry("agent.reclaim.amount.label").message, exact: true }).fill(agent.spend.remaining.amount.toString());
  const reclaimedResponse = mutation(page, `/agents/${encodeURIComponent(agent.agent_id)}/reclaim`);
  await archiveDialog.getByRole("button", { name: copyEntry("agent.control.reclaim").message, exact: true }).click();
  await settled(page, await result(await reclaimedResponse, decodeJourney));
  await expect.poll(async () => archiveDispositionReady(await currentAgent(page, agent.agent_id)), { timeout: 30_000 }).toBe(true);
  await page.reload({ waitUntil: "networkidle" });
  await page.getByRole("button", { name: copyEntry("agent.control.archive").message, exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: copyEntry("agent.archive.continue").message, exact: true }).click();
  const typedDialog = page.getByRole("dialog");
  const confirm = typedDialog.getByRole("button", { name: copyEntry("agent.control.archive").message, exact: true });
  await expect(confirm).toBeDisabled();
  await typedDialog.getByRole("textbox").fill(`${name} wrong`);
  await expect(confirm).toBeDisabled();
  await typedDialog.getByRole("textbox").fill(name);
  await expect(confirm).toBeEnabled();
  const archivedResponse = mutation(page, `/agents/${encodeURIComponent(agent.agent_id)}/archive`);
  await confirm.click();
  await settled(page, await result(await archivedResponse, decodeJourney));
  await expect.poll(async () => (await currentAgent(page, agent.agent_id)).state, { timeout: 30_000 }).toBe("archived");
  await page.reload({ waitUntil: "networkidle" });
  await expect(page.getByText(copyEntry("agent.archive.readonly").message, { exact: true })).toBeVisible();
  for (const control of ["fund", "reclaim", "limit", "pause", "resume", "archive", "rotate", "recover"]) {
    await expect(page.getByRole("button", { name: copyEntry(`agent.control.${control}`).message, exact: true })).toHaveCount(0);
  }
});
