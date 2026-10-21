import { notFound } from "next/navigation";

import { ApprovalsJourneyScreen } from "../../../../journeys/approvals";

export default async function ApprovalDetailPage({
  params,
  searchParams,
}: Readonly<{ params: Promise<{ approvalId: string; }>; searchParams: Promise<{ kind?: string | string[]; }>; }>) {
  const [{ approvalId }, query] = await Promise.all([params, searchParams]);
  if (query.kind !== undefined && query.kind !== "program") notFound();
  if (approvalId.length === 0 || approvalId.length > 128) {
    notFound();
  }
  return <ApprovalsJourneyScreen approvalId={approvalId} program={query.kind === "program"} />;
}
