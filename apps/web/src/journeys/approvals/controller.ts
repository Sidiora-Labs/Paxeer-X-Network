import {
  HumanApiError,
  type ActivityEntryDetail,
  type ApprovalDetail,
  type ApprovalSummary,
  type ProgramApprovalSummary,
  type ProgramApprovalDetail,
  type ProgramApprovalBudget,
  type ProgramApprovalMaterial,
  type ProgramApprovalDecision,
  type EvidenceMaterial,
  type HumanApiClient,
  type StepUpEvidence,
} from "../../api/index.ts";
import { performStepUp, type PasskeyAuthenticator } from "./ceremony.ts";
import {
  decidedOutcome,
  convergedOutcome,
  decisionKey,
  defectiveOutcome,
  failureOutcome,
  heldDigest,
  releasedActivity,
  stepUpEvidenceReference,
  validateProgramApproval,
  validateProgramBudget,
  programDecisionMessage,
  canDecideProgram,
  type ApprovalOutcome,
} from "./model.ts";

export interface ApprovalsOptions {
  readonly client: HumanApiClient;
  readonly authenticator: PasskeyAuthenticator;
}

export async function loadProgramApprovals(client: HumanApiClient): Promise<ProgramApprovalSummary[]> {
  const rows: ProgramApprovalSummary[] = [];
  const cursors = new Set<string>();
  const ids = new Set<string>();
  let cursor = "start";
  while (cursor.length > 0) {
    if (cursors.has(cursor) || cursors.size >= 4096) {
      throw new TypeError("The Programs approval inventory returned an invalid cursor chain");
    }
    cursors.add(cursor);
    const page = await client.approvalProgramList(cursor);
    if (page.approvals.length > 100) throw new TypeError("The Programs approval page exceeds its bound");
    for (const row of page.approvals) {
      validateProgramApproval(row);
      if (ids.has(row.approval_id)) throw new TypeError("Duplicate Programs approval identity");
      ids.add(row.approval_id);
      rows.push(row);
    }
    cursor = page.next_cursor;
  }
  return rows;
}


export class Approvals {
  readonly #client: HumanApiClient;
  readonly #authenticator: PasskeyAuthenticator;
  readonly #programDecisions = new Set<string>();
  readonly #programKeys = new Map<string, string>();

  constructor(options: ApprovalsOptions) {
    this.#client = options.client;
    this.#authenticator = options.authenticator;
  }

  async inbox(): Promise<ApprovalSummary[]> {
    const page = await this.#client.approvalList();
    return page.approvals;
  }

  async programInbox(): Promise<ProgramApprovalSummary[]> {
    return loadProgramApprovals(this.#client);
  }

  async programDetail(approvalId: string): Promise<ProgramApprovalDetail> {
    const detail = await this.#client.approvalProgramGet(approvalId);
    if (detail.approval_id !== approvalId) throw new TypeError("Programs detail identity mismatch");
    validateProgramApproval(detail);
    if (detail.budget !== undefined) validateProgramBudget(detail, detail.budget);
    return detail;
  }

  async programMaterial(detail: ProgramApprovalDetail): Promise<ProgramApprovalMaterial> {
    const material = await this.#client.approvalProgramMaterial(detail.approval_id);
    if (material.approval_id !== detail.approval_id || material.held_digest !== detail.held_digest
      || material.provenance !== "local-owned"
      || [material.canonical_unsigned, material.immutable_carrier, material.budget_reservation]
        .some((item) => item.class !== "approval-hold" || item.verification !== "unverified")) {
      throw new TypeError("Programs request material has mismatched ownership or provenance");
    }
    return material;
  }

  async programBudget(detail: ProgramApprovalDetail): Promise<ProgramApprovalBudget> {
    const budget = await this.#client.approvalProgramBudget(detail.approval_id);
    validateProgramBudget(detail, budget);
    return budget;
  }

  async programEvidence(evidenceId: string): Promise<EvidenceMaterial> {
    const material = await this.#client.evidenceGet(evidenceId);
    if (material.evidence_id !== evidenceId) throw new TypeError("Programs evidence identity mismatch");
    return material;
  }

  async decideProgram(detail: ProgramApprovalDetail, action: "approve" | "reject"): Promise<ProgramApprovalDecision> {
    if (this.#programDecisions.has(detail.approval_id)) throw new TypeError("A Programs decision is already in progress");
    this.#programDecisions.add(detail.approval_id);
    try {
      if (!canDecideProgram(detail, new Date())) throw new TypeError("This Programs permission is not awaiting a valid decision");
      const storageKey = `layerx.program-decision.v1.${detail.approval_id}.${detail.held_digest}.${action}`;
      let key: string;
      const retainedKey = this.#programKeys.get(storageKey);
      try {
        const retained = window.sessionStorage.getItem(storageKey);
        key = retainedKey ?? retained ?? decisionKey();
        if (retained === null) window.sessionStorage.setItem(storageKey, key);
      } catch { key = retainedKey ?? decisionKey(); }
      this.#programKeys.set(storageKey, key);
      const disclosure = await this.#client.approvalProgramDisclosure(detail.approval_id, {
        decision: action, held_digest: detail.held_digest, idempotency_key: key,
      });
      if (disclosure.approval_id !== detail.approval_id || disclosure.held_digest !== detail.held_digest
        || disclosure.decision !== action) throw new TypeError("The Programs step-up disclosure does not match the reviewed request");
      const evidence = await performStepUp(this.#client, disclosure.confirms, this.#authenticator);
      const request = { held_digest: detail.held_digest, step_up_evidence: stepUpEvidenceReference(evidence) };
      const decision = action === "approve"
        ? await this.#client.approvalProgramApprove(detail.approval_id, request, key)
        : await this.#client.approvalProgramReject(detail.approval_id, request, key);
      if (decision.approval_id !== detail.approval_id || decision.held_digest !== detail.held_digest) {
        throw new TypeError("The Programs decision is not bound to the reviewed request");
      }
      programDecisionMessage(decision);
      return decision;
    } finally { this.#programDecisions.delete(detail.approval_id); }
  }

  programDecisionPending(approvalId: string): boolean {
    return this.#programDecisions.has(approvalId);
  }

  async detail(approvalId: string): Promise<ApprovalDetail> {
    return this.#client.approvalGet(approvalId);
  }

  async approve(detail: ApprovalDetail, key: string = decisionKey()): Promise<ApprovalOutcome> {
    const digest = heldDigest(detail);
    if (digest === undefined) {
      return defectiveOutcome();
    }
    let evidence: StepUpEvidence;
    try {
      evidence = await performStepUp(this.#client, digest, this.#authenticator);
    } catch (error) {
      if (error instanceof HumanApiError) {
        return this.#failure(error);
      }
      throw error;
    }
    try {
      const decision = await this.#client.approvalApprove(
        detail.approval_id,
        { step_up_evidence: stepUpEvidenceReference(evidence) },
        key,
      );
      return decidedOutcome(decision);
    } catch (error) {
      if (error instanceof HumanApiError) {
        const failure = this.#failure(error);
        if (failure.kind !== "already-decided") {
          return failure;
        }
      }
      return this.resolve(detail.approval_id);
    }
  }

  async reject(approvalId: string, key: string = decisionKey()): Promise<ApprovalOutcome> {
    try {
      const decision = await this.#client.approvalReject(approvalId, key);
      return decidedOutcome(decision);
    } catch (error) {
      if (error instanceof HumanApiError) {
        const failure = this.#failure(error);
        if (failure.kind !== "already-decided") {
          return failure;
        }
      }
      return this.resolve(approvalId);
    }
  }

  async resolve(approvalId: string): Promise<ApprovalOutcome> {
    return convergedOutcome(await this.#client.approvalGet(approvalId));
  }

  async released(approvalId: string): Promise<ActivityEntryDetail | undefined> {
    const page = await this.#client.activityQuery({});
    const entry = releasedActivity(page, approvalId);
    return entry === undefined ? undefined : this.#client.activityEntry(entry.entry_id);
  }

  #failure(error: HumanApiError): ApprovalOutcome {
    const outcome = failureOutcome(error.detail);
    if (outcome === undefined) {
      throw error;
    }
    return outcome;
  }
}
