"use client";

import { useRouter } from "next/navigation";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import type {
  ActivityEntryDetail,
  ApprovalDetail,
  ApprovalSummary,
  ProgramApprovalSummary,
  ProgramApprovalDetail,
  HumanApiClient,
} from "../../api";
import { humanApi } from "../../api";
import { useAuthenticatedShell } from "../../shell/app-shell";
import { errorPresentation, ErrorSurface } from "../../states/error";
import { LoadingSurface, OfflineSurface } from "../../states/surfaces";
import { browserPasskeyAuthenticator } from "./ceremony";
import { Approvals } from "./controller";
import { approvalRoute, programApprovalRoute, type ApprovalOutcome } from "./model";
import { useNotificationCenter } from "../notifications/store";
import {
  ApprovalDecisionConfirmations,
  ApprovalDetailCard,
  DesktopApprovalSplit,
  MobileApprovalInbox,
} from "./screens";

import { ProgramApprovalDetailCard } from "./programs";

type LoadState =
  | Readonly<{ status: "loading"; }>
  | Readonly<{ status: "offline"; }>
  | Readonly<{ status: "error"; error: unknown; }>
  | Readonly<{
    status: "ready";
    approvals: readonly ApprovalSummary[];
    programApprovals: readonly ProgramApprovalSummary[];
    programDetail?: ProgramApprovalDetail;
    detail?: ApprovalDetail;
    released?: ActivityEntryDetail;
  }>;

type DecisionAction = "approve" | "reject";

function durableDecisionKey(approvalId: string, action: DecisionAction): string {
  const storageKey = `layerx.approval-decision.v1.${approvalId}.${action}`;
  try {
    const existing = window.sessionStorage.getItem(storageKey);
    if (existing !== null && existing.length > 0) {
      return existing;
    }
    const created = crypto.randomUUID().replaceAll("-", "");
    window.sessionStorage.setItem(storageKey, created);
    return created;
  } catch {
    return crypto.randomUUID().replaceAll("-", "");
  }
}

function clearDecisionKey(approvalId: string, action: DecisionAction): void {
  try {
    window.sessionStorage.removeItem(`layerx.approval-decision.v1.${approvalId}.${action}`);
  } catch {
    return;
  }
}

export function ApprovalsJourneyScreen({
  approvalId,
  program = false,
  client: suppliedClient,
}: Readonly<{ approvalId?: string; program?: boolean; client?: HumanApiClient; }>) {
  const client = useMemo(() => suppliedClient ?? humanApi(), [suppliedClient]);
  const approvals = useMemo(() => new Approvals({
    client,
    authenticator: browserPasskeyAuthenticator(),
  }), [client]);
  const { shell } = useAuthenticatedShell();
  const notificationCenter = useNotificationCenter();
  const router = useRouter();
  const loadGeneration = useRef(0);
  const [loadState, setLoadState] = useState<LoadState>({ status: "loading" });
  const [at, setAt] = useState(() => new Date());
  const [outcome, setOutcome] = useState<ApprovalOutcome | undefined>(undefined);
  const [deciding, setDeciding] = useState(false);
  const [approveOpen, setApproveOpen] = useState(false);
  const [rejectOpen, setRejectOpen] = useState(false);

  const load = useCallback(async () => {
    const generation = ++loadGeneration.current;
    setLoadState({ status: "loading" });
    try {
      const [inbox, programApprovals, detail, programDetail] = await Promise.all([
        approvals.inbox(), approvals.programInbox(),
        approvalId === undefined || program ? Promise.resolve(undefined) : approvals.detail(approvalId),
        approvalId === undefined || !program ? Promise.resolve(undefined) : approvals.programDetail(approvalId),
      ]);
      const released = detail?.state === "approved"
        ? await approvals.released(detail.approval_id)
        : undefined;
      if (generation !== loadGeneration.current) return;
      setLoadState({
        status: "ready",
        approvals: inbox,
        programApprovals,
        ...(programDetail === undefined ? {} : { programDetail }),
        ...(detail === undefined ? {} : { detail }),
        ...(released === undefined ? {} : { released }),
      });
    } catch (error) {
      if (generation !== loadGeneration.current) return;
      setLoadState(navigator.onLine ? { status: "error", error } : { status: "offline" });
    }
  }, [approvalId, approvals, program]);

  useEffect(() => {
    setOutcome(undefined);
    void load();
    return () => { loadGeneration.current += 1; };
  }, [load, notificationCenter.revision]);

  useEffect(() => {
    const timer = window.setInterval(() => { setAt(new Date()); }, 1_000);
    return () => { window.clearInterval(timer); };
  }, []);

  const refreshResolved = useCallback(async () => {
    if (approvalId === undefined) {
      return;
    }
    const resolved = await approvals.resolve(approvalId);
    setOutcome(resolved);
    if (resolved.kind !== "still-checking") {
      await load();
    }
  }, [approvalId, approvals, load]);

  useEffect(() => {
    if (outcome?.kind !== "still-checking") {
      return;
    }
    const timer = window.setInterval(() => { void refreshResolved(); }, 3_000);
    return () => { window.clearInterval(timer); };
  }, [outcome, refreshResolved]);

  useEffect(() => {
    if (
      loadState.status !== "ready"
      || loadState.detail?.state !== "approved"
      || loadState.released !== undefined
    ) {
      return;
    }
    const approvalReference = loadState.detail.approval_id;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const track = async () => {
      try {
        const released = await approvals.released(approvalReference);
        if (cancelled) {
          return;
        }
        if (released !== undefined) {
          setLoadState((current) => current.status === "ready"
            ? { ...current, released }
            : current);
          return;
        }
      } catch {
        if (cancelled) {
          return;
        }
      }
      timer = setTimeout(() => { void track(); }, 3_000);
    };
    void track();
    return () => {
      cancelled = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
    };
  }, [approvals, loadState]);

  if (loadState.status === "loading") {
    return <LoadingSurface rows={5} />;
  }
  if (loadState.status === "offline") {
    return <OfflineSurface onRetry={() => { void load(); }} />;
  }
  if (loadState.status === "error") {
    return (
      <ErrorSurface
        error={errorPresentation(loadState.error)}
        route={approvalId === undefined ? "/app/approvals" : approvalRoute(approvalId)}
        onRetry={() => { void load(); }}
        onReload={() => { window.location.reload(); }}
      />
    );
  }

  const open = (id: string) => { router.push(approvalRoute(id)); };
  const openProgram = (id: string) => { router.push(programApprovalRoute(id)); };
  const detail = loadState.detail;

  const decide = async (action: DecisionAction) => {
    if (detail === undefined || deciding) {
      return;
    }
    setDeciding(true);
    const key = durableDecisionKey(detail.approval_id, action);
    try {
      const next = action === "approve"
        ? await approvals.approve(detail, key)
        : await approvals.reject(detail.approval_id, key);
      setOutcome(next);
      setApproveOpen(false);
      setRejectOpen(false);
      if (next.kind !== "still-checking") {
        clearDecisionKey(detail.approval_id, action);
        await Promise.all([load(), notificationCenter.refresh()]);
      }
    } catch (error) {
      setLoadState({ status: "error", error });
    } finally {
      setDeciding(false);
    }
  };

  const detailCard = loadState.programDetail !== undefined ? (
    <ProgramApprovalDetailCard detail={loadState.programDetail} controller={approvals} at={at} shell={shell}
      onChanged={async () => { await Promise.all([load(), notificationCenter.refresh()]); }} />
  ) : detail === undefined ? undefined : (
    <>
      <ApprovalDetailCard
        detail={detail}
        at={at}
        shell={shell}
        outcome={outcome}
        released={loadState.released}
        deciding={deciding || outcome?.kind === "still-checking"}
        onApprove={() => { setApproveOpen(true); }}
        onReject={() => { setRejectOpen(true); }}
      />
      <ApprovalDecisionConfirmations
        detail={detail}
        shell={shell}
        approveOpen={approveOpen}
        rejectOpen={rejectOpen}
        onApproveOpenChange={setApproveOpen}
        onRejectOpenChange={setRejectOpen}
        onConfirmApprove={() => { void decide("approve"); }}
        onConfirmReject={() => { void decide("reject"); }}
        deciding={deciding}
      />
    </>
  );

  return shell === "mobile" ? (
    detailCard ?? <MobileApprovalInbox approvals={loadState.approvals} programApprovals={loadState.programApprovals} at={at} onOpen={open} onOpenProgram={openProgram} />
  ) : (
    <DesktopApprovalSplit
      approvals={loadState.approvals}
      programApprovals={loadState.programApprovals}
      onOpenProgram={openProgram}
      at={at}
      onOpen={open}
      selectedId={detail?.approval_id ?? loadState.programDetail?.approval_id}
    >
      {detailCard}
    </DesktopApprovalSplit>
  );
}
