"use client";

import {
  AppShell,
  Badge,
  Card,
  EmptyState,
  List,
  ListItem,
  SectionHeader,
  SegmentedControl,
  SkeletonRow,
  Stat,
  StatPair,
} from "@layerx/ui";
import { useCallback, useEffect, useMemo, useState } from "react";

import { decodeOverview, decodeRequests, decodeDeliveries, decodeReceipt, type Verification, type KeyView, type RequestRecord, type Endpoint, type Delivery, type Payment, type Overview, type Receipt } from "./data";

const tabs = [
  { value: "overview", label: "Overview" },
  { value: "keys", label: "Keys" },
  { value: "requests", label: "Logs" },
  { value: "webhooks", label: "Webhooks" },
  { value: "payments", label: "Payments" },
];

function verificationBadge(level: Verification) {
  return <Badge size="sm" variant={level === "unverified" ? "warning" : "success"}>{level}</Badge>;
}

async function read<T>(path: string, decode: (value: unknown) => T, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`/v1/dashboard${path}`, { credentials: "include", cache: "no-store", signal });
  if (!response.ok) throw new Error(`Dashboard dependency returned ${response.status}`);
  return decode(await response.json());
}

export default function DeveloperDashboard() {
  const [active, setActive] = useState("overview");
  const [overview, setOverview] = useState<Overview>();
  const [requests, setRequests] = useState<RequestRecord[]>([]);
  const [deliveries, setDeliveries] = useState<Delivery[]>([]);
  const [error, setError] = useState<string>();
  const [receipt, setReceipt] = useState<Receipt>();

  const refresh = useCallback(async (signal?: AbortSignal) => {
    try {
      const [summary, requestLog, webhookLog] = await Promise.all([
        read("/overview", decodeOverview, signal),
        read("/requests?limit=100", decodeRequests, signal),
        read("/webhook-deliveries?limit=100", decodeDeliveries, signal),
      ]);
      setOverview(summary);
      setRequests(requestLog);
      setDeliveries(webhookLog);
      setError(undefined);
    } catch (cause) {
      if ((cause as Error).name !== "AbortError") { setError((cause as Error).message); setOverview(undefined); setRequests([]); setDeliveries([]); setReceipt(undefined); }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  const nav = useMemo(() => [
    { id: "home", label: "Overview" },
    { id: "agents", label: "Keys" },
    { id: "activity", label: "Logs", badge: overview?.dead_letters.length },
    { id: "more", label: "Webhooks" },
  ], [overview]);

  return (
    <AppShell
      nav={nav}
      activeNav={active === "overview" ? "home" : active === "keys" ? "agents" : active === "requests" ? "activity" : "more"}
      onNavigate={(value) => setActive(value === "home" ? "overview" : value === "agents" ? "keys" : value === "activity" ? "requests" : "webhooks")}
      title="Developer dashboard"
      user={{ name: overview?.principal ?? "Developer", initials: "DX" }}
    >
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 md:p-6">
        <SegmentedControl aria-label="Dashboard section" options={tabs} value={active} onValueChange={setActive} />
        {error ? <Card elevation="outline"><EmptyState title="Dashboard unavailable" description={error} /></Card> : null}
        {!overview && !error ? <Card><SkeletonRow /><SkeletonRow /><SkeletonRow /></Card> : null}
        {overview && active === "overview" ? <OverviewPanel value={overview} /> : null}
        {overview && active === "keys" ? <KeysPanel keys={overview.keys} /> : null}
        {overview && active === "requests" ? <RequestsPanel requests={requests} /> : null}
        {overview && active === "webhooks" ? <WebhooksPanel endpoints={overview.endpoints} deliveries={deliveries} /> : null}
        {overview && active === "payments" ? <PaymentsPanel payments={overview.payments} onReceipt={async (activity) => {
          try {
            const value = await read(`/receipts/${encodeURIComponent(activity)}`, decodeReceipt);
            if (value.activity_id !== activity) throw new Error("Receipt activity mismatch");
            setReceipt(value);
            setError(undefined);
          } catch (cause) { setReceipt(undefined); setError((cause as Error).message); }
        }} /> : null}
        {receipt && active === "payments" ? <Card><SectionHeader title="Verified receipt" /><List><ListItem title={receipt.activity_id} subtitle={`${receipt.event} · ${receipt.receipt_digest}`} trailing={verificationBadge(receipt.verification)} /></List></Card> : null}
      </div>
    </AppShell>
  );
}

function OverviewPanel({ value }: { value: Overview }) {
  return <>
    <Card><StatPair left={{ value: value.usage.requests_used, label: "Requests used" }} right={{ value: value.usage.requests_remaining, label: "Requests remaining" }} /></Card>
    <div className="grid gap-4 md:grid-cols-3">
      <Card><Stat value={value.usage.live_keys} label="Live API keys" /></Card>
      <Card><Stat value={value.endpoints.length} label="Webhook endpoints" /></Card>
      <Card><Stat value={value.payments.filter((payment) => payment.settled).length} label="Verified test payments" /></Card>
    </div>
    <Card><SectionHeader title="Recent protocol activity" /><List>{value.recent_requests.slice(0, 8).map((request) => <ListItem key={`${request.at}-${request.operation_digest}`} title={request.outcome} subtitle={request.operation_digest} trailing={verificationBadge(request.verification)} trailingCaption={new Date(request.at * 1000).toLocaleString()} />)}</List></Card>
  </>;
}

function KeysPanel({ keys }: { keys: KeyView[] }) {
  return <Card><SectionHeader title="API keys and current quota windows" />{keys.length === 0 ? <EmptyState title="No API keys" description="Issue a principal-bound key through the gateway key management surface." /> : <List>{keys.map((key) => <ListItem key={key.key_id} title={key.key_id} subtitle={`${key.used_in_window} of ${key.requests_per_window} requests used · ${key.window_seconds}s window`} trailing={<Badge variant={key.disabled ? "destructive" : "success"}>{key.disabled ? "revoked" : "active"}</Badge>} trailingCaption={`${key.remaining_in_window} remaining`} />)}</List>}</Card>;
}

function RequestsPanel({ requests }: { requests: RequestRecord[] }) {
  return <Card><SectionHeader title="Principal-isolated request log" /><List>{requests.map((request) => <ListItem key={`${request.at}-${request.operation_digest}`} title={request.outcome} subtitle={request.operation_digest} trailing={verificationBadge(request.verification)} trailingCaption={new Date(request.at * 1000).toLocaleString()} />)}</List></Card>;
}

function WebhooksPanel({ endpoints, deliveries }: { endpoints: Endpoint[]; deliveries: Delivery[] }) {
  return <div className="grid gap-5 lg:grid-cols-2"><Card><SectionHeader title="Webhook endpoints" /><List>{endpoints.map((endpoint) => <ListItem key={endpoint.endpoint} title={endpoint.url} subtitle={`${endpoint.pending} pending · ${endpoint.in_flight} in flight · ${endpoint.retrying} retrying`} trailing={<Badge variant={endpoint.suspended ? "destructive" : "success"}>{endpoint.suspended ? "suspended" : "active"}</Badge>} trailingCaption={`${endpoint.delivered_total} delivered / ${endpoint.dead_lettered_total} dead-lettered`} />)}</List></Card><Card><SectionHeader title="Delivery log" /><List>{deliveries.map((delivery) => <ListItem key={delivery.delivery} title={delivery.event} subtitle={`${delivery.endpoint} · ${delivery.delivery}`} trailing={verificationBadge(delivery.verification)} trailingCaption={delivery.state.state} />)}</List></Card></div>;
}

function PaymentsPanel({ payments, onReceipt }: { payments: Payment[]; onReceipt: (activity: string) => Promise<void> }) {
  return <Card><SectionHeader title="Real test payments and verified receipts" />{payments.length === 0 ? <EmptyState title="No test payments" description="Payments appear only after the canonical payment source and independent receipt authority agree." /> : <List>{payments.map((payment) => <ListItem key={payment.event} title={`${payment.amount ?? "—"} ${payment.asset ?? ""}`} subtitle={`${payment.event} · ${payment.facts.map((fact) => `${fact.name}=${fact.value} [${fact.verification}]`).join(" · ")} · receipt ${payment.receipt_digest ?? "not available"} · all facts ${payment.verification}`} trailing={<div>{verificationBadge(payment.settlement_verification)}{payment.settled ? payment.facts.filter((fact) => fact.name === "activity_id" && fact.verification !== "unverified" && fact.receipt_digest === payment.receipt_digest).map((fact) => <button key={fact.value} type="button" onClick={() => void onReceipt(fact.value)}>View receipt</button>) : null}</div>} trailingCaption={payment.settled ? "settlement receipt verified" : "settlement not verified"} />)}</List>}</Card>;
}
