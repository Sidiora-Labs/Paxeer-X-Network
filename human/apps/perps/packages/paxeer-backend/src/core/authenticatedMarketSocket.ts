import { usesDirectMarketFeeds } from "./feeds";

/** Approved production origins use direct sockets; previews mint scoped credentials. */
export async function authenticatedMarketSocketUrl(
  url: string,
): Promise<string> {
  const socketUrl = new URL(url);
  if (!["wss:", "ws:"].includes(socketUrl.protocol))
    throw new Error("Invalid market stream URL");
  const scope = socketUrl.pathname.split("/").pop();
  if (!scope || !/^[a-z0-9]+$/.test(scope))
    throw new Error("Invalid market stream");
  if (usesDirectMarketFeeds()) {
    socketUrl.searchParams.delete("token");
    return socketUrl.toString();
  }
  const response = await fetch(
    `/api/price-feed/ws-token?symbol=${encodeURIComponent(scope)}`,
    { cache: "no-store", signal: AbortSignal.timeout(15_000) },
  );
  if (!response.ok) throw new Error("Market stream authentication unavailable");
  const credential = (await response.json()) as {
    token: string;
    symbol: string;
    url: string;
    expiresAt: string;
  };
  if (
    credential.symbol !== scope ||
    !credential.token ||
    Date.parse(credential.expiresAt) <= Date.now()
  )
    throw new Error("Invalid market stream credential");
  const target = new URL(credential.url);
  if (!["wss:", "ws:"].includes(target.protocol))
    throw new Error("Invalid market stream URL");
  target.searchParams.set("token", credential.token);
  return target.toString();
}
