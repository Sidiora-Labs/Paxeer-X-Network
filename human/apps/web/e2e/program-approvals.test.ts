import assert from "node:assert/strict";
import { readFile, lstat } from "node:fs/promises";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { chromium } from "@playwright/test";
import { createHumanApiClient, HumanApiError } from "../src/api/generated/index.ts";
import { loadProgramApprovals } from "../src/journeys/approvals/controller.ts";
import { canDecideProgram, programApprovalTitle, programApprovalRoute, validateProgramBudget } from "../src/journeys/approvals/model.ts";
import { copyEntry } from "../copy/runtime.ts";

interface GenuineFixture {
  schema: string;
  url: string;
  origin: string;
  ui_url: string;
  cookies: Record<string, string>;
  browser_cookies: Array<{ name: string; value: string; domain: string; path: string; secure: boolean; httpOnly: boolean; sameSite: "Strict" | "Lax" | "None"; }>;
  operation_only_id: string;
  authorized_limits_id: string;
  expired_step_up_evidence: string;
  browser_executable: string;
  expected_program_id: string;
}

test("genuine Programs owner reads, evidence, disclosure, browser and push remain honest", { timeout: 420_000 }, async () => {
  const path = process.env.PAXEER_X_HUMAN_PROGRAM_APPROVAL_FIXTURE;
  assert.ok(path, "genuine protected Human/Programs/browser fixture is required");
  const info = await lstat(path);
  assert.ok(info.isFile() && info.size > 0 && info.size <= 65_536 && (info.mode & 0o077) === 0);
  const fixture = JSON.parse(await readFile(path, "utf8")) as GenuineFixture;
  assert.equal(fixture.schema, "paxeer-x.human-program-approvals.v1");
  const jar = new Map(Object.entries(fixture.cookies));
  const client = createHumanApiClient({
    baseUrl: fixture.url,
    csrfToken: () => jar.get("__Host-layerx_csrf"),
    fetch: async (url, init) => {
      const headers = new Headers(init?.headers);
      headers.set("Origin", fixture.origin);
      headers.set("Cookie", [...jar].map(([name, value]) => `${name}=${value}`).join("; "));
      const response = await fetch(url, { ...init, headers, redirect: "error" });
      for (const cookie of response.headers.getSetCookie()) {
        const pair = cookie.split(";")[0]!;
        const index = pair.indexOf("=");
        jar.set(pair.slice(0, index), pair.slice(index + 1));
      }
      return response;
    },
  });
  const cursor = (await client.streamOpen()).cursor;
  const abort = new AbortController();
  const iterator = client.streamSubscribe(cursor, { signal: abort.signal })[Symbol.asyncIterator]();
  const next = iterator.next();
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const rows = await loadProgramApprovals(client);
    assert.ok(rows.some((row) => row.approval_id === fixture.operation_only_id));
    assert.ok(rows.some((row) => row.approval_id === fixture.authorized_limits_id));
    const simple = await client.approvalProgramGet(fixture.operation_only_id);
    const bounded = await client.approvalProgramGet(fixture.authorized_limits_id);
    assert.equal(simple.operation.program_id, fixture.expected_program_id);
    assert.equal(simple.semantics, "operation-only");
    assert.equal(simple.authorized_limits.length, 0);
    assert.equal(bounded.semantics, "authorized-limits");
    assert.ok(bounded.authorized_limits.length > 0);
    assert.ok(!("amount" in simple) && !("counterparty" in simple) && !("money" in simple));
    assert.equal(programApprovalTitle(simple), copyEntry(`approval.program.${simple.operation.kind}`).message);
    assert.ok(canDecideProgram(simple, new Date()));
    const material = await client.approvalProgramMaterial(simple.approval_id);
    assert.equal(material.held_digest, simple.held_digest);
    assert.equal(material.provenance, "local-owned");
    for (const reference of material.evidence) {
      const record = await client.evidenceGet(reference.evidence_id);
      assert.equal(record.verification, "unverified");
      assert.equal(record.class, "approval-hold");
      assert.equal(record.evidence_id, reference.evidence_id);
      const bytes = Buffer.from(record.bytes_base64, "base64");
      assert.ok(bytes.length > 0);
      const digest = createHash("sha256").update(bytes).digest("hex");
      if (reference.evidence_id === material.canonical_unsigned.evidence_id) assert.equal(digest, simple.approval_id.slice(4));
      if (reference.evidence_id === material.immutable_carrier.evidence_id) assert.equal(digest, simple.held_digest);
    }
    const budget = await client.approvalProgramBudget(bounded.approval_id);
    validateProgramBudget(bounded, budget);
    for (const reference of budget.evidence) {
      const record = await client.evidenceGet(reference.evidence_id);
      assert.equal(record.class, "checkpoint-proof");
      assert.equal(record.verification, budget.verification);
      assert.equal(createHash("sha256").update(Buffer.from(record.bytes_base64, "base64")).digest("hex"), budget.proof_digest);
    }
    const idempotency = crypto.randomUUID().replaceAll("-", "");
    const disclosure = await client.approvalProgramDisclosure(simple.approval_id, {
      decision: "approve", held_digest: simple.held_digest, idempotency_key: idempotency,
    });
    assert.equal(disclosure.held_digest, simple.held_digest);
    assert.equal(disclosure.decision, "approve");
    const challenge = await client.stepupBegin({ confirms: disclosure.confirms });
    assert.equal(challenge.confirms, disclosure.confirms);
    await assert.rejects(client.approvalProgramApprove(simple.approval_id, {
      held_digest: simple.held_digest, step_up_evidence: fixture.expired_step_up_evidence,
    }, idempotency), (error: unknown) => error instanceof HumanApiError && error.detail.code === "step-up-required");
    const current = await client.approvalProgramGet(simple.approval_id);
    assert.equal(current.state, simple.state);
    assert.equal(current.release_ref, simple.release_ref);
    const pushed = await Promise.race([next, new Promise<never>((_, reject) => {
      timer = setTimeout(() => { reject(new Error("actual durable Programs push deadline")); }, 30_000);
    })]);
    assert.equal(pushed.done, false);
    assert.ok(pushed.value?.program_approval !== undefined);
    assert.ok(pushed.value.cursor !== cursor);
    const browser = await chromium.launch({ executablePath: fixture.browser_executable, headless: true });
    try {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      await context.addCookies(fixture.browser_cookies);
      const page = await context.newPage();
      await page.goto(`${fixture.ui_url}${programApprovalRoute(simple.approval_id)}`);
      const detail = page.locator('[data-application="program-approval-detail"]');
      await detail.waitFor({ state: "visible" });
      assert.ok((await detail.innerText()).includes(copyEntry("approval.program.consequence").message));
      assert.equal(await detail.getByText(copyEntry("approval.detail.counterparty").message, { exact: true }).count(), 0);
      assert.equal(await detail.getByText(copyEntry("approval.detail.amount").message, { exact: true }).count(), 0);
      await detail.getByRole("button", { name: copyEntry("approval.approve.action").message, exact: true }).click();
      assert.ok(await page.getByText(copyEntry("approval.program.confirm").message, { exact: true }).isVisible());
      await context.close();
    } finally { await browser.close(); }
  } finally {
    if (timer !== undefined) clearTimeout(timer);
    abort.abort();
    await iterator.return?.();
  }
});
