"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { HumanApiError, humanApi, type HumanApiClient } from "../../api";
import { observeHumanStream } from "../../api/stream";
import { ACTIVE_ACCOUNT_STORAGE_KEY } from "../../auth/session";
import { Notifications, type NotificationLanding } from "./controller";
import { unreadNotificationCount, type PresentedNotification } from "./model";

export type NotificationCenterState =
  | Readonly<{ status: "loading"; notifications: readonly []; unreadCount: 0; approvalCount: 0; }>
  | Readonly<{ status: "error"; notifications: readonly PresentedNotification[]; unreadCount: number; approvalCount: number; error: unknown; }>
  | Readonly<{
    status: "ready";
    notifications: readonly PresentedNotification[];
    unreadCount: number;
    approvalCount: number;
  }>;

interface NotificationCenterValue {
  readonly state: NotificationCenterState;
  readonly revision: number;
  readonly refresh: () => Promise<void>;
  readonly open: (notification: PresentedNotification) => Promise<NotificationLanding>;
}

const NotificationCenterContext = createContext<NotificationCenterValue | undefined>(undefined);

export function NotificationCenterProvider({
  children,
  client: suppliedClient,
}: Readonly<{ children: ReactNode; client?: HumanApiClient; }>) {
  const client = useMemo(() => suppliedClient ?? humanApi(), [suppliedClient]);
  const notifications = useMemo(() => new Notifications({ client }), [client]);
  const generation = useRef(0);
  const [connection, setConnection] = useState(0);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<NotificationCenterState>({
    status: "loading",
    notifications: [],
    unreadCount: 0,
    approvalCount: 0,
  });

  const reconcile = useCallback(async (signal?: AbortSignal) => {
    const currentGeneration = generation.current;
    const [archive, approvalCount] = await Promise.all([
      notifications.archive(),
      notifications.pendingApprovals(),
    ]);
    if (signal?.aborted || currentGeneration !== generation.current) return;
    setState({
      status: "ready",
      notifications: archive,
      unreadCount: unreadNotificationCount(archive),
      approvalCount,
    });
    setRevision((current) => current + 1);
  }, [notifications]);

  const refresh = useCallback(async () => {
    const currentGeneration = generation.current;
    try {
      await reconcile();
    } catch (error) {
      if (currentGeneration !== generation.current) return;
      setState((current) => ({ ...current, status: "error", error }));
    }
  }, [reconcile]);

  useEffect(() => {
    generation.current += 1;
    const abort = new AbortController();
    const failed = (error: unknown) => {
      if (abort.signal.aborted) return;
      if (error instanceof HumanApiError && (
        error.detail.code === "unauthenticated"
        || error.detail.code === "session-expired"
        || error.detail.code === "forbidden"
      )) {
        setState({ status: "error", notifications: [], unreadCount: 0, approvalCount: 0, error });
      } else {
        setState((current) => ({ ...current, status: "error", error }));
      }
    };
    void observeHumanStream(client, {
      signal: abort.signal,
      reset: () => {
        generation.current += 1;
        setState({ status: "loading", notifications: [], unreadCount: 0, approvalCount: 0 });
      },
      reconcile: () => reconcile(abort.signal),
      process: async () => { await reconcile(abort.signal); },
      failed,
    }).catch(failed);
    const reconnect = () => {
      abort.abort();
      generation.current += 1;
      setState({ status: "loading", notifications: [], unreadCount: 0, approvalCount: 0 });
      setConnection((current) => current + 1);
    };
    const accountChanged = (event: StorageEvent) => {
      if (event.key === ACTIVE_ACCOUNT_STORAGE_KEY || event.key === null) reconnect();
    };
    window.addEventListener("storage", accountChanged);
    return () => {
      abort.abort();
      generation.current += 1;
      window.removeEventListener("storage", accountChanged);
    };
  }, [client, connection, reconcile]);

  const open = useCallback(async (notification: PresentedNotification) => {
    const currentGeneration = generation.current;
    const landing = await notifications.open(notification);
    if (currentGeneration !== generation.current) return landing;
    setState((current) => {
      if (current.status !== "ready") {
        return current;
      }
      const next = current.notifications.map((item) => item.source.notification_id === landing.notification.notification_id
        ? Object.freeze({ ...item, source: landing.notification })
        : item);
      return {
        ...current,
        notifications: next,
        unreadCount: unreadNotificationCount(next),
      };
    });
    void refresh();
    return landing;
  }, [notifications, refresh]);

  const value = useMemo(() => ({ state, revision, refresh, open }), [state, revision, refresh, open]);
  return (
    <NotificationCenterContext.Provider value={value}>
      {children}
    </NotificationCenterContext.Provider>
  );
}

export function useNotificationCenter(): NotificationCenterValue {
  const value = useContext(NotificationCenterContext);
  if (value === undefined) {
    throw new Error("NotificationCenterProvider is required on authenticated surfaces");
  }
  return value;
}
