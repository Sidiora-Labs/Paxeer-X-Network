import { cookies, headers } from "next/headers";

import { selectServerShell } from "../../../shell/server";
import { SupportChat } from "../../../support/chat";
import type { TraceId } from "../../../api";

export const dynamic = "force-dynamic";

export default async function SupportPage({
  searchParams,
}: Readonly<{ searchParams: Promise<Readonly<Record<string, string | string[] | undefined>>> }>) {
  const [requestHeaders, requestCookies, params] = await Promise.all([headers(), cookies(), searchParams]);
  const selection = selectServerShell(requestHeaders, requestCookies);
  const traceId: TraceId | undefined = typeof params.trace === "string"
    && /^trc_[0-9a-f]{32}$/u.test(params.trace)
    ? params.trace
    : undefined;
  return <SupportChat platform={selection.shell} {...(traceId === undefined ? {} : { traceId })} />;
}
