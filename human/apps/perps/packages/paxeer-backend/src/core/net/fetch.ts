// Shared bounded HTTP transport. Mutations are never retried automatically.
export type FetchErrorKind = "timeout" | "abort" | "network" | "http" | "parse" | "unknown";
export class FetchPolicyError extends Error {
  readonly kind: FetchErrorKind;
  readonly status?: number;
  readonly url: string;
  readonly cause?: unknown;
  constructor(kind: FetchErrorKind, url: string, message: string, opts?: { status?: number; cause?: unknown }) {
    super(message); this.name = "FetchPolicyError"; this.kind = kind; this.url = url;
    this.status = opts?.status; this.cause = opts?.cause;
  }
}
export interface FetchPolicy extends Omit<RequestInit, "body"> {
  timeoutMs?: number;
  retries?: number;
  retryBaseMs?: number;
  idempotent?: boolean;
  stream?: boolean;
  body?: BodyInit | object | null;
}
function aborted(url: string) { return new FetchPolicyError("abort", url, "Request cancelled"); }
export function retryAfterMs(value: string | null, now = Date.now()): number {
  if (!value) return 0;
  const seconds = Number(value);
  return Math.max(0, Number.isFinite(seconds) ? seconds * 1000 : (Date.parse(value) || now) - now);
}
function wait(ms: number, signal: AbortSignal | null | undefined, url: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) { reject(aborted(url)); return; }
    const abort = () => { clearTimeout(timer); signal?.removeEventListener("abort", abort); reject(aborted(url)); };
    const timer = setTimeout(() => { signal?.removeEventListener("abort", abort); resolve(); }, ms);
    signal?.addEventListener("abort", abort, { once: true });
  });
}
/** Includes body download in the deadline, so stalled JSON can't hang after headers. */
export async function fetchWithPolicy(input: string | URL | Request, policy: FetchPolicy = {}): Promise<Response> {
  const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
  const { timeoutMs = 8000, retries: requested = 2, retryBaseMs = 250, idempotent, stream = false, body: rawBody, ...init } = policy;
  const method = (init.method || (input instanceof Request ? input.method : "GET")).toUpperCase();
  const retries = idempotent === true || ["GET", "HEAD", "OPTIONS"].includes(method) ? Math.min(3, Math.max(0, requested)) : 0;
  const headers = new Headers(init.headers);
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  let body: BodyInit | undefined;
  if (rawBody !== undefined && rawBody !== null) {
    if (typeof rawBody === "string" || rawBody instanceof FormData || rawBody instanceof Blob || rawBody instanceof URLSearchParams || rawBody instanceof ArrayBuffer || ArrayBuffer.isView(rawBody) || rawBody instanceof ReadableStream) body = rawBody as BodyInit;
    else { body = JSON.stringify(rawBody); if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json"); }
  }
  const callerSignal = init.signal || (input instanceof Request ? input.signal : undefined);
  for (let attempt = 0; ; attempt++) {
    if (callerSignal?.aborted) throw aborted(url);
    const controller = new AbortController();
    const forwardAbort = () => controller.abort(callerSignal?.reason);
    callerSignal?.addEventListener("abort", forwardAbort, { once: true });
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    let failure: FetchPolicyError | undefined;
    let delay = Math.random() * retryBaseMs * 2 ** attempt;
    try {
      const response = await globalThis.fetch(input, { ...init, method, headers, body, signal: controller.signal });
      if (!response.ok) {
        failure = new FetchPolicyError("http", url, `Request failed (HTTP ${response.status})`, { status: response.status });
        const retryable = [408, 425, 429].includes(response.status) || response.status >= 500;
        delay = Math.max(delay, retryAfterMs(response.headers.get("Retry-After")));
        await response.body?.cancel();
        // Do not shorten a server's Retry-After; surface long waits to the caller.
        if (!retryable || attempt >= retries || delay > 60000) throw failure;
      } else {
        if (stream && response.body) {
          const reader = response.body.getReader();
          let idleTimer: ReturnType<typeof setTimeout>;
          const cancel = () => { controller.abort(); void reader.cancel().catch(() => {}); };
          const cleanup = () => { clearTimeout(idleTimer); callerSignal?.removeEventListener('abort', cancel); };
          callerSignal?.addEventListener('abort', cancel, { once: true });
          const streamed = new ReadableStream({
            async pull(sink) {
              idleTimer = setTimeout(() => controller.abort(), timeoutMs);
              try {
                const { value, done } = await reader.read();
                clearTimeout(idleTimer);
                if (done) { cleanup(); sink.close(); } else sink.enqueue(value);
              } catch (cause) {
                cleanup(); sink.error(new FetchPolicyError(callerSignal?.aborted ? 'abort' : controller.signal.aborted ? 'timeout' : 'network', url, 'Stream interrupted', { cause }));
              }
            },
            async cancel() { cleanup(); controller.abort(); await reader.cancel(); },
          });
          return new Response(streamed, { status: response.status, headers: response.headers });
        }
        const data = await response.arrayBuffer();
        const result = new Response(method === "HEAD" || [204, 205, 304].includes(response.status) ? null : data,
          { status: response.status, statusText: response.statusText, headers: response.headers });
        const parse = result.json.bind(result);
        result.json = async () => {
          try { return await parse(); }
          catch (cause) { throw new FetchPolicyError("parse", url, "Invalid JSON response", { cause }); }
        };
        return result;
      }
    } catch (error) {
      if (callerSignal?.aborted) throw aborted(url);
      if (error instanceof FetchPolicyError) throw error;
      failure = new FetchPolicyError(controller.signal.aborted ? "timeout" : "network", url,
        controller.signal.aborted ? "Request timed out" : "Network unavailable", { cause: error });
      if (attempt >= retries) throw failure;
    } finally {
      clearTimeout(timer); callerSignal?.removeEventListener("abort", forwardAbort);
    }
    if (failure) await wait(delay, callerSignal, url);
  }
}
export async function fetchJsonWithPolicy<T = unknown>(url: string, policy: FetchPolicy = {}): Promise<T> {
  const response = await fetchWithPolicy(url, policy);
  try { return await response.json() as T; }
  catch (cause) { throw new FetchPolicyError("parse", url, "Invalid JSON response", { cause }); }
}
