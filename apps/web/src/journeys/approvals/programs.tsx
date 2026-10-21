"use client";

import { useEffect, useRef, useState } from "react";
import { copyEntry } from "../../../copy/runtime";
import { formatCopy } from "../../../copy/format";
import { HumanApiError } from "../../api";
import type { EvidenceRef, ProgramApprovalBudget, ProgramApprovalDetail, ProgramApprovalMaterial, ProgramApprovalSummary } from "../../api";
import { Badge, ListItem } from "../../kit/collection";
import { KitButton } from "../../kit/control";
import { CopyableIdentifier, LabelValue } from "../../kit/money";
import { DesktopDetail, MobileDetail } from "../../kit/pattern-detail";
import { DesktopConfirmation, MobileConfirmation } from "../../kit/confirm";
import { ScreenCard } from "../../kit/surface";
import { errorPresentation } from "../../states/error";
import type { Shell } from "../../shell/selector";
import type { Approvals } from "./controller";
import { canDecideProgram, expiryCountdown, programApprovalState, programApprovalTitle, programDecisionMessage, requestedAtLabel, verificationLabel } from "./model";

export function ProgramApprovalInboxRows({ approvals, at, onOpen, selectedId }: Readonly<{
  approvals: readonly ProgramApprovalSummary[];
  at: Date;
  onOpen?: ((id: string) => void) | undefined;
  selectedId?: string | undefined;
}>) {
  return <>{approvals.map((approval) => {
    const countdown = expiryCountdown(approval.expires_at, at);
    const state = programApprovalState(approval.state === "pending" && countdown.expired ? "expired" : approval.state);
    return <ListItem key={`program:${approval.approval_id}`} navigates
      aria-current={selectedId === approval.approval_id ? "true" : undefined}
      onClick={onOpen === undefined ? undefined : () => onOpen(approval.approval_id)}
      title={programApprovalTitle(approval)} subtitle={approval.agent_name}
      trailing={<Badge variant={state.tone} size="sm">{state.label}</Badge>}
      trailingCaption={<span role="timer">{countdown.label}</span>} />;
  })}</>;
}

function retainedRecord(bytesBase64: string): Uint8Array<ArrayBuffer> {
  if (bytesBase64.length === 0 || bytesBase64.length > 1_398_104) throw new TypeError("Record exceeds the actual evidence bound");
  const binary = atob(bytesBase64);
  if (binary.length === 0 || binary.length > 1_048_576) throw new TypeError("Invalid retained record length");
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes;
}

export function ProgramApprovalDetailCard({ detail, controller, at, shell, onChanged }: Readonly<{
  detail: ProgramApprovalDetail;
  controller: Approvals;
  at: Date;
  shell: Shell;
  onChanged: () => Promise<void>;
}>) {
  const generation = useRef(0);
  const [open, setOpen] = useState(false);
  const [material, setMaterial] = useState<ProgramApprovalMaterial>();
  const [budget, setBudget] = useState<ProgramApprovalBudget | undefined>(detail.budget);
  const [error, setError] = useState<unknown>();
  const [message, setMessage] = useState<string>();
  const [action, setAction] = useState<"approve" | "reject">();
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  useEffect(() => {
    generation.current += 1;
    setBudget(detail.budget);
    setMaterial(undefined);
    setMessage(undefined);
    setError(undefined);
    setAction(undefined);
    setUncertain(false);
    return () => { generation.current += 1; };
  }, [detail]);
  const StateDetail = shell === "mobile" ? MobileDetail : DesktopDetail;
  const Confirmation = shell === "mobile" ? MobileConfirmation : DesktopConfirmation;
  const expired = expiryCountdown(detail.expires_at, at);
  const state = programApprovalState(detail.state === "pending" && expired.expired ? "expired" : detail.state);
  const approvable = !uncertain && !controller.programDecisionPending(detail.approval_id) && canDecideProgram(detail, at)
    && (detail.semantics === "operation-only" || budget !== undefined);
  const showMaterial = async () => {
    setOpen(true);
    const current = generation.current;
    try { const next = await controller.programMaterial(detail); if (current === generation.current) setMaterial(next); }
    catch (failure) { setError(failure); }
  };
  const checkBudget = async () => {
    const current = generation.current;
    try { const next = await controller.programBudget(detail); if (current === generation.current) setBudget(next); }
    catch (failure) { setError(failure); }
  };
  const download = async (reference: EvidenceRef) => {
    const current = generation.current;
    try {
      const record = await controller.programEvidence(reference.evidence_id);
      if (record.class !== reference.class || record.verification !== reference.verification) {
        throw new TypeError("The exported record differs from its actual evidence reference");
      }
      if (current !== generation.current) return;
      const content = retainedRecord(record.bytes_base64);
      const url = URL.createObjectURL(new Blob([content.buffer], { type: record.content_type }));
      const anchor = document.createElement("a");
      anchor.href = url; anchor.download = `${record.evidence_id}.bin`; anchor.click();
      URL.revokeObjectURL(url);
    } catch (failure) { setError(failure); }
  };
  const decide = async () => {
    if (action === undefined || busy || !approvable) return;
    const current = generation.current;
    setBusy(true); setError(undefined);
    try {
      const decision = await controller.decideProgram(detail, action);
      if (current !== generation.current) return;
      setMessage(programDecisionMessage(decision)); setAction(undefined);
      await onChanged();
    } catch (failure) {
      if (current !== generation.current) return;
      setError(failure);
      if (!(failure instanceof HumanApiError) || failure.detail.retry !== "final") setUncertain(true);
    }
    finally { if (current === generation.current) setBusy(false); }
  };
  return <ScreenCard dataApplication="program-approval-detail" landmark="section" title={programApprovalTitle(detail)}>
    <div className="flex items-center gap-3"><Badge variant={state.tone}>{state.label}</Badge>
      {detail.state === "pending" ? <span role="timer">{expired.label}</span> : null}</div>
    <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <LabelValue label={copyEntry("approval.detail.agent").message} value={detail.agent_name} />
      <LabelValue label={copyEntry("approval.detail.requested").message} value={requestedAtLabel(detail.created_at)} />
    </dl>
    <p>{copyEntry(`approval.program.${detail.semantics}`).message}</p>
    <p>{copyEntry("approval.program.consequence").message}</p>
    {detail.semantics === "authorized-limits" ? <section className="flex flex-col gap-3">
      <h2>{copyEntry("approval.program.limits").message}</h2>
      {detail.authorized_limits.map((limit, index) => <div key={`${limit.source_account}:${limit.asset_id}:${index}`}>
        <p>{formatCopy("approval.program.units", { amount: limit.maximum_amount.toString() })}</p>
        <CopyableIdentifier label={copyEntry("approval.program.asset").message} value={limit.asset_id} />
        {limit.destination === undefined ? null : <CopyableIdentifier label={copyEntry("approval.program.destination").message} value={limit.destination} />}
      </div>)}
    </section> : null}
    <section className="flex flex-col gap-3">
      <h2>{copyEntry("approval.program.budget").message}</h2>
      {budget === undefined ? <p>{copyEntry("approval.program.budget-unavailable").message}</p> : <>
        <p>{`${formatCopy("approval.program.units", { amount: budget.remaining.toString() })} · ${verificationLabel(budget.verification)}`}</p>
        <CopyableIdentifier label={copyEntry("approval.program.asset").message} value={budget.asset_id} />
        {budget.evidence.map((reference) => <KitButton key={reference.evidence_id} variant="secondary" onClick={() => { void download(reference); }}>{copyEntry("approval.program.export").message}</KitButton>)}
      </>}
      <KitButton variant="secondary" {...(busy ? { disabled: true as const, disabledReason: copyEntry("state.loading.body").message } : {})} onClick={() => { void checkBudget(); }}>{copyEntry("action.retry").message}</KitButton>
    </section>
    <KitButton variant="secondary" onClick={() => { void showMaterial(); }}>{copyEntry("error.technical.title").message}</KitButton>
    <StateDetail open={open} onOpenChange={setOpen} title={copyEntry("approval.program.material").message} summary={copyEntry("error.technical.title").message} desktopVariant="inline">
      <div className="flex flex-col gap-3">
        <CopyableIdentifier label={copyEntry("approval.program.program").message} value={detail.operation.program_id} />
        <CopyableIdentifier label={copyEntry("approval.program.held").message} value={detail.held_digest} />
        {detail.release_ref === undefined ? null : <CopyableIdentifier label={copyEntry("approval.program.permission").message} value={detail.release_ref} />}
        <LabelValue label={copyEntry("approval.program.created-sequence").message} value={detail.created_at_sequence.toString()} />
        <LabelValue label={copyEntry("approval.program.expiry-sequence").message} value={detail.budget_expiry_sequence.toString()} />
        <pre className="overflow-auto whitespace-pre-wrap text-xs">{JSON.stringify(detail.operation, (_, value: unknown) => typeof value === "bigint" ? value.toString() : value, 2)}</pre>
        <p>{copyEntry("approval.program.material-local").message}</p>
        {material?.evidence.map((reference) => <div key={reference.evidence_id}>
          <CopyableIdentifier label={verificationLabel(reference.verification)} value={reference.evidence_id} />
          <KitButton variant="secondary" onClick={() => { void download(reference); }}>{copyEntry("approval.program.export").message}</KitButton>
        </div>)}
      </div>
    </StateDetail>
    {message === undefined ? null : <p role="status">{message}</p>}
    {error === undefined ? null : <p role="alert">{copyEntry(errorPresentation(error).descriptionKey).message}</p>}
    {uncertain ? <div role="status"><p>{copyEntry("approval.program.checking").message}</p>
      <KitButton variant="secondary" onClick={() => { void onChanged(); }}>{copyEntry("action.retry").message}</KitButton></div> : null}
    {detail.state === "approved" ? <p role="status">{copyEntry("approval.program.approved").message}</p> : null}
    {approvable ? <div className="flex gap-3">
      <KitButton variant="primary" {...(busy ? { disabled: true as const, disabledReason: copyEntry("state.loading.body").message } : {})} onClick={() => { setAction("approve"); }}>{copyEntry("approval.approve.action").message}</KitButton>
      <KitButton variant="secondary" {...(busy ? { disabled: true as const, disabledReason: copyEntry("state.loading.body").message } : {})} onClick={() => { setAction("reject"); }}>{copyEntry("approval.reject.action").message}</KitButton>
    </div> : null}
    <Confirmation kind="destructive" open={action !== undefined} onOpenChange={(next) => { if (!next && !busy) setAction(undefined); }}
      title={copyEntry("approval.program.confirm").message} consequence={copyEntry("approval.program.consequence").message}
      confirmLabel={copyEntry(action === "reject" ? "approval.reject.action" : "approval.approve.action").message}
      onConfirm={() => { void decide(); }} loading={busy} />
  </ScreenCard>;
}
