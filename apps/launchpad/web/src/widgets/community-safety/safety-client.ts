import type { PrivateAppealOutcome, ResolveReportCommand, SafetyCase } from './contracts';
export class SafetyRequestError extends Error {
    constructor(readonly status: number, readonly retryAfterSeconds: number | null = null) { super(status === 409 ? 'Refresh the current revision.' : 'Safety request failed.'); }
}
export interface ReportCommand { targetType: 'message' | 'comment' | 'stream' | 'profile' | 'token' | 'voice_participant'; targetId: string; category: string; description: string }
/**
 * Client for the gateway's safety routes (`/v2/safety/*`, gateway
 * proxy/connected.go SocialConnectedRoutes, all session-bound). The web origin
 * is cross-site to the gateway, so the session travels as
 * `Authorization: Bearer <accessToken>` with `credentials: 'omit'`; a bearer
 * request needs no CSRF token. Without a session the request is refused
 * locally with 401 instead of being sent anonymously.
 */
export class SafetyClient {
    constructor(private readonly origin: string, private readonly accessToken: () => string | undefined) {
        const url = new URL(origin, globalThis.location?.origin ?? 'https://localhost');
        if (url.protocol !== 'https:' && url.hostname !== 'localhost' && url.hostname !== '127.0.0.1') throw new Error('Safety API requires HTTPS.');
    }
    async request<T>(path: string, method = 'GET', body?: unknown, signal?: AbortSignal): Promise<T> {
        const token = this.accessToken();
        if (!token) throw new SafetyRequestError(401);
        const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Bearer ${token}` };
        if (body !== undefined) headers['Content-Type'] = 'application/json';
        const response = await fetch(new URL(path, this.origin), { method, headers, credentials: 'omit', cache: 'no-store', redirect: 'error', body: body === undefined ? undefined : JSON.stringify(body), signal });
        if (!response.ok) { const retry = Number(response.headers.get('Retry-After')); throw new SafetyRequestError(response.status, Number.isFinite(retry) && retry > 0 ? retry : null); }
        return await response.json() as T;
    }
    report(command: ReportCommand) { return this.request<SafetyCase>('/v2/safety/reports', 'POST', command); }
    cases(signal?: AbortSignal) { return this.request<SafetyCase[]>('/v2/safety/cases', 'GET', undefined, signal); }
    resolve(caseId: string, command: ResolveReportCommand) { return this.request<{revision: number}>(`/v2/safety/cases/${encodeURIComponent(caseId)}`, 'PATCH', command); }
    appeal(actionId: string, reason: string) { return this.request<PrivateAppealOutcome>('/v2/safety/appeals', 'POST', { actionId, reason }); }
    privateAppeal(appealId: string, signal?: AbortSignal) { return this.request<PrivateAppealOutcome>(`/v2/safety/appeals/${encodeURIComponent(appealId)}`, 'GET', undefined, signal); }
}
