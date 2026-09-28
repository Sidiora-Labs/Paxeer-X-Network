export interface RemoteBrowserTab {
  id: string;
  url: string;
  title: string;
  navigationGeneration: number;
  active: boolean;
}

export interface RemoteBrowserState {
  sessionId: string;
  activeTabId: string | null;
  url: string;
  title: string;
  navigationGeneration: number;
  tabs: RemoteBrowserTab[];
}

export interface RemoteBrowserSession extends RemoteBrowserState {
  token: string;
  streamPath: string;
}

export interface RemoteRpcRequest {
  id: string;
  pageRequestId: string;
  tabId: string;
  navigationGeneration: number;
  origin: string;
  method: string;
  params: unknown;
}

export type RemoteBrowserEvent =
  | { sequence: number; type: 'state'; state: RemoteBrowserState }
  | { sequence: number; type: 'rpc'; request: RemoteRpcRequest }
  | {
      sequence: number;
      type: 'notice';
      level: 'warning' | 'error' | 'info';
      message: string;
    };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function record(input: unknown): Record<string, unknown> {
  if (!input || typeof input !== 'object' || Array.isArray(input)) {
    throw new Error('The secure browser returned an invalid response.');
  }
  return input as Record<string, unknown>;
}

function boundedString(input: unknown, maxLength: number): string {
  if (typeof input !== 'string' || input.length > maxLength) {
    throw new Error('The secure browser returned an invalid response.');
  }
  return input;
}

function parseTab(input: unknown): RemoteBrowserTab {
  const value = record(input);
  const id = boundedString(value.id, 64);
  if (!UUID.test(id)) throw new Error('The secure browser returned an invalid tab.');
  if (
    typeof value.navigationGeneration !== 'number' ||
    !Number.isSafeInteger(value.navigationGeneration) ||
    value.navigationGeneration < 0 ||
    typeof value.active !== 'boolean'
  ) {
    throw new Error('The secure browser returned invalid tab state.');
  }
  return {
    id,
    url: boundedString(value.url, 2_048),
    title: boundedString(value.title, 256),
    navigationGeneration: value.navigationGeneration,
    active: value.active,
  };
}

export function parseRemoteBrowserState(input: unknown): RemoteBrowserState {
  const value = record(input);
  const sessionId = boundedString(value.sessionId, 64);
  if (!UUID.test(sessionId)) throw new Error('The secure browser returned an invalid session.');
  const activeTabId =
    value.activeTabId === null ? null : boundedString(value.activeTabId, 64);
  if (activeTabId !== null && !UUID.test(activeTabId)) {
    throw new Error('The secure browser returned an invalid active tab.');
  }
  if (
    typeof value.navigationGeneration !== 'number' ||
    !Number.isSafeInteger(value.navigationGeneration) ||
    value.navigationGeneration < 0 ||
    !Array.isArray(value.tabs) ||
    value.tabs.length > 16
  ) {
    throw new Error('The secure browser returned invalid state.');
  }
  return {
    sessionId,
    activeTabId,
    url: boundedString(value.url, 2_048),
    title: boundedString(value.title, 256),
    navigationGeneration: value.navigationGeneration,
    tabs: value.tabs.map(parseTab),
  };
}

export function parseRemoteBrowserSession(input: unknown): RemoteBrowserSession {
  const value = record(input);
  const state = parseRemoteBrowserState(value);
  const token = boundedString(value.token, 128);
  const streamPath = boundedString(value.streamPath, 128);
  if (!/^[A-Za-z0-9_-]{32,128}$/.test(token)) {
    throw new Error('The secure browser returned an invalid session capability.');
  }
  if (streamPath !== '/api/browser-stream/') {
    throw new Error('The secure browser returned an invalid stream path.');
  }
  return { ...state, token, streamPath };
}

function parseRpcRequest(input: unknown): RemoteRpcRequest {
  const value = record(input);
  const id = boundedString(value.id, 64);
  const tabId = boundedString(value.tabId, 64);
  if (!UUID.test(id) || !UUID.test(tabId)) {
    throw new Error('The secure browser returned an invalid wallet request.');
  }
  if (
    typeof value.navigationGeneration !== 'number' ||
    !Number.isSafeInteger(value.navigationGeneration) ||
    value.navigationGeneration < 0
  ) {
    throw new Error('The secure browser returned invalid wallet request state.');
  }
  return {
    id,
    pageRequestId: boundedString(value.pageRequestId, 128),
    tabId,
    navigationGeneration: value.navigationGeneration,
    origin: new URL(boundedString(value.origin, 512)).origin,
    method: boundedString(value.method, 64),
    params: value.params,
  };
}

export function parseRemoteBrowserEvents(input: unknown): {
  events: RemoteBrowserEvent[];
  cursor: number;
} {
  const value = record(input);
  if (
    !Array.isArray(value.events) ||
    value.events.length > 256 ||
    typeof value.cursor !== 'number' ||
    !Number.isSafeInteger(value.cursor) ||
    value.cursor < 0
  ) {
    throw new Error('The secure browser returned invalid events.');
  }
  const events = value.events.map((item): RemoteBrowserEvent => {
    const event = record(item);
    if (
      typeof event.sequence !== 'number' ||
      !Number.isSafeInteger(event.sequence) ||
      event.sequence < 1
    ) {
      throw new Error('The secure browser returned an invalid event.');
    }
    if (event.type === 'state') {
      return {
        sequence: event.sequence,
        type: 'state',
        state: parseRemoteBrowserState(event.state),
      };
    }
    if (event.type === 'rpc') {
      return {
        sequence: event.sequence,
        type: 'rpc',
        request: parseRpcRequest(event.request),
      };
    }
    if (
      event.type === 'notice' &&
      ['warning', 'error', 'info'].includes(String(event.level))
    ) {
      return {
        sequence: event.sequence,
        type: 'notice',
        level: event.level as 'warning' | 'error' | 'info',
        message: boundedString(event.message, 512),
      };
    }
    throw new Error('The secure browser returned an unsupported event.');
  });
  return { events, cursor: value.cursor };
}

async function responseError(response: Response): Promise<Error> {
  let message = 'The secure browser request failed.';
  try {
    const body = record(await response.json());
    if (typeof body.error === 'string') message = body.error;
    else if (body.error && typeof body.error === 'object') {
      const nested = body.error as Record<string, unknown>;
      if (typeof nested.message === 'string') message = nested.message;
    }
  } catch {
  }
  return new Error(message);
}

export async function remoteBrowserRequest(
  path: string,
  options: {
    token?: string;
    method?: 'GET' | 'POST' | 'DELETE';
    body?: unknown;
    signal?: AbortSignal;
  } = {},
): Promise<Response> {
  const response = await fetch(`/api/browser/v1/${path}`, {
    method: options.method ?? 'GET',
    headers: {
      ...(options.token ? { Authorization: `Bearer ${options.token}` } : {}),
      ...(options.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    cache: 'no-store',
    credentials: 'same-origin',
    signal: options.signal,
  });
  if (!response.ok) throw await responseError(response);
  return response;
}
